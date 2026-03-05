// internal/pipeline/pipeline_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM returns canned responses for testing.
type mockLLM struct {
	responses map[string]string // role → response
}

func (m *mockLLM) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	// Determine role from system prompt content
	role := "implementer"
	if contains(req.SystemPrompt, "decomposing a ticket") {
		role = "planner"
	} else if contains(req.SystemPrompt, "verify that the implementation satisfies") {
		role = "spec_reviewer"
	} else if contains(req.SystemPrompt, "review code quality") {
		role = "quality_reviewer"
	}

	response, ok := m.responses[role]
	if !ok {
		response = "STATUS: APPROVED"
	}

	return &models.LlmResponse{
		Content:      response,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        "test-model",
		StopReason:   models.StopReasonEndTurn,
	}, nil
}

func (m *mockLLM) ProviderName() string                  { return "mock" }
func (m *mockLLM) HealthCheck(ctx context.Context) error { return nil }

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline(PipelineConfig{
		MaxImplementationRetries: 2,
		MaxSpecReviewCycles:      2,
		MaxQualityReviewCycles:   1,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
	})
	require.NotNil(t, p)
}

func TestPipeline_CheckTicketClarity_Sufficient(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: true})
	ticket := &models.Ticket{
		Description:        "Add a REST endpoint that returns a list of users from the database. Support pagination with limit and offset query params.",
		AcceptanceCriteria: "GET /api/users returns JSON array",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.True(t, clear)
}

func TestPipeline_CheckTicketClarity_TooVague(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: true})
	ticket := &models.Ticket{
		Description:        "fix bug",
		AcceptanceCriteria: "",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.False(t, clear)
}

func TestPipeline_CheckTicketClarity_Disabled(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: false})
	ticket := &models.Ticket{
		Description:        "fix",
		AcceptanceCriteria: "",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.True(t, clear) // Always clear when disabled
}

func TestPipeline_TopologicalSort(t *testing.T) {
	tasks := []PlannedTask{
		{Title: "Task C", DependsOn: []string{"Task A", "Task B"}},
		{Title: "Task A", DependsOn: []string{}},
		{Title: "Task B", DependsOn: []string{"Task A"}},
	}
	sorted, err := TopologicalSort(tasks)
	require.NoError(t, err)
	assert.Equal(t, "Task A", sorted[0].Title)
	assert.Equal(t, "Task B", sorted[1].Title)
	assert.Equal(t, "Task C", sorted[2].Title)
}

func TestPipeline_TopologicalSort_Cycle(t *testing.T) {
	tasks := []PlannedTask{
		{Title: "A", DependsOn: []string{"B"}},
		{Title: "B", DependsOn: []string{"A"}},
	}
	_, err := TopologicalSort(tasks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}
