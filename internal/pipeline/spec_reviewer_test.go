package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specMock is a simple mock LLM that returns canned responses.
type specMock struct {
	response string
}

func (m *specMock) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{
		Content:      m.response,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        "test",
		StopReason:   models.StopReasonEndTurn,
	}, nil
}
func (m *specMock) ProviderName() string                { return "mock" }
func (m *specMock) HealthCheck(_ context.Context) error { return nil }

func TestSpecReviewer_Approved(t *testing.T) {
	mock := &specMock{response: `STATUS: APPROVED

CRITERIA:
- [pass] Handler returns 200
- [pass] Response is JSON

ISSUES:
- None

EXTRAS:
- None`}

	reviewer := NewSpecReviewer(mock, mustLoadTestRegistry(t))
	result, err := reviewer.Review(context.Background(), SpecReviewInput{
		TaskTitle:          "Add user handler",
		AcceptanceCriteria: []string{"Handler returns 200", "Response is JSON"},
		Diff:               "diff --git a/handler.go\n+func GetUsers() {}",
		TestOutput:         "PASS: all tests",
	})

	require.NoError(t, err)
	assert.True(t, result.Approved)
}

func TestSpecReviewer_EmptyCriteria(t *testing.T) {
	reviewer := NewSpecReviewer(&specMock{}, mustLoadTestRegistry(t))
	_, err := reviewer.Review(context.Background(), SpecReviewInput{
		TaskTitle:          "Add user handler",
		AcceptanceCriteria: []string{},
		Diff:               "diff",
		TestOutput:         "PASS",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acceptance criterion")
}

func TestSpecReviewer_Rejected(t *testing.T) {
	mock := &specMock{response: `STATUS: REJECTED

CRITERIA:
- [pass] Handler returns 200
- [fail] Response is JSON

ISSUES:
- Handler returns plain text, not JSON. Need to set Content-Type header and use json.Marshal.

EXTRAS:
- None`}

	reviewer := NewSpecReviewer(mock, mustLoadTestRegistry(t))
	result, err := reviewer.Review(context.Background(), SpecReviewInput{
		TaskTitle:          "Add user handler",
		AcceptanceCriteria: []string{"Handler returns 200", "Response is JSON"},
		Diff:               "diff --git a/handler.go\n+func GetUsers() { w.Write([]byte(\"ok\")) }",
		TestOutput:         "PASS",
	})

	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.NotEmpty(t, result.Issues)
	assert.Contains(t, result.Issues[0].Description, "plain text")
}
