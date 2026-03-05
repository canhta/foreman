# Phase 10: Builtin Runner V2 — Design

**Status:** Approved
**Date:** 2026-03-05
**Scope:** Complete refactor of the builtin agent runner — tool registry, parallel execution, per-tool hooks, reactive context injection, 14 built-in tools, skill engine additions, MCP stub.

---

## Problem Statement

The current `builtin` runner (`internal/agent/builtin.go`) has four gaps that limit its usefulness:

1. **Weak tool set** — Only Read/Glob/Grep/Edit/Write. No git tools, no code intelligence, no structured test/lint execution.
2. **Sequential tool execution** — Claude Code and the Anthropic SDK run all tool calls in a turn in parallel via `errgroup`. Foreman's loop is sequential — 3× slower on multi-tool turns.
3. **No context injection** — `.foreman-context.md` is not injected. Progress patterns from the DB are not loaded reactively. The model has no project conventions.
4. **No hooks** — No way to enforce security cross-cuttingly. Secrets scanning on Write is embedded in the tool; blocking a forbidden path requires touching every tool.

---

## Architecture

### Two-Layer Context System

**Layer 1 — Pre-assembly (all three runners)**

The skills engine always builds a `SystemPromptAppend` before calling any runner:

```
skills/engine.go collects:
  1. .foreman-context.md  — walked up from workDir, project conventions
  2. Static path-scoped rules — rules for file types the skill declares
  3. Ticket/task metadata — acceptance criteria, progress summary

→ injected into AgentRequest.SystemPrompt (prepended)
→ used by builtin, claudecode, AND copilot identically
```

**Layer 2 — Reactive injection (builtin only)**

After each file-touching tool call, the builtin runner asks: *"now that the model just read `src/auth/`, is there relevant context not yet injected?"*

```
PostToolUse hook fires
  → extract touched paths from tool call
  → call ContextProvider.OnFilesAccessed(paths)
  → get back progress patterns + scoped rules for those dirs
  → inject as context message before next LLM turn
  → track what's been injected — never duplicate
```

### Dependency Ownership

```
db.Database
  └── owned by: skills/engine.go (already)
      ├── pre-assembles SystemPromptAppend for ALL runners
      └── implements ContextProvider for builtin reactive injection

git.GitProvider
  └── owned by: tools.Registry
      └── GetDiff, GetCommitLog, TreeSummary tool implementations

runner.CommandRunner
  └── owned by: tools.Registry
      └── Bash, RunTest, GetErrors tool implementations

BuiltinRunner holds:
  ├── llm.LlmProvider
  ├── *tools.Registry
  ├── ContextProvider  (interface — nil-safe, optional)
  └── BuiltinConfig

AgentRunner interface: NO db or GitProvider — runner executes, does not query pipeline state.
```

---

## Component Design

### 1. Tool Interface

```go
// internal/agent/tools/tool.go
type Tool interface {
    Name()        string
    Description() string
    Schema()      json.RawMessage  // hand-written JSON Schema — no extra deps
    Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error)
}
```

Hand-written schemas (not `invopop/jsonschema`) — schemas are stable and small, no reflection dep needed.

### 2. Registry

```go
// internal/agent/tools/registry.go
type Registry struct {
    tools map[string]Tool
    hooks ToolHooks
}

type ToolHooks struct {
    PreToolUse  func(ctx context.Context, name string, input json.RawMessage) error
    PostToolUse func(ctx context.Context, name string, output string, err error)
}

// NewRegistry constructs a Registry with optional git and command dependencies.
// git and cmd may be nil — those tool groups return informative errors if invoked.
func NewRegistry(git git.GitProvider, cmd runner.CommandRunner, hooks ToolHooks) *Registry

func (r *Registry) Execute(ctx context.Context, workDir, name string, input json.RawMessage) (string, error)
func (r *Registry) Defs(names []string) []models.ToolDef
func (r *Registry) Has(name string) bool
```

### 3. Parallel Execution in builtin.go

Mirrors the Anthropic SDK's `betatoolrunner.go` pattern exactly:

