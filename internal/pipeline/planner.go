// internal/pipeline/planner.go
package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// LLMProvider is the interface for making LLM calls (matches llm.LlmProvider).
type LLMProvider interface {
	Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

// HandoffStorer persists handoff records produced during the planning stage.
// It is a subset of db.Database, defined here to avoid an import cycle.
type HandoffStorer interface {
	SetHandoff(ctx context.Context, h *models.HandoffRecord) error
}

const (
	plannerMaxTokens   = 4096
	plannerTemperature = 0.2
)

// Planner decomposes a ticket into implementation tasks via LLM.
type Planner struct {
	llm          LLMProvider
	handoffStore HandoffStorer
	limits       *models.LimitsConfig
	model        string
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

// WithHandoffStore attaches a store for persisting the plan_confidence score
// as a handoff record after scoring (REQ-PIPE-002).
func (p *Planner) WithHandoffStore(store HandoffStorer) *Planner {
	p.handoffStore = store
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
			// Non-fatal: log warning and proceed without blocking.
			log.Warn().Err(confErr).
				Str("ticket_id", ticket.ID).
				Msg("plan confidence scoring failed (non-fatal, proceeding)")
		} else {
			result.ConfidenceScore = confidence.Score
			result.ConfidenceConcerns = confidence.Concerns

			// Store confidence score as a handoff for downstream consumers.
			if p.handoffStore != nil {
				scoreStr := fmt.Sprintf("%.2f", confidence.Score)
				handoffErr := p.handoffStore.SetHandoff(ctx, &models.HandoffRecord{
					ID:        uuid.New().String(),
					TicketID:  ticket.ID,
					FromRole:  "planner",
					ToRole:    "implementer",
					Key:       "plan_confidence",
					Value:     scoreStr,
					CreatedAt: time.Now(),
				})
				if handoffErr != nil {
					log.Warn().Err(handoffErr).
						Str("ticket_id", ticket.ID).
						Msg("failed to store plan_confidence handoff (non-fatal)")
				}
			}

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
