# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Foreman is an autonomous software development daemon written in Go (1.23+). It polls issue trackers (Jira, GitHub Issues, Linear), decomposes tickets into tasks, generates code via LLM using TDD, runs reviews, and creates pull requests — all autonomously.

**Status:** Pre-implementation. The repository contains only a detailed technical specification at `docs/spec.md`. All architecture, interfaces, and implementation phases are defined there.

## Build & Development Commands

Once implemented, the project will use a Makefile. Expected commands:

```bash
go build -o foreman ./main.go        # Build binary
go test ./...                         # Run all tests
go test ./internal/pipeline/...       # Run tests for a specific package
go test -run TestPlanValidator ./internal/pipeline/  # Run a single test
go vet ./...                          # Static analysis
golangci-lint run                     # Lint
```

## Architecture

### Core Design Principles

1. **Stateless LLM calls** — Every LLM call receives fully assembled context; state persists in git + database only. No accumulated memory between calls.
2. **Deterministic scaffolding** — Git ops, linting, tests, PR creation are deterministic Go code. LLM handles only judgment tasks (coding, reviewing, planning).
3. **Pluggable interfaces** — LLM provider, issue tracker, git host, and command runner are all Go interfaces in `internal/`.

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
| `dashboard` | Web UI server, REST API, WebSocket, bearer token auth |
| `telemetry` | Cost controller, Prometheus metrics, structured events |
| `skills` | YAML skill engine for extensible pipeline hooks |
| `models` | Domain models (Ticket, Task, LlmCall, pipeline states) |

### Pipeline Flow

```
Ticket → Clarification Check → Planning (LLM) → Plan Validation →
  Per-Task: [Implement (TDD) → Lint → Spec Review → Quality Review → Commit] →
  Rebase → Full Test Suite → Final Review → PR Creation
```

Key constraints: max 8 LLM calls per task, tiered retry strategy, file reservations prevent parallel conflicts.

### Prompt Templates

LLM prompts live in `prompts/` using Jinja2/pongo2 syntax (`.md.j2` files). Each pipeline role has its own template with specific context variables.

### YAML Skills

Extensible workflow hooks in `skills/` — composable YAML files that run at `post_lint`, `pre_pr`, or `post_pr` hook points without code changes.

### Configuration

TOML config (`foreman.toml`) with `${ENV_VAR}` substitution for secrets. Key sections: `daemon`, `tracker`, `git`, `llm`, `limits`, `secrets`, `runner`, `dashboard`.

## Coding Conventions

- **Go module:** `github.com/canhta/foreman`
- **Interface-first design:** Every external dependency (LLM, tracker, git, runner, database) is behind a Go interface in `internal/`. Implementations are swappable.
- **Error handling:** Wrap errors with `fmt.Errorf("context: %w", err)` for stack traces. Return errors, don't panic.
- **Logging:** Use `zerolog` exclusively. Structured JSON logging with contextual fields.
- **All code in `internal/`:** Nothing is exported outside the module except `cmd/` (CLI) and `main.go`.
- **Config via Viper:** TOML config with environment variable substitution (`${ENV_VAR}`).

## Key Dependencies

- **CLI:** cobra + viper
- **Database:** go-sqlite3 (CGo required), pgx (PostgreSQL), sqlx
- **Git:** native CLI (default), go-git (fallback)
- **Templates:** pongo2 (Jinja2-compatible)
- **Logging:** zerolog
- **Metrics:** prometheus/client_golang
- **HTTP:** stdlib net/http + gorilla/websocket
- **Tokens:** tiktoken-go

## Implementation Phases

The spec defines a 6-phase build order (see `docs/spec.md` §16):
1. Core Execution (LLM provider + implementer + TDD)
2. Full Pipeline (orchestrator + planner + feedback loops)
3. Review + Quality + Skills
4. Daemon + Tracker integrations
5. Dashboard + Polish
6. Open Source readiness
