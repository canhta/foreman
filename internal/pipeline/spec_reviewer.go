package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
)

// SpecReviewInput is what the spec reviewer needs.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type SpecReviewInput struct {
	TaskTitle          string
	Diff               string
	TestOutput         string
	AcceptanceCriteria []string
	PromptVersion      string
}

// SpecReviewRunner is the interface for spec compliance checking.
type SpecReviewRunner interface {
	Review(ctx context.Context, input SpecReviewInput) (*models.ReviewOutput, error)
}

// Compile-time check.
var _ SpecReviewRunner = (*SpecReviewer)(nil)

// SpecReviewer checks if implementation meets acceptance criteria.
type SpecReviewer struct {
	llm      llm.LlmProvider
	registry *prompts.Registry
}

// NewSpecReviewer creates a spec reviewer.
// Registry is required; NewSpecReviewer panics if reg is nil.
func NewSpecReviewer(provider llm.LlmProvider, reg *prompts.Registry) *SpecReviewer {
	if reg == nil {
		panic("spec_reviewer: registry must not be nil")
	}
	return &SpecReviewer{llm: provider, registry: reg}
}

// Review runs a spec review and returns the parsed result.
func (r *SpecReviewer) Review(ctx context.Context, input SpecReviewInput) (*models.ReviewOutput, error) {
	if len(input.AcceptanceCriteria) == 0 {
		return nil, fmt.Errorf("spec review requires at least one acceptance criterion")
	}

	system, err := r.registry.Render(prompts.KindRole, "spec-reviewer", map[string]any{
		"task_title":          input.TaskTitle,
		"acceptance_criteria": input.AcceptanceCriteria,
		"diff":                input.Diff,
	})
	if err != nil {
		return nil, fmt.Errorf("render spec_reviewer prompt: %w", err)
	}

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt:  system,
		UserPrompt:    "Please provide your review.",
		PromptVersion: input.PromptVersion,
		Stage:         "spec_review",
		MaxTokens:     2048,
		Temperature:   0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("spec review LLM call: %w", err)
	}

	return ParseReviewOutputTyped(resp.Content), nil
}
