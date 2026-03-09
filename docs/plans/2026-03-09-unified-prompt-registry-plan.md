# Unified Prompt Registry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace Foreman's 4 disconnected prompt systems with a unified Markdown+frontmatter registry that serves both builtin and Claude Code runners from a single source of truth.

**Architecture:** All prompts/agents/skills/commands are Markdown files with YAML frontmatter in `prompts/`. A Go registry loads, parses, resolves includes, and renders them. Builtin runner calls `Render()` for prompt strings. Claude Code runner calls `ForClaude()` to write `.claude/` directory structure into worktrees.

**Tech Stack:** Go, pongo2 (Jinja2-compatible templates), YAML frontmatter parsing, `embed.FS` for production bundling

---

### Task 1: Create frontmatter parser

**Files:**
- Create: `internal/prompts/frontmatter.go`
- Test: `internal/prompts/frontmatter_test.go`

**Step 1: Write the failing test**

```go
// internal/prompts/frontmatter_test.go
package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_Basic(t *testing.T) {
	input := `---
name: implementer
description: "Expert engineer"
max_tokens: 8192
temperature: 0.0
includes:
  - fragments/tdd-rules.md
---

You are an expert engineer.

## Task
**{{ task_title }}**`

	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Equal(t, "implementer", fm["name"])
	assert.Equal(t, "Expert engineer", fm["description"])
	assert.Equal(t, 8192, fm["max_tokens"])
	assert.Contains(t, body, "You are an expert engineer.")
	assert.Contains(t, body, "{{ task_title }}")
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	input := "Just plain markdown content"
	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Empty(t, fm)
	assert.Equal(t, "Just plain markdown content", body)
}

func TestParseFrontmatter_EmptyFrontmatter(t *testing.T) {
	input := "---\n---\nBody here"
	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Empty(t, fm)
	assert.Equal(t, "Body here", body)
}

func TestParseFrontmatter_InvalidYAML(t *testing.T) {
	input := "---\n: invalid: yaml: [[\n---\nBody"
	_, _, err := ParseFrontmatter(input)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestParseFrontmatter -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/prompts/frontmatter.go
package prompts

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter splits a Markdown document into YAML frontmatter and body.
// If no frontmatter delimiter is found, returns empty map and full content as body.
func ParseFrontmatter(content string) (map[string]any, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return map[string]any{}, content, nil
	}

	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return map[string]any{}, content, nil
	}

	yamlStr := content[4 : 4+end]
	body := strings.TrimLeft(content[4+end+4:], "\n")

	if strings.TrimSpace(yamlStr) == "" {
		return map[string]any{}, body, nil
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	return fm, body, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestParseFrontmatter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prompts/frontmatter.go internal/prompts/frontmatter_test.go
git commit -m "feat(prompts): add frontmatter parser for unified prompt registry"
```

---

### Task 2: Create registry loader

**Files:**
- Create: `internal/prompts/registry.go`
- Test: `internal/prompts/registry_test.go`
- Create: test fixtures in `internal/prompts/testdata/`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistry -v`
Expected: FAIL — `Load`, `KindRole`, etc. undefined

**Step 3: Write minimal implementation**

```go
// internal/prompts/registry.go
package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EntryKind distinguishes the type of prompt entry.
type EntryKind string

const (
	KindRole     EntryKind = "role"
	KindAgent    EntryKind = "agent"
	KindSkill    EntryKind = "skill"
	KindCommand  EntryKind = "command"
	KindFragment EntryKind = "fragment"
)

// Entry is a single loaded prompt/agent/skill/command/fragment.
type Entry struct {
	Name        string
	Kind        EntryKind
	Description string
	Metadata    map[string]any
	RawContent  string
	Path        string
	Includes    []string
}

// Registry holds all loaded prompt entries indexed by kind and name.
type Registry struct {
	entries map[EntryKind]map[string]*Entry
	baseDir string
}

// dirForKind maps each kind to its subdirectory name and file convention.
var dirForKind = map[EntryKind]struct {
	dir      string
	filename string
}{
	KindRole:    {dir: "roles", filename: "ROLE.md"},
	KindAgent:   {dir: "agents", filename: "AGENT.md"},
	KindSkill:   {dir: "skills", filename: "SKILL.md"},
	KindCommand: {dir: "commands", filename: "COMMAND.md"},
}

