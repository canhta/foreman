// internal/pipeline/final_reviewer.go
package pipeline

import (
	"context"
	"fmt"

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
	completedTasks := make([]CompletedTask, len(input.TaskSummaries))
	for i, t := range input.TaskSummaries {
		completedTasks[i] = CompletedTask(t)
	}

	system, err := RenderPrompt("final_reviewer", PromptContext{
		TicketTitle:       input.TicketTitle,
		TicketDescription: input.TicketDescription,
		FullDiff:          input.FullDiff,
		CompletedTasks:    completedTasks,
	})
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

	return ParseReviewOutput(resp.Content), nil
}
