package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// Context window thresholds for compaction.
const (
	// contextWindowBudget is the token budget for the message history.
	// Claude's context window is 200K tokens; we reserve headroom for system prompt and response.
	contextWindowBudget = 150_000
	// compactionThreshold is the fraction of budget at which Phase 1 compaction is triggered
	// (truncate old tool outputs).
	compactionThreshold = 0.70
	// summarizationThreshold is the fraction of budget at which Phase 2 LLM summarization
	// is triggered (replace old messages with a structured summary).
	summarizationThreshold = 0.85
)

// defaultReflectionInterval is the number of turns between self-reflection injections.
const defaultReflectionInterval = 5

// reflectionPrompt is the structured message injected every N turns.
const reflectionPrompt = "Before continuing, briefly summarize: (1) what you have accomplished, (2) which files you have changed, and (3) what remains. If the task is already complete, reply with exactly: TASK_COMPLETE"

// BuiltinConfig holds configuration for the builtin runner.
type BuiltinConfig struct {
	// Model overrides the LLM model for all calls within agent sessions (REQ-LOOP-005).
	// When empty, the model passed to NewBuiltinRunner is used unchanged.
	Model               string
	DefaultAllowedTools []string
	MaxTurnsDefault     int
	ContextWindowBudget int // optional override; defaults to contextWindowBudget
	// MaxTokens is the maximum number of output tokens per LLM call. 0 uses the default (8192).
	MaxTokens int
	// ReflectionInterval is the number of turns after which a self-reflection
	// message is injected (REQ-LOOP-001). 0 uses the default (5). -1 disables.
	ReflectionInterval int
}

// BuiltinRunner runs a multi-turn tool-use loop against the LlmProvider.
// Tool calls within a single turn execute in parallel via errgroup.
type BuiltinRunner struct {
	provider        llm.LlmProvider
	contextProvider ContextProvider
	registry        *tools.Registry
	model           string
	config          BuiltinConfig
}

// NewBuiltinRunner creates a builtin runner.
// registry is required; cp (ContextProvider) may be nil.
//
// Two-phase init for SubagentTool:
//
//	reg    := tools.NewRegistry(git, cmd, hooks)
//	runner := NewBuiltinRunner(provider, model, config, reg, cp)
//	reg.SetRunFn(runner.subagentRunFn)  // inject after construction
func NewBuiltinRunner(
	provider llm.LlmProvider,
	model string,
	config BuiltinConfig,
	registry *tools.Registry,
	cp ContextProvider,
) *BuiltinRunner {
	// REQ-LOOP-005: if a model is explicitly configured, it takes priority over
	// the fallback model passed by the caller (pipeline's implementer model).
	effectiveModel := model
	if config.Model != "" {
		effectiveModel = config.Model
	}
	return &BuiltinRunner{
		provider:        provider,
		model:           effectiveModel,
		config:          config,
		registry:        registry,
		contextProvider: cp,
	}
}

