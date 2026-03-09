// internal/pipeline/planner_test.go
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plannerWorkDir creates a minimal repo directory suitable for the planner.
func plannerWorkDir(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test\ngo 1.23"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))
	return workDir
}

// singleTaskPlanYAML is a valid planner YAML with one task referencing internal/handler.go.
const singleTaskPlanYAML = `status: OK
message: ""
codebase_patterns:
  language: "go"
  framework: "stdlib"
  test_runner: "go test"
  style_notes: "standard go"
tasks:
  - title: "Add endpoint"
    description: "Add a GET /users endpoint"
    acceptance_criteria:
      - "GET /users returns 200"
    test_assertions:
      - "TestGetUsers returns status 200"
    files_to_read:
      - "internal/handler.go"
    files_to_modify:
      - "internal/handler.go"
    estimated_complexity: "simple"
    depends_on: []`

// mockHandoffStore captures SetHandoff calls for assertions.
type mockHandoffStore struct {
	records []*models.HandoffRecord
	err     error
}

func (m *mockHandoffStore) SetHandoff(_ context.Context, h *models.HandoffRecord) error {
	if m.err != nil {
		return m.err
	}
	m.records = append(m.records, h)
	return nil
}

// stageLLM returns different responses based on the Stage field of the request.
type stageLLM struct {
	planResponse       string
	confidenceResponse string
	confidenceErr      error
}

func (s *stageLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	if req.Stage == "plan_confidence" {
		if s.confidenceErr != nil {
			return nil, s.confidenceErr
		}
		return &models.LlmResponse{
			Content:    s.confidenceResponse,
			StopReason: models.StopReasonEndTurn,
		}, nil
	}
	return &models.LlmResponse{
		Content:    s.planResponse,
		StopReason: models.StopReasonEndTurn,
	}, nil
}
func (s *stageLLM) ProviderName() string                { return "stage-mock" }
func (s *stageLLM) HealthCheck(_ context.Context) error { return nil }

func TestPlanner_Plan(t *testing.T) {
	workDir := plannerWorkDir(t)
	llm := &mockLLM{responses: map[string]string{"planner": singleTaskPlanYAML}}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	})

	ticket := &models.Ticket{
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "Add endpoint", result.Tasks[0].Title)
}

func TestPlanner_Plan_ClarificationNeeded(t *testing.T) {
	llm := &mockLLM{responses: map[string]string{
		"planner": "CLARIFICATION_NEEDED: What authentication method should be used?",
	}}
	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	})

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	ticket := &models.Ticket{
		Title:       "Add auth",
		Description: "Add authentication to the app.",
	}

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "CLARIFICATION_NEEDED", result.Status)
	assert.Contains(t, result.Message, "authentication")
}

func TestPlanner_Plan_ValidationFails(t *testing.T) {
	plannerYAML := `status: OK
tasks:
  - title: "Read missing file"
    description: "test"
    acceptance_criteria: ["test"]
    test_assertions: ["test"]
    files_to_read:
      - "nonexistent/path.go"
    files_to_modify: []
    estimated_complexity: "simple"
    depends_on: []`

	llm := &mockLLM{responses: map[string]string{"planner": plannerYAML}}
	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	})

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	ticket := &models.Ticket{
		Title:       "Test",
		Description: "Test ticket with sufficient description for the planner to work with.",
	}

	_, err := planner.Plan(context.Background(), workDir, ticket)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// ---------------------------------------------------------------------------
// Plan confidence scoring integration tests (REQ-PIPE-002)
// ---------------------------------------------------------------------------

// TestPlanner_ConfidenceScoring_LowScoreTrigersClarification verifies that a
// plan with confidence < threshold causes the result to have status
// CLARIFICATION_NEEDED and that the score is stored via the handoff store.
func TestPlanner_ConfidenceScoring_LowScoreTrigersClarification(t *testing.T) {
	workDir := plannerWorkDir(t)

	llm := &stageLLM{
		planResponse:       singleTaskPlanYAML,
		confidenceResponse: "CONFIDENCE_SCORE: 0.3\nCONCERNS:\n- tasks too vague\n- missing tests",
	}

	store := &mockHandoffStore{}
	ticket := &models.Ticket{
		ID:          "ticket-123",
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	}).WithConfidenceScoring(DefaultPlanConfidenceThreshold).WithHandoffStore(store)

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "CLARIFICATION_NEEDED", result.Status)
	assert.Contains(t, result.Message, "0.30")

	// Confidence score must be stored as a handoff.
	require.Len(t, store.records, 1)
	assert.Equal(t, "ticket-123", store.records[0].TicketID)
	assert.Equal(t, "plan_confidence", store.records[0].Key)
	assert.Equal(t, "0.30", store.records[0].Value)
}

