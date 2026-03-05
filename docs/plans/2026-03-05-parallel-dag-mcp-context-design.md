# Design: Parallel DAG Executor + MCP Client + Context Generate

**Date:** 2026-03-05
**Status:** Approved
**Scope:** Three features that compound into a single narrative shift: "Foreman completes real work faster, connects to anything, and is trivial to adopt."

---

## 1. Parallel DAG Executor

### Problem

The `depends_on` DAG is validated at plan time but executed sequentially. Users watch 8 tasks complete one-by-one when 5 have no dependencies. This makes Foreman feel prototype-grade.

### Architecture

```
Workers --> resultChan --> Coordinator --> readyChan --> Workers
```

Single coordinator goroutine owns all mutable DAG state. Zero mutexes.

**Components:**
- `internal/daemon/dag_executor.go` — coordinator + worker pool
- Modified `internal/daemon/scheduler.go` — delegates to DAG executor instead of sequential loop

**Flow:**
1. Coordinator builds adjacency list from plan's `depends_on` edges
2. Identifies root tasks (zero in-degree), pushes to `readyChan`
3. Workers pull from `readyChan`, execute via injected `TaskRunner` interface, send results to `resultChan`
4. Coordinator receives results, marks tasks done, checks dependents — if all deps satisfied, pushes to `readyChan`
5. On failure: BFS full transitive closure of failed task, mark all reachable as `StatusSkipped`
6. Loop until all tasks are done, failed, or skipped

**Key types:**

```go
type TaskRunner interface {
    Run(ctx context.Context, task *Task) TaskResult
}

type TaskResult struct {
    TaskID string
    Status TaskStatus // completed | failed
    Error  error
}

type DAGExecutor struct {
    runner         TaskRunner
    maxWorkers     int
    taskTimeout    time.Duration
    readyChan      chan *Task        // buffered: total task count
    resultChan     chan TaskResult   // buffered: total task count
    // All below owned exclusively by coordinator goroutine:
    adjacency      map[string][]string  // task -> dependents
    inDegree       map[string]int       // remaining unmet deps
    taskMap        map[string]*Task
    completed      map[string]bool
}
```

### Configuration

```toml
[daemon]
max_parallel_tasks = 3        # worker pool size, default 3
task_timeout_minutes = 15     # per-task timeout, default 15
```

### Error Semantics

- Task fails -> BFS subtree pruning, all transitive dependents marked `StatusSkipped`
- Independent branches continue executing
- All tasks fail -> ticket marked `failed`, no PR
- Some succeed -> partial PR with explicit checklist:

```markdown
## Completed Tasks (3/5)
- [x] Add user authentication middleware
- [x] Write auth unit tests
- [x] Update OpenAPI spec

## Skipped Tasks (2/5)
- [ ] ~~Add rate limiting~~ (skipped: depends on failed task)
- [ ] ~~Write rate limit tests~~ (skipped: depends on failed task)
```

### Per-Task Timeout

```go
taskCtx, cancel := context.WithTimeout(ticketCtx, cfg.TaskTimeout)
defer cancel()
```

Prevents a hung task on one branch from silently blocking its dependents while other branches complete.

### Metrics

All Prometheus gauge writes (`tasks_in_progress`, etc.) must use atomic increments. Audit existing pipeline metrics before integration — a gauge showing 3 tasks "in_progress" simultaneously is correct; a race-corrupted counter is not.

### Testability

`TaskRunner` interface injected at construction. In tests: mock that records execution order, simulates failures, tracks concurrent invocation count. No LLM calls needed to test DAG logic.

### Validation Trace

DAG: `A -> B -> D, A -> C -> D, E (independent)`. A fails.

Expected: B skipped, C skipped, D skipped (via BFS), E completes, partial PR with E's changes only.

---

## 2. MCP Client (stdio transport)

### Problem

`MCPClient` is a stub (`NoopClient`). Completing it lets Foreman connect to any company's internal tooling — the open-source equivalent of Stripe Minions' MCP-based enterprise integration.

### Architecture

```
Agent Runner (builtin)
    |
    +-- Built-in Tools (14)
    |
    +-- MCP Tools (dynamic, from configured servers)
            |
            v
     +--------------+
     |  MCP Client  |  (JSON-RPC 2.0 over stdin/stdout)
     +------+-------+
            |
     +--------------+
     |  MCP Server  |  (subprocess)
     +--------------+
```

### Lifecycle

1. Daemon startup: for each `[[mcp.servers]]`, spawn subprocess
2. `initialize` handshake with 10s timeout — on timeout, skip server, log warning, don't block daemon startup
3. Cache `capabilities` from initialize response; verify `tools` capability exists
4. `tools/list` to discover tools; register in agent tool registry with full JSON schema passthrough
5. During execution: `tools/call` per LLM request
6. Daemon shutdown: `shutdown` + `exit` notifications, kill subprocesses

### Concurrent Request Multiplexing

With parallel DAG execution, multiple workers may call MCP tools on the same server simultaneously.

```go
type pendingRequest struct {
    id      int64
    resp    chan json.RawMessage
    errChan chan error
}

// Reader goroutine continuously deserializes stdout responses,
// routes to correct pending request by JSON-RPC id.
// MCPClient.pending: sync.Map keyed by request id.
```

Workers call `client.CallTool()` which: allocates atomic ID, registers pending request, writes to stdin (mutex-protected), blocks on response channel.

### Restart Policy

```toml
[[mcp.servers]]
name = "internal-db"
command = "npx"
args = ["-y", "@company/db-mcp-server"]
allowed_tools = ["query", "schema"]       # optional whitelist
restart_policy = "on-failure"             # always | never | on-failure (default: on-failure)
max_restarts = 3                          # default: 3
restart_delay_seconds = 2                 # default: 2
env = { DB_URL = "${DATABASE_URL}" }      # explicit env only, not inherited
```

