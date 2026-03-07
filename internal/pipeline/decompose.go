package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog/log"
)

// scopeKeywords are words that suggest a ticket covers multiple features.
var scopeKeywords = []string{"and", "also", "plus", "additionally"}

// DecompEventRecorder is the subset of db.Database needed to record decomposition events.
type DecompEventRecorder interface {
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// NeedsDecomposition returns true if the ticket is too large for a single PR.
func NeedsDecomposition(ticket *models.Ticket, cfg *models.DecomposeConfig) bool {
	if !cfg.Enabled || ticket.DecomposeDepth > 0 {
		return false
	}
	wordCount := len(strings.Fields(ticket.Description))
	if wordCount > cfg.MaxTicketWords {
		return true
	}
	if countScopeKeywords(ticket.Description) > cfg.MaxScopeKeywords {
		return true
	}
	vagueAndLong := ticket.AcceptanceCriteria == "" && wordCount > 100
	return vagueAndLong
}

// DecompositionResult is the structured output from the decomposition LLM call.
type DecompositionResult struct {
	Rationale string            `json:"rationale"`
	Children  []ChildTicketSpec `json:"children"`
}

// ChildTicketSpec describes a single child ticket to create.
type ChildTicketSpec struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	AcceptanceCriteria  []string `json:"acceptance_criteria"`
	FilesToModify       []string `json:"files_to_modify"`
	EstimatedComplexity string   `json:"estimated_complexity"`
	DependsOn           []string `json:"depends_on"`
}

// Decomposer breaks oversized tickets into child tracker issues.
type Decomposer struct {
	llm     LLMProvider
	tracker tracker.IssueTracker
	cfg     *models.DecomposeConfig
	db      DecompEventRecorder
}

// NewDecomposer creates a new Decomposer.
func NewDecomposer(llm LLMProvider, tr tracker.IssueTracker, cfg *models.DecomposeConfig) *Decomposer {
	return &Decomposer{llm: llm, tracker: tr, cfg: cfg}
}

// NewDecomposerWithDB creates a Decomposer that records events to a database.
func NewDecomposerWithDB(llm LLMProvider, tr tracker.IssueTracker, cfg *models.DecomposeConfig, db DecompEventRecorder) *Decomposer {
	return &Decomposer{llm: llm, tracker: tr, cfg: cfg, db: db}
}

// NeedsDecomposition checks whether the ticket needs decomposition using heuristics
// first, then optionally an LLM check when heuristics return false and
// cfg.LLMAssist is enabled. The heuristic result always takes precedence for
// safety: if heuristics say decompose we decompose regardless.
func (d *Decomposer) NeedsDecomposition(ctx context.Context, ticket *models.Ticket) (bool, error) {
	heuristicResult := NeedsDecomposition(ticket, d.cfg)

	if heuristicResult {
		d.recordDecompEvent(ctx, ticket.ID, heuristicResult, "skipped", "heuristics triggered")
		return true, nil
	}

	if d.cfg.LLMAssist {
		llmResult, reason, err := d.llmAssistCheck(ctx, ticket)
		if err != nil {
			log.Warn().Err(err).Str("ticket_id", ticket.ID).Msg("LLM decomposition check failed, using heuristic result")
			d.recordDecompEvent(ctx, ticket.ID, heuristicResult, "error", err.Error())
			return heuristicResult, nil
		}
		d.recordDecompEvent(ctx, ticket.ID, heuristicResult, boolToYesNo(llmResult), reason)
		return llmResult, nil
	}

	d.recordDecompEvent(ctx, ticket.ID, heuristicResult, "skipped", "llm_assist disabled")
	return heuristicResult, nil
}

