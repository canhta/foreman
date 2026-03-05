# Architecture

## Overview

Foreman is a single Go binary structured as a daemon with a pluggable, interface-first core. Every external dependency — LLM provider, issue tracker, git host, command runner, database — is hidden behind a Go interface. Implementations are swappable without touching the pipeline.

```
┌──────────────────────────────────────────────────────────┐
│                        DAEMON                             │
│  Runs 24/7. Polls issue tracker. Manages goroutine pool.  │
│  File reservation layer. Crash recovery.                  │
└──────────────┬───────────────────────────────────────────┘
               │  (up to N parallel)
    ┌──────────┴──────────┐
    ▼                     ▼
┌──────────┐       ┌──────────┐
│ Pipeline │       │ Pipeline │
│ Ticket A │       │ Ticket B │  ...
└──────┬───┘       └──────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────┐
│                    PIPELINE (per ticket)                  │
│                                                          │
│  Issue Sync → Context Assembly → Planner → Validator     │
│    → Per-Task [Implement → TDD → Lint → Reviews → Commit]│
│    → Rebase → Full Tests → Final Review → PR             │
│                                                          │
│  LLM Router ───────────────────────────────────────────  │
│  Cost Controller ──────────────────────────────────────  │
└─────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────┐
│    PERSISTENCE: SQLite (default) or PostgreSQL           │
│    Git: code changes only. Filesystem: repo clones.      │
└──────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────┐
│    DASHBOARD: HTTP Server, REST API, WebSocket, Auth     │
└──────────────────────────────────────────────────────────┘
```

## Design Principles

### 1. Stateless LLM Calls
Every LLM call receives fully assembled context reconstructed from the database and repository. There is no accumulated conversation memory between calls. State lives in git history and the database. Retries are explicitly managed by deterministic code, not by LLM memory.

### 2. Deterministic Scaffolding
Git operations, linting, test execution, PR creation, and issue tracker updates are deterministic Go code. The LLM is used only for tasks that require judgment — planning, coding, reviewing. This keeps costs predictable and makes failures debuggable.

### 3. Fresh Context Per Call
Every LLM call starts with zero memory. Context is reconstructed from structured handoffs stored in the database, git history, and the current repository state. This avoids hallucinated state from previous calls.

### 4. Pluggable Everything
All external dependencies are behind Go interfaces. You can swap the LLM provider, issue tracker, git backend, and command runner without modifying the pipeline.

### 5. Graceful Degradation
Partial success is better than total failure. If 4 of 5 tasks succeed, Foreman creates a partial PR with the completed work. If a provider is down, the pipeline pauses and retries on the next poll cycle instead of failing the ticket.

---

## Package Structure

```
internal/
├── config/         TOML config loading, validation, env-var substitution
├── daemon/         Event loop, scheduler, file reservations, crash recovery
├── db/             Database interface + SQLite and PostgreSQL implementations
├── pipeline/       State machine orchestrator — all pipeline stages
├── context/        Context assembly: file selection, token budgets, secrets scanning
├── llm/            LLM provider interface + Anthropic, OpenAI, OpenRouter, local
├── tracker/        Issue tracker interface + Jira, GitHub, Linear, local file
├── git/            Git operations interface + native CLI and go-git fallback
├── runner/         Command runner interface + local and Docker implementations
├── agent/          AgentRunner interface + builtin, claudecode, copilot runners
│   ├── tools/      Typed tool registry (14 tools) with parallel execution
│   └── mcp/        MCP server config and client stub
├── skills/         YAML skill engine, loader, hook executor
├── dashboard/      HTTP server, REST API, WebSocket, bearer token auth
├── telemetry/      Cost controller, Prometheus metrics, structured events
└── models/         Shared domain types: Ticket, Task, LlmCall, pipeline states
```

---

## Key Interfaces

All interfaces live in their respective packages under `internal/`.

### LlmProvider (`internal/llm`)

```go
type LlmProvider interface {
    Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error)
    ProviderName() string
    HealthCheck(ctx context.Context) error
}
```

`LlmRequest` supports single-turn (`UserPrompt`) and multi-turn (`Messages`) modes, tool definitions (`Tools []ToolDef`), structured output (`OutputSchema`), and optional extended thinking.

Implementations: `anthropic.go`, `openai.go`, `openrouter.go`, `local.go`.

### IssueTracker (`internal/tracker`)

```go
type IssueTracker interface {
    FetchReadyTickets(ctx context.Context) ([]Ticket, error)
    GetTicket(ctx context.Context, externalID string) (*Ticket, error)
    UpdateStatus(ctx context.Context, externalID, status string) error
    AddComment(ctx context.Context, externalID, comment string) error
    AttachPR(ctx context.Context, externalID, prURL string) error
    AddLabel(ctx context.Context, externalID, label string) error
    RemoveLabel(ctx context.Context, externalID, label string) error
    HasLabel(ctx context.Context, externalID, label string) (bool, error)
    ProviderName() string
}
```

Implementations: `jira.go`, `github_issues.go`, `linear.go`, `local_file.go`.

### GitProvider (`internal/git`)

