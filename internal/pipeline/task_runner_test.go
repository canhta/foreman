package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockTaskRunnerDB struct {
	tasks      map[string]*models.Task
	callCounts map[string]int
	statuses   map[string]models.TaskStatus
	updateErr  error
}

func newMockTaskRunnerDB() *mockTaskRunnerDB {
	return &mockTaskRunnerDB{
		tasks:      make(map[string]*models.Task),
		callCounts: make(map[string]int),
		statuses:   make(map[string]models.TaskStatus),
	}
}

func (m *mockTaskRunnerDB) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	return &models.Ticket{ID: id}, nil
}

func (m *mockTaskRunnerDB) ListTasks(_ context.Context, _ string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockTaskRunnerDB) UpdateTaskStatus(_ context.Context, id string, status models.TaskStatus) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.statuses[id] = status
	return nil
}

func (m *mockTaskRunnerDB) IncrementTaskLlmCalls(_ context.Context, id string) (int, error) {
	m.callCounts[id]++
	return m.callCounts[id], nil
}

// --- Tests ---

func TestDetectEscalation(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "no escalation",
			output:   "=== NEW FILE: main.go ===\npackage main\n=== END FILE ===",
			expected: "",
		},
		{
			name:     "NEEDS_CLARIFICATION marker",
			output:   "NEEDS_CLARIFICATION: Should user IDs be UUIDs or auto-increment integers?",
			expected: "Should user IDs be UUIDs or auto-increment integers?",
		},
		{
			name:     "CLARIFICATION_NEEDED marker",
			output:   "CLARIFICATION_NEEDED: Which authentication method should be used?",
			expected: "Which authentication method should be used?",
		},
		{
			name:     "multiline takes first line",
			output:   "NEEDS_CLARIFICATION: First question\nSecond question\nThird question",
			expected: "First question",
		},
		{
			name:     "marker without content",
			output:   "NEEDS_CLARIFICATION: ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectEscalation(tt.output)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscalationError(t *testing.T) {
	err := &EscalationError{Question: "What DB should I use?"}
	assert.Contains(t, err.Error(), "clarification")
	assert.Contains(t, err.Error(), "What DB should I use?")

	var escalation *EscalationError
	assert.True(t, errors.As(err, &escalation))
}

func TestFeedbackAccumulator_Integration(t *testing.T) {
	fb := NewFeedbackAccumulator()
	assert.False(t, fb.HasFeedback())

	fb.AddSpecFeedback("Missing user validation")
	fb.AddQualityFeedback("[CRITICAL] SQL injection in handler")
	assert.True(t, fb.HasFeedback())

	rendered := fb.Render()
	assert.Contains(t, rendered, "Spec review issues")
	assert.Contains(t, rendered, "Missing user validation")
	assert.Contains(t, rendered, "Quality review issues")
	assert.Contains(t, rendered, "SQL injection")

	fb.Reset()
	assert.False(t, fb.HasFeedback())
	assert.Equal(t, "", fb.Render())
}

func TestCallCapExceeded(t *testing.T) {
	db := newMockTaskRunnerDB()

	// First 8 calls should succeed.
	for i := 0; i < 8; i++ {
		err := CheckTaskCallCap(context.Background(), db, "task-1", 8)
		require.NoError(t, err)
	}

	// 9th call should fail.
	err := CheckTaskCallCap(context.Background(), db, "task-1", 8)
	require.Error(t, err)

	var capErr *CallCapExceededError
	assert.True(t, errors.As(err, &capErr))
	assert.Equal(t, "task-1", capErr.TaskID)
}

func TestReviewRejectedError(t *testing.T) {
	err := &reviewRejectedError{reviewer: "spec"}
	assert.Equal(t, "spec review rejected", err.Error())
}

func TestQualityReviewPassesTaskID(t *testing.T) {
	db := newMockTaskRunnerDB()
	// Pre-seed call count so next increment exceeds cap.
	taskID := "qr-task-42"
	db.callCounts[taskID] = 8

	llm := &mockLLM{
		responses: map[string]string{
			"quality_reviewer": "STATUS: APPROVED\nISSUES:\n- None",
		},
	}
	r := &PipelineTaskRunner{
		db:              db,
		qualityReviewer: NewQualityReviewer(llm),
		config: TaskRunnerConfig{
			WorkDir:            "/some/filesystem/path",
			MaxLlmCallsPerTask: 8,
		},
	}
	feedback := NewFeedbackAccumulator()

	err := r.runQualityReview(context.Background(), taskID, "+diff", feedback)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call cap")

	// The DB must have been called with the task ID, not the WorkDir path.
	var capErr *CallCapExceededError
	require.True(t, errors.As(err, &capErr))
	assert.Equal(t, taskID, capErr.TaskID, "CheckTaskCallCap must receive the task ID, not a filesystem path")

	// WorkDir should never appear as a key in call counts.
	_, usedPath := db.callCounts["/some/filesystem/path"]
	assert.False(t, usedPath, "DB must not be called with WorkDir as task ID")
}

func TestFeedbackAccumulator_ResetBetweenRetries(t *testing.T) {
	// Simulate what RunTask does: feedback should be cleared at the start
	// of each retry loop iteration so that stale feedback from attempt N
	// does not leak into attempt N+1.
	feedback := NewFeedbackAccumulator()

	for attempt := 1; attempt <= 3; attempt++ {
		// Reset at the very start of each iteration (the fix).
		feedback.Reset()

		// At the start of every attempt, feedback must be empty.
		assert.False(t, feedback.HasFeedback(),
			"attempt %d: feedback should be empty at start of loop iteration", attempt)

		// Simulate a failure that adds feedback.
		feedback.AddTestError("test failed on attempt " + fmt.Sprintf("%d", attempt))
		assert.True(t, feedback.HasFeedback())
	}
}

func TestTaskRunnerConfig_Defaults(t *testing.T) {
	config := TaskRunnerConfig{
		MaxImplementationRetries: 2,
		MaxSpecReviewCycles:      2,
		MaxQualityReviewCycles:   1,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    true,
		SearchReplaceSimilarity:  0.92,
	}

	assert.Equal(t, 2, config.MaxImplementationRetries)
	assert.Equal(t, 8, config.MaxLlmCallsPerTask)
	assert.True(t, config.EnableTDDVerification)
}
