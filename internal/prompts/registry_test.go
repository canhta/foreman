// internal/prompts/registry_test.go
package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// roles/planner/ROLE.md
	mkdirp(t, filepath.Join(dir, "roles", "planner"))
	writeFile(t, filepath.Join(dir, "roles", "planner", "ROLE.md"), `---
name: planner
description: "Decomposes tickets into tasks"
model_hint: planner
max_tokens: 8192
---

Decompose this ticket into tasks.

## Ticket
**{{ ticket_title }}**
`)

	// agents/tdd-writer/AGENT.md
	mkdirp(t, filepath.Join(dir, "agents", "tdd-writer"))
	writeFile(t, filepath.Join(dir, "agents", "tdd-writer", "AGENT.md"), `---
name: tdd-writer
description: "RED phase agent"
mode: subagent
tools:
  - Read
  - Write
---

Write failing tests.
`)

	// skills/bug-fix/SKILL.md
	mkdirp(t, filepath.Join(dir, "skills", "bug-fix"))
	writeFile(t, filepath.Join(dir, "skills", "bug-fix", "SKILL.md"), `---
name: bug-fix
description: "Bug fixing workflow"
trigger: post_lint
steps:
  - id: regression-check
    type: llm_call
    prompt: "Check for regressions"
---

Bug fix context.
`)

	// fragments/tdd-rules.md (no frontmatter)
	mkdirp(t, filepath.Join(dir, "fragments"))
	writeFile(t, filepath.Join(dir, "fragments", "tdd-rules.md"),
		"## TDD Rules\n1. Write tests FIRST\n2. Tests must fail\n3. Minimal implementation\n")

	return dir
}

func mkdirp(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestRegistryLoad(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	// Roles
	entry, err := reg.Get(KindRole, "planner")
	require.NoError(t, err)
	assert.Equal(t, "planner", entry.Name)
	assert.Equal(t, KindRole, entry.Kind)
	assert.Contains(t, entry.RawContent, "{{ ticket_title }}")

	// Agents
	entry, err = reg.Get(KindAgent, "tdd-writer")
	require.NoError(t, err)
	assert.Equal(t, "tdd-writer", entry.Name)
	assert.Equal(t, KindAgent, entry.Kind)

	// Skills
	entry, err = reg.Get(KindSkill, "bug-fix")
	require.NoError(t, err)
	assert.Equal(t, "bug-fix", entry.Name)

	// Fragments
	entry, err = reg.Get(KindFragment, "tdd-rules")
	require.NoError(t, err)
	assert.Contains(t, entry.RawContent, "Write tests FIRST")

	// Not found
	_, err = reg.Get(KindRole, "nonexistent")
	assert.Error(t, err)
}

func TestRegistryList(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	roles := reg.List(KindRole)
	assert.Len(t, roles, 1)
	assert.Equal(t, "planner", roles[0].Name)

	agents := reg.List(KindAgent)
	assert.Len(t, agents, 1)

	skills := reg.List(KindSkill)
	assert.Len(t, skills, 1)
}