```go
type GitProvider interface {
    EnsureRepo(ctx context.Context, workDir string) error
    CreateBranch(ctx context.Context, workDir, branchName string) error
    Commit(ctx context.Context, workDir, message string) (sha string, err error)
    Diff(ctx context.Context, workDir, base, head string) (string, error)
    Push(ctx context.Context, workDir, branchName string) error
    RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error)
    CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error)
    FileTree(ctx context.Context, workDir string) ([]FileEntry, error)
    Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error)
    CheckFileOverlap(ctx context.Context, workDir, branchA string, filesB []string) ([]string, error)
}
```

Default implementation: native `git` CLI (`native.go`). Fallback: `go-git/v5` (`gogit.go`).

### CommandRunner (`internal/runner`)

```go
type CommandRunner interface {
    Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error)
    CommandExists(ctx context.Context, command string) bool
}
```

Implementations: `local.go` (host machine), `docker.go` (Docker container).

### Database (`internal/db`)

```go
type Database interface {
    // Tickets, Tasks, LLM calls, Handoffs, Progress patterns,
    // File reservations, Cost tracking, Events, Auth tokens
}
```

Implementations: `sqlite.go` (default, serialized writer for concurrency safety), `postgres.go`.

### AgentRunner (`internal/agent`)

```go
type AgentRunner interface {
    Run(ctx context.Context, req AgentRequest) (AgentResult, error)
    HealthCheck(ctx context.Context) error
    RunnerName() string
    Close() error
}
```

Implementations: `builtin.go`, `claudecode.go`, `copilot.go`.

---

## Data Flow

### 1. Daemon Startup
1. Load and validate `foreman.toml`
2. Open database, run schema migrations
3. Recover any interrupted pipelines from `last_completed_task_seq`
4. Start the HTTP dashboard server
5. Begin polling the issue tracker

### 2. Ticket Pickup
1. Fetch tickets with the pickup label from the issue tracker
2. Check each ticket's external ID against the database — skip duplicates and already-active tickets
3. Check file reservations — if the ticket's planned files conflict with an active pipeline, re-queue
4. Mark the ticket as `in_progress` in the database and the tracker

### 3. Context Assembly
For each LLM call, the context assembler:
1. Builds a repo file tree using `GitProvider.FileTree`
2. Scores and selects relevant files using import graph analysis, path proximity, and explicit `files_to_read` lists
3. Scans selected files for secrets and redacts or excludes matches
4. Loads directory-specific rules (`.foreman-rules.md` files walked from the target path)
5. Loads progress patterns from the database for the current ticket
6. Assembles everything into a prompt within the configured token budget, honoring priority tiers

### 4. Per-Task Execution
See [Pipeline](pipeline.md) for the detailed state machine.

### 5. PR Creation
1. Rebase onto the default branch
2. Run the full test suite
3. Call the final reviewer
4. Run `pre_pr` skill hooks
5. Push the branch and create a PR via `GitProvider.CreatePR`
6. Sync the PR URL to the issue tracker
7. Run `post_pr` skill hooks
8. Release file reservations

---

## Concurrency Model

### Goroutine Pool
The daemon runs up to `max_parallel_tickets` pipelines concurrently. Each pipeline is a goroutine. A shared rate limiter (token bucket using `golang.org/x/time/rate`) prevents all workers from hammering the LLM provider simultaneously.

### SQLite Serialized Writer
When using SQLite, all writes go through a single writer goroutine via a buffered channel. This prevents `SQLITE_BUSY` errors under concurrent load. Non-critical writes (events, metrics) are batched and flushed on a configurable interval. Reads go directly to the SQLite connection (WAL mode allows concurrent reads alongside a single writer).

**SQLite concurrency limit:** `max_parallel_tickets` is capped at 3 when using SQLite. Use PostgreSQL for higher concurrency.

### File Reservations
File reservations are stored in the database, not in memory. Before a pipeline begins, it inserts reservation rows for all files it plans to modify. If any row already exists (another pipeline has reserved the file), the ticket is re-queued. Reservations are released in a single transaction when the pipeline finishes.

---

## Technology Stack

| Layer | Technology |
|---|---|
| Language | Go 1.23+ |
| CLI framework | cobra + viper |
| Database (default) | SQLite via `go-sqlite3` (CGO required) |
| Database (optional) | PostgreSQL via `pgx/v5` |
| SQL extensions | `sqlx` |
| Git fallback | `go-git/v5` |
| LLM prompt templates | `pongo2` (Jinja2-compatible) |
| Token counting | `tiktoken-go` |
| Logging | `zerolog` (structured JSON) |
| Metrics | `prometheus/client_golang` |
| WebSocket | `gorilla/websocket` |
| HTTP | stdlib `net/http` |
| Rate limiting | `golang.org/x/time/rate` |
| Parallel tool execution | `golang.org/x/sync/errgroup` |
| Fuzzy matching (SEARCH/REPLACE) | `adrg/strutil` |
| HTTP client | `go-resty/resty` |
| Terminal color | `fatih/color` |
| UUID | `google/uuid` |
| YAML | `gopkg.in/yaml.v3` |
| TOML | `github.com/BurntSushi/toml` |
