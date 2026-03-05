package pipeline

import (
	"context"
	"fmt"
	"strings"

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
	// TODO: move to prompts/quality_reviewer.md.j2 when template engine is wired
	system := `You review code quality only. Do NOT check spec compliance.

## Check
- Style matches codebase patterns
- Naming consistency
- Error handling
- No obvious bugs/edge cases
- DRY
- Tests are meaningful
- No security issues (hardcoded secrets, injection, XSS)
- No performance anti-patterns

## Severity
- CRITICAL: Must fix. Security, data loss, production breakage.
- IMPORTANT: Should fix. Code smell, subtle bug.
- MINOR: Nice to fix. Does NOT block approval.

## Output Format
STATUS: APPROVED | CHANGES_REQUESTED

ISSUES:
- [CRITICAL|IMPORTANT|MINOR] <file, issue, fix suggestion>

STRENGTHS:
- <what was done well>`

	var user strings.Builder
	user.WriteString(fmt.Sprintf("## Codebase Patterns\n%s\n\n## Diff\n```diff\n%s\n```\n", input.CodebasePatterns, input.Diff))

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   user.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("quality review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content), nil
}
