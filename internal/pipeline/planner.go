// internal/pipeline/planner.go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// planOutputSchema is the JSON schema for structured plan output.
// When the builtin runner is used, this schema is passed as OutputSchema
// so the LLM produces a schema-validated JSON payload instead of YAML.
var planOutputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tasks": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"title": {"type": "string"},
					"description": {"type": "string"},
					"acceptance_criteria": {"type": "array", "items": {"type": "string"}},
					"files_to_read": {"type": "array", "items": {"type": "string"}},
					"files_to_modify": {"type": "array", "items": {"type": "string"}},
					"test_assertions": {"type": "array", "items": {"type": "string"}},
					"estimated_complexity": {"type": "string", "enum": ["simple", "medium", "complex"]},
					"depends_on": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["title", "description", "acceptance_criteria"]
			}
		}
	},
	"required": ["tasks"]
}`)

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
	plannerMaxTokens   = 8192
	plannerTemperature = 0.2
)

// Planner decomposes a ticket into implementation tasks via LLM.
type Planner struct {
	llm          LLMProvider
	agentRunner  agent.AgentRunner
	handoffStore HandoffStorer
	metrics      *telemetry.Metrics
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

// WithMetrics attaches a Metrics instance so the planner can emit
// PlanConfidenceScore observations (REQ-TELE-001).
func (p *Planner) WithMetrics(m *telemetry.Metrics) *Planner {
	p.metrics = m
	return p
}

// WithAgentRunner attaches an AgentRunner that can produce structured plan output.
// When set and the runner is the builtin runner, Plan() will request structured JSON
// output directly instead of relying on YAML parsing (REQ-PIPE-003).
func (p *Planner) WithAgentRunner(r agent.AgentRunner) *Planner {
	p.agentRunner = r
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

	var result *PlannerResult

	// Structured output path: use builtin runner with OutputSchema for reliable JSON output.
	// Fall back to direct LLM call + YAML parsing when no builtin runner is configured.
	if _, isBuiltin := p.agentRunner.(*agent.BuiltinRunner); p.agentRunner != nil && isBuiltin {
		agentResp, agentErr := p.agentRunner.Run(ctx, agent.AgentRequest{
			Prompt:       assembled.UserPrompt,
			SystemPrompt: assembled.SystemPrompt,
			WorkDir:      workDir,
			OutputSchema: planOutputSchema,
			MaxTurns:     10,
		})
		if agentErr != nil {
			return nil, fmt.Errorf("planner agent call: %w", agentErr)
		}
		if raw := agentResp.Structured; json.Valid(raw) {
			// Parse from structured JSON output (REQ-PIPE-003).
			var pr PlannerResult
			if jsonErr := json.Unmarshal(raw, &pr); jsonErr == nil {
				if pr.Status == "" {
					pr.Status = "OK"
				}
				result = &pr
				log.Info().Str("ticket_id", ticket.ID).Msg("planner: used structured output path")
			}
		}
		// If structured output wasn't usable, fall through to YAML parsing of Output.
		if result == nil {
			result, err = ParsePlannerOutput(agentResp.Output)
			if err != nil {
				return nil, fmt.Errorf("parsing planner output (structured fallback): %w", err)
			}
		}
	} else {
		// Standard LLM call path.
		resp, llmErr := p.llm.Complete(ctx, models.LlmRequest{
			Model:        p.model,
			SystemPrompt: assembled.SystemPrompt,
			UserPrompt:   assembled.UserPrompt,
			Stage:        "planning",
			MaxTokens:    plannerMaxTokens,
			Temperature:  plannerTemperature,
		})
		if llmErr != nil {
			return nil, fmt.Errorf("planner LLM call: %w", llmErr)
		}
		result, err = ParsePlannerOutput(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("parsing planner output: %w", err)
		}
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

			// Emit Prometheus metric for the confidence score (REQ-TELE-001).
			if p.metrics != nil {
				p.metrics.PlanConfidenceScore.Observe(confidence.Score)
			}

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
