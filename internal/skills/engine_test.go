// internal/skills/engine_test.go
package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

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
	// First step failed but allowed, second step ran
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
