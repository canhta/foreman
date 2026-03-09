package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAgentRunnerForPlanner struct {
	result agent.AgentResult
	err    error
	gotReq agent.AgentRequest
}

func (m *mockAgentRunnerForPlanner) Run(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	m.gotReq = req
	return m.result, m.err
}
func (m *mockAgentRunnerForPlanner) HealthCheck(_ context.Context) error { return nil }
func (m *mockAgentRunnerForPlanner) RunnerName() string                  { return "mock" }
func (m *mockAgentRunnerForPlanner) Close() error                        { return nil }

func TestAgentPlanner_Plan_StructuredOutput(t *testing.T) {
	planResult := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Add handler", Description: "Create HTTP handler", FilesToModify: []string{"handler.go"}},
			{Title: "Add tests", Description: "Write tests", DependsOn: []string{"Add handler"}},
		},
		CodebasePatterns: CodebasePatterns{Language: "Go", Framework: "net/http"},
	}
	structured, _ := json.Marshal(planResult)

	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{
			Structured: json.RawMessage(structured),
		},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxTasksPerTicket: 10})

	result, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{
		ID:    "t-1",
		Title: "Add user signup",
	})
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 2)
	assert.Equal(t, "Add handler", result.Tasks[0].Title)

	// Should have set OutputSchema
	assert.NotNil(t, mock.gotReq.OutputSchema)
	// Should have set WorkDir
	assert.Equal(t, "/tmp/repo", mock.gotReq.WorkDir)
}

func TestAgentPlanner_Plan_AgentError_ReturnsFallbackError(t *testing.T) {
	mock := &mockAgentRunnerForPlanner{
		err: fmt.Errorf("agent timeout"),
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxTasksPerTicket: 10})

	_, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{Title: "Test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent timeout")
}

func TestAgentPlanner_Plan_InvalidStructured_ReturnsError(t *testing.T) {
	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{
			Structured: "not valid json object",
		},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxTasksPerTicket: 10})

	_, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{Title: "Test"})
	assert.Error(t, err)
}

func TestAgentPlanner_Plan_ValidatesAndSorts(t *testing.T) {
	planResult := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "B", DependsOn: []string{"A"}},
			{Title: "A"},
		},
	}
	structured, _ := json.Marshal(planResult)

	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{Structured: json.RawMessage(structured)},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxTasksPerTicket: 10})

	result, err := ap.Plan(context.Background(), "/tmp", &models.Ticket{Title: "T"})
	require.NoError(t, err)

	// Should be topologically sorted: A before B
	assert.Equal(t, "A", result.Tasks[0].Title)
	assert.Equal(t, "B", result.Tasks[1].Title)
}
