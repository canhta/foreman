// internal/pipeline/final_reviewer.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// TaskSummary is a brief task status for the final reviewer.
type TaskSummary struct {
	Title  string
	Status string
}

// FinalReviewInput is what the final reviewer needs.
type FinalReviewInput struct {
	TicketTitle       string
	TicketDescription string
	FullDiff          string
	TestOutput        string
	TaskSummaries     []TaskSummary
}

// FinalReviewRunner is the interface for final changeset gating.
type FinalReviewRunner interface {
	Review(ctx context.Context, input FinalReviewInput) (*ReviewResult, error)
}

// FinalReviewer performs a final review of the complete changeset before PR creation.
type FinalReviewer struct {
	llm llm.LlmProvider
}

// NewFinalReviewer creates a final reviewer.
func NewFinalReviewer(provider llm.LlmProvider) *FinalReviewer {
	return &FinalReviewer{llm: provider}
}

// Compile-time check.
var _ FinalReviewRunner = (*FinalReviewer)(nil)

// Review runs the final review and returns the parsed result.
func (r *FinalReviewer) Review(ctx context.Context, input FinalReviewInput) (*ReviewResult, error) {
	// TODO: move to prompts/final_reviewer.md.j2 when template engine is wired
	system := `Final review of the complete changeset before PR creation.

## Check
1. Changes as a whole address the original ticket
2. Integration issues between tasks
3. Cross-cutting concerns (error handling consistency, migrations, etc.)

## Output Format
STATUS: APPROVED | REJECTED
SUMMARY: <2-3 sentences>
CHANGES: <key changes by area>
CONCERNS: <issues if any>
REVIEW_NOTES: <notes for human reviewer>`

	var prompt strings.Builder
	fmt.Fprintf(&prompt, "## Ticket\n%s\n%s\n\n", input.TicketTitle, input.TicketDescription)
	fmt.Fprintf(&prompt, "## Full Diff\n```diff\n%s\n```\n\n", input.FullDiff)
	prompt.WriteString("## Tasks\n")
	for i, t := range input.TaskSummaries {
		fmt.Fprintf(&prompt, "%d. %s — %s\n", i+1, t.Title, t.Status)
	}
	fmt.Fprintf(&prompt, "\n## Tests\n```\n%s\n```\n", input.TestOutput)

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   prompt.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("final review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content), nil
}
