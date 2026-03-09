## Project Overview

Foreman is an autonomous software development daemon written in Go (1.25+). It polls issue trackers (Jira, GitHub Issues, Linear), decomposes tickets into tasks, generates code via LLM using TDD, runs reviews, and creates pull requests — all autonomously.

**Status:** Core pipeline, daemon, tracker integrations, LLM providers, agent runner, skills engine, ticket decomposition, and PR merge lifecycle are all implemented and working.

## Build & Development Commands

```bash
make build                            # Build dashboard assets + binary
go build -o foreman ./main.go        # Build binary (after dashboard assets exist)
go test ./...                         # Run all tests
go test ./internal/pipeline/...       # Run tests for a specific package
go test -run TestPlanValidator ./internal/pipeline/  # Run a single test
go vet ./...                          # Static analysis
golangci-lint run                     # Lint (requires golangci-lint)
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
| `envloader` | `.env` file parser — loads vars into process environment, copies files into worktrees |
| `agent` | Agent runner interface + builtin runner (parallel tool execution, context injection), Claude Code runner, Copilot runner |
| `agent/tools` | Typed tool registry: Read, ReadRange, Write, Edit, MultiEdit, ApplyPatch, ListDir, Glob, Grep, GetDiff, GetCommitLog, TreeSummary, GetSymbol, GetErrors, get_type_definition, semantic_search, Bash, RunTest, Subagent, ListMCPTools, ReadMCPResource |
| `agent/mcp` | MCP Manager, StdioClient (JSON-RPC 2.0 over stdin/stdout), tool name normalization; MCPServerConfig for Anthropic API-side MCP |
| `skills` | YAML skill engine for extensible pipeline hooks — subskill composition, output_format validation, ContextProvider |
| `prompts` | Unified prompt registry — loads ROLE.md/AGENT.md/SKILL.md/COMMAND.md files, renders pongo2 templates, writes .claude/ structure |
| `dashboard` | Web UI server, REST API, WebSocket, bearer token auth |
| `telemetry` | Cost controller, Prometheus metrics, structured events |
| `models` | Domain models (Ticket, Task, LlmCall, pipeline states) |

### Pipeline Flow

```
Ticket → Decomposition Check (NeedsDecomposition) → [if large] Decompose into child tickets → await approval
         ↓ (normal or approved child)
         Clarification Check → Planning (LLM) → Plan Validation →
           Per-Task (parallel DAG, bounded by max_parallel_tasks):
             [Implement (TDD) → Lint → Spec Review → Quality Review → Commit] →
           Rebase → Full Test Suite → Final Review → PR Creation → awaiting_merge
```

**PR Merge Lifecycle:** A dedicated `MergeChecker` goroutine polls `awaiting_merge` tickets at a configurable interval, updates status to `merged` or `pr_closed`, fires `post_merge` skill hooks, and auto-closes parent tickets when all children merge.

Key constraints: max 8 LLM calls per task, tiered retry strategy, file reservations prevent parallel conflicts.

### Agent Runner

The `agent` package provides `AgentRunner`, an interface for executing bounded agent tasks. Three implementations:

- **builtin** — multi-turn tool-use loop over `llm.LlmProvider`; parallel tool execution via `errgroup`; 14 built-in tools; two-layer context injection (AGENTS.md pre-assembly + reactive `ContextProvider`)
- **claudecode** — delegates to the `claude` CLI binary
- **copilot** — delegates to the GitHub Copilot CLI

The builtin runner uses a `tools.Registry` that is constructed separately and wired via two-phase init to avoid circular dependencies with the `Subagent` tool.

### Ticket Decomposition

`pipeline.Decomposer` detects oversized tickets (`NeedsDecomposition`) and uses an LLM to generate 3–6 focused child ticket specs, creates them in the tracker with an approval label, and comments on the parent. Children are processed as independent tickets; when all children reach `merged` status the parent is automatically closed.

Config key: `[decompose]` in `foreman.toml` — `enabled`, `max_ticket_words`, `max_scope_keywords`, `approval_label`, `parent_label`.

### YAML Skills

Extensible workflow hooks in `skills/` — composable YAML files triggered at `post_lint`, `pre_pr`, `post_pr`, or `post_merge` hook points.

Step types: `llm_call`, `run_command`, `file_write`, `git_diff`, `agentsdk`, `subskill`.

`agentsdk` steps support `output_format` (json/diff/checklist), `output_schema`, and `fallback_model`.

### Prompt Registry

All prompts, agent definitions, skills, and commands live as Markdown files with YAML frontmatter in `prompts/`:

```
prompts/
  roles/*/ROLE.md          # Pipeline stage system prompts (7 roles)
  agents/*/AGENT.md        # Claude Code agent personas (.claude/agents/)
  skills/*/SKILL.md        # Workflow hooks (replacing skills/*.yml)
  commands/*/COMMAND.md    # Claude Code slash commands (.claude/commands/)
  fragments/*.md           # Reusable prompt pieces (included by roles/agents)
```

A Go registry (`internal/prompts`) loads them at startup, resolves includes, and renders pongo2 templates. Builtin runner calls `registry.Render()` for prompt strings. Claude Code runner calls `registry.ForClaude()` to write the `.claude/` directory structure.

The registry is loaded from `prompts_dir` in `foreman.toml` (default: `prompts/`). If the directory doesn't exist, registry is nil and components fall back to legacy behavior.

**File formats:**
- **ROLE.md**: YAML frontmatter (name, description, model_hint, max_tokens, temperature, includes) + pongo2 template body
- **AGENT.md**: YAML frontmatter (name, description, mode, tools) + markdown body
- **SKILL.md**: YAML frontmatter (name, description, trigger, steps) + optional body
- **COMMAND.md**: YAML frontmatter (name, description) + command prompt body
- **Fragment .md**: No frontmatter, plain markdown, included via `{% include "fragments/name.md" %}`

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
- **Concurrency:** golang.org/x/sync/errgroup (parallel tool execution)
