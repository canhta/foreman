package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/daemon"
	dbpkg "github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

// mockAdapterDB is a TaskRunnerDB that returns a configurable task list.
type mockAdapterDB struct {
	listErr    error
	updateErr  error
	callCounts map[string]int
	statuses   map[string]models.TaskStatus
	tasks      []models.Task
}

func newMockAdapterDB(tasks []models.Task) *mockAdapterDB {
	return &mockAdapterDB{
		tasks:      tasks,
		callCounts: make(map[string]int),
		statuses:   make(map[string]models.TaskStatus),
	}
}

func (m *mockAdapterDB) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	return &models.Ticket{ID: id}, nil
}

func (m *mockAdapterDB) ListTasks(_ context.Context, _ string) ([]models.Task, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.tasks, nil
}

func (m *mockAdapterDB) UpdateTaskStatus(_ context.Context, id string, status models.TaskStatus) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.statuses[id] = status
	return nil
}

func (m *mockAdapterDB) IncrementTaskLlmCalls(_ context.Context, id string) (int, error) {
	m.callCounts[id]++
	return m.callCounts[id], nil
}

func (m *mockAdapterDB) SetTaskErrorType(_ context.Context, _, _ string) error { return nil }

func (m *mockAdapterDB) WriteContextFeedback(_ context.Context, _ dbpkg.ContextFeedbackRow) error {
	return nil
}

func (m *mockAdapterDB) QueryContextFeedback(_ context.Context, _ []string, _ float64) ([]dbpkg.ContextFeedbackRow, error) {
	return nil, nil
}

// realMockGitProvider implements git.GitProvider using the real package types.
type realMockGitProvider struct {
	commitErr   error
	cleanErr    error
	cleanCalled *int
	diffOutput  string
	commitSHA   string
}

func (m *realMockGitProvider) EnsureRepo(_ context.Context, _ string) error      { return nil }
func (m *realMockGitProvider) CreateBranch(_ context.Context, _, _ string) error { return nil }
func (m *realMockGitProvider) Commit(_ context.Context, _, _ string) (string, error) {
	return m.commitSHA, m.commitErr
}
func (m *realMockGitProvider) Diff(_ context.Context, _, _, _ string) (string, error) {
	return m.diffOutput, nil
}
func (m *realMockGitProvider) DiffWorking(_ context.Context, _ string) (string, error) {
	return m.diffOutput, nil
}
func (m *realMockGitProvider) Push(_ context.Context, _, _ string) error { return nil }
func (m *realMockGitProvider) RebaseOnto(_ context.Context, _, _ string) (*git.RebaseResult, error) {
	return &git.RebaseResult{Success: true}, nil
}
func (m *realMockGitProvider) FileTree(_ context.Context, _ string) ([]git.FileEntry, error) {
	return nil, nil
}
func (m *realMockGitProvider) Log(_ context.Context, _ string, _ int) ([]git.CommitEntry, error) {
	return nil, nil
}
func (m *realMockGitProvider) StageAll(_ context.Context, _ string) error { return nil }
func (m *realMockGitProvider) CleanWorkingTree(_ context.Context, _ string) error {
	if m.cleanCalled != nil {
		*m.cleanCalled++
	}
	return m.cleanErr
}

// realMockCmdRunner implements runner.CommandRunner.
type realMockCmdRunner struct {
	runErr   error
	stdout   string
	stderr   string
	exitCode int
}

func (m *realMockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	if m.runErr != nil {
		return nil, m.runErr
	}
	return &runner.CommandOutput{
		Stdout:   m.stdout,
		Stderr:   m.stderr,
		ExitCode: m.exitCode,
	}, nil
}

func (m *realMockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

// buildMinimalTaskRunner wires a PipelineTaskRunner with no-op mocks.
func buildMinimalTaskRunner(db TaskRunnerDB) *PipelineTaskRunner {
	llm := &mockLLM{responses: map[string]string{}}
	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{}
	return NewPipelineTaskRunner(llm, db, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 0,
		MaxLlmCallsPerTask:       8,
		SearchReplaceSimilarity:  0.8,
	})
}