// Load scans baseDir for all prompt entries and returns a populated Registry.
func Load(baseDir string) (*Registry, error) {
	r := &Registry{
		entries: make(map[EntryKind]map[string]*Entry),
		baseDir: baseDir,
	}
	for kind := range dirForKind {
		r.entries[kind] = make(map[string]*Entry)
	}
	r.entries[KindFragment] = make(map[string]*Entry)

	// Load structured entries (roles, agents, skills, commands)
	for kind, info := range dirForKind {
		dir := filepath.Join(baseDir, info.dir)
		if err := r.scanDir(dir, kind, info.filename); err != nil {
			// Directory might not exist — not an error
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("scan %s: %w", info.dir, err)
			}
		}
	}

	// Load fragments
	fragDir := filepath.Join(baseDir, "fragments")
	if err := r.scanFragments(fragDir); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scan fragments: %w", err)
	}

	return r, nil
}

func (r *Registry) scanDir(dir string, kind EntryKind, filename string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), filename)
		if _, statErr := os.Stat(path); statErr != nil {
			// Recurse into subdirectories (e.g., skills/community/)
			subDir := filepath.Join(dir, e.Name())
			_ = r.scanDir(subDir, kind, filename)
			continue
		}
		if err := r.loadEntry(path, kind); err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
	}
	return nil
}

func (r *Registry) scanFragments(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		r.entries[KindFragment][name] = &Entry{
			Name:       name,
			Kind:       KindFragment,
			RawContent: string(data),
			Path:       path,
		}
	}
	return nil
}

func (r *Registry) loadEntry(path string, kind EntryKind) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fm, body, err := ParseFrontmatter(string(data))
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	name, _ := fm["name"].(string)
	if name == "" {
		return fmt.Errorf("%s: missing 'name' in frontmatter", path)
	}

	desc, _ := fm["description"].(string)

	var includes []string
	if inc, ok := fm["includes"]; ok {
		if list, ok := inc.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					includes = append(includes, s)
				}
			}
		}
	}

	r.entries[kind][name] = &Entry{
		Name:        name,
		Kind:        kind,
		Description: desc,
		Metadata:    fm,
		RawContent:  body,
		Path:        path,
		Includes:    includes,
	}
	return nil
}

// Get retrieves a single entry by kind and name.
func (r *Registry) Get(kind EntryKind, name string) (*Entry, error) {
	m, ok := r.entries[kind]
	if !ok {
		return nil, fmt.Errorf("unknown entry kind: %s", kind)
	}
	entry, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("%s %q not found", kind, name)
	}
	return entry, nil
}

