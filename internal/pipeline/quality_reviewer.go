package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// QualityReviewInput is what the quality reviewer needs.
type QualityReviewInput struct {
	Diff             string
	CodebasePatterns string
}

// QualityReviewRunner is the interface for code quality checking.
type QualityReviewRunner interface {
	Review(ctx context.Context, input QualityReviewInput) (*ReviewResult, error)
}

// QualityReviewer checks code quality, not spec compliance.
type QualityReviewer struct {
	llm llm.LlmProvider
}

// NewQualityReviewer creates a quality reviewer.
func NewQualityReviewer(provider llm.LlmProvider) *QualityReviewer {
	return &QualityReviewer{llm: provider}
}

// Compile-time check.
var _ QualityReviewRunner = (*QualityReviewer)(nil)

// Review runs a quality review and returns the parsed result.
func (r *QualityReviewer) Review(ctx context.Context, input QualityReviewInput) (*ReviewResult, error) {
	system, err := RenderPrompt("quality_reviewer", PromptContext{
		Diff:             input.Diff,
		CodebasePatterns: input.CodebasePatterns,
	})
	if err != nil {
		return nil, fmt.Errorf("render quality_reviewer prompt: %w", err)
	}

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   "Please provide your review.",
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("quality review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content), nil
}
