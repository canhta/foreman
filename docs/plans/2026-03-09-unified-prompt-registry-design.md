# Unified Prompt Registry — Design Document

## Problem

Foreman has 4 disconnected prompt/instruction systems:

1. **Jinja2 templates** (`prompts/*.md.j2`) — rendered via pongo2 for pipeline stages
2. **Hardcoded Go strings** (`implementer.go:buildImplementerSystemPrompt()`) — inline in Go functions
3. **YAML skill files** (`skills/*.yml`) — workflow hooks with step sequences
4. **Embedded Claude assets** (`internal/pipeline/assets/claude/`) — markdown injected into worktrees

Issues: dual prompt systems for same role, 4 template engines (pongo2/fmt.Sprintf/text/template/Go templates), hollow skill files, prompt content mixed with business logic, ad-hoc context injection.

## Goal

One format, one template engine, one source of truth serving both builtin and Claude Code runners. Composable, testable, self-describing prompt content.

## Architecture

All prompts, agent definitions, skills, and commands live as **Markdown files with YAML frontmatter** in a unified `prompts/` directory tree. A Go registry loads them at startup, resolves includes, and renders templates. Each runner consumes the registry differently: builtin gets rendered strings, Claude Code gets a `.claude/` directory written to the worktree.

## Directory Structure

```
prompts/
  registry.go                  # Loader, parser, renderer, ForClaude() writer
  registry_test.go
  frontmatter.go               # YAML frontmatter parser
  frontmatter_test.go

  roles/                       # Pipeline stage system prompts (builtin runner LLM calls)
    planner/ROLE.md
    implementer/ROLE.md
    implementer-retry/ROLE.md
    spec-reviewer/ROLE.md
    quality-reviewer/ROLE.md
    final-reviewer/ROLE.md
    clarifier/ROLE.md

  agents/                      # Claude Code agent personas (.claude/agents/)
    tdd-orchestrator/AGENT.md
    tdd-test-writer/AGENT.md
    tdd-implementer/AGENT.md
    tdd-refactorer/AGENT.md

  skills/                      # Workflow hooks (replacing skills/*.yml)
    bug-fix/SKILL.md
    feature-dev/SKILL.md
    refactor/SKILL.md
    community/
      security-scan/SKILL.md
      write-changelog/SKILL.md

  commands/                    # Claude Code slash commands (.claude/commands/)
    implement/COMMAND.md
    review/COMMAND.md

  fragments/                   # Reusable prompt pieces (included by roles/agents)
    tdd-rules.md
    output-format.md
    retry-guidance.md
    review-output.md
```

## File Formats

### ROLE.md (pipeline stage prompts)

```yaml
---
name: implementer
description: "Expert engineer implementing tasks using TDD"
model_hint: implementer
max_tokens: 8192
temperature: 0.0
cache_system_prompt: true
includes:
  - fragments/tdd-rules.md
  - fragments/output-format.md
---
```

Body is pongo2 template with `{{ variable }}` syntax. Variables: `task_title`, `task_description`, `acceptance_criteria`, `context_files`, `codebase_patterns`, `attempt`, `retry_feedback`, etc.

### AGENT.md (Claude Code agent personas)

```yaml
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
```

Body is plain markdown with `{{ variable }}` for task-specific rendering (e.g., `{{ test_command }}`).

### SKILL.md (workflow hooks)

```yaml
---
name: bug-fix
description: "Bug fixing workflow — emphasizes regression tests"
trigger: post_lint
steps:
  - id: regression-check
    type: agentsdk
    prompt: |
      Review this bug fix and check:
      1. Root cause addressed?
      2. Regression test present?
      3. Related areas?
---
```

Body is optional additional context.

### COMMAND.md (Claude Code commands)

```yaml
---
name: implement
description: "Implement a task with TDD"
subtask: true
---
```

Body is the command prompt.

### Fragment .md (reusable pieces)

No frontmatter. Plain markdown content included by `{% include %}`.

## Registry API

```go
package prompts

type EntryKind string
const (
    KindRole     EntryKind = "role"
    KindAgent    EntryKind = "agent"
    KindSkill    EntryKind = "skill"
    KindCommand  EntryKind = "command"
    KindFragment EntryKind = "fragment"
)

type Entry struct {
    Name        string
    Kind        EntryKind
    Description string
    Metadata    map[string]any  // all frontmatter fields
    RawContent  string          // markdown body (pre-template)
    Path        string          // source file path
    Includes    []string        // fragment references
}

type SkillStep struct {
    ID           string
    Type         string
    Prompt       string
    Model        string
    // ... same fields as current SkillStep
}

type Registry struct { ... }

func Load(baseDir string) (*Registry, error)
func (r *Registry) Get(kind EntryKind, name string) (*Entry, error)
func (r *Registry) List(kind EntryKind) []*Entry
func (r *Registry) Render(kind EntryKind, name string, vars map[string]any) (string, error)
func (r *Registry) RenderEntry(entry *Entry, vars map[string]any) (string, error)
func (r *Registry) SkillSteps(name string) ([]SkillStep, error)
func (r *Registry) ForClaude(workDir string, vars map[string]any) error
```

## Runner Integration

### Builtin Runner
```go
// Before: hardcoded in implementer.go
systemPrompt := buildImplementerSystemPrompt()

// After: from registry
systemPrompt, _ := registry.Render(prompts.KindRole, "implementer", vars)
```

### Claude Code Runner
```go
// Before: SkillInjector with embedded assets
injector := NewSkillInjector(config)
injector.Inject(workDir)

// After: registry writes full .claude/ structure
registry.ForClaude(workDir, vars)
// Writes: .claude/agents/*.md, .claude/commands/*.md, .claude/settings.json, AGENTS.md
```

### Skills Engine
```go
// Before: LoadSkill(path) reads YAML
skill, _ := skills.LoadSkill("skills/bug-fix.yml")

// After: registry provides skill steps
entry, _ := registry.Get(prompts.KindSkill, "bug-fix")
steps, _ := registry.SkillSteps("bug-fix")
```

## What Gets Deleted

| Current | Replacement |
|---------|------------|
| `prompts/*.md.j2` (7 files) | `prompts/roles/*/ROLE.md` |
| `internal/pipeline/implementer.go` hardcoded strings | `prompts/roles/implementer/ROLE.md` |
| `internal/pipeline/prompt_renderer.go` | `prompts/registry.go` |
| `internal/pipeline/prompt_builder.go` | Merged into registry render |
| `internal/pipeline/skill_injector.go` | `registry.ForClaude()` |
| `internal/pipeline/assets/claude/` (embedded) | `prompts/agents/` + `prompts/commands/` |
| `skills/*.yml` (top-level) | `prompts/skills/*/SKILL.md` |
| `internal/skills/loader.go` YAML parsing | Registry handles loading |