// buildNewFileResponse builds a valid implementer LLM response that creates one new file.
func buildNewFileResponse(path, content string) string {
	return "=== NEW FILE: " + path + " ===\n" + content + "\n=== END FILE ==="
}

// --- Tests ---

// TestDAGTaskAdapter_ImplementsDaemonInterface verifies compile-time interface.
func TestDAGTaskAdapter_ImplementsDaemonInterface(t *testing.T) {
	var _ daemon.TaskRunner = (*DAGTaskAdapter)(nil)
}

// TestNewDAGTaskAdapter verifies constructor wiring.
func TestNewDAGTaskAdapter(t *testing.T) {
	db := newMockAdapterDB(nil)
	r := buildMinimalTaskRunner(db)
	adapter := NewDAGTaskAdapter(r, db, "ticket-1")
	require.NotNil(t, adapter)
	assert.Same(t, r, adapter.runner)
	assert.Equal(t, "ticket-1", adapter.ticketID)
}

// TestDAGTaskAdapter_findTask_FoundByID verifies findTask returns the matching task.
func TestDAGTaskAdapter_findTask_FoundByID(t *testing.T) {
	tasks := []models.Task{
		{ID: "task-1", Title: "First task"},
		{ID: "task-2", Title: "Second task"},
	}
	db := newMockAdapterDB(tasks)
	adapter := &DAGTaskAdapter{runner: buildMinimalTaskRunner(db), db: db, ticketID: "ticket-1"}

	task, err := adapter.findTask(context.Background(), "task-2")
	require.NoError(t, err)
	assert.Equal(t, "task-2", task.ID)
	assert.Equal(t, "Second task", task.Title)
}

// TestDAGTaskAdapter_findTask_ErrorWhenNotFound verifies findTask returns an
// error when the task ID is absent from the list.
func TestDAGTaskAdapter_findTask_ErrorWhenNotFound(t *testing.T) {
	tasks := []models.Task{{ID: "task-1", Title: "Something"}}
	db := newMockAdapterDB(tasks)
	adapter := &DAGTaskAdapter{runner: buildMinimalTaskRunner(db), db: db, ticketID: "ticket-1"}

	task, err := adapter.findTask(context.Background(), "unknown-task")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "not found")
}

// TestDAGTaskAdapter_findTask_ErrorWhenDBError verifies findTask returns an
// error when the DB call fails.
func TestDAGTaskAdapter_findTask_ErrorWhenDBError(t *testing.T) {
	db := newMockAdapterDB(nil)
	db.listErr = fmt.Errorf("db unavailable")
	adapter := &DAGTaskAdapter{runner: buildMinimalTaskRunner(db), db: db, ticketID: "ticket-1"}

	task, err := adapter.findTask(context.Background(), "task-xyz")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "db unavailable")
}

// TestDAGTaskAdapter_findTask_ErrorWhenEmptyList verifies findTask returns an
// error when no tasks exist for the ticket.
func TestDAGTaskAdapter_findTask_ErrorWhenEmptyList(t *testing.T) {
	db := newMockAdapterDB([]models.Task{})
	adapter := &DAGTaskAdapter{runner: buildMinimalTaskRunner(db), db: db, ticketID: "ticket-1"}

	task, err := adapter.findTask(context.Background(), "task-abc")
	require.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "not found")
}

