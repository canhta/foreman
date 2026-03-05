// internal/skills/engine_test.go
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLLMProvider struct {
	response string
}

func (m *mockLLMProvider) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response, TokensInput: 10, TokensOutput: 5}, nil
}
func (m *mockLLMProvider) ProviderName() string                { return "mock" }
func (m *mockLLMProvider) HealthCheck(_ context.Context) error { return nil }

type mockRunner struct {
	stdout   string
	exitCode int
}

func (m *mockRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stdout: m.stdout, ExitCode: m.exitCode}, nil
}
func (m *mockRunner) CommandExists(_ context.Context, _ string) bool { return true }

type mockSmartRunner struct {
	fn func() (*runner.CommandOutput, error)
}

func (m *mockSmartRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return m.fn()
}
func (m *mockSmartRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestEngine_ExecuteRunCommand(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "hello", exitCode: 0}, workDir, "main")

	skill := &Skill{
		ID:      "test-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo", Args: []string{"hello"}},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "hello", sCtx.Steps["run"].Output)
}

func TestEngine_ExecuteFileWrite(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")

	skill := &Skill{
		ID:      "write-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "write", Type: "file_write", Path: "output.txt", Content: "hello world", Mode: "overwrite"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "output.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestEngine_ExecuteFileWrite_Prepend(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "log.txt"), []byte("existing content"), 0o644))

	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")
	skill := &Skill{
		ID:      "prepend-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "prepend", Type: "file_write", Path: "log.txt", Content: "new header", Mode: "prepend"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "log.txt"))
	require.NoError(t, err)
	assert.True(t, len(string(content)) > 0)
	assert.Contains(t, string(content), "new header")
	assert.Contains(t, string(content), "existing content")
}

