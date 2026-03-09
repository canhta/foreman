package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
)

// QualityReviewInput is what the quality reviewer needs.
type QualityReviewInput struct {
	Diff             string
	CodebasePatterns string
	PromptVersion    string
}

// QualityReviewRunner is the interface for code quality checking.
type QualityReviewRunner interface {
	Review(ctx context.Context, input QualityReviewInput) (*models.ReviewOutput, error)
}

// QualityReviewer checks code quality, not spec compliance.
type QualityReviewer struct {
	llm      llm.LlmProvider
	registry *prompts.Registry
}

// Compile-time check.
var _ QualityReviewRunner = (*QualityReviewer)(nil)

// NewQualityReviewer creates a quality reviewer.
// Registry is required; NewQualityReviewer panics if reg is nil.
func NewQualityReviewer(provider llm.LlmProvider, reg *prompts.Registry) *QualityReviewer {
	if reg == nil {
		panic("quality_reviewer: registry must not be nil")
	}
	return &QualityReviewer{llm: provider, registry: reg}
}

// Review runs a quality review and returns the parsed result.
func (r *QualityReviewer) Review(ctx context.Context, input QualityReviewInput) (*models.ReviewOutput, error) {
	system, err := r.registry.Render(prompts.KindRole, "quality-reviewer", map[string]any{
		"diff":              input.Diff,
		"codebase_patterns": input.CodebasePatterns,
	})
	if err != nil {
		return nil, fmt.Errorf("render quality_reviewer prompt: %w", err)
	}

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt:  system,
		UserPrompt:    "Please provide your review.",
		PromptVersion: input.PromptVersion,
		Stage:         "quality_review",
		MaxTokens:     2048,
		Temperature:   0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("quality review LLM call: %w", err)
	}

	return ParseReviewOutputTyped(resp.Content), nil
}
