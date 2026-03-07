package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/canhta/foreman/internal/db"
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

func (m *mockTaskRunnerDB) SetTaskErrorType(_ context.Context, _, _ string) error { return nil }

func (m *mockTaskRunnerDB) WriteContextFeedback(_ context.Context, _ dbpkg.ContextFeedbackRow) error {
	return nil
}

func (m *mockTaskRunnerDB) QueryContextFeedback(_ context.Context, _ []string, _ float64) ([]dbpkg.ContextFeedbackRow, error) {
	return nil, nil
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
	// Simulate what RunTask does: feedback should be collapsed at the start
	// of each retry loop iteration (attempt > 1) so that stale raw entries
	// from attempt N do not grow unboundedly into attempt N+1.
	feedback := NewFeedbackAccumulator()

	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			// ResetKeepingSummary collapses prior feedback to one summary entry.
			feedback.ResetKeepingSummary()
			// After collapse, should have at most 1 entry (the summary).
			assert.LessOrEqual(t, feedback.Attempt(), 1,
				"attempt %d: after ResetKeepingSummary should have at most 1 summary entry", attempt)
		}

		// Simulate a failure that adds feedback.
		feedback.AddTestError("test failed on attempt " + fmt.Sprintf("%d", attempt))
		assert.True(t, feedback.HasFeedback())
	}
}

func TestFeedbackAccumulator_ResetKeepingSummary_CarriesPriorContext(t *testing.T) {
	feedback := NewFeedbackAccumulator()

	// Attempt 1: quality reviewer found a critical issue.
	feedback.AddQualityFeedback("[CRITICAL] hardcoded secret key in config.go")
	require.Equal(t, 1, feedback.Attempt())

	// Between attempt 1 and 2: collapse.
	feedback.ResetKeepingSummary()
	assert.Equal(t, 1, feedback.Attempt(), "collapsed to single summary entry")

	// Attempt 2: test failure.
	feedback.AddTestError("panic: runtime error in handler_test.go:42")
	assert.Equal(t, 2, feedback.Attempt())

	rendered := feedback.Render()
	// The collapsed prior context must still be visible to the implementer.
	assert.Contains(t, rendered, "Prior attempt summary")
	assert.Contains(t, rendered, "hardcoded secret")
	// And the new attempt 2 error must be there too.
	assert.Contains(t, rendered, "runtime error")
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

// --- loadContextFiles tests ---

func TestLoadContextFiles_TokenBudget_StopsAfterBudgetExceeded(t *testing.T) {
	// Create a temp dir with three files, each large enough that two exceed the budget.
	dir := t.TempDir()

	// Write three files: each ~400 tokens (roughly 1600 chars at 4 chars/token).
	bigContent := strings.Repeat("hello world foo bar baz qux ", 57) // ~57*7 = 399 words ≈ 400 tokens
	for i, name := range []string{"a.txt", "b.txt", "c.txt"} {
		content := fmt.Sprintf("file%d: ", i) + bigContent
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: dir}}
	// Budget of 600 tokens: should fit file a.txt (~400 tokens) but NOT b.txt (400+400 > 600).
	result := r.loadContextFiles([]string{"a.txt", "b.txt", "c.txt"}, 600)

	assert.Contains(t, result, "a.txt", "first file must be included")
	assert.NotContains(t, result, "b.txt", "second file must be excluded when budget exceeded")
	assert.NotContains(t, result, "c.txt", "third file must be excluded when budget exceeded")
}

func TestLoadContextFiles_FirstRealFileAlwaysIncluded_EvenIfExceedsBudget(t *testing.T) {
	// Create a file that is larger than the budget.
	dir := t.TempDir()
	bigContent := strings.Repeat("hello world foo bar baz qux ", 200) // ~1400 tokens
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(bigContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.txt"), []byte("tiny"), 0o644))

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: dir}}
	// Budget of 100 tokens: big.txt exceeds it, but since it's the first real file it must be included.
	result := r.loadContextFiles([]string{"big.txt", "small.txt"}, 100)

	assert.Contains(t, result, "big.txt", "first real file must be included even if it exceeds the budget")
	assert.NotContains(t, result, "small.txt", "subsequent files must be excluded after first oversized file")
}

func TestLoadContextFiles_MissingFileDoesNotCountAsFirstRealFile(t *testing.T) {
	// A FILE NOT FOUND placeholder must NOT exempt the next real file from budget checks.
	// Scenario: missing.txt (placeholder), then big.txt (exceeds budget), then small.txt
	// Because big.txt is the FIRST REAL file, it must still be included (at least one real file).
	// But after big.txt, small.txt must be excluded since realFilesAdded > 0 and budget exceeded.
	dir := t.TempDir()
	bigContent := strings.Repeat("hello world foo bar baz qux ", 200) // ~1400 tokens
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(bigContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.txt"), []byte("tiny"), 0o644))
	// missing.txt is intentionally not created.

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: dir}}
	// Budget of 100 tokens. missing.txt → placeholder, big.txt → first real file (must be included).
	// After big.txt, tokensUsed >> budget, so small.txt must be excluded.
	result := r.loadContextFiles([]string{"missing.txt", "big.txt", "small.txt"}, 100)

	assert.Contains(t, result, "missing.txt", "placeholder must be present for missing file")
	assert.Contains(t, result["missing.txt"], "[FILE NOT FOUND", "placeholder must contain FILE NOT FOUND marker")
	assert.Contains(t, result, "big.txt", "first real file after placeholder must be included")
	assert.NotContains(t, result, "small.txt", "file after first-real-file-that-exceeded-budget must be excluded")
}

func TestLoadContextFiles_TokensAccumulateCorrectly(t *testing.T) {
	// Verify that tokensUsed accumulates from the FIRST real file, not the second.
	// Three files: a.txt (~200 tokens), b.txt (~200 tokens), c.txt (~200 tokens).
	// Budget: 350 tokens. After a.txt (200), adding b.txt would reach 400 > 350, so b.txt excluded.
	dir := t.TempDir()
	content200 := strings.Repeat("hello world foo bar baz ", 40) // ~40*5 = 200 words ≈ 200 tokens
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content200), 0o644))
	}

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: dir}}
	result := r.loadContextFiles([]string{"a.txt", "b.txt", "c.txt"}, 350)

	assert.Contains(t, result, "a.txt", "a.txt should be included (~200 tokens)")
	assert.NotContains(t, result, "b.txt", "b.txt should be excluded (200+200=400 > 350)")
	assert.NotContains(t, result, "c.txt", "c.txt should be excluded after b.txt stops iteration")
}

func TestLoadContextFiles_NoBudget_AllFilesIncluded(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("content of "+name), 0o644))
	}

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: dir}}
	result := r.loadContextFiles([]string{"a.txt", "b.txt", "c.txt"}, 0)

	assert.Contains(t, result, "a.txt")
	assert.Contains(t, result, "b.txt")
	assert.Contains(t, result, "c.txt")
}