```go
g, gctx := errgroup.WithContext(ctx)
results := make([]models.ToolResult, len(toolCalls))
for i, tc := range toolCalls {
    i, tc := i, tc
    g.Go(func() error {
        out, err := r.registry.Execute(gctx, req.WorkDir, tc.Name, tc.Input)
        if err != nil {
            results[i] = models.ToolResult{ToolCallID: tc.ID, Content: err.Error(), IsError: true}
        } else {
            results[i] = models.ToolResult{ToolCallID: tc.ID, Content: out}
        }
        return nil // tool errors become result content, not Go errors
    })
}
g.Wait()
```

`golang.org/x/sync/errgroup` is already a transitive dependency.

### 4. ContextProvider Interface

```go
// internal/agent/context.go
type ContextProvider interface {
    // Called after each file-touching tool (Read, Edit, Write, GetDiff).
    // Returns new context to inject as a user message, empty string if nothing new.
    OnFilesAccessed(ctx context.Context, paths []string) (string, error)
}
```

**`SkillsContextProvider`** in `internal/skills/`:

```go
type SkillsContextProvider struct {
    db       db.Database
    ticketID string
    injected map[string]bool
}

func (p *SkillsContextProvider) OnFilesAccessed(ctx context.Context, paths []string) (string, error) {
    dirs := uniqueDirs(paths)
    patterns, _ := context.GetPrunedPatterns(ctx, p.db, p.ticketID, dirs)
    // filter already-injected patterns, mark new ones, format for prompt
}
```

### 5. Updated BuiltinRunner

```go
type BuiltinRunner struct {
    provider        llm.LlmProvider
    model           string
    config          BuiltinConfig
    registry        *tools.Registry
    contextProvider agent.ContextProvider  // nil-safe, optional
}

func NewBuiltinRunner(
    provider llm.LlmProvider,
    model    string,
    config   BuiltinConfig,
    registry *tools.Registry,
    cp       agent.ContextProvider,  // nil ok
) *BuiltinRunner
```

### 6. MCP Stub

```go
// internal/agent/mcp/client.go
type Client interface {
    ListTools(ctx context.Context) ([]models.ToolDef, error)
    Call(ctx context.Context, name string, input json.RawMessage) (string, error)
}

type NoopClient struct{}
func (n *NoopClient) ListTools(ctx context.Context) ([]models.ToolDef, error) { return nil, nil }
func (n *NoopClient) Call(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
    return "", fmt.Errorf("MCP not yet implemented")
}
```

`AgentRequest` gains `MCPServers []MCPServerConfig`. Registry merges MCP tool defs at session start if a real client is provided. The tool-use loop is unchanged.

---

## Tool Set

### Tier 1 — Filesystem (`internal/agent/tools/fs.go`)

