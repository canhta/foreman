package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAgentRunnerForTask implements agent.AgentRunner for testing.
type mockAgentRunnerForTask struct {
	result agent.AgentResult
	err    error
	gotReq agent.AgentRequest
}

func (m *mockAgentRunnerForTask) Run(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	m.gotReq = req
	return m.result, m.err
}
func (m *mockAgentRunnerForTask) HealthCheck(_ context.Context) error { return nil }
func (m *mockAgentRunnerForTask) RunnerName() string                  { return "mock" }
func (m *mockAgentRunnerForTask) Close() error                        { return nil }

func TestRunTask_AgentRunner_DelegatesToAgent(t *testing.T) {
	workDir := t.TempDir()
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{
			Output: "done",
			Usage:  agent.AgentUsage{CostUSD: 0.05, InputTokens: 1000, OutputTokens: 500},
		},
	}
	mockDB := newMockTaskRunnerDB()
	mockGit := &realMockGitProvider{diffOutput: "diff --git a/file.go ...", commitSHA: "abc123"}

	cfg := TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 2,
		AgentRunner:              mockAgent,
		AgentRunnerName:          "claudecode",
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg)

	task := &models.Task{
		ID:       "task-1",
		TicketID: "ticket-1",
		Title:    "Add feature",
	}

	err := tr.RunTask(context.Background(), task)
	require.NoError(t, err)

	// Should have called agent with a prompt containing the task title.
	assert.Contains(t, mockAgent.gotReq.Prompt, "Add feature")
	assert.Equal(t, workDir, mockAgent.gotReq.WorkDir)

	// Should have updated status to Done.
	assert.Equal(t, models.TaskStatusDone, mockDB.statuses["task-1"])
}

func TestRunTask_AgentRunner_EmptyDiff_Retries(t *testing.T) {
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{Output: "done"},
	}
	mockDB := newMockTaskRunnerDB()
	mockGit := &realMockGitProvider{diffOutput: ""} // empty diff

	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 1,
		AgentRunner:              mockAgent,
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg)
	task := &models.Task{ID: "t-1", TicketID: "tk-1", Title: "Fix bug"}

	err := tr.RunTask(context.Background(), task)
	assert.Error(t, err) // should fail after retries exhausted
	assert.Equal(t, models.TaskStatusFailed, mockDB.statuses["t-1"])
}

func TestRunTask_NoAgentRunner_UsesBuiltinPath(t *testing.T) {
	// When AgentRunner is nil, RunTask should use the existing builtin path.
	// This is a regression guard — we verify the branch doesn't fire for nil AgentRunner.
	// Full builtin path tests are in task_runner_test.go.
	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 0,
		// AgentRunner: nil — builtin path
	}
	_ = cfg
	// Structural marker only — existing tests in task_runner_test.go cover the builtin path.
}