func TestEngine_ExecuteLLMCall(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(
		&mockLLMProvider{response: "Generated changelog entry"},
		&mockRunner{},
		workDir,
		"main",
	)

	skill := &Skill{
		ID:      "llm-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "generate", Type: "llm_call", Model: "test-model"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "Generated changelog entry", sCtx.Steps["generate"].Output)
}

func TestEngine_AllowFailure(t *testing.T) {
	workDir := t.TempDir()

	// First call fails, subsequent calls succeed
	callCount := 0
	smartRunner := &mockSmartRunner{
		fn: func() (*runner.CommandOutput, error) {
			callCount++
			if callCount == 1 {
				return &runner.CommandOutput{Stdout: "error", ExitCode: 1}, nil
			}
			return &runner.CommandOutput{Stdout: "ok", ExitCode: 0}, nil
		},
	}
	engine := NewEngine(&mockLLMProvider{}, smartRunner, workDir, "main")

	skill := &Skill{
		ID:      "failing-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "may-fail", Type: "run_command", Command: "false", AllowFailure: true},
			{ID: "after", Type: "run_command", Command: "echo", Args: []string{"ok"}},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.NotEmpty(t, sCtx.Steps["may-fail"].Error)
	assert.NotNil(t, sCtx.Steps["after"])
}

func TestEngine_StepFailureStopsExecution(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

	skill := &Skill{
		ID:      "strict-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "must-pass", Type: "run_command", Command: "false", AllowFailure: false},
			{ID: "never-runs", Type: "run_command", Command: "echo"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must-pass")
	// Second step should not have run
	assert.Nil(t, sCtx.Steps["never-runs"])
}

// --- agentsdk step type tests ---

type mockAgentRunner struct {
	output  string
	err     error
	lastReq agent.AgentRequest
}

func (m *mockAgentRunner) Run(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	m.lastReq = req
	if m.err != nil {
		return agent.AgentResult{}, m.err
	}
	return agent.AgentResult{
		Output: m.output,
		Usage: agent.AgentUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			NumTurns:     3,
		},
	}, nil
}
func (m *mockAgentRunner) HealthCheck(_ context.Context) error { return nil }
func (m *mockAgentRunner) RunnerName() string                  { return "mock" }
func (m *mockAgentRunner) Close() error                        { return nil }

// mockGitForEngine satisfies git.GitProvider for engine tests.
type mockGitForEngine struct {
	diff string
	err  error
}

func (m *mockGitForEngine) EnsureRepo(_ context.Context, _ string) error  { return nil }
func (m *mockGitForEngine) CreateBranch(_ context.Context, _, _ string) error { return nil }
func (m *mockGitForEngine) Commit(_ context.Context, _, _ string) (string, error) { return "", nil }
func (m *mockGitForEngine) Diff(_ context.Context, _, _, _ string) (string, error) { return "", nil }
func (m *mockGitForEngine) DiffWorking(_ context.Context, _ string) (string, error) {
	return m.diff, m.err
}
func (m *mockGitForEngine) Push(_ context.Context, _, _ string) error { return nil }
func (m *mockGitForEngine) RebaseOnto(_ context.Context, _, _ string) (*git.RebaseResult, error) {
	return nil, nil
}
func (m *mockGitForEngine) CreatePR(_ context.Context, _ git.PrRequest) (*git.PrResponse, error) {
	return nil, nil
}
func (m *mockGitForEngine) StageAll(_ context.Context, _ string) error               { return nil }
func (m *mockGitForEngine) FileTree(_ context.Context, _ string) ([]git.FileEntry, error) {
	return nil, nil
}
func (m *mockGitForEngine) Log(_ context.Context, _ string, _ int) ([]git.CommitEntry, error) {
	return nil, nil
}
func (m *mockGitForEngine) CheckFileOverlap(_ context.Context, _, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func TestEngine_ExecuteAgentSDK(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")
	engine.SetAgentRunner(&mockAgentRunner{output: `{"severity":"low","findings":[]}`})

	skill := &Skill{
		ID:      "security-scan",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{
				ID:           "audit",
				Type:         "agentsdk",
				Content:      "Review this diff for security issues",
				AllowedTools: []string{"Read"},
				MaxTurns:     6,
				OutputKey:    "result",
			},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)

	result, ok := sCtx.Steps["audit"]
	require.True(t, ok, "expected step result for 'audit'")
	assert.Equal(t, `{"severity":"low","findings":[]}`, result.Output)
}

func TestEngine_ExecuteAgentSDK_NoRunner(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")
	// No agent runner set

	skill := &Skill{
		ID:      "test",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "run", Type: "agentsdk", Content: "do something"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no agent runner configured")
}

func TestEngine_ExecuteAgentSDK_AllowFailure(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")
	engine.SetAgentRunner(&mockAgentRunner{err: fmt.Errorf("agent failed")})

	skill := &Skill{
		ID:      "test",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "agent", Type: "agentsdk", Content: "do something", AllowFailure: true},
			{ID: "after", Type: "llm_call", Model: "test"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.NotEmpty(t, sCtx.Steps["agent"].Error)
	assert.NotNil(t, sCtx.Steps["after"])
}

// --- Phase 9 field wiring tests ---

func TestEngine_ExecuteAgentSDK_WiresPhase9Fields(t *testing.T) {
	mock := &mockAgentRunner{}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)

	schema := map[string]interface{}{"type": "object"}
	step := SkillStep{
		ID:            "s1",
		Type:          "agentsdk",
		Content:       "do thing",
		FallbackModel: "openrouter:claude-sonnet",
		OutputSchema:  schema,
	}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	require.NoError(t, err)
	assert.Equal(t, "openrouter:claude-sonnet", mock.lastReq.FallbackModel)
	assert.NotNil(t, mock.lastReq.OutputSchema)
}

// --- git_diff tests ---

func TestEngine_GitDiff_Implemented(t *testing.T) {
	mockGit := &mockGitForEngine{diff: "diff content"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetGitProvider(mockGit)

	result, err := e.executeGitDiff(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "diff content", result.Output)
}

func TestEngine_GitDiff_NoProvider(t *testing.T) {
	e := NewEngine(nil, nil, t.TempDir(), "main")
	_, err := e.executeGitDiff(context.Background())
	assert.Error(t, err)
}

// --- subskill tests ---

func TestEngine_SubSkill(t *testing.T) {
	mock := &mockAgentRunner{output: "sub result"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)

	sub := &Skill{
		ID:      "child",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "s1", Type: "agentsdk", Content: "child task"}},
	}
	parent := &Skill{
		ID:      "parent",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "call-child", Type: "subskill", SkillRef: "child"}},
	}
	e.RegisterSkills([]*Skill{sub, parent})

	sCtx := NewSkillContext()
	err := e.Execute(context.Background(), parent, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "sub result", sCtx.Steps["call-child"].Output)
}

func TestEngine_SubSkill_MissingRef(t *testing.T) {
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.RegisterSkills(nil)
	step := SkillStep{ID: "s1", Type: "subskill", SkillRef: "does-not-exist"}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	assert.Error(t, err)
}

// --- output_format tests ---

func TestEngine_OutputFormat_JSON_Valid(t *testing.T) {
	mock := &mockAgentRunner{output: `{"key":"value"}`}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "json"}
	result, err := e.executeStep(context.Background(), step, NewSkillContext())
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, result.Output)
}

func TestEngine_OutputFormat_JSON_Invalid(t *testing.T) {
	mock := &mockAgentRunner{output: "not json"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "json"}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	assert.Error(t, err)
}

func TestEngine_OutputFormat_Checklist(t *testing.T) {
	mock := &mockAgentRunner{output: "- [x] item 1\n- [ ] item 2\n- [x] item 3"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "checklist"}
	result, err := e.executeStep(context.Background(), step, NewSkillContext())
	require.NoError(t, err)
	assert.Equal(t, 1, result.ExitCode, "expected 1 failed checklist item")
}
