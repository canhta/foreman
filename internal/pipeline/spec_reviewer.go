package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// SpecReviewInput is what the spec reviewer needs.
type SpecReviewInput struct {
	TaskTitle          string
	AcceptanceCriteria []string
	Diff               string
	TestOutput         string
}

// SpecReviewer checks if implementation meets acceptance criteria.
type SpecReviewer struct {
	llm llm.LlmProvider
}

// NewSpecReviewer creates a spec reviewer.
func NewSpecReviewer(provider llm.LlmProvider) *SpecReviewer {
	return &SpecReviewer{llm: provider}
}

// Review runs a spec review and returns the parsed result.
func (r *SpecReviewer) Review(ctx context.Context, input SpecReviewInput) (*ReviewResult, error) {
	system := `You verify that the implementation satisfies every acceptance criterion. Nothing more.

## Rules
1. Check EVERY criterion. Mark pass or fail.
2. Flag any extra functionality not requested (YAGNI).
3. Do NOT comment on code quality or style.
4. Be specific — say exactly what's missing and where.

## Output Format
STATUS: APPROVED | REJECTED

CRITERIA:
- [pass/fail] <criterion>

ISSUES:
- <what's missing, which file, what's needed>

EXTRAS:
- <anything not requested>`

	var user strings.Builder
	user.WriteString(fmt.Sprintf("## Task\n%s\n\nCriteria:\n", input.TaskTitle))
	for _, c := range input.AcceptanceCriteria {
		user.WriteString(fmt.Sprintf("- %s\n", c))
	}
	user.WriteString(fmt.Sprintf("\n## Diff\n```diff\n%s\n```\n\n## Test Output\n```\n%s\n```\n", input.Diff, input.TestOutput))

	resp, err := r.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   user.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("spec review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content), nil
}