// List returns all entries of a given kind, sorted by name.
func (r *Registry) List(kind EntryKind) []*Entry {
	m := r.entries[kind]
	result := make([]*Entry, 0, len(m))
	for _, e := range m {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prompts/registry.go internal/prompts/registry_test.go
git commit -m "feat(prompts): add registry loader for unified prompt entries"
```

---

### Task 3: Add template rendering with include resolution

**Files:**
- Modify: `internal/prompts/registry.go`
- Test: `internal/prompts/registry_test.go`

**Step 1: Write the failing test**

```go
func TestRegistryRender(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	result, err := reg.Render(KindRole, "planner", map[string]any{
		"ticket_title": "Add user auth",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "**Add user auth**")
	assert.Contains(t, result, "Decompose this ticket")
}

func TestRegistryRenderWithIncludes(t *testing.T) {
	dir := setupTestFixtures(t)

	// Create a role that includes a fragment
	mkdirp(t, filepath.Join(dir, "roles", "coder"))
	writeFile(t, filepath.Join(dir, "roles", "coder", "ROLE.md"), `---
name: coder
description: "Coder with TDD"
includes:
  - fragments/tdd-rules.md
---

You are a coder.

{% include "fragments/tdd-rules.md" %}

## Task
**{{ task_title }}**
`)

	reg, err := Load(dir)
	require.NoError(t, err)

	result, err := reg.Render(KindRole, "coder", map[string]any{
		"task_title": "Fix the bug",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "Write tests FIRST")
	assert.Contains(t, result, "**Fix the bug**")
}

func TestRegistryRenderNotFound(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	_, err = reg.Render(KindRole, "missing", nil)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistryRender -v`
Expected: FAIL — `Render` method undefined

**Step 3: Write minimal implementation**

Add to `internal/prompts/registry.go`:

```go
import (
	"github.com/flosch/pongo2/v6"
)

// Render resolves includes and renders template variables for an entry.
func (r *Registry) Render(kind EntryKind, name string, vars map[string]any) (string, error) {
	entry, err := r.Get(kind, name)
	if err != nil {
		return "", err
	}
	return r.RenderEntry(entry, vars)
}

// RenderEntry renders a single entry with the given variables.
func (r *Registry) RenderEntry(entry *Entry, vars map[string]any) (string, error) {
	// Build a pongo2 template set rooted at baseDir so {% include %} works
	loader, err := pongo2.NewLocalFileSystemLoader(r.baseDir)
	if err != nil {
		return "", fmt.Errorf("create template loader: %w", err)
	}
	tplSet := pongo2.NewSet("prompts", loader)

	tpl, err := tplSet.FromString(entry.RawContent)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", entry.Name, err)
	}

	ctx := pongo2.Context{}
	for k, v := range vars {
		ctx[k] = v
	}

	result, err := tpl.Execute(ctx)
	if err != nil {
		return "", fmt.Errorf("render %s: %w", entry.Name, err)
	}
	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistryRender -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prompts/registry.go internal/prompts/registry_test.go
git commit -m "feat(prompts): add template rendering with pongo2 include resolution"
```

---

### Task 4: Add ForClaude() — write .claude/ directory structure

**Files:**
- Modify: `internal/prompts/registry.go`
- Test: `internal/prompts/registry_test.go`

**Step 1: Write the failing test**

```go
func TestRegistryForClaude(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	workDir := t.TempDir()
	err = reg.ForClaude(workDir, map[string]any{
		"test_command": "go test ./...",
	})
	require.NoError(t, err)

	// Check .claude/agents/ exists with rendered agent files
	agentFile := filepath.Join(workDir, ".claude", "agents", "tdd-writer.md")
	data, err := os.ReadFile(agentFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Write failing tests")

	// Check .claude/settings.json exists
	settingsFile := filepath.Join(workDir, ".claude", "settings.json")
	_, err = os.Stat(settingsFile)
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistryForClaude -v`
Expected: FAIL — `ForClaude` undefined

**Step 3: Write minimal implementation**

Add to `internal/prompts/registry.go`:

```go
import (
	"encoding/json"
)

// ForClaude writes .claude/ directory structure into workDir for Claude Code runner.
// Renders all agents as .claude/agents/*.md, commands as .claude/commands/*.md,
// and generates settings.json with permissions.
func (r *Registry) ForClaude(workDir string, vars map[string]any) error {
	claudeDir := filepath.Join(workDir, ".claude")

	// Write agents
	agentsDir := filepath.Join(claudeDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir agents: %w", err)
	}
	for _, agent := range r.List(KindAgent) {
		rendered, err := r.RenderEntry(agent, vars)
		if err != nil {
			return fmt.Errorf("render agent %s: %w", agent.Name, err)
		}
		path := filepath.Join(agentsDir, agent.Name+".md")
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write agent %s: %w", agent.Name, err)
		}
	}

	// Write commands
	cmdsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir commands: %w", err)
	}
	for _, cmd := range r.List(KindCommand) {
		rendered, err := r.RenderEntry(cmd, vars)
		if err != nil {
			return fmt.Errorf("render command %s: %w", cmd.Name, err)
		}
		path := filepath.Join(cmdsDir, cmd.Name+".md")
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write command %s: %w", cmd.Name, err)
		}
	}

	// Write settings.json
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash"},
		},
	}
	settingsData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, settingsData, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistryForClaude -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prompts/registry.go internal/prompts/registry_test.go
git commit -m "feat(prompts): add ForClaude() to generate .claude/ directory for Claude Code runner"
```

---

### Task 5: Add SkillSteps() — parse skill steps from frontmatter

**Files:**
- Modify: `internal/prompts/registry.go`
- Test: `internal/prompts/registry_test.go`

**Step 1: Write the failing test**

```go
func TestRegistrySkillSteps(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	steps, err := reg.SkillSteps("bug-fix")
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "regression-check", steps[0].ID)
	assert.Equal(t, "llm_call", steps[0].Type)
	assert.Contains(t, steps[0].Prompt, "Check for regressions")
}

func TestRegistrySkillSteps_NotFound(t *testing.T) {
	dir := setupTestFixtures(t)
	reg, err := Load(dir)
	require.NoError(t, err)

	_, err = reg.SkillSteps("nonexistent")
	assert.Error(t, err)
}

func TestRegistrySkillSteps_NoSteps(t *testing.T) {
	dir := setupTestFixtures(t)

	// Create skill with no steps
	mkdirp(t, filepath.Join(dir, "skills", "empty"))
	writeFile(t, filepath.Join(dir, "skills", "empty", "SKILL.md"), `---
name: empty
description: "Empty skill"
trigger: post_lint
---

No steps here.
`)

	reg, err := Load(dir)
	require.NoError(t, err)

	steps, err := reg.SkillSteps("empty")
	require.NoError(t, err)
	assert.Empty(t, steps)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistrySkillSteps -v`
Expected: FAIL — `SkillSteps` undefined

**Step 3: Write minimal implementation**

Add to `internal/prompts/registry.go`:

```go
// SkillStep represents a single step in a skill workflow.
type SkillStep struct {
	ID            string   `yaml:"id"`
	Type          string   `yaml:"type"`
	Prompt        string   `yaml:"prompt,omitempty"`
	Model         string   `yaml:"model,omitempty"`
	Command       string   `yaml:"command,omitempty"`
	AllowedTools  []string `yaml:"allowed_tools,omitempty"`
	MaxTurns      int      `yaml:"max_turns,omitempty"`
	TimeoutSecs   int      `yaml:"timeout_secs,omitempty"`
	MaxTokens     int      `yaml:"max_tokens,omitempty"`
	OutputFormat  string   `yaml:"output_format,omitempty"`
	FallbackModel string   `yaml:"fallback_model,omitempty"`
	SkillRef      string   `yaml:"skill_ref,omitempty"`
	AllowFailure  bool     `yaml:"allow_failure,omitempty"`
}

// SkillSteps extracts the step definitions from a skill entry's frontmatter.
func (r *Registry) SkillSteps(name string) ([]SkillStep, error) {
	entry, err := r.Get(KindSkill, name)
	if err != nil {
		return nil, err
	}

	rawSteps, ok := entry.Metadata["steps"]
	if !ok || rawSteps == nil {
		return nil, nil
	}

	// Re-marshal and unmarshal to get typed steps
	stepsYAML, err := yaml.Marshal(rawSteps)
	if err != nil {
		return nil, fmt.Errorf("marshal steps for %s: %w", name, err)
	}

	var steps []SkillStep
	if err := yaml.Unmarshal(stepsYAML, &steps); err != nil {
		return nil, fmt.Errorf("parse steps for %s: %w", name, err)
	}
	return steps, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -run TestRegistrySkillSteps -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prompts/registry.go internal/prompts/registry_test.go
git commit -m "feat(prompts): add SkillSteps() for parsing skill workflow steps from frontmatter"
```

---

### Task 6: Create all ROLE.md files from existing prompts

Extract all 7 pipeline role prompts from their current locations into `prompts/roles/*/ROLE.md`.

**Files:**
- Create: `prompts/roles/planner/ROLE.md` (from `prompts/planner.md.j2`)
- Create: `prompts/roles/implementer/ROLE.md` (from `implementer.go:buildImplementerSystemPrompt()` + `prompts/implementer.md.j2`)
- Create: `prompts/roles/implementer-retry/ROLE.md` (from `prompts/implementer_retry.md.j2` + `implementer.go:retryLabelAndGuidance()`)
- Create: `prompts/roles/spec-reviewer/ROLE.md` (from `prompts/spec_reviewer.md.j2`)
- Create: `prompts/roles/quality-reviewer/ROLE.md` (from `prompts/quality_reviewer.md.j2`)
- Create: `prompts/roles/final-reviewer/ROLE.md` (from `prompts/final_reviewer.md.j2`)
- Create: `prompts/roles/clarifier/ROLE.md` (from `prompts/clarifier.md.j2`)

**Step 1: Create directory structure**

```bash
mkdir -p prompts/roles/{planner,implementer,implementer-retry,spec-reviewer,quality-reviewer,final-reviewer,clarifier}
```

**Step 2: Write ROLE.md files**

Each file consolidates its system prompt + user prompt template into a single ROLE.md. Example for `implementer`:

```markdown
---
name: implementer
description: "Expert software engineer implementing tasks using TDD"
model_hint: implementer
max_tokens: 8192
temperature: 0.0
cache_system_prompt: true
includes:
  - fragments/tdd-rules.md
  - fragments/output-format.md
---

You are an expert software engineer implementing a single task using TDD.

{% include "fragments/tdd-rules.md" %}

{% include "fragments/output-format.md" %}

## Task

**{{ task_title }}**
{{ task_description }}

{% if acceptance_criteria %}
**Acceptance Criteria:**
{% for ac in acceptance_criteria %}
- {{ ac }}
{% endfor %}
{% endif %}

## Codebase Context

{% for path, content in context_files %}
### {{ path }}
` ` `
{{ content }}
` ` `

{% endfor %}

{% if codebase_patterns %}
## Codebase Patterns
{{ codebase_patterns }}
{% endif %}
```

Repeat for all 7 roles, preserving the exact prompt content from the current sources.

**Step 3: Verify loading**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/prompts/ -v`
Expected: PASS (existing tests still work)

**Step 4: Commit**

```bash
git add prompts/roles/
git commit -m "feat(prompts): create ROLE.md files for all 7 pipeline stages"
```

---

### Task 7: Create fragment files for shared prompt pieces

**Files:**
- Create: `prompts/fragments/tdd-rules.md`
- Create: `prompts/fragments/output-format.md`
- Create: `prompts/fragments/retry-guidance.md`
- Create: `prompts/fragments/review-output.md`

**Step 1: Extract shared content**

`tdd-rules.md` — from `implementer.go:buildImplementerSystemPrompt()` TDD section:
```markdown
## TDD Rules (MANDATORY)

1. Write tests FIRST that capture the acceptance criteria
2. Tests must be runnable and fail for the right reason before implementation
3. Write minimal implementation to make tests pass
4. Never skip writing tests
```

`output-format.md` — from `implementer.go:buildImplementerSystemPrompt()` output section:
```markdown
## Output Format

Output ONLY machine-parseable file blocks. Do not include explanations.

For NEW files:
=== NEW FILE: path/to/file.ext ===
<complete file content>
=== END FILE ===

For EXISTING files:
=== MODIFY FILE: path/to/file.ext ===
<<<< SEARCH
<exact existing lines>
>>>>
<<<< REPLACE
<replacement lines>
>>>>
=== END FILE ===

Rules:
- Output test files before implementation files.
- Include at least 3 lines in each SEARCH block.
- Preserve indentation/whitespace exactly.
- Do not output markdown fences.
```

`retry-guidance.md` — from `implementer.go:retryLabelAndGuidance()`:
```markdown
{% if retry_error_type == "compile" %}
Focus on fixing the build error. Check import paths, undefined symbols, and missing return statements. Do not refactor unrelated code.
{% elif retry_error_type == "type_error" %}
Focus on fixing the type mismatch. Verify interface implementations, check function signatures, and ensure correct type assertions.
{% elif retry_error_type == "lint_style" %}
Focus on fixing the lint/style issues listed below. Do not rewrite working logic.
{% elif retry_error_type == "test_assertion" %}
Focus on making the failing test assertions pass. Read the expected vs actual values carefully and adjust implementation, not tests.
{% elif retry_error_type == "test_runtime" %}
Focus on preventing the runtime panic. Check nil pointer dereferences, slice/map bounds, and error returns before use.
{% elif retry_error_type == "spec_violation" %}
Focus on satisfying the acceptance criteria listed below. Do not change code unrelated to the failing criteria.
{% elif retry_error_type == "quality_concern" %}
Focus on addressing the quality concerns listed below. Refactor only the flagged areas.
{% endif %}
```

`review-output.md` — shared across all reviewers:
```markdown
## Output Format

Always start your response with a STATUS line:

If approved:
STATUS: APPROVED

If issues found:
STATUS: REJECTED
ISSUES:
- Issue 1 description
- Issue 2 description
```

**Step 2: Commit**

```bash
git add prompts/fragments/
git commit -m "feat(prompts): extract shared fragments for TDD rules, output format, retry guidance, review output"
```

---

### Task 8: Create AGENT.md files from embedded assets

**Files:**
- Create: `prompts/agents/tdd-orchestrator/AGENT.md` (from `assets/claude/foreman/skills/tdd.md`)
- Create: `prompts/agents/tdd-test-writer/AGENT.md` (from `assets/claude/foreman/agents/tdd-test-writer.md`)
- Create: `prompts/agents/tdd-implementer/AGENT.md` (from `assets/claude/foreman/agents/tdd-implementer.md`)
- Create: `prompts/agents/tdd-refactorer/AGENT.md` (from `assets/claude/foreman/agents/tdd-refactorer.md`)

**Step 1: Write AGENT.md files with proper frontmatter**

Example for `tdd-test-writer`:
```markdown
---
name: tdd-test-writer
description: "RED phase agent — writes failing tests only"
mode: subagent
tools:
  - Read
  - Write
  - Edit
  - Bash
  - Glob
  - Grep
---

# TDD Test Writer

You are the RED phase agent. Your job is to write FAILING tests only.

## Rules
- Write tests that cover all acceptance criteria
- Tests MUST fail before implementation exists
- Do NOT write any implementation code
- Run `{{ test_command }}` to verify tests fail

## Output
Tests that fail with assertion errors (not compile errors).
```

**Step 2: Commit**

```bash
git add prompts/agents/
git commit -m "feat(prompts): create AGENT.md files for TDD agents from embedded assets"
```

---

### Task 9: Convert YAML skills to SKILL.md format

**Files:**
- Create: `prompts/skills/bug-fix/SKILL.md` (from `skills/bug-fix.yml`)
- Create: `prompts/skills/feature-dev/SKILL.md` (from `skills/feature-dev.yml`)
- Create: `prompts/skills/refactor/SKILL.md` (from `skills/refactor.yml`)
- Create: `prompts/skills/community/security-scan/SKILL.md` (from `skills/community/security-scan.yml`)
- Create: `prompts/skills/community/write-changelog/SKILL.md` (from `skills/community/write-changelog.yml`)

**Step 1: Read existing YAML skills and convert**

Example conversion for `bug-fix.yml` → `bug-fix/SKILL.md`:

```markdown
---
name: bug-fix
description: "Bug fixing workflow — emphasizes regression tests"
trigger: post_lint
steps:
  - id: regression-check
    type: llm_call
    prompt: |
      Review this bug fix diff and check:
      1. Does the fix address the root cause, not just symptoms?
      2. Is there a regression test that would catch this bug if reintroduced?
      3. Are there related areas that might have the same bug?
      Respond with APPROVED or ISSUES: followed by a bullet list.
    model: "{{ models_quality_reviewer }}"
---

# Bug Fix Skill

This skill runs after linting to validate bug fixes emphasize root cause analysis and regression testing.
```

Note: Template syntax changes from Go `{{ .Models.QualityReviewer }}` to pongo2 `{{ models_quality_reviewer }}`.

**Step 2: Commit**

```bash
git add prompts/skills/
git commit -m "feat(prompts): convert YAML skills to SKILL.md format"
```

---

### Task 10: Wire registry into pipeline — replace prompt_renderer.go

**Files:**
- Modify: `internal/pipeline/planner.go` — use `registry.Render(KindRole, "planner", vars)`
- Modify: `internal/pipeline/implementer.go` — use `registry.Render(KindRole, "implementer", vars)`
- Modify: `internal/pipeline/spec_reviewer.go` — use `registry.Render(KindRole, "spec-reviewer", vars)`
- Modify: `internal/pipeline/quality_reviewer.go` — use `registry.Render(KindRole, "quality-reviewer", vars)`
- Modify: `internal/pipeline/final_reviewer.go` — use `registry.Render(KindRole, "final-reviewer", vars)`
- Modify: `internal/pipeline/feedback.go` — if it calls RenderPrompt
- Test: Run existing tests to ensure no regressions

**Step 1: Add registry as dependency to pipeline components**

Each pipeline component that currently calls `RenderPrompt()` or builds strings inline needs a `*prompts.Registry` field. Example for `Planner`:

```go
type Planner struct {
	llm      LLMProvider
	registry *prompts.Registry
	// ... existing fields
}
```

**Step 2: Replace prompt building**

Before (planner.go):
```go
rendered, err := RenderPrompt("planner", PromptContext{...})
```

After:
```go
rendered, err := p.registry.Render(prompts.KindRole, "planner", map[string]any{
	"ticket_title":       ticket.Title,
	"ticket_description": ticket.Description,
	"acceptance_criteria": ticket.AcceptanceCriteria,
	"file_tree":          fileTree,
	"project_context":    projectContext,
	"max_tasks":          p.limits.MaxTasksPerTicket,
})
```

Before (implementer.go):
```go
systemPrompt := buildImplementerSystemPrompt() // hardcoded string
userPrompt := buildImplementerUserPrompt(input) // string building
```

After:
```go
systemPrompt, _ := impl.registry.Render(prompts.KindRole, "implementer", map[string]any{
	"task_title":         input.Task.Title,
	"task_description":   input.Task.Description,
	"acceptance_criteria": input.Task.AcceptanceCriteria,
	"context_files":      input.ContextFiles,
	"codebase_patterns":  "", // from config
	"attempt":            input.Attempt,
	"retry_feedback":     input.Feedback,
	"retry_error_type":   string(input.RetryErrorType),
})
```

**Step 3: Run all existing tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -v`
Expected: PASS — output should be equivalent to old prompts

**Step 4: Commit**

```bash
git add internal/pipeline/
git commit -m "refactor(pipeline): replace RenderPrompt and hardcoded strings with registry.Render()"
```

---

### Task 11: Wire registry into skills engine — replace YAML loader

**Files:**
- Modify: `internal/skills/loader.go` — add `LoadFromRegistry()` function
- Modify: `internal/skills/engine.go` — accept registry-sourced skills
- Modify: `internal/skills/hooks.go` — work with both formats during migration
- Test: `internal/skills/engine_test.go`

**Step 1: Add LoadFromRegistry()**

```go
// LoadFromRegistry converts registry skill entries into Skill structs
// compatible with the existing engine.
func LoadFromRegistry(reg *prompts.Registry) ([]*Skill, error) {
	var skills []*Skill
	for _, entry := range reg.List(prompts.KindSkill) {
		steps, err := reg.SkillSteps(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("load steps for %s: %w", entry.Name, err)
		}

		trigger, _ := entry.Metadata["trigger"].(string)

		skill := &Skill{
			ID:          entry.Name,
			Description: entry.Description,
			Trigger:     trigger,
		}
		for _, s := range steps {
			skill.Steps = append(skill.Steps, SkillStep{
				ID:           s.ID,
				Type:         s.Type,
				Content:      s.Prompt,
				Model:        s.Model,
				Command:      s.Command,
				AllowedTools: s.AllowedTools,
				MaxTurns:     s.MaxTurns,
				TimeoutSecs:  s.TimeoutSecs,
				MaxTokens:    s.MaxTokens,
				OutputFormat: s.OutputFormat,
				FallbackModel: s.FallbackModel,
				SkillRef:     s.SkillRef,
				AllowFailure: s.AllowFailure,
			})
		}
		skills = append(skills, skill)
	}
	return skills, nil
}
```

**Step 2: Run existing skill engine tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/skills/ -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/skills/
git commit -m "refactor(skills): add LoadFromRegistry() to bridge registry skills into engine"
```

---

### Task 12: Wire registry into Claude Code runner — replace SkillInjector

**Files:**
- Modify: `internal/agent/claudecode.go` — call `registry.ForClaude()` before running
- Modify: `internal/pipeline/task_runner.go` — pass registry to Claude Code runner
- Delete: `internal/pipeline/skill_injector.go` (after migration complete)
- Delete: `internal/pipeline/assets/claude/` (after migration complete)

**Step 1: Update ClaudeCodeRunner to accept registry**

```go
type ClaudeCodeRunner struct {
	bin      string
	runner   runner.CommandRunner
	config   ClaudeCodeConfig
	registry *prompts.Registry // NEW
}
```

**Step 2: Call ForClaude() before execution**

In `Run()`, before building args:
```go
if r.registry != nil {
	vars := map[string]any{
		"test_command": "go test ./...", // from task context
	}
	if err := r.registry.ForClaude(req.WorkDir, vars); err != nil {
		return AgentResult{}, fmt.Errorf("claudecode: write .claude/: %w", err)
	}
}
```

**Step 3: Generate richer AGENTS.md**

The `ForClaude()` method should also generate/merge AGENTS.md with role-specific context:
```go
// In ForClaude(), after writing .claude/ structure:
agentsMD := filepath.Join(workDir, "AGENTS.md")
// Read existing AGENTS.md if present, append Foreman context
```

**Step 4: Run Claude Code runner tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestClaudeCode -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/ internal/pipeline/
git commit -m "refactor(claudecode): replace SkillInjector with registry.ForClaude()"
```

---

### Task 13: Wire registry into daemon/orchestrator startup

**Files:**
- Modify: `internal/daemon/orchestrator.go` — load registry once at startup, pass to pipeline
- Modify: `internal/config/config.go` — add `PromptsDir` config field (already exists as `prompts_dir`)
- Modify: `internal/agent/factory.go` — pass registry to runners

**Step 1: Load registry at daemon startup**

```go
// In orchestrator initialization:
reg, err := prompts.Load(cfg.PromptsDir)
if err != nil {
	return fmt.Errorf("load prompts registry: %w", err)
}
```

**Step 2: Pass registry through the pipeline**

Wire `reg` to: Planner, Implementer, SpecReviewer, QualityReviewer, FinalReviewer, Clarifier, SkillEngine, AgentFactory.

**Step 3: Run integration tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/daemon/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/daemon/ internal/config/ internal/agent/
git commit -m "feat(daemon): load prompt registry at startup and wire to all pipeline components"
```

---

### Task 14: Delete old prompt infrastructure

**Files:**
- Delete: `prompts/planner.md.j2`
- Delete: `prompts/implementer.md.j2`
- Delete: `prompts/implementer_retry.md.j2`
- Delete: `prompts/spec_reviewer.md.j2`
- Delete: `prompts/quality_reviewer.md.j2`
- Delete: `prompts/final_reviewer.md.j2`
- Delete: `prompts/clarifier.md.j2`
- Delete: `internal/pipeline/prompt_renderer.go`
- Delete: `internal/pipeline/prompt_renderer_test.go`
- Delete: `internal/pipeline/prompt_builder.go`
- Delete: `internal/pipeline/prompt_builder_test.go`
- Delete: `internal/pipeline/skill_injector.go`
- Delete: `internal/pipeline/skill_injector_test.go`
- Delete: `internal/pipeline/assets/` (entire directory)
- Delete: `skills/` (top-level, replaced by `prompts/skills/`)
- Remove: `buildImplementerSystemPrompt()` and `buildImplementerUserPrompt()` from `implementer.go`
- Remove: `retryLabelAndGuidance()` etc. from `implementer.go` (moved to fragment)

**Step 1: Delete files**

```bash
rm prompts/*.md.j2
rm internal/pipeline/prompt_renderer.go internal/pipeline/prompt_renderer_test.go
rm internal/pipeline/prompt_builder.go internal/pipeline/prompt_builder_test.go
rm internal/pipeline/skill_injector.go internal/pipeline/skill_injector_test.go
rm -rf internal/pipeline/assets/
rm -rf skills/
```

**Step 2: Remove dead code from implementer.go**

Remove `buildImplementerSystemPrompt()`, `buildImplementerUserPrompt()`, `retryLabelAndGuidance()`, `retryHeadingAndGuidance()`, `promptBuilderRetryHeadingAndGuidance()`.

**Step 3: Verify build and tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./... && go test ./...`
Expected: PASS — no references to deleted code remain

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor: delete old prompt templates, YAML skills, embedded assets, and hardcoded prompt strings"
```

---

### Task 15: Update AGENTS.md and documentation

**Files:**
- Modify: `AGENTS.md` — update architecture section to document new prompt registry
- Modify: `docs/skills.md` — update to describe SKILL.md format instead of YAML
- Modify: `foreman.example.toml` — update `prompts_dir` default

**Step 1: Update AGENTS.md**

Replace the "YAML Skills" section with the new prompt registry architecture. Document the new directory structure, file formats, and how to add custom roles/agents/skills.

**Step 2: Update docs/skills.md**

Document: how to create a SKILL.md, frontmatter fields, step types, how fragments work.

**Step 3: Commit**

```bash
git add AGENTS.md docs/ foreman.example.toml
git commit -m "docs: update architecture docs for unified prompt registry"
```

---

### Task 16: Final verification — full test suite

**Step 1: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1`
Expected: All PASS

**Step 2: Build binary**

Run: `cd /Users/canh/Projects/Indies/Foreman && make build`
Expected: Clean build

**Step 3: Verify prompt loading**

Run: `cd /Users/canh/Projects/Indies/Foreman && go run main.go --help`
Verify the binary starts without prompt loading errors.

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: address any remaining issues from prompt registry migration"
```