// llmAssistCheck calls the LLM to evaluate whether the ticket needs decomposition.
// Returns (needsDecomp, reason, error).
func (d *Decomposer) llmAssistCheck(ctx context.Context, ticket *models.Ticket) (bool, string, error) {
	prompt := fmt.Sprintf(`You are evaluating whether a software ticket is too large to implement in a single PR.

Ticket title: %s
Ticket description:
%s

Is this ticket too large for a single PR (more than 5 files, more than 400 lines of changes, or touches multiple unrelated systems)?

Answer with exactly one of:
YES: <one sentence reason>
NO: <one sentence reason>`, ticket.Title, ticket.Description)

	model := d.cfg.LLMAssistModel
	req := models.LlmRequest{
		SystemPrompt: "You are a technical project manager evaluating ticket scope.",
		UserPrompt:   prompt,
		MaxTokens:    256,
		Temperature:  0.0,
	}
	if model != "" {
		req.Model = model
	}

	resp, err := d.llm.Complete(ctx, req)
	if err != nil {
		return false, "", fmt.Errorf("llm decomposition check: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	upper := strings.ToUpper(content)
	if strings.HasPrefix(upper, "YES") {
		reason := extractReason(content)
		return true, reason, nil
	}
	reason := extractReason(content)
	return false, reason, nil
}

// extractReason pulls the text after the YES:/NO: prefix.
func extractReason(content string) string {
	if idx := strings.Index(content, ":"); idx >= 0 && idx < len(content)-1 {
		return strings.TrimSpace(content[idx+1:])
	}
	return content
}

// recordDecompEvent writes a decomposition_check event to the db (if configured).
func (d *Decomposer) recordDecompEvent(ctx context.Context, ticketID string, heuristic bool, llmResult, reason string) {
	if d.db == nil {
		return
	}
	type eventPayload struct {
		Type            string `json:"type"`
		LLMResult       string `json:"llm_result"`
		Reason          string `json:"reason"`
		HeuristicResult bool   `json:"heuristic_result"`
	}
	payload := eventPayload{
		Type:            "decomposition_check",
		HeuristicResult: heuristic,
		LLMResult:       llmResult,
		Reason:          reason,
	}
	details, _ := json.Marshal(payload)
	err := d.db.RecordEvent(ctx, &models.EventRecord{
		ID:        fmt.Sprintf("evt-%s-%d", ticketID, time.Now().UnixNano()),
		TicketID:  ticketID,
		EventType: "decomposition_check",
		Details:   string(details),
		CreatedAt: time.Now(),
	})
	if err != nil {
		log.Warn().Err(err).Str("ticket_id", ticketID).Msg("failed to record decomposition_check event")
	}
}

// boolToYesNo converts a bool to "yes" or "no".
func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Execute decomposes a ticket into child issues and creates them in the tracker.
// Returns the external IDs of the created child tickets.
func (d *Decomposer) Execute(ctx context.Context, ticket *models.Ticket) ([]string, error) {
	result, err := d.generateChildSpecs(ctx, ticket)
	if err != nil {
		return nil, fmt.Errorf("generating child specs: %w", err)
	}

	if conflicts := DetectDecompositionConflicts(result.Children); len(conflicts) > 0 {
		log.Warn().
			Str("ticket_id", ticket.ID).
			Int("conflict_count", len(conflicts)).
			Interface("conflicts", conflicts).
			Msg("decomposition conflict: multiple children modify the same file")

		var sb strings.Builder
		sb.WriteString("## Decomposition File Conflicts Detected\n\n")
		sb.WriteString("The following files are claimed by multiple child tickets. This may cause merge conflicts:\n\n")
		for _, c := range conflicts {
			fmt.Fprintf(&sb, "- **%s** — claimed by: %s\n", c.File, strings.Join(c.Children, ", "))
		}
		sb.WriteString("\nConsider adding dependency edges between the affected children before approving them.\n")
		if err := d.tracker.AddComment(ctx, ticket.ExternalID, sb.String()); err != nil {
			log.Warn().Err(err).Str("ticket_id", ticket.ID).Msg("failed to post conflict warning comment")
		}
	}

	var childIDs []string
	for _, spec := range result.Children {
		created, err := d.tracker.CreateTicket(ctx, tracker.CreateTicketRequest{
			Title:              spec.Title,
			Description:        spec.Description,
			AcceptanceCriteria: strings.Join(spec.AcceptanceCriteria, "\n"),
			Labels:             []string{d.cfg.ApprovalLabel + "-pending"},
			ParentID:           ticket.ExternalID,
			Metadata:           map[string]string{"foreman_depth": "1"},
		})
		if err != nil {
			return childIDs, fmt.Errorf("creating child ticket %q: %w", spec.Title, err)
		}
		childIDs = append(childIDs, created.ExternalID)
	}

	if err := d.tracker.AddLabel(ctx, ticket.ExternalID, d.cfg.ParentLabel); err != nil {
		return childIDs, fmt.Errorf("labeling parent: %w", err)
	}

	comment := d.formatComment(result, childIDs)
	if err := d.tracker.AddComment(ctx, ticket.ExternalID, comment); err != nil {
		return childIDs, fmt.Errorf("commenting on parent: %w", err)
	}

	return childIDs, nil
}

func (d *Decomposer) generateChildSpecs(ctx context.Context, ticket *models.Ticket) (*DecompositionResult, error) {
	prompt := fmt.Sprintf(`Decompose this ticket into 3-6 child tickets. Each child should represent one reviewable PR that is independently testable.

Title: %s
Description: %s
Acceptance Criteria: %s

Return a JSON object with this schema:
{"children": [{"title": "...", "description": "...", "acceptance_criteria": ["..."], "estimated_complexity": "low|medium|high", "depends_on": ["title of other child"]}], "rationale": "..."}`,
		ticket.Title, ticket.Description, ticket.AcceptanceCriteria)

	resp, err := d.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: "You are a technical project manager. Decompose tickets into focused, implementable child tickets.",
		UserPrompt:   prompt,
		MaxTokens:    4096,
		Temperature:  0.2,
	})
	if err != nil {
		return nil, err
	}

	var result DecompositionResult
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w", err)
	}
	return &result, nil
}

func (d *Decomposer) formatComment(result *DecompositionResult, childIDs []string) string {
	var sb strings.Builder
	sb.WriteString("## Ticket Decomposed\n\n")
	fmt.Fprintf(&sb, "**Rationale:** %s\n\n", result.Rationale)
	sb.WriteString("**Child tickets:**\n")
	for i, spec := range result.Children {
		id := "pending"
		if i < len(childIDs) {
			id = "#" + childIDs[i]
		}
		fmt.Fprintf(&sb, "- %s (%s) — %s\n", spec.Title, id, spec.EstimatedComplexity)
	}
	fmt.Fprintf(&sb, "\nApprove children by changing their label to `%s`.\n", d.cfg.ApprovalLabel)
	return sb.String()
}

// countScopeKeywords counts occurrences of scope-expanding words.
func countScopeKeywords(text string) int {
	words := strings.Fields(strings.ToLower(text))
	count := 0
	for _, w := range words {
		for _, kw := range scopeKeywords {
			if w == kw {
				count++
				break
			}
		}
	}
	return count
}