// TestPlanner_ConfidenceScoring_HighScoreProceeds verifies that a plan with
// confidence >= threshold keeps status OK and still stores the score.
func TestPlanner_ConfidenceScoring_HighScoreProceeds(t *testing.T) {
	workDir := plannerWorkDir(t)

	llm := &stageLLM{
		planResponse:       singleTaskPlanYAML,
		confidenceResponse: "CONFIDENCE_SCORE: 0.9\nCONCERNS:\n- none",
	}

	store := &mockHandoffStore{}
	ticket := &models.Ticket{
		ID:          "ticket-456",
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	}).WithConfidenceScoring(DefaultPlanConfidenceThreshold).WithHandoffStore(store)

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)

	// Score still stored even when plan proceeds.
	require.Len(t, store.records, 1)
	assert.Equal(t, "ticket-456", store.records[0].TicketID)
	assert.Equal(t, "plan_confidence", store.records[0].Key)
	assert.Equal(t, "0.90", store.records[0].Value)
}

// TestPlanner_WithMetrics_ObservesCalled verifies that when WithMetrics is set,
// PlanConfidenceScore.Observe() is called during confidence scoring (REQ-TELE-001).
func TestPlanner_WithMetrics_ObservesCalled(t *testing.T) {
	workDir := plannerWorkDir(t)

	llm := &stageLLM{
		planResponse:       singleTaskPlanYAML,
		confidenceResponse: "CONFIDENCE_SCORE: 0.8\nCONCERNS:\n- none",
	}

	reg := prometheus.NewRegistry()
	m := telemetry.NewMetrics(reg)

	ticket := &models.Ticket{
		ID:          "ticket-metrics",
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	}).WithConfidenceScoring(DefaultPlanConfidenceThreshold).WithMetrics(m)

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)

	// Gather metrics and verify histogram has an observation.
	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "foreman_plan_confidence_score" {
			for _, m := range mf.GetMetric() {
				if m.GetHistogram().GetSampleCount() > 0 {
					found = true
				}
			}
		}
	}
	assert.True(t, found, "expected at least one observation in foreman_plan_confidence_score histogram")
}

// TestPlanner_ConfidenceScoring_LLMErrorIsNonFatal verifies that an LLM error
// during confidence scoring does not abort the plan — the plan proceeds with
// status OK and no handoff is stored.
func TestPlanner_ConfidenceScoring_LLMErrorIsNonFatal(t *testing.T) {
	workDir := plannerWorkDir(t)

	llm := &stageLLM{
		planResponse:  singleTaskPlanYAML,
		confidenceErr: errors.New("connection timeout"),
	}

	store := &mockHandoffStore{}
	ticket := &models.Ticket{
		ID:          "ticket-789",
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	}).WithConfidenceScoring(DefaultPlanConfidenceThreshold).WithHandoffStore(store)

	// Must not error — confidence scoring failure is graceful degradation.
	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)

	// No handoff stored because scoring failed.
	assert.Empty(t, store.records)
}

// ---------------------------------------------------------------------------
// Task 5: Structured output plan parsing (REQ-PIPE-003)
// ---------------------------------------------------------------------------

// structuredOutputLLM simulates the builtin runner's mock LLM that immediately
// calls the structured_output tool with a valid plan JSON payload.
type structuredOutputLLM struct {
	payload json.RawMessage
}

func (s *structuredOutputLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{
		StopReason: models.StopReasonToolUse,
		ToolCalls: []models.ToolCall{
			{ID: "so_1", Name: "structured_output", Input: s.payload},
		},
		TokensInput: 100, TokensOutput: 50, Model: req.Model,
	}, nil
}
func (s *structuredOutputLLM) ProviderName() string                { return "structured-mock" }
func (s *structuredOutputLLM) HealthCheck(_ context.Context) error { return nil }

// TestPlanner_UsesStructuredOutputWhenBuiltinRunnerSet verifies that when
// WithAgentRunner is set to a *BuiltinRunner, the planner uses the structured
// output path and parses the plan from AgentResult.Structured.
func TestPlanner_UsesStructuredOutputWhenBuiltinRunnerSet(t *testing.T) {
	workDir := plannerWorkDir(t)

	// Build a JSON payload matching PlannerResult but only "tasks" (as the schema requires).
	planPayload := json.RawMessage(`{
		"status": "OK",
		"tasks": [
			{
				"title": "Structured task",
				"description": "Created via structured output",
				"acceptance_criteria": ["it works"],
				"estimated_complexity": "simple",
				"depends_on": []
			}
		]
	}`)

	mockLLM := &structuredOutputLLM{payload: planPayload}
	builtinRunner := agent.NewBuiltinRunner(mockLLM, "test-model", agent.BuiltinConfig{MaxTurnsDefault: 5}, nil, nil)

	// The planner still needs an LLMProvider for the non-builtin path, but it won't be used.
	planner := NewPlanner(&stageLLM{planResponse: "should not be called"}, &models.LimitsConfig{
		MaxTasksPerTicket:  20,
		ContextTokenBudget: 30000,
	}).WithAgentRunner(builtinRunner)

	ticket := &models.Ticket{
		Title:       "Structured output test",
		Description: "Verify that the planner uses structured output when builtin runner is configured.",
	}

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	require.Equal(t, "OK", result.Status)
	require.Len(t, result.Tasks, 1)
	assert.Equal(t, "Structured task", result.Tasks[0].Title)
	assert.Equal(t, "Created via structured output", result.Tasks[0].Description)
}
