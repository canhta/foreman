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
}

// NewPlanner creates a planner with the given LLM provider and limits.
func NewPlanner(llm LLMProvider, limits *models.LimitsConfig) *Planner {
	return &Planner{llm: llm, limits: limits}
}

// NewPlannerWithModel creates a planner that uses a specific model for every call.
func NewPlannerWithModel(llm LLMProvider, limits *models.LimitsConfig, model string) *Planner {
	return &Planner{llm: llm, limits: limits, model: model}
}

// Plan generates a task plan for the given ticket.
func (p *Planner) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlannerResult, error) {
	// Assemble planner context
	assembled, err := appcontext.AssemblePlannerContext(workDir, ticket, p.limits.ContextTokenBudget)
	if err != nil {
		return nil, fmt.Errorf("assembling planner context: %w", err)
	}

	// Make LLM call
	resp, err := p.llm.Complete(ctx, models.LlmRequest{
		Model:        p.model,
		SystemPrompt: assembled.SystemPrompt,
		UserPrompt:   assembled.UserPrompt,
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

	return result, nil
}