// TestDAGTaskAdapter_Run_Success verifies a successful run produces Done status.
func TestDAGTaskAdapter_Run_Success(t *testing.T) {
	tasks := []models.Task{{ID: "task-1", Title: "Do the thing"}}
	db := newMockAdapterDB(tasks)

	llm := &mockLLM{
		responses: map[string]string{
			"implementer":      buildNewFileResponse("hello.go", "package main\n"),
			"spec_reviewer":    "STATUS: APPROVED\nCRITERIA:\n- [pass] all good\nISSUES:\n- None",
			"quality_reviewer": "STATUS: APPROVED\nISSUES:\n- None",
		},
	}
	g := &realMockGitProvider{diffOutput: "+package main", commitSHA: "abc123"}
	cmd := &realMockCmdRunner{exitCode: 0}

	workDir := t.TempDir()
	r := NewPipelineTaskRunner(llm, db, g, cmd, TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 1,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
		SearchReplaceSimilarity:  0.8,
	})
	adapter := NewDAGTaskAdapter(r, db, "ticket-1")

	result := adapter.Run(context.Background(), "task-1")
	assert.Equal(t, "task-1", result.TaskID)
	assert.Equal(t, models.TaskStatusDone, result.Status)
	assert.NoError(t, result.Error)
}

// TestDAGTaskAdapter_Run_EscalationReturnsFailed verifies that an escalation
// error maps to TaskStatusEscalated (BUG-M01 fix) and preserves the *EscalationError.
func TestDAGTaskAdapter_Run_EscalationReturnsFailed(t *testing.T) {
	tasks := []models.Task{{ID: "task-esc", Title: "Ambiguous task"}}
	db := newMockAdapterDB(tasks)

	llm := &mockLLM{
		responses: map[string]string{
			"implementer": "NEEDS_CLARIFICATION: Should this be sync or async?",
		},
	}
	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{exitCode: 0}

	workDir := t.TempDir()
	r := NewPipelineTaskRunner(llm, db, g, cmd, TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 0,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
		SearchReplaceSimilarity:  0.8,
	})
	adapter := NewDAGTaskAdapter(r, db, "ticket-1")

	result := adapter.Run(context.Background(), "task-esc")
	assert.Equal(t, "task-esc", result.TaskID)
	assert.Equal(t, models.TaskStatusEscalated, result.Status)
	require.Error(t, result.Error)

	var escalation *EscalationError
	assert.True(t, errors.As(result.Error, &escalation), "error should be *EscalationError")
	assert.Contains(t, escalation.Question, "sync or async")
}

// TestDAGTaskAdapter_Run_AllRetriesExhausted verifies that exhausting all retries
// (bad LLM output every time) yields Failed status.
func TestDAGTaskAdapter_Run_AllRetriesExhausted(t *testing.T) {
	tasks := []models.Task{{ID: "task-bad", Title: "Bad task"}}
	db := newMockAdapterDB(tasks)

	// LLM returns unparseable output on every attempt.
	llm := &mockLLM{
		responses: map[string]string{
			"implementer": "I have no idea what to do here.",
		},
	}
	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{exitCode: 0}

	workDir := t.TempDir()
	r := NewPipelineTaskRunner(llm, db, g, cmd, TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 1, // 2 total attempts
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
		SearchReplaceSimilarity:  0.8,
	})
	adapter := NewDAGTaskAdapter(r, db, "ticket-1")

	result := adapter.Run(context.Background(), "task-bad")
	assert.Equal(t, "task-bad", result.TaskID)
	assert.Equal(t, models.TaskStatusFailed, result.Status)
	require.Error(t, result.Error)
}

// TestDAGTaskAdapter_Run_TaskNotFound_ReturnsError verifies that when the task
// is absent from the DB, the adapter returns a Failed result with an error.
func TestDAGTaskAdapter_Run_TaskNotFound_ReturnsError(t *testing.T) {
	db := newMockAdapterDB([]models.Task{})

	r := buildMinimalTaskRunner(db)
	adapter := NewDAGTaskAdapter(r, db, "ticket-1")

	result := adapter.Run(context.Background(), "ghost-task")
	assert.Equal(t, "ghost-task", result.TaskID)
	assert.Equal(t, models.TaskStatusFailed, result.Status)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "not found")
}
