// internal/pipeline/plan_confidence.go
package pipeline

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// DefaultPlanConfidenceThreshold is the minimum score required to proceed.
// Plans below this threshold trigger clarification (REQ-PIPE-002).
const DefaultPlanConfidenceThreshold = 0.6

// PlanConfidenceResult holds the LLM-evaluated quality score for a plan.
type PlanConfidenceResult struct {
	Concerns []string
	Score    float64
}

// ScorePlanConfidence asks the LLM to evaluate plan quality and return a
// confidence score (0.0-1.0) and a list of concerns (REQ-PIPE-002).
// Returns an error only on LLM call failure; parsing failures return score=0.
func ScorePlanConfidence(ctx context.Context, provider LLMProvider, tasks []PlannedTask, model string) (*PlanConfidenceResult, error) {
	if len(tasks) == 0 {
		return &PlanConfidenceResult{Score: 0, Concerns: []string{"plan has no tasks"}}, nil
	}

	var sb strings.Builder
	sb.WriteString("## Plan Tasks\n")
	for i, t := range tasks {
		fmt.Fprintf(&sb, "%d. **%s** (complexity: %s)\n", i+1, t.Title, t.EstimatedComplexity)
		if t.Description != "" {
			fmt.Fprintf(&sb, "   %s\n", t.Description)
		}
		if len(t.AcceptanceCriteria) > 0 {
			sb.WriteString("   Criteria:\n")
			for _, c := range t.AcceptanceCriteria {
				fmt.Fprintf(&sb, "   - %s\n", c)
			}
		}
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&sb, "   Depends on: %s\n", strings.Join(t.DependsOn, ", "))
		}
	}

	resp, err := provider.Complete(ctx, models.LlmRequest{
		Model: model,
		SystemPrompt: `You evaluate software implementation plans for quality and completeness.

Score the plan 0.0-1.0 on:
- Task granularity (each task completable in one LLM session)
- Acceptance criteria specificity
- Dependency ordering correctness
- File coverage completeness
- Test assertion quality

Output format (strict):
CONFIDENCE_SCORE: <0.0-1.0>
CONCERNS:
- <concern 1>
- <concern 2>

If no concerns, write "CONCERNS:\n- none"`,
		UserPrompt:  sb.String(),
		MaxTokens:   512,
		Temperature: 0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("plan confidence LLM call: %w", err)
	}

	return parsePlanConfidenceResponse(resp.Content), nil
}

func parsePlanConfidenceResponse(content string) *PlanConfidenceResult {
	result := &PlanConfidenceResult{}
	lines := strings.Split(content, "\n")
	inConcerns := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CONFIDENCE_SCORE:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "CONFIDENCE_SCORE:"))
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				result.Score = v
			}
		} else if strings.HasPrefix(line, "CONCERNS:") {
			inConcerns = true
		} else if inConcerns && strings.HasPrefix(line, "- ") {
			concern := strings.TrimPrefix(line, "- ")
			if concern != "none" && concern != "" {
				result.Concerns = append(result.Concerns, concern)
			}
		}
	}
	return result
}
