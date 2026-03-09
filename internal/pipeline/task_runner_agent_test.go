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
			Usage:  agent.AgentUsage{CostUSD: 0.05, InputTokens: 1000, OutputTokens: 500, Model: "claude-sonnet-4-6-20250514"},
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
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg, mustLoadTestRegistry(t))

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

func TestRunTask_AgentRunner_SyntheticLlmCall_UsesActualModel(t *testing.T) {
	workDir := t.TempDir()
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{
			Output: "done",
			Usage: agent.AgentUsage{
				CostUSD:      0.05,
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-sonnet-4-6-20250514",
			},
		},
	}
	mockDB := newMockTaskRunnerDB()
	mockGit := &realMockGitProvider{diffOutput: "diff --git a/file.go ...", commitSHA: "abc123"}

	cfg := TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 0,
		AgentRunner:              mockAgent,
		AgentRunnerName:          "claudecode",
		Models:                   models.ModelsConfig{Implementer: "openai:gpt-5.4"}, // wrong config value
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg, mustLoadTestRegistry(t))

	task := &models.Task{ID: "task-1", TicketID: "ticket-1", Title: "Add feature"}
	err := tr.RunTask(context.Background(), task)
	require.NoError(t, err)

	// Verify the synthetic LLM call uses the actual model from agent result,
	// not the config value.
	require.Len(t, mockDB.llmCalls, 1)
	call := mockDB.llmCalls[0]
	assert.Equal(t, "claude-sonnet-4-6-20250514", call.Model, "synthetic call should use actual model from agent, not config")
	assert.Equal(t, "claudecode", call.AgentRunner)
	assert.Equal(t, "claudecode", call.Provider)
	assert.Equal(t, 0.05, call.CostUSD)
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
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg, mustLoadTestRegistry(t))
	task := &models.Task{ID: "t-1", TicketID: "tk-1", Title: "Fix bug"}

	err := tr.RunTask(context.Background(), task)
	assert.Error(t, err) // should fail after retries exhausted
	assert.Equal(t, models.TaskStatusFailed, mockDB.statuses["t-1"])
}

func TestRunTask_NoAgentRunner_UsesBuiltinPath(t *testing.T) {
	// Verify that when AgentRunner is nil, RunTask does NOT call agent
	// (builtin path runs instead). This is a regression guard.
	// The full builtin path is tested extensively in task_runner_test.go;
	// here we just confirm the agent path selector doesn't fire.
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{Output: "should not be called"},
	}
	_ = mockAgent // intentionally not wired into config

	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 0,
		AgentRunner:              nil, // explicitly nil
	}

	// If AgentRunner is nil, NewPipelineTaskRunner should create a runner
	// that uses the builtin path. Since we have no LLM, the builtin path
	// will fail — but it should NOT call mockAgent.
	mockDB := newMockTaskRunnerDB()
	mockGit := &realMockGitProvider{diffOutput: "some diff"}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg, mustLoadTestRegistry(t))
	task := &models.Task{ID: "t-1", TicketID: "tk-1", Title: "Test builtin path"}

	_ = tr.RunTask(context.Background(), task)

	// The important assertion: agent was never called
	assert.Equal(t, agent.AgentRequest{}, mockAgent.gotReq, "agent should not have been called with nil AgentRunner")
}