// subagentRunFn is injected into the registry for SubagentTool.
// It receives remainingBudget and agentDepth from the subagent tool's enforcement logic.
func (r *BuiltinRunner) subagentRunFn(ctx context.Context, task, workDir string, toolNames []string, maxTurns, remainingBudget, agentDepth int) (string, error) {
	result, err := r.Run(ctx, AgentRequest{
		Prompt:          task,
		WorkDir:         workDir,
		AllowedTools:    toolNames,
		MaxTurns:        maxTurns,
		RemainingBudget: remainingBudget,
		AgentDepth:      agentDepth,
		// No ContextProvider for subagents — baseline context only
	})
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func (r *BuiltinRunner) RunnerName() string { return "builtin" }

func (r *BuiltinRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	systemPrompt := "You are a focused task executor. Complete the task and return only the result."

	// Layer 1: inject AGENTS.md or .foreman/context.md if present
	if fc := loadForemanContext(req.WorkDir); fc != "" {
		systemPrompt = fc + "\n\n" + systemPrompt
	}
	if req.SystemPrompt != "" {
		systemPrompt = systemPrompt + "\n\n" + req.SystemPrompt
	}

	toolNames := req.AllowedTools
	if len(toolNames) == 0 {
		toolNames = r.config.DefaultAllowedTools
	}

	var toolDefs []models.ToolDef
	if r.registry != nil {
		toolDefs = r.registry.Defs(toolNames)
	}

	maxTurns := req.MaxTurns
	if maxTurns == 0 {
		maxTurns = r.config.MaxTurnsDefault
	}
	if maxTurns == 0 {
		maxTurns = 10
	}

	var outputSchema *json.RawMessage
	if req.OutputSchema != nil {
		s := req.OutputSchema
		outputSchema = &s
	}

	fallbackModel := req.FallbackModel
	messages := []models.Message{{Role: "user", Content: req.Prompt}}
	usage := AgentUsage{Model: r.model}

	// Tool call deduplication: fingerprint → count of times called.
	// Guidance-only — injects a warning but does NOT block execution.
	toolCallCounts := make(map[string]int)

	// Wire budget and depth into the registry so SubagentTool can enforce constraints.
	if r.registry != nil {
		r.registry.SetParentBudgetAndDepth(req.RemainingBudget, req.AgentDepth)
	}

	// Determine reflection interval (REQ-LOOP-001).
	reflectionInterval := r.config.ReflectionInterval
	if reflectionInterval == 0 {
		reflectionInterval = defaultReflectionInterval
	}

	for turn := 0; turn < maxTurns; turn++ {
		if req.OnProgress != nil {
			req.OnProgress(AgentEvent{Type: AgentEventTurnStart, Turn: turn + 1})
		}

		// Self-reflection injection: every N turns (after the first), inject a
		// structured prompt asking the agent to assess progress (REQ-LOOP-001).
		if reflectionInterval > 0 && turn > 0 && turn%reflectionInterval == 0 {
			messages = append(messages, models.Message{Role: "user", Content: reflectionPrompt})
			log.Info().Int("turn", turn+1).Msg("builtin: self-reflection turn injected")
		}

		// Update remaining budget for the subagent tool before each LLM call.
		if r.registry != nil {
			remaining := maxTurns - turn
			// Honor the inherited budget cap: subagent can't claim more turns than the parent allows.
			if req.RemainingBudget > 0 && remaining > req.RemainingBudget {
				remaining = req.RemainingBudget
			}
			// Propagate negative budget (exhausted) as-is.
			if req.RemainingBudget < 0 {
				remaining = req.RemainingBudget
			}
			r.registry.SetParentBudgetAndDepth(remaining, req.AgentDepth)
		}
		maxTokens := r.config.MaxTokens
		if maxTokens == 0 {
			maxTokens = 8192
		}
		llmReq := models.LlmRequest{
			Model:        r.model,
			SystemPrompt: systemPrompt,
			MaxTokens:    maxTokens,
			Temperature:  0.2,
			Messages:     messages,
			Tools:        toolDefs,
			OutputSchema: outputSchema,
			Thinking:     req.Thinking,
		}

		resp, err := r.provider.Complete(ctx, llmReq)
		if err != nil {
			var rateLimitErr *llm.RateLimitError
			if errors.As(err, &rateLimitErr) && fallbackModel != "" {
				llmReq.Model = fallbackModel
				fallbackModel = ""
				resp, err = r.provider.Complete(ctx, llmReq)
			}
		}
		if err != nil {
			return AgentResult{}, fmt.Errorf("builtin: turn %d: %w", turn+1, err)
		}

		usage.InputTokens += resp.TokensInput
		usage.OutputTokens += resp.TokensOutput
		usage.DurationMs += int(resp.DurationMs)
		usage.NumTurns++

		if req.OnProgress != nil {
			req.OnProgress(AgentEvent{
				Type:      AgentEventTurnEnd,
				Turn:      turn + 1,
				TokensIn:  resp.TokensInput,
				TokensOut: resp.TokensOutput,
			})
		}

		if resp.StopReason == models.StopReasonEndTurn || resp.StopReason == models.StopReasonMaxTokens {
			return enrichResult(AgentResult{Output: resp.Content, Usage: usage}), nil
		}

		// Implicit stop from self-reflection: if the agent replied TASK_COMPLETE
		// to a reflection prompt, treat it as a graceful end-of-turn (REQ-LOOP-001).
		if strings.Contains(resp.Content, "TASK_COMPLETE") {
			log.Info().Int("turn", turn+1).Msg("builtin: implicit stop via self-reflection TASK_COMPLETE")
			return enrichResult(AgentResult{Output: resp.Content, Usage: usage}), nil
		}

		if resp.StopReason == models.StopReasonToolUse && len(resp.ToolCalls) > 0 {
			messages = append(messages, models.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// File-aware parallel tool execution (REQ-LOOP-003):
			// Tool calls sharing a file path are serialized; disjoint ones run in parallel.
			results := make([]models.ToolResult, len(resp.ToolCalls))
			var touchedPaths []string
			if execErr := r.executeToolsFileAware(ctx, req, turn, resp.ToolCalls, results, &touchedPaths); execErr != nil {
				return AgentResult{}, execErr
			}

			messages = append(messages, models.Message{Role: "user", ToolResults: results})

			// Layer 2: reactive context injection (once per turn, after all tools complete)
			if r.contextProvider != nil && len(touchedPaths) > 0 {
				inject, cpErr := r.contextProvider.OnFilesAccessed(ctx, touchedPaths)
				if cpErr != nil {
					log.Warn().Err(cpErr).Strs("paths", touchedPaths).Msg("context provider failed")
				} else if inject != "" {
					messages = append(messages, models.Message{Role: "user", Content: "[context update]\n" + inject})
				}
			}

			// Layer 3: context window management — compact if approaching token limit.
			budget := r.config.ContextWindowBudget
			if budget <= 0 {
				budget = contextWindowBudget
			}
			p1Threshold := int(float64(budget) * compactionThreshold)    // 70%
			p2Threshold := int(float64(budget) * summarizationThreshold) // 85%
			currentTokens := countAllTokens(messages)

			// Phase 2 (85%): LLM summarization takes priority — preserve as much context as possible.
			// Skip if the first message is already a summary (avoid progressive fidelity loss).
			if currentTokens > p2Threshold {
				alreadySummarized := len(messages) > 0 && strings.HasPrefix(messages[0].Content, "[context summary]")
				if !alreadySummarized {
					oldMessages, recentMessages := splitForSummarization(messages)
					if len(oldMessages) > 0 {
						summaryMsg := SummarizeHistory(ctx, unwrapProvider(r.provider), r.model, oldMessages)
						messages = append([]models.Message{summaryMsg}, recentMessages...)
						log.Info().
							Int("tokens_before", currentTokens).
							Int("old_messages", len(oldMessages)).
							Int("remaining", len(messages)).
							Msg("builtin: context summarized via LLM")
					}
				}
			}

			// Phase 1 (70%): fall-through truncation if still over budget
			// (e.g., last 3 turns alone are large, or summarization was skipped).
			if tokensAfterP2 := countAllTokens(messages); tokensAfterP2 > p1Threshold {
				before := len(messages)
				messages = CompactMessages(messages, budget)
				after := len(messages)
				if after < before {
					dropped := before - after
					log.Info().
						Int("tokens_before", tokensAfterP2).
						Int("messages_dropped", dropped).
						Int("budget", budget).
						Msg("builtin: context compacted")
					messages = append(messages, models.Message{
						Role:    "user",
						Content: compactionSummary(dropped / 2),
					})
				}
			}

			// Layer 4: deduplication detector — track fingerprints and warn on repeats.
			var dupWarnings []string
			for _, tc := range resp.ToolCalls {
				fp := toolCallFingerprint(tc.Name, tc.Input)
				toolCallCounts[fp]++
				if toolCallCounts[fp] == 2 {
					// Inject warning on the second occurrence of an identical call.
					dupWarnings = append(dupWarnings,
						fmt.Sprintf("[warning: you already called %s with identical arguments. This may indicate a loop. Consider a different approach or check if the prior result was sufficient.]", tc.Name))
					log.Warn().Str("tool", tc.Name).Str("fingerprint", fp).Msg("builtin: duplicate tool call detected")
				}
			}
			if len(dupWarnings) > 0 {
				for _, w := range dupWarnings {
					messages = append(messages, models.Message{Role: "user", Content: w})
				}
			}
			continue
		}

		return enrichResult(AgentResult{Output: resp.Content, Usage: usage}), nil
	}

	return AgentResult{}, fmt.Errorf("builtin: exceeded max turns %d without completion", maxTurns)
}

// executeToolsFileAware executes tool calls with file-path conflict awareness.
// Tool calls that share any file path in their arguments run sequentially (in LLM order).
// Tool calls operating on disjoint file sets run in parallel.
// Non-filesystem tool calls (no file path in args) are treated as non-conflicting.
//
// Algorithm: build a conflict graph, then run independent groups in parallel batches.
func (r *BuiltinRunner) executeToolsFileAware(
	ctx context.Context,
	req AgentRequest,
	turn int,
	toolCalls []models.ToolCall,
	results []models.ToolResult,
	touchedPaths *[]string,
) error {
	// Build per-call file-path sets.
	type callMeta struct {
		files map[string]bool
		tc    models.ToolCall
		index int
	}
	metas := make([]callMeta, len(toolCalls))
	for i, tc := range toolCalls {
		files := make(map[string]bool)
		if p := extractPath(tc.Input); p != "" {
			files[p] = true
		}
		metas[i] = callMeta{index: i, tc: tc, files: files}
	}

	// Topological batching: greedily assign each call to the earliest batch
	// that has no conflicting file with any call already in that batch.
	type batch struct {
		claimed map[string]bool
		calls   []callMeta
	}
	var batches []batch
	for _, m := range metas {
		placed := false
		for bi := range batches {
			conflict := false
			for f := range m.files {
				if batches[bi].claimed[f] {
					conflict = true
					break
				}
			}
			if !conflict {
				batches[bi].calls = append(batches[bi].calls, m)
				for f := range m.files {
					batches[bi].claimed[f] = true
				}
				placed = true
				break
			}
		}
		if !placed {
			newClaimed := make(map[string]bool, len(m.files))
			for f := range m.files {
				newClaimed[f] = true
			}
			batches = append(batches, batch{calls: []callMeta{m}, claimed: newClaimed})
		}
	}

	// Execute each batch in parallel, batches sequentially.
	for _, b := range batches {
		g, gctx := errgroup.WithContext(ctx)
		for _, m := range b.calls {
			m := m
			if req.OnProgress != nil {
				req.OnProgress(AgentEvent{Type: AgentEventToolStart, Turn: turn + 1, ToolName: m.tc.Name})
			}
			g.Go(func() error {
				var out string
				var err error
				if r.registry != nil {
					out, err = r.registry.Execute(gctx, req.WorkDir, m.tc.Name, m.tc.Input)
				} else {
					err = fmt.Errorf("unknown tool: %s", m.tc.Name)
				}
				if err != nil {
					results[m.index] = models.ToolResult{ToolCallID: m.tc.ID, Content: err.Error(), IsError: true}
				} else {
					results[m.index] = models.ToolResult{ToolCallID: m.tc.ID, Content: out}
				}
				if req.OnProgress != nil {
					req.OnProgress(AgentEvent{Type: AgentEventToolEnd, Turn: turn + 1, ToolName: m.tc.Name})
				}
				return nil // tool errors become result content, not Go errors
			})
		}
		if waitErr := g.Wait(); waitErr != nil {
			return fmt.Errorf("builtin: tool execution: %w", waitErr)
		}
	}

	// Collect touched paths after all batches complete (no data race).
	for _, tc := range toolCalls {
		if p := extractPath(tc.Input); p != "" {
			*touchedPaths = append(*touchedPaths, p)
		}
	}
	return nil
}

func (r *BuiltinRunner) HealthCheck(ctx context.Context) error {
	if r.provider == nil {
		return nil
	}
	return r.provider.HealthCheck(ctx)
}

func (r *BuiltinRunner) Close() error { return nil }

// loadForemanContext reads project context from workDir.
// AGENTS.md is the standard cross-tool convention; .foreman/context.md is for Foreman-specific cached content.
func loadForemanContext(workDir string) string {
	candidates := []string{
		filepath.Join(workDir, "AGENTS.md"),
		filepath.Join(workDir, ".foreman", "context.md"),
	}
	for _, path := range candidates {
		if content, err := os.ReadFile(path); err == nil {
			return string(content)
		}
	}
	return ""
}

// extractPath reads the "path" field from tool input JSON.
func extractPath(input json.RawMessage) string {
	var v struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(input, &v)
	return v.Path
}

// toolCallFingerprint returns a canonical hash of (toolName, input) for dedup tracking.
func toolCallFingerprint(toolName string, input json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte("|"))
	// Canonicalize: re-marshal to remove whitespace differences.
	var canonical interface{}
	if err := json.Unmarshal(input, &canonical); err == nil {
		if b, err := json.Marshal(canonical); err == nil {
			h.Write(b)
			goto done
		}
	}
	h.Write(input)
done:
	return fmt.Sprintf("%x", h.Sum(nil))
}

// builtinStatusRe matches STATUS: APPROVED|REJECTED|CHANGES_REQUESTED patterns
// in agent output (same grammar as pipeline/review_parser.go).
var builtinStatusRe = regexp.MustCompile(`(?i)(?:^|\n)STATUS:\s*(APPROVED|REJECTED|CHANGES_REQUESTED)`)

// builtinNewFileRe and builtinModifyFileRe match the structured file-change blocks
// produced by the implementer prompt (same grammar as pipeline/output_parser.go).
var (
	builtinNewFileRe    = regexp.MustCompile(`===\s*NEW FILE:\s*(.+?)\s*===`)
	builtinModifyFileRe = regexp.MustCompile(`===\s*MODIFY FILE:\s*(.+?)\s*===`)
	builtinEndFileRe    = regexp.MustCompile(`===\s*END FILE\s*===`)
	builtinSearchRe     = regexp.MustCompile(`<<<<\s*SEARCH`)
	builtinReplaceRe    = regexp.MustCompile(`<<<<\s*REPLACE`)
	// builtinEndBlockRe is intentionally NOT a regex: strings.HasPrefix(line, ">>>>")
	// is used instead to avoid false-positive matches on code containing ">>>>".
)

// enrichResult populates FileChanges and ReviewResult on an AgentResult by
// parsing the Output string for known structured patterns.
// It is a best-effort enhancement — errors are silently ignored so that the
// raw Output is always available to callers.
func enrichResult(r AgentResult) AgentResult {
	r.FileChanges = parseFileChanges(r.Output)
	r.ReviewResult = parseReviewResult(r.Output)
	return r
}

// parseFileChanges extracts NEW FILE / MODIFY FILE blocks from agent output.
func parseFileChanges(raw string) []FileChange {
	lines := strings.Split(raw, "\n")
	var changes []FileChange
	i := 0

	for i < len(lines) {
		line := lines[i]

		if m := builtinNewFileRe.FindStringSubmatch(line); m != nil {
			path := strings.TrimSpace(m[1])
			i++
			var contentLines []string
			for i < len(lines) && !builtinEndFileRe.MatchString(lines[i]) {
				contentLines = append(contentLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			changes = append(changes, FileChange{
				Path:       path,
				NewContent: strings.Join(contentLines, "\n"),
				IsDiff:     false,
			})
			continue
		}

		if m := builtinModifyFileRe.FindStringSubmatch(line); m != nil {
			path := strings.TrimSpace(m[1])
			i++
			for i < len(lines) && !builtinEndFileRe.MatchString(lines[i]) {
				if builtinSearchRe.MatchString(lines[i]) {
					i++
					var searchLines []string
					for i < len(lines) && !strings.HasPrefix(lines[i], ">>>>") {
						searchLines = append(searchLines, lines[i])
						i++
					}
					if i < len(lines) {
						i++ // skip >>>>
					}
					if i < len(lines) && builtinReplaceRe.MatchString(lines[i]) {
						i++
						var replaceLines []string
						for i < len(lines) && !strings.HasPrefix(lines[i], ">>>>") {
							replaceLines = append(replaceLines, lines[i])
							i++
						}
						if i < len(lines) {
							i++ // skip >>>>
						}
						changes = append(changes, FileChange{
							Path:       path,
							OldContent: strings.Join(searchLines, "\n"),
							NewContent: strings.Join(replaceLines, "\n"),
							IsDiff:     false,
						})
					}
				} else {
					i++
				}
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			continue
		}

		i++
	}
	return changes
}

// parseReviewResult parses a STATUS: APPROVED|REJECTED|CHANGES_REQUESTED line
// from the agent output and returns a *ReviewResult, or nil if none is found.
// Note: Issues and Summary fields are not populated; only Approved and Severity are extracted.
func parseReviewResult(raw string) *ReviewResult {
	m := builtinStatusRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return nil
	}
	status := strings.ToUpper(strings.TrimSpace(m[1]))
	approved := status == "APPROVED"

	// Derive severity from status and check for [CRITICAL] tags in the output.
	severity := "none"
	switch status {
	case "CHANGES_REQUESTED":
		severity = "minor"
		if strings.Contains(strings.ToUpper(raw), "[CRITICAL]") {
			severity = "critical"
		} else if strings.Contains(strings.ToUpper(raw), "[MAJOR]") {
			severity = "major"
		}
	case "REJECTED":
		severity = "major"
		if strings.Contains(strings.ToUpper(raw), "[CRITICAL]") {
			severity = "critical"
		}
	}

	return &ReviewResult{
		Approved: approved,
		Severity: severity,
	}
}

// unwrapProvider returns the innermost non-recording provider.
// This is used to bypass RecordingProvider for internal summarization calls
// that should not appear in the observability store.
func unwrapProvider(p llm.LlmProvider) llm.LlmProvider {
	type hasInner interface {
		Inner() llm.LlmProvider
	}
	for {
		if u, ok := p.(hasInner); ok {
			p = u.Inner()
		} else {
			break
		}
	}
	return p
}
