// internal/pipeline/plan_confidence_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockConfidenceLLM struct {
	response string
}

func (m *mockConfidenceLLM) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response, StopReason: models.StopReasonEndTurn}, nil
}
func (m *mockConfidenceLLM) ProviderName() string                { return "mock" }
func (m *mockConfidenceLLM) HealthCheck(_ context.Context) error { return nil }

func TestScorePlanConfidence_ParsesScore(t *testing.T) {
	llm := &mockConfidenceLLM{response: `CONFIDENCE_SCORE: 0.85
CONCERNS:
- Task 2 has vague acceptance criteria
- Missing test assertions for edge cases`}

	tasks := []PlannedTask{
		{Title: "Add endpoint", EstimatedComplexity: "medium"},
		{Title: "Write tests", EstimatedComplexity: "simple"},
	}

	result, err := ScorePlanConfidence(context.Background(), llm, tasks, "test-model")
	require.NoError(t, err)
	assert.InDelta(t, 0.85, result.Score, 0.001)
	assert.Len(t, result.Concerns, 2)
}

func TestScorePlanConfidence_NoConcerns(t *testing.T) {
	llm := &mockConfidenceLLM{response: "CONFIDENCE_SCORE: 0.95\nCONCERNS:\n- none"}
	tasks := []PlannedTask{{Title: "Simple task", EstimatedComplexity: "simple"}}

	result, err := ScorePlanConfidence(context.Background(), llm, tasks, "test-model")
	require.NoError(t, err)
	assert.InDelta(t, 0.95, result.Score, 0.001)
	assert.Empty(t, result.Concerns)
}

func TestScorePlanConfidence_EmptyPlan(t *testing.T) {
	llm := &mockConfidenceLLM{response: "CONFIDENCE_SCORE: 0.0\nCONCERNS:\n- none"}
	result, err := ScorePlanConfidence(context.Background(), llm, nil, "test-model")
	require.NoError(t, err)
	assert.Equal(t, 0.0, result.Score)
	assert.NotEmpty(t, result.Concerns)
}

func TestParsePlanConfidenceResponse(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantScore    float64
		wantConcerns int
	}{
		{
			name:         "valid response",
			input:        "CONFIDENCE_SCORE: 0.7\nCONCERNS:\n- missing tests\n- vague criteria",
			wantScore:    0.7,
			wantConcerns: 2,
		},
		{
			name:         "no concerns",
			input:        "CONFIDENCE_SCORE: 1.0\nCONCERNS:\n- none",
			wantScore:    1.0,
			wantConcerns: 0,
		},
		{
			name:         "malformed response",
			input:        "unable to evaluate",
			wantScore:    0.0,
			wantConcerns: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parsePlanConfidenceResponse(tt.input)
			assert.InDelta(t, tt.wantScore, r.Score, 0.001)
			assert.Len(t, r.Concerns, tt.wantConcerns)
		})
	}
}
