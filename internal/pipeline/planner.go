// internal/pipeline/planner.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/models"
)

// LLMProvider is the interface for making LLM calls (matches llm.LlmProvider).
type LLMProvider interface {
	Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

const (
	plannerMaxTokens   = 4096
	plannerTemperature = 0.2
)

// Planner decomposes a ticket into implementation tasks via LLM.
type Planner struct {
	llm    LLMProvider
	limits *models.LimitsConfig
	model  string
	// planConfidenceThreshold triggers clarification when confidence < threshold.
	// 0 disables confidence scoring (no second LLM call).
	planConfidenceThreshold float64
}

// NewPlanner creates a planner with the given LLM provider and limits.
func NewPlanner(llm LLMProvider, limits *models.LimitsConfig) *Planner {
	return &Planner{llm: llm, limits: limits}
}

// NewPlannerWithModel creates a planner that uses a specific model for every call.
func NewPlannerWithModel(llm LLMProvider, limits *models.LimitsConfig, model string) *Planner {
	return &Planner{llm: llm, limits: limits, model: model}
}

// WithConfidenceScoring enables LLM-based plan confidence scoring (REQ-PIPE-002).
// Plans with score < threshold trigger CLARIFICATION_NEEDED.
// threshold=0 disables scoring (default).
func (p *Planner) WithConfidenceScoring(threshold float64) *Planner {
	p.planConfidenceThreshold = threshold
	return p
}

// Plan generates a task plan for the given ticket.
func (p *Planner) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlannerResult, error) {
	// Extract per-ticket context cache (injected by the orchestrator via context.Context).
	cache := appcontext.CacheFromContext(ctx)

	// Assemble planner context, reusing cached file tree if available.
	assembled, err := appcontext.AssemblePlannerContext(workDir, ticket, p.limits.ContextTokenBudget, cache)
	if err != nil {
		return nil, fmt.Errorf("assembling planner context: %w", err)
	}

	// Make LLM call
	resp, err := p.llm.Complete(ctx, models.LlmRequest{
		Model:        p.model,
		SystemPrompt: assembled.SystemPrompt,
		UserPrompt:   assembled.UserPrompt,
		Stage:        "planning",
		MaxTokens:    plannerMaxTokens,
		Temperature:  plannerTemperature,
	})
	if err != nil {
		return nil, fmt.Errorf("planner LLM call: %w", err)
	}

	// Parse the output
	result, err := ParsePlannerOutput(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parsing planner output: %w", err)
	}

	// If not OK, return early (clarification needed, too large, etc.)
	if result.Status != "OK" {
		return result, nil
	}

	// Validate the plan
	validation := ValidatePlan(result, workDir, p.limits)
	if !validation.Valid {
		return nil, fmt.Errorf("plan validation failed: %s", strings.Join(validation.Errors, "; "))
	}

	// Sort tasks topologically
	sorted, err := TopologicalSort(result.Tasks)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}
	result.Tasks = sorted

	// Plan confidence scoring (REQ-PIPE-002): optional second LLM call.
	if p.planConfidenceThreshold > 0 {
		confidence, confErr := ScorePlanConfidence(ctx, p.llm, result.Tasks, p.model)
		if confErr != nil {
			// Non-fatal: log and continue without blocking.
			_ = confErr
		} else {
			result.ConfidenceScore = confidence.Score
			result.ConfidenceConcerns = confidence.Concerns
			if confidence.Score < p.planConfidenceThreshold {
				result.Status = "CLARIFICATION_NEEDED"
				concerns := strings.Join(confidence.Concerns, "; ")
				result.Message = fmt.Sprintf("plan confidence %.2f below threshold %.2f: %s",
					confidence.Score, p.planConfidenceThreshold, concerns)
			}
		}
	}

	return result, nil
}
