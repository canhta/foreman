package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
)

// scopeKeywords are words that suggest a ticket covers multiple features.
var scopeKeywords = []string{"and", "also", "plus", "additionally"}

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
	EstimatedComplexity string   `json:"estimated_complexity"`
	DependsOn           []string `json:"depends_on"`
}

// Decomposer breaks oversized tickets into child tracker issues.
type Decomposer struct {
	llm     LLMProvider
	tracker tracker.IssueTracker
	cfg     *models.DecomposeConfig
}

// NewDecomposer creates a new Decomposer.
func NewDecomposer(llm LLMProvider, tr tracker.IssueTracker, cfg *models.DecomposeConfig) *Decomposer {
	return &Decomposer{llm: llm, tracker: tr, cfg: cfg}
}

// Execute decomposes a ticket into child issues and creates them in the tracker.
// Returns the external IDs of the created child tickets.
func (d *Decomposer) Execute(ctx context.Context, ticket *models.Ticket) ([]string, error) {
	result, err := d.generateChildSpecs(ctx, ticket)
	if err != nil {
		return nil, fmt.Errorf("generating child specs: %w", err)
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
