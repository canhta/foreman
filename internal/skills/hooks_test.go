// internal/skills/hooks_test.go
package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookRunner_RunHook_PostLint(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "security-scan", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "scan", Type: "run_command", Command: "echo", Args: []string{"scanned"}},
		}},
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{
			{ID: "gen", Type: "run_command", Command: "echo", Args: []string{"changelog"}},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "security-scan", results[0].SkillID)
	assert.NoError(t, results[0].Error)
}

func TestHookRunner_RunHook_PrePR(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{
			{ID: "gen", Type: "run_command", Command: "echo", Args: []string{"changelog"}},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "pre_pr", NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "changelog", results[0].SkillID)
	assert.NoError(t, results[0].Error)
}

func TestHookRunner_RunHook_NoMatchingSkills(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")

	skills := []*Skill{
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	assert.Empty(t, results)
}

func TestHookRunner_RunHook_SkillFailure(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

	skills := []*Skill{
		{ID: "broken", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "fail", Type: "run_command", Command: "false"},
		}},
		{ID: "working", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "ok", Type: "run_command", Command: "echo"},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	// Both skills attempted — failure doesn't block
	assert.Len(t, results, 2)
	assert.Error(t, results[0].Error)
	// Second skill also fails because mock always returns exitCode=1
	// The important thing is both ran
}

func TestHookRunner_FilterByNames(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "skill-a", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo"},
		}},
		{ID: "skill-b", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo"},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHookByNames(context.Background(), "post_lint", []string{"skill-a"}, NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "skill-a", results[0].SkillID)
}
