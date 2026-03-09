// internal/pipeline/final_reviewer.go
package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
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
	Review(ctx context.Context, input FinalReviewInput) (*models.ReviewOutput, error)
}

// FinalReviewer performs a final review of the complete changeset before PR creation.
type FinalReviewer struct {
	llm      llm.LlmProvider
	registry *prompts.Registry
}

// NewFinalReviewer creates a final reviewer.
func NewFinalReviewer(provider llm.LlmProvider) *FinalReviewer {
	return &FinalReviewer{llm: provider}
}

// WithRegistry attaches a prompt registry so the reviewer uses registry.Render()
// instead of the legacy RenderPrompt() function.
func (r *FinalReviewer) WithRegistry(reg *prompts.Registry) *FinalReviewer {
	r.registry = reg
	return r
}

// Compile-time check.
var _ FinalReviewRunner = (*FinalReviewer)(nil)

// Review runs the final review and returns the parsed result.
func (r *FinalReviewer) Review(ctx context.Context, input FinalReviewInput) (*models.ReviewOutput, error) {
	completedTasks := make([]CompletedTask, len(input.TaskSummaries))
	for i, t := range input.TaskSummaries {
		completedTasks[i] = CompletedTask(t)
	}

	var (
		system string
		err    error
	)
	if r.registry != nil {
		system, err = r.registry.Render(prompts.KindRole, "final-reviewer", map[string]any{
			"ticket_title":       input.TicketTitle,
			"ticket_description": input.TicketDescription,
			"full_diff":          input.FullDiff,
			"completed_tasks":    completedTasks,
		})
	} else {
		system, err = RenderPrompt("final_reviewer", PromptContext{
			TicketTitle:       input.TicketTitle,
			TicketDescription: input.TicketDescription,
			FullDiff:          input.FullDiff,
			CompletedTasks:    completedTasks,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("render final_reviewer prompt: %w", err)
	}

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   "Please provide your review.",
		Stage:        "final_review",
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("final review LLM call: %w", err)
	}

	return ParseReviewOutputTyped(resp.Content), nil
}
