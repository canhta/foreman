## Project Overview

Foreman is an autonomous software development daemon written in Go (1.23+). It polls issue trackers (Jira, GitHub Issues, Linear), decomposes tickets into tasks, generates code via LLM using TDD, runs reviews, and creates pull requests — all autonomously.

**Status:** Core pipeline, daemon, tracker integrations, LLM providers, agent runner, and skills engine are all implemented and working.

## Build & Development Commands

```bash
go build -o foreman ./main.go        # Build binary
go test ./...                         # Run all tests
go test ./internal/pipeline/...       # Run tests for a specific package
go test -run TestPlanValidator ./internal/pipeline/  # Run a single test
go vet ./...                          # Static analysis
golangci-lint run                     # Lint (requires golangci-lint)
make build                            # Build via Makefile
make test                             # Test via Makefile
```

CGO is required (go-sqlite3). Ensure a C toolchain is available.

## Architecture

### Core Design Principles

1. **Stateless LLM calls** — Every LLM call receives fully assembled context; state persists in git + database only. No accumulated memory between calls.
2. **Deterministic scaffolding** — Git ops, linting, tests, PR creation are deterministic Go code. LLM handles only judgment tasks (coding, reviewing, planning).
3. **Pluggable interfaces** — LLM provider, issue tracker, git host, and command runner are all Go interfaces in `internal/`. Implementations are swappable.

### Package Structure (`internal/`)

| Package | Purpose |
|---------|---------|
| `config` | TOML config loading via Viper |
| `daemon` | 24/7 scheduler, file reservation, crash recovery |
| `db` | Database interface with SQLite (default) and PostgreSQL backends |
| `pipeline` | State machine orchestrator — planner, implementer, TDD verifier, reviewers (spec, quality, final), feedback loops |
| `context` | Context assembly — file selection, token budgets, secrets scanning, repo analysis |
| `llm` | LLM provider interface + implementations (Anthropic, OpenAI, OpenRouter, local) |
| `tracker` | Issue tracker interface + implementations (Jira, GitHub, Linear, local file) |
| `git` | Git operations interface — native CLI (default) and go-git fallback |
| `runner` | Command execution — local and Docker modes |
| `agent` | Agent runner interface + builtin runner (parallel tool execution, context injection), Claude Code runner, Copilot runner |
| `agent/tools` | Typed tool registry: Read, Write, Edit, MultiEdit, ListDir, Glob, Grep, GetDiff, GetCommitLog, TreeSummary, GetSymbol, GetErrors, Bash, RunTest, Subagent |
| `agent/mcp` | MCP stub — MCPServerConfig for Anthropic API-side MCP; Client interface for future client-side proxying |
| `skills` | YAML skill engine for extensible pipeline hooks — subskill composition, output_format validation, ContextProvider |
| `dashboard` | Web UI server, REST API, WebSocket, bearer token auth |
| `telemetry` | Cost controller, Prometheus metrics, structured events |
| `models` | Domain models (Ticket, Task, LlmCall, pipeline states) |

### Pipeline Flow

```
Ticket → Clarification Check → Planning (LLM) → Plan Validation →
  Per-Task: [Implement (TDD) → Lint → Spec Review → Quality Review → Commit] →
  Rebase → Full Test Suite → Final Review → PR Creation
```

Key constraints: max 8 LLM calls per task, tiered retry strategy, file reservations prevent parallel conflicts.

### Agent Runner

The `agent` package provides `AgentRunner`, an interface for executing bounded agent tasks. Three implementations:

- **builtin** — multi-turn tool-use loop over `llm.LlmProvider`; parallel tool execution via `errgroup`; 14 built-in tools; two-layer context injection (AGENTS.md pre-assembly + reactive `ContextProvider`)
- **claudecode** — delegates to the `claude` CLI binary
- **copilot** — delegates to the GitHub Copilot CLI

The builtin runner uses a `tools.Registry` that is constructed separately and wired via two-phase init to avoid circular dependencies with the `Subagent` tool.

### YAML Skills

Extensible workflow hooks in `skills/` — composable YAML files triggered at `post_lint`, `pre_pr`, or `post_pr` hook points.

Step types: `llm_call`, `run_command`, `file_write`, `git_diff`, `agentsdk`, `subskill`.

`agentsdk` steps support `output_format` (json/diff/checklist), `output_schema`, and `fallback_model`.

### Project Context Injection

Add an `AGENTS.md` at the repo root (or `.foreman/context.md` for Foreman-specific cached content). Foreman injects it into every agent call's system prompt automatically — for all three runner implementations.

### Configuration

TOML config (`foreman.toml`) with `${ENV_VAR}` substitution for secrets. Key sections: `daemon`, `tracker`, `git`, `llm`, `limits`, `secrets`, `runner`, `dashboard`, `agent_runner`.

## Coding Conventions

- **Go module:** `github.com/canhta/foreman`
- **Interface-first design:** Every external dependency (LLM, tracker, git, runner, database) is behind a Go interface in `internal/`. Implementations are swappable.
- **Error handling:** Wrap errors with `fmt.Errorf("context: %w", err)` for stack traces. Return errors, don't panic.
- **Logging:** Use `zerolog` exclusively. Structured JSON logging with contextual fields.
- **All code in `internal/`:** Nothing is exported outside the module except `cmd/` (CLI) and `main.go`.
- **Config via Viper:** TOML config with environment variable substitution (`${ENV_VAR}`).
- **Tool schemas:** Hand-written JSON Schema in tool implementations — no reflection dependency.

## Key Dependencies

- **CLI:** cobra + viper
- **Database:** go-sqlite3 (CGo required), pgx (PostgreSQL), sqlx
- **Git:** native CLI (default), go-git (fallback)
- **Templates:** pongo2 (Jinja2-compatible)
- **Logging:** zerolog
- **Metrics:** prometheus/client_golang
- **HTTP:** stdlib net/http + gorilla/websocket
- **Tokens:** tiktoken-go
- **Concurrency:** golang.org/x/sync/errgroup (parallel tool execution)
