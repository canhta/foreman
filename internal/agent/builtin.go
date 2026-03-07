package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	// compactionThreshold is the fraction of budget at which compaction is triggered.
	compactionThreshold = 0.70
)

// BuiltinConfig holds configuration for the builtin runner.
type BuiltinConfig struct {
	DefaultAllowedTools []string
	MaxTurnsDefault     int
	ContextWindowBudget int // optional override; defaults to contextWindowBudget
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
	return &BuiltinRunner{
		provider:        provider,
		model:           model,
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
	var usage AgentUsage

	// Tool call deduplication: fingerprint → count of times called.
	// Guidance-only — injects a warning but does NOT block execution.
	toolCallCounts := make(map[string]int)

	// Wire budget and depth into the registry so SubagentTool can enforce constraints.
	if r.registry != nil {
		r.registry.SetParentBudgetAndDepth(req.RemainingBudget, req.AgentDepth)
	}

	for turn := 0; turn < maxTurns; turn++ {
		if req.OnProgress != nil {
			req.OnProgress(AgentEvent{Type: AgentEventTurnStart, Turn: turn + 1})
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
		llmReq := models.LlmRequest{
			Model:        r.model,
			SystemPrompt: systemPrompt,
			MaxTokens:    4096,
			Temperature:  0.2,
			Messages:     messages,
			Tools:        toolDefs,
			OutputSchema: outputSchema,
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
			return AgentResult{Output: resp.Content, Usage: usage}, nil
		}

		if resp.StopReason == models.StopReasonToolUse && len(resp.ToolCalls) > 0 {
			messages = append(messages, models.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// Execute all tool calls in parallel (mirrors SDK betatoolrunner.go)
			results := make([]models.ToolResult, len(resp.ToolCalls))
			g, gctx := errgroup.WithContext(ctx)
			for i, tc := range resp.ToolCalls {
				i, tc := i, tc
				if req.OnProgress != nil {
					req.OnProgress(AgentEvent{Type: AgentEventToolStart, Turn: turn + 1, ToolName: tc.Name})
				}
				g.Go(func() error {
					var out string
					var err error
					if r.registry != nil {
						out, err = r.registry.Execute(gctx, req.WorkDir, tc.Name, tc.Input)
					} else {
						err = fmt.Errorf("unknown tool: %s", tc.Name)
					}
					if err != nil {
						results[i] = models.ToolResult{ToolCallID: tc.ID, Content: err.Error(), IsError: true}
					} else {
						results[i] = models.ToolResult{ToolCallID: tc.ID, Content: out}
					}
					if req.OnProgress != nil {
						req.OnProgress(AgentEvent{Type: AgentEventToolEnd, Turn: turn + 1, ToolName: tc.Name})
					}
					return nil // tool errors become result content, not Go errors
				})
			}
			if waitErr := g.Wait(); waitErr != nil {
				return AgentResult{}, fmt.Errorf("builtin: tool execution: %w", waitErr)
			}

			// Collect all touched paths (after parallel completion — no data race)
			var touchedPaths []string
			for _, tc := range resp.ToolCalls {
				if path := extractPath(tc.Input); path != "" {
					touchedPaths = append(touchedPaths, path)
				}
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
			threshold := int(float64(budget) * compactionThreshold)
			currentTokens := countAllTokens(messages)
			if currentTokens > threshold {
				before := len(messages)
				messages = CompactMessages(messages, budget)
				after := len(messages)
				if after < before {
					dropped := before - after
					log.Info().
						Int("tokens_before", currentTokens).
						Int("messages_dropped", dropped).
						Int("budget", budget).
						Msg("builtin: context compacted")
					messages = append(messages, models.Message{
						Role:    "user",
						Content: compactionSummary(dropped / 2), // pairs
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

		return AgentResult{Output: resp.Content, Usage: usage}, nil
	}

	return AgentResult{}, fmt.Errorf("builtin: exceeded max turns %d without completion", maxTurns)
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
