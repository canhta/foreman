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

// Compile-time check.
var _ FinalReviewRunner = (*FinalReviewer)(nil)

// NewFinalReviewer creates a final reviewer.
// Registry is required; NewFinalReviewer panics if reg is nil.
func NewFinalReviewer(provider llm.LlmProvider, reg *prompts.Registry) *FinalReviewer {
	if reg == nil {
		panic("final_reviewer: registry must not be nil")
	}
	return &FinalReviewer{llm: provider, registry: reg}
}

// Review runs the final review and returns the parsed result.
func (r *FinalReviewer) Review(ctx context.Context, input FinalReviewInput) (*models.ReviewOutput, error) {
	system, err := r.registry.Render(prompts.KindRole, "final-reviewer", map[string]any{
		"ticket_title":       input.TicketTitle,
		"ticket_description": input.TicketDescription,
		"full_diff":          input.FullDiff,
		"completed_tasks":    input.TaskSummaries,
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

	return ParseReviewOutputTyped(resp.Content), nil
}
