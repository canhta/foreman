// internal/skills/loader_test.go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSkill_Valid(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: write-changelog
description: "Generate changelog entry"
trigger: pre_pr
steps:
  - id: generate
    type: llm_call
    prompt_template: "prompts/changelog.md.j2"
    model: "anthropic:claude-haiku"
  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ .Steps.generate.output }}"
    mode: prepend
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "write-changelog.yml"), []byte(skillYAML), 0o644))

	skill, err := LoadSkill(filepath.Join(dir, "write-changelog.yml"))
	require.NoError(t, err)
	assert.Equal(t, "write-changelog", skill.ID)
	assert.Equal(t, "pre_pr", skill.Trigger)
	assert.Len(t, skill.Steps, 2)
	assert.Equal(t, "llm_call", skill.Steps[0].Type)
	assert.Equal(t, "file_write", skill.Steps[1].Type)
}

func TestLoadSkill_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte("{{invalid yaml"), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
}

func TestLoadSkill_InvalidStepType(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: bad-skill
description: "Invalid"
trigger: post_lint
steps:
  - id: bad
    type: execute_arbitrary_code
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(skillYAML), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step type")
}

func TestLoadSkill_InvalidTrigger(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: bad-trigger
description: "Invalid trigger"
trigger: on_deploy
steps:
  - id: ok
    type: run_command
    command: "echo"
    args: ["hello"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(skillYAML), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid trigger")
}

func TestLoadSkillsDir(t *testing.T) {
	dir := t.TempDir()
	skill1 := `id: skill-a
description: "Skill A"
trigger: post_lint
steps:
  - id: run
    type: run_command
    command: "echo"
    args: ["a"]
`
	skill2 := `id: skill-b
description: "Skill B"
trigger: pre_pr
steps:
  - id: run
    type: run_command
    command: "echo"
    args: ["b"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skill-a.yml"), []byte(skill1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skill-b.yml"), []byte(skill2), 0o644))
	// Non-YAML file should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0o644))

	skills, err := LoadSkillsDir(dir)
	require.NoError(t, err)
	assert.Len(t, skills, 2)
	assert.Contains(t, skillIDs(skills), "skill-a")
	assert.Contains(t, skillIDs(skills), "skill-b")
}

func TestLoadSkillsDir_Empty(t *testing.T) {
	dir := t.TempDir()
	skills, err := LoadSkillsDir(dir)
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestLoadSkillsDir_NonExistent(t *testing.T) {
	skills, err := LoadSkillsDir("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func skillIDs(skills []*Skill) []string {
	ids := make([]string, len(skills))
	for i, s := range skills {
		ids[i] = s.ID
	}
	return ids
}