After exceeding restart budget: mark server's tools unavailable, log clear error. Graceful degradation — agent continues with built-in tools only.

### Tool Naming

```go
func mcpToolName(server, tool string) string {
    r := strings.NewReplacer("-", "_", ".", "_", " ", "_")
    name := "mcp_" + r.Replace(server) + "_" + r.Replace(tool)
    if len(name) > 64 { // OpenAI limit
        // truncate server to 20, tool to 40, add hash suffix
    }
    return name
}
```

### Security

- Subprocess inherits only explicitly configured env vars
- Path must be absolute or PATH-resolvable
- `allowed_tools` whitelist prevents accidental exposure

### Stderr Handling

MCP server stderr captured to zerolog with server name prefix:
```
[mcp:internal-db] Failed to connect to database: timeout
```

### Scope Boundaries (v1)

- Tools only. No `resources`, no `prompts`.
- stdio transport only. HTTP/SSE deferred.

---

## 3. Context Generate (`foreman context generate`)

### Problem

The blank-slate AGENTS.md problem is the single biggest onboarding friction point. Users don't know what to write, write it poorly, and blame the tool.

### Commands

#### `foreman context generate`

LLM-first by default (matching Claude Code's `/init` approach). `--offline` flag for static-only fallback.

**Flow:**
1. Scan codebase (deterministic, tiered file selection)
2. Assemble scan results into structured prompt
3. LLM call (single, using configured provider)
4. Write `./AGENTS.md`

#### `foreman context update`

Post-merge learning loop. Reads observation log, updates AGENTS.md with newly discovered patterns.

**Flow:**
1. Read existing AGENTS.md
2. Read `.foreman/observations.jsonl` from cursor position
3. Single LLM call: "Update AGENTS.md with these observations"
4. Write updated AGENTS.md
5. Commit: `chore: update AGENTS.md from learned patterns`

### Tiered File Selection

```
Tier 1 (always):    go.mod, package.json, Cargo.toml, README.md,
                    CONTRIBUTING.md, existing AGENTS.md, .golangci.yml
Tier 2 (up to 10):  CI configs, Dockerfile, main entry points (main.go, index.ts)
Tier 3 (up to 20):  Key package/module files (one per top-level directory)
Tier 4 (remaining):  Fill to budget with highest-signal files by size heuristic
```

### Token Budget

```go
if estimatedTokens(prompt) > cfg.ContextGenerateMaxTokens {
    // drop Tier 4 files first, then Tier 3, until under budget
    trimToTokenBudget(&fileSet, cfg.ContextGenerateMaxTokens)
}
```

Config: `context_generate_max_tokens = 32000` in `[context]` section.

### System Prompt

```
You are generating an AGENTS.md for Foreman, a fully autonomous coding daemon.
This file is read by an LLM agent, not a human developer. Optimize for:
- Precise naming conventions (the agent will follow them literally)
- Exact test commands (the agent will run them verbatim)
- Explicit anti-patterns to avoid (the agent has no implicit human intuition)
- File organization rules (the agent must know where to create new files)
Omit marketing language, narrative prose, and generic best practices.
```

### AGENTS.md Output Sections

1. Project overview (language, framework, purpose)
2. Architecture summary (key packages/modules, entry points)
3. Coding conventions (naming, error handling, patterns)
4. Build & test commands (exact, copy-pasteable)
5. Key dependencies and their roles
6. File organization rules

### Observation Log

Pipeline appends structured entries to `.foreman/observations.jsonl` after each successful PR merge:

```json
{"type": "naming_correction", "original": "getUserData", "corrected": "fetchUser", "file": "api/users.go", "ts": "2026-03-05T10:00:00Z"}
{"type": "test_pattern", "description": "table-driven tests with t.Run subtests", "file": "api/users_test.go", "ts": "2026-03-05T10:00:00Z"}
{"type": "convention_discovered", "description": "errors wrapped with fmt.Errorf and %w verb", "ts": "2026-03-05T10:00:00Z"}
```

### Update Cursor

Footer marker in AGENTS.md:
```html
<!--foreman:last-update:2026-03-05T10:00:00Z:observations-cursor:142-->
```

Byte offset into observations.jsonl for resumable reads without timestamp parsing.

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--offline` | false | Static analysis only, no LLM call |
| `--dry-run` | false | Print to stdout, don't write file |
| `--force` | false | Overwrite existing AGENTS.md without confirmation |
| `--output` | `./AGENTS.md` | Custom output path |

### Non-Interactive Safety

Detect `!isatty(os.Stdin)`: print to stdout + exit non-zero with message to use `--force`. No silent overwrites in CI.

### Configuration

```toml
[context]
context_generate_max_tokens = 32000  # token budget for LLM prompt
```

### Integration with `foreman init`

Wire `context generate` into the existing `--analyze` flag in `cmd/init.go` (currently a TODO stub).

---

## Implementation Order

1. **Parallel DAG Executor** — highest standalone value, unblocks production use
2. **MCP Client** — builds on parallel execution (concurrent MCP calls need it), unlocks enterprise narrative
3. **Context Generate** — needs working LLM pipeline (already exists), observation log needs pipeline changes

## Dependencies Between Features

- DAG executor is independent, implement first
- MCP client benefits from DAG executor (concurrent tool calls) but doesn't require it
- Context generate is fully independent but observation log touches the pipeline, so implement after DAG changes settle
- `context update` depends on observation log infrastructure, ship as a fast-follow after `context generate`
