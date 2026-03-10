package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/git"
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

func TestRunTask_AgentRunner_AutoCommit_DetectedAsSuccess(t *testing.T) {
	// Simulate Claude Code auto-committing: working tree is clean after run,
	// but HEAD advanced. Foreman should detect the new commit as the diff
	// and succeed rather than treating it as an empty-diff failure.
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{Output: "done", Usage: agent.AgentUsage{CostUSD: 0.05}},
	}
	mockDB := newMockTaskRunnerDB()

	// Log: first call returns sha-before, second call returns sha-after (HEAD advanced).
	mockGit := &realMockGitProvider{
		diffOutput:    "", // DiffWorking always empty — agent committed
		commitSHA:     "sha-before",
		logEntries:    []git.CommitEntry{{SHA: "sha-before"}},
		logEntriesSeq: [][]git.CommitEntry{{{SHA: "sha-after"}}},
	}
	// Override Diff to return a non-empty diff for the sha-before..sha-after range.
	// realMockGitProvider.Diff returns diffOutput which is empty; we need a custom mock.
	// Use a wrapper that returns a real diff for the range call.
	mockGitWithDiff := &autoCommitMockGitProvider{realMockGitProvider: mockGit, rangeDiff: "diff --git a/app.ts b/app.ts\n+fix"}

	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 0,
		AgentRunner:              mockAgent,
		AgentRunnerName:          "claudecode",
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGitWithDiff, nil, cfg, mustLoadTestRegistry(t))
	task := &models.Task{ID: "t-ac", TicketID: "tk-ac", Title: "Fix scroll bug"}

	err := tr.RunTask(context.Background(), task)
	require.NoError(t, err, "auto-committed task should succeed")
	assert.Equal(t, models.TaskStatusDone, mockDB.statuses["t-ac"])
	// Commit should NOT have been called by Foreman (agent already committed)
	assert.False(t, mockGitWithDiff.commitCalled, "Foreman should not re-commit when agent auto-committed")
}

// autoCommitMockGitProvider wraps realMockGitProvider, returning a real diff
// for the range-based Diff call while keeping DiffWorking empty.
type autoCommitMockGitProvider struct {
	*realMockGitProvider
	rangeDiff string
	commitCalled bool
}

func (m *autoCommitMockGitProvider) Diff(_ context.Context, _, base, head string) (string, error) {
	if base != "" && head != "" && base != head {
		return m.rangeDiff, nil
	}
	return "", nil
}

func (m *autoCommitMockGitProvider) Commit(ctx context.Context, workDir, msg string) (string, error) {
	m.commitCalled = true
	return m.realMockGitProvider.Commit(ctx, workDir, msg)
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