| Tool | Input | Notes |
|---|---|---|
| `Read` | `path`, `start_line?`, `end_line?` | Optional line range; enforce workDir boundary |
| `Write` | `path`, `content` | Secrets check on content before write |
| `Edit` | `path`, `old_string`, `new_string` | First-occurrence replace; workDir + secrets guard |
| `MultiEdit` | `path`, `edits[]` | Batch edits, atomic — reduces turn count |
| `ListDir` | `path`, `recursive?` | `fs.WalkDir`; returns name/type/size/modified per entry |
| `Glob` | `pattern`, `base?` | Fix `**` support via `fs.WalkDir` (current `filepath.Glob` doesn't support `**`) |
| `Grep` | `pattern`, `path`, `file_pattern?`, `case_sensitive?` | Cap at 200 matches; pure Go regexp |

### Tier 2 — Git & Code (`internal/agent/tools/git.go`, `code.go`)

| Tool | Input | Implementation |
|---|---|---|
| `GetDiff` | `base?`, `head?`, `path?` | `git.GitProvider.Diff()` or `DiffWorking()` |
| `GetCommitLog` | `path?`, `count?` | `git.GitProvider.Log()` |
| `TreeSummary` | `max_depth?`, `focus?` | `git.GitProvider.FileTree()` formatted as tree |
| `GetSymbol` | `symbol`, `kind?`, `path?` | Regex: `func X\b`, `type X\b`, `class X\b` |
| `GetErrors` | `tool`, `path?` | `CommandRunner` + `runner.ParseLintOutput()` → structured |

### Tier 3 — Execution (`internal/agent/tools/exec.go`, restricted)

| Tool | Input | Security |
|---|---|---|
| `Bash` | `command`, `timeout_secs?` | Whitelist against `config.AllowedCommands`; deny `rm`, `curl`, `git push`, `ssh`, `wget` |
| `RunTest` | `test?`, `package?`, `timeout_secs?` | Fixed command from config; returns `{passed, failed, output}` struct |

### Tier 3 — Subagent (`internal/agent/tools/exec.go`)

| Tool | Input | Security |
|---|---|---|
| `Subagent` | `task`, `tools[]`, `max_turns?` | Depth ≤ 3 (shared via `AgentRequest.AgentDepth`); no ContextProvider passed (baseline context only) |

`Subagent` calls back into `BuiltinRunner.Run()` with a scoped `AgentRequest`. Because `SubagentTool` holds a reference to the runner and the runner holds the registry, construction is two-phase: registry is created first (without runner), then runner, then `registry.SetRunner(runner)`.

### Never Build

`CreatePR`, `PushBranch`, `CreateBranch` — Foreman pipeline owns these.
`Computer`, `exit_plan_mode` — not applicable to daemon context.

---

## Security Model

All write tools (Write, Edit, MultiEdit) go through a shared `guard.go`:

```go
// internal/agent/tools/guard.go
func ValidatePath(workDir, path string) error     // no traversal, within workDir
func CheckSecrets(path, content string) error      // no .env, *.key, *.pem writes
func IsForbiddenPath(path string) bool            // configurable forbidden list
```

`PreToolUse` hook: validate path before execution.
`PostToolUse` hook: log tool call events to the event log; scan Write output for secrets.

Bash whitelist is checked in `PreToolUse` — the tool itself never runs if the command isn't on the list.

---

## Skills Engine Additions

### Wire Phase 9 Fields

`executeAgentSDK` currently ignores `step.OutputSchema`, `step.Thinking`, `step.FallbackModel`. These must be marshalled and passed through to `AgentRequest`.

### New `subskill` Step Type

```go
// engine.go
case "subskill":
    return e.executeSubSkill(ctx, step, sCtx)

func (e *Engine) executeSubSkill(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
    subSkill := e.skillsByID[step.SkillRef]
    subCtx := sCtx.forkWith(step.Input)
    if err := e.Execute(ctx, subSkill, subCtx); err != nil {
        return nil, err
    }
    // collect last step's output as this step's result
}
```

Loader gains `SkillStep.SkillRef string` and `SkillStep.Input map[string]string`.
Engine gains `skillsByID map[string]*Skill` built at construction.

### Implement `git_diff` Step

```go
func (e *Engine) executeGitDiff(ctx context.Context) (*StepResult, error) {
    diff, err := e.git.DiffWorking(ctx, e.workDir)
    // ...
}
```

Engine gains optional `git git.GitProvider` field.

### Output Format

`SkillStep.OutputFormat` field: `markdown` (default) / `json` / `diff` / `checklist`.

- `json`: engine auto-sets `OutputSchema` on `AgentRequest` if not already set; validates response parses as JSON
- `diff`: engine validates output is a unified diff (`--- a/` header present)
- `checklist`: engine parses `- [x]`/`- [ ]` items; exposes `passed`/`failed` counts on `StepResult`

---

## File Map (Final)

```
internal/agent/
├── context.go                   NEW — ContextProvider interface
├── runner.go                    MODIFY — add MCPServers to AgentRequest
├── builtin.go                   REFACTOR — Registry, parallel exec, ContextProvider
├── mcp/
│   └── client.go                NEW — Client interface + NoopClient
└── tools/
    ├── tool.go                  NEW — Tool interface
    ├── registry.go              NEW — Registry, ToolHooks, parallel dispatch
    ├── guard.go                 NEW — path validation, secrets check (replaces validateWritePath)
    ├── fs.go                    NEW — Read, Write, Edit, MultiEdit, ListDir, Glob, Grep
    ├── git.go                   NEW — GetDiff, GetCommitLog, TreeSummary
    ├── code.go                  NEW — GetSymbol, GetErrors
    └── exec.go                  NEW — Bash, RunTest, Subagent (two-phase init via SetRunner)

internal/skills/
├── context_provider.go          NEW — SkillsContextProvider implements agent.ContextProvider
├── engine.go                    MODIFY — wire Phase9 fields, subskill, git_diff, output_format, foreman-context.md
└── loader.go                    MODIFY — SkillRef, Input, OutputFormat fields on SkillStep

internal/agent/tools.go          DELETED — replaced by tools/ package
```

---

## What Does NOT Change

- `llm/` package — untouched
- `models/pipeline.go` — untouched (Phase 9 already added the fields)
- `claudecode.go`, `copilot.go` — untouched (pre-assembly via SystemPromptAppend handles their context needs)
- `AgentRunner` interface — no new methods
- `runner.go` AgentRequest — additive fields only (`AgentDepth int`, `MCPServers []MCPServerConfig`)

---

## Design Amendments (V2)

Six gaps identified before implementation — all addressed here.

### Gap 1: Subagent Tool

`subskill` (deterministic YAML composition) and `Subagent` (LLM decides mid-turn) are distinct. `Subagent` is in Tier 3 (see above). Two-phase construction solves the circular reference:

```go
registry := tools.NewRegistry(git, cmd, hooks)   // Step 1 — no runner yet
runner   := agent.NewBuiltinRunner(...)           // Step 2 — holds registry
registry.SetRunner(runner)                         // Step 3 — inject after construction
```

### Gap 2: Parallel Execution + ContextProvider Race

The plan already collects all touched paths *after* `g.Wait()` before calling `OnFilesAccessed` — one call per turn with the full batch. No data race. `SkillsContextProvider.injected` is only accessed from the main goroutine. ✓ Already correct.

### Gap 3: ContextProvider Token Budget

`SkillsContextProvider` tracks cumulative injected tokens and stops injecting when the budget is consumed:

```go
type SkillsContextProvider struct {
    db             progressStore
    ticketID       string
    injected       map[string]bool
    tokensBudget   int // from config (0 = unlimited)
    tokensInjected int // running total (rough: len(text)/4)
}
```

Budget check: `if p.tokensBudget > 0 && p.tokensInjected >= p.tokensBudget { return "", nil }`. Default budget: 8000 tokens (configurable via `BuiltinConfig.ContextTokenBudget`).

### Gap 4: Circular Subagent Reference

Solved by two-phase init (see Gap 1). `Registry` gains `SetRunner(r AgentRunner)` and stores it for `SubagentTool`. `SubagentTool` holds a `runFn func(ctx, AgentRequest) (AgentResult, error)` — a function reference, not the runner struct directly — to avoid import cycles.

### Gap 5: Shared Depth Counter

`AgentRequest` gains `AgentDepth int`. `BuiltinRunner` increments it when constructing sub-`AgentRequest` for `Subagent` tool. `SubagentTool` checks `req.AgentDepth >= 3` and returns an error before calling the runner. Both `subskill` and `Subagent` paths use the same field, so the combined depth is visible across the whole call stack.

### Gap 6: Tool Hook Events Not Recorded

`PostToolUse` hook in `NewBuiltinRunner` emits to the event log:

```
skillAgentToolCalled     — tool name, workDir, duration_ms
skillAgentToolBlocked    — tool name, reason (from PreToolUse error)
skillAgentToolFailed     — tool name, error message
skillAgentContextInjected — dirs_count, tokens_injected
```

These are emitted via the existing event emitter passed to `NewBuiltinRunner` through `BuiltinConfig.EventEmitter` (optional, nil-safe). If nil, hooks still fire for security enforcement but events are not recorded.

---

## Testing Strategy

- `tools/fs_test.go` — table-driven, uses `t.TempDir()`, covers path traversal + secrets + line ranges
- `tools/git_test.go` — mock `GitProvider`
- `tools/exec_test.go` — mock `CommandRunner`, verify whitelist enforcement
- `tools/registry_test.go` — parallel execution, hook firing order, unknown tool error
- `agent/builtin_test.go` — existing tests pass with new constructor; add ContextProvider mock test
- `skills/context_provider_test.go` — deduplication test, dir extraction test
- `skills/engine_test.go` — subskill, git_diff, output_format tests added
