# Foreman — Autonomous Software Development Daemon

## Complete System Design Specification

> **Purpose of this document:** This is the complete technical specification for building Foreman. An AI coding agent (or human developer) should be able to read this document end-to-end and implement the entire system without additional context.

---

## 1. What is Foreman?

Foreman is an open-source, autonomous software development daemon written in Go. It continuously polls an issue tracker (Jira, GitHub Issues, Linear), picks up tickets, and produces tested, reviewed pull requests — minimizing human interaction between "ticket created" and "PR ready for review."

### 1.1 One-Liner

**An autonomous coding daemon that turns issue tracker tickets into tested, reviewed pull requests while you sleep.**

### 1.2 Core Philosophy

1. **Stateless LLM calls with explicit context reconstruction.** Every LLM call receives fully assembled context and produces a complete result. The system maintains state externally (SQLite/Postgres + git + database) and reconstructs context for each call, including retries. Each call is independent, but the system manages a structured feedback chain across calls. The retry loops with accumulated lint errors, spec rejection reasons, and quality complaints form an explicit conversation managed by deterministic code, not by LLM memory.
2. **Deterministic scaffolding.** Git operations, linting, test running, PR creation, issue tracker syncing — all deterministic Go code. The LLM only handles tasks that require judgment: planning, coding, reviewing. Deterministic code is cheaper, faster, and more predictable — save the model for the parts that actually require judgment.
3. **Fresh context per call.** Every LLM call starts with zero LLM memory. No accumulated context window. No hallucinated state from previous calls. Memory persists only through git history, database records, and structured handoffs.
4. **Quality through architecture.** Two-stage review (spec compliance + code quality), mechanically enforced TDD with failure-type discrimination, tiered feedback loops, pragmatic retry caps with absolute call limits, partial PR support. The system is designed so even a mediocre model produces decent output.
5. **Pluggable everything.** LLM provider, issue tracker, git host, command runner — all behind Go interfaces. Swap any component without touching the pipeline.
6. **Graceful degradation over atomic failure.** Partial success is better than total failure. If 4 of 5 tasks succeed, ship a partial PR and let a human finish the rest.

### 1.3 Inspirations & What We Take From Each

| Source | What we take | What we skip |
|---|---|---|
| **Stripe Minions** | Stateless LLM execution, context engineering as core product, deterministic scaffolding, tiered feedback (lint→test→escalate), 2-attempt cap per tier, isolated sandbox per task | Internal-only, Slack invocation, Goose fork, MCP/Toolshed 400+ tools |
| **Superpowers (obra)** | Two-stage review (spec + quality), granular task plans (2-5 min each), TDD enforcement with mechanical verification, fresh subagent per task, mandatory skill workflows | Interactive brainstorming, Claude Code plugin, human "go" trigger |
| **Antfarm** | YAML workflow definitions, SQLite state tracking, structured KEY:VALUE handoffs, issue tracker as task queue, retry-with-feedback | OpenClaw dependency, cron orchestration, TypeScript CLI |
| **ZeroClaw** | Single binary, interface-based swappable architecture, minimal resource footprint, security by default | Messaging channels, identity/persona system, AIEOS |
| **Claude Code** | Subagent pattern (fresh process per task), tool-use for file ops and bash | Interactive mode, single-session |
| **Codex** | Sandboxed execution, parallel task execution | OpenAI-locked, cloud-only |

---

## 2. Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                        DAEMON                             │
│  Runs 24/7 as systemd service or Docker container.        │
│  Go goroutines + channels. Polls issue tracker.           │
│  Manages concurrency pool (configurable parallel tickets).│
│  Shared rate limiter across all pipeline workers.         │
│  File reservation layer for parallel conflict prevention. │
│  Crash recovery: resumes from last committed task.        │
└──────────────┬───────────────────────────────────────────┘
               │
    ┌──────────┴──────────┐
    ▼                     ▼
┌──────────┐       ┌──────────┐       (up to N parallel)
│ Pipeline │       │ Pipeline │
│ Ticket A │       │ Ticket B │
└──────┬───┘       └──────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────┐
│                    PIPELINE (per ticket)                  │
│                                                          │
│  ┌─────────────┐                                        │
│  │ Issue Sync   │ ◄── Pull ticket details + comments    │
│  └──────┬──────┘     Push status, comments, PR links    │
│         │            Clarification requests if needed    │
│         ▼                                                │
│  ┌─────────────┐                                        │
│  │ Git Ops      │ ◄── Clone/pull, branch, commit, push  │
│  └──────┬──────┘     Rebase before PR creation          │
│         ▼                                                │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Secrets Scanner                                  │    │
│  │ Pre-flight scan before any file enters context:  │    │
│  │ - Pattern-match known secret formats              │    │
│  │ - Redact or skip matching files                   │    │
│  │ - Log security events                             │    │
│  └──────┬──────────────────────────────────────────┘    │
│         ▼                                                │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Context Assembler                                │    │
│  │ Builds surgical context per LLM call:            │    │
│  │ - Repo tree + relevant files (scored & ranked)    │    │
│  │ - Directory-specific rules/conventions            │    │
│  │ - Progress data (pruned, relevant patterns only)  │    │
│  │ - Token budget management with priority tiers     │    │
│  └──────┬──────────────────────────────────────────┘    │
│         ▼                                                │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Pipeline State Machine (Sequential)              │    │
│  │                                                  │    │
│  │  PLAN ─► VALIDATE ─► FOR EACH TASK: ─► PR       │    │
│  │                        │                         │    │
│  │                        ├─ Implement (TDD)         │    │
│  │                        ├─ Verify TDD (mechanical) │    │
│  │                        ├─ Lint + Test (determ.)    │    │
│  │                        ├─ Spec Review              │    │
│  │                        ├─ Quality Review           │    │
│  │                        ├─ Dep change detection     │    │
│  │                        └─ Commit + sync status    │    │
│  │                                                  │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌─────────────────────────────────────────────────┐    │
│  │ LLM Router                                       │    │
│  │ Stateless calls. Pluggable provider.              │    │
│  │ Routes to correct model per role.                 │    │
│  │ Shared rate limiter (token bucket) across workers.│    │
│  │ Tracks tokens + cost per call.                    │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Cost Controller                                   │    │
│  │ Per-ticket budget. Per-day budget. Monthly budget. │    │
│  │ Absolute cap on LLM calls per task.               │    │
│  │ Token-aware cost estimation at plan validation.    │    │
│  │ Real-time tracking. Alerts. Kill switch.           │    │
│  └──────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────────┐
│                     PERSISTENCE                          │
│  SQLite (default, max 3 parallel) or PostgreSQL          │
│  Serialized writer for SQLite with batched non-critical  │
│  Git: code changes only (no progress files)              │
│  Filesystem: repo clones, config                         │
└─────────────────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────────┐
│                   DASHBOARD (web UI)                     │
│  Real-time pipeline status. Cost tracking. Logs.         │
│  Bearer token authentication on all endpoints.           │
│  Built-in. HTTP server on configurable port.             │
└─────────────────────────────────────────────────────────┘
```

---

## 3. Technology Stack

### 3.1 Language: Go

**Why Go:**
- Single static binary. No runtime dependencies. Cross-compile trivially.
- Goroutines + channels = natural fit for concurrent pipeline workers.
- Fast compilation (<10s for full build). Fast startup (<5ms).
- Low memory footprint (~15-25MB idle).
- Excellent HTTP client/server in stdlib. No external web framework needed.
- Strong ecosystem for CLI tools (cobra), database (sqlx/pgx), and git operations.
- Easy for contributors — simpler learning curve than Rust.
- Battle-tested for daemon/infrastructure workloads (Docker, Kubernetes, Terraform all in Go).

**Module Dependencies:**

```go
// go.mod
module github.com/anthropics/foreman

go 1.23

require (
    // CLI
    github.com/spf13/cobra v1.8.0
    github.com/spf13/viper v1.18.0    // Config management with env var support

    // HTTP (stdlib net/http for server, this for client convenience)
    github.com/go-resty/resty/v2 v2.11.0

    // Database
    github.com/mattn/go-sqlite3 v1.14.22    // SQLite driver (CGo)
    github.com/jackc/pgx/v5 v5.5.0          // PostgreSQL driver (optional)
    github.com/jmoiron/sqlx v1.3.5          // SQL extensions

    // Git (fallback only — native git CLI is the default)
    github.com/go-git/go-git/v5 v5.12.0    // Pure Go git, used when native git unavailable

    // Template rendering (for prompts)
    github.com/flosch/pongo2/v6 v6.0.0     // Jinja2-compatible templates

    // Token counting (approximate)
    github.com/pkoukk/tiktoken-go v0.1.6

    // Logging
    github.com/rs/zerolog v1.32.0

    // UUID
    github.com/google/uuid v1.6.0

    // Rate limiting
    golang.org/x/time v0.5.0               // rate.Limiter (token bucket)

    // Metrics
    github.com/prometheus/client_golang v1.18.0

    // WebSocket (for dashboard live updates)
    github.com/gorilla/websocket v1.5.1

    // Color terminal output
    github.com/fatih/color v1.16.0

    // YAML parsing
    gopkg.in/yaml.v3 v3.0.1

    // TOML parsing
    github.com/BurntSushi/toml v1.3.2

    // Fuzzy string matching (for SEARCH/REPLACE tolerance)
    github.com/adrg/strutil v0.3.1
)
```

### 3.2 Project Structure

```
foreman/
├── go.mod
├── go.sum
├── main.go                            # Entry point
├── README.md
├── LICENSE-MIT
├── LICENSE-APACHE
├── CONTRIBUTING.md
├── CHANGELOG.md
├── Makefile                           # Build, test, lint, release targets
├── Dockerfile                         # Multi-stage build
├── docker-compose.yml
├── foreman.example.toml               # Example config
│
├── cmd/                               # CLI commands (cobra)
│   ├── root.go                        # Root command + global flags
│   ├── start.go                       # foreman start [--daemon]
│   ├── stop.go                        # foreman stop
│   ├── status.go                      # foreman status
│   ├── run.go                         # foreman run "PROJ-123" [--dry-run]
│   ├── ps.go                          # foreman ps [--all]
│   ├── logs.go                        # foreman logs [--follow] [TICKET]
│   ├── cost.go                        # foreman cost today|week|month|ticket
│   ├── dashboard.go                   # foreman dashboard [--port]
│   ├── doctor.go                      # foreman doctor
│   ├── init.go                        # foreman init [--analyze]
│   └── token.go                       # foreman token generate
│
├── internal/
│   ├── config/
│   │   ├── config.go                  # Config struct + loading + validation
│   │   └── config_test.go
│   │
│   ├── daemon/
│   │   ├── daemon.go                  # 24/7 event loop, goroutine pool
│   │   ├── scheduler.go              # Ticket prioritization + conflict check
│   │   ├── file_lock.go              # File reservation layer for parallel tickets
│   │   ├── recovery.go               # Crash recovery: resume from last committed task
│   │   └── daemon_test.go
│   │
│   ├── db/
│   │   ├── db.go                      # Database interface
│   │   ├── sqlite.go                  # SQLite impl + serialized writer + batch flush
│   │   ├── postgres.go                # PostgreSQL implementation
│   │   ├── schema.go                  # Schema creation + migrations
│   │   └── db_test.go
│   │
│   ├── pipeline/
│   │   ├── pipeline.go                # Pipeline orchestrator (state machine)
│   │   ├── planner.go                 # Plan step: ticket → tasks
│   │   ├── plan_validator.go          # Validate planner output before execution
│   │   ├── implementer.go            # Implement step: task → code (TDD)
│   │   ├── tdd_verifier.go           # Mechanical TDD verification w/ failure-type discrimination
│   │   ├── spec_reviewer.go          # Spec review: diff → pass/fail
│   │   ├── quality_reviewer.go       # Quality review: diff → pass/fail
│   │   ├── final_reviewer.go         # Final review: full diff → approve/reject
│   │   ├── feedback.go               # Tiered feedback: lint → test → retry
│   │   ├── output_parser.go          # Robust LLM output parser with fallbacks
│   │   ├── yaml_parser.go            # Robust YAML parser with fallback chain
│   │   ├── dep_detector.go           # Detect package file changes between tasks
│   │   └── pipeline_test.go
│   │
│   ├── context/
│   │   ├── assembler.go              # Context assembler orchestrator
│   │   ├── repo_analyzer.go          # Repo tree, stack detection, pattern detection
│   │   ├── file_selector.go          # Scored file selection (import graph + proximity)
│   │   ├── token_budget.go           # Token counting + budget management
│   │   ├── rules.go                  # Directory-specific rules/conventions
│   │   ├── progress.go               # Progress data management with pruning (DB-backed)
│   │   ├── secrets_scanner.go        # Pre-flight secrets detection and redaction
│   │   └── context_test.go
│   │
│   ├── llm/
│   │   ├── provider.go               # LLM provider interface + router
│   │   ├── anthropic.go              # Claude API (Sonnet, Haiku, Opus)
│   │   ├── openai.go                 # OpenAI API (GPT-4o, o1, o3)
│   │   ├── openrouter.go             # OpenRouter (any model)
│   │   ├── local.go                  # Ollama / any OpenAI-compatible local server
│   │   ├── cost.go                   # Token tracking + cost calculation
│   │   ├── ratelimiter.go            # Shared rate limiter (token bucket + backoff)
│   │   └── llm_test.go
│   │
│   ├── tracker/
│   │   ├── tracker.go                # Issue tracker interface
│   │   ├── jira.go                   # Jira Cloud/Server API
│   │   ├── github_issues.go          # GitHub Issues API
│   │   ├── linear.go                 # Linear API
│   │   ├── local_file.go             # Local file-based tracker (for testing/dev)
│   │   └── tracker_test.go
│   │
│   ├── git/
│   │   ├── git.go                    # Git operations interface
│   │   ├── native.go                 # Native git CLI implementation (default)
│   │   ├── gogit.go                  # go-git fallback for environments without git CLI
│   │   ├── pr.go                     # PR creation (GitHub, GitLab, Bitbucket)
│   │   └── git_test.go
│   │
│   ├── runner/
│   │   ├── runner.go                  # Command runner interface
│   │   ├── local.go                   # Run commands on local machine
│   │   ├── docker.go                  # Run commands in Docker container
│   │   ├── output_parser.go          # Parse lint/test output into structured results
│   │   └── runner_test.go
│   │
│   ├── dashboard/
│   │   ├── server.go                  # HTTP server (net/http)
│   │   ├── api.go                     # REST API endpoints
│   │   ├── auth.go                    # Bearer token authentication
│   │   ├── ws.go                      # WebSocket for live updates
│   │   └── dashboard_test.go
│   │
│   ├── telemetry/
│   │   ├── cost_controller.go         # Budget enforcement + alerts
│   │   ├── metrics.go                 # Prometheus-compatible metrics
│   │   └── events.go                  # Structured event log
│   │
│   ├── skills/
│   │   ├── engine.go                  # YAML skill interpreter (~500 LOC)
│   │   ├── loader.go                  # Load + validate skill files from skills/
│   │   ├── hooks.go                   # Pipeline hook point execution (post_lint, pre_pr, post_pr)
│   │   └── skills_test.go
│   │
│   └── models/
│       ├── ticket.go                  # Ticket, Task, LlmCall structs
│       ├── pipeline.go               # PipelineState, StepResult enums
│       └── config.go                  # Config structs
│
├── prompts/                           # LLM prompt templates (pongo2/Jinja2 syntax)
│   ├── planner.md.j2
│   ├── implementer.md.j2
│   ├── implementer_retry.md.j2
│   ├── spec_reviewer.md.j2
│   ├── quality_reviewer.md.j2
│   ├── final_reviewer.md.j2
│   └── clarifier.md.j2
│
├── skills/                            # YAML skill definitions (composable pipeline extensions)
│   ├── feature-dev.yml                # Default: feature development workflow
│   ├── bug-fix.yml                    # Bug fixing workflow
│   ├── refactor.yml                   # Refactoring workflow
│   └── community/                     # Community-contributed skills (submitted via PR)
│       ├── write-changelog.yml
│       └── security-scan.yml
│
├── web/                               # Dashboard frontend (embedded via go:embed)
│   ├── index.html
│   ├── app.js
│   └── style.css
│
└── tests/
    ├── integration/
    │   ├── pipeline_test.go           # End-to-end pipeline tests
    │   ├── context_test.go            # Context assembly tests
    │   ├── file_selector_test.go      # File selection precision/recall tests
    │   └── llm_mock_test.go           # Tests with mock LLM responses
    └── fixtures/
        ├── sample_repo/               # Test repo for pipeline tests
        └── sample_tickets/            # Sample issue tracker JSON
```

---

## 4. Configuration

### 4.1 Config File: `foreman.toml`

```toml
[daemon]
poll_interval_secs = 60              # How often to check for new tickets
idle_poll_interval_secs = 300        # Poll interval when no work available
max_parallel_tickets = 3             # Concurrent pipelines (max 3 for SQLite)
work_dir = "~/.foreman/work"        # Where repos are cloned
log_level = "info"                   # trace, debug, info, warn, error
log_format = "json"                  # json or pretty

[dashboard]
enabled = true
port = 3333
host = "127.0.0.1"                  # Bind to localhost only by default
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"  # Required. Generate with `foreman token generate`

# ─── Issue Tracker ───────────────────────────────────────

[tracker]
provider = "jira"                    # jira | github | linear | local_file

[tracker.jira]
base_url = "https://yourcompany.atlassian.net"
email = "bot@yourcompany.com"
api_token = "${JIRA_API_TOKEN}"
project_key = "PROJ"
pickup_label = "foreman-ready"       # Label that triggers pickup
clarification_label = "foreman-needs-info"  # Applied when ticket lacks detail
clarification_timeout_hours = 72     # Abandon after 72h with no response
# Status mapping
status_in_progress = "In Progress"
status_in_review = "In Review"
status_done = "Done"
status_blocked = "Blocked"

[tracker.github]
owner = "your-org"
repo = "your-repo"
token = "${GITHUB_TOKEN}"
pickup_label = "foreman-ready"
clarification_timeout_hours = 72

[tracker.linear]
api_key = "${LINEAR_API_KEY}"
team_id = "TEAM_ID"
pickup_label = "foreman-ready"
clarification_timeout_hours = 72

# ─── Git ─────────────────────────────────────────────────

[git]
provider = "github"                  # github | gitlab | bitbucket
backend = "native"                   # native (git CLI, default) | gogit (pure Go fallback)
clone_url = "git@github.com:your-org/your-repo.git"
default_branch = "main"
auto_push = true
pr_draft = true
pr_reviewers = ["team-lead"]
branch_prefix = "foreman"            # foreman/PROJ-123-add-auth
rebase_before_pr = true              # Rebase onto latest default branch before PR

[git.github]
token = "${GITHUB_TOKEN}"

[git.gitlab]
token = "${GITLAB_TOKEN}"
base_url = "https://gitlab.com"

# ─── LLM Providers ──────────────────────────────────────

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"
base_url = "https://api.anthropic.com"

[llm.openai]
api_key = "${OPENAI_API_KEY}"
base_url = "https://api.openai.com"

[llm.openrouter]
api_key = "${OPENROUTER_API_KEY}"
base_url = "https://openrouter.ai/api"

[llm.local]
base_url = "http://localhost:11434"  # Ollama or any OpenAI-compatible server

# ─── LLM Provider Outage Behavior ───────────────────────
[llm.outage]
# When a provider is fully down (not rate-limited):
max_connection_retries = 3           # Retry connection failures
connection_retry_delay_secs = 30     # Delay between connection retries
fallback_provider = ""               # Optional: fallback to another provider ("openai", etc.)
# If all retries exhausted and no fallback: pause pipeline, emit provider_down event,
# retry on next daemon poll cycle. Do NOT fail the ticket.

# ─── Model Routing (per role) ───────────────────────────

[models]
# Format: "provider:model_name"
planner = "anthropic:claude-sonnet-4-5-20250929"
implementer = "anthropic:claude-sonnet-4-5-20250929"
spec_reviewer = "anthropic:claude-haiku-4-5-20251001"
quality_reviewer = "anthropic:claude-haiku-4-5-20251001"
final_reviewer = "anthropic:claude-sonnet-4-5-20250929"
clarifier = "anthropic:claude-haiku-4-5-20251001"

# ─── Cost Control ────────────────────────────────────────

[cost]
max_cost_per_ticket_usd = 15.00         # Abort + escalate if exceeded
max_cost_per_day_usd = 150.00           # Pause all pipelines if exceeded
max_cost_per_month_usd = 3000.00        # Hard stop
alert_threshold_percent = 80             # Alert at 80% of any budget
max_llm_calls_per_task = 8              # Absolute cap — prevents runaway retries

# Cost per 1M tokens (override defaults if pricing changes)
[cost.pricing]
"anthropic:claude-sonnet-4-5-20250929" = { input = 3.00, output = 15.00 }
"anthropic:claude-haiku-4-5-20251001" = { input = 0.80, output = 4.00 }
"openai:gpt-4o" = { input = 2.50, output = 10.00 }
"openai:o3-mini" = { input = 1.10, output = 4.40 }

# ─── Pipeline Limits ────────────────────────────────────

[limits]
max_tasks_per_ticket = 20               # Planner limit
max_implementation_retries = 2           # Per feedback tier
max_spec_review_cycles = 2
max_quality_review_cycles = 1
max_task_duration_secs = 600             # 10 min per task timeout
max_total_duration_secs = 7200           # 2 hour total per ticket
context_token_budget = 80000             # Max tokens per LLM call context
enable_partial_pr = true                 # Create PR with completed tasks on partial failure
enable_clarification = true              # Ask for clarification if ticket is ambiguous
enable_tdd_verification = true           # Mechanical TDD check (run tests without impl)
search_replace_similarity = 0.92         # Fuzzy match threshold for SEARCH blocks (0.0–1.0)
search_replace_min_context_lines = 3     # Minimum lines in each SEARCH block

# ─── Pipeline Hooks ──────────────────────────────────────

[pipeline.hooks]
# Each hook runs named skills from skills/. No subprocess, no external binary.
# Only three hook points — covers 90% of real use cases.
post_lint = []                           # After lint passes, before tests (e.g., ["security-scan"])
pre_pr    = []                           # Before PR creation (e.g., ["write-changelog"])
post_pr   = []                           # After PR created (e.g., ["notify-slack"])

# ─── Secrets Scanning ───────────────────────────────────

[secrets]
enabled = true
# Additional patterns beyond built-in defaults (regex)
extra_patterns = []
# Files to always exclude from LLM context
always_exclude = [".env", ".env.*", "*.pem", "*.key", "*.p12"]

# ─── Rate Limiting ───────────────────────────────────────

[rate_limit]
# Shared across all pipeline workers per provider
requests_per_minute = 50                 # Token bucket refill rate
burst_size = 10                          # Max burst
backoff_base_ms = 1000                   # Base delay on 429
backoff_max_ms = 60000                   # Max delay
jitter_percent = 25                      # Random jitter on retries

# ─── Execution Environment ──────────────────────────────

[runner]
mode = "local"                           # local | docker

[runner.docker]
image = "node:22-slim"                   # Default (override per repo in .foreman-context.md)
persist_per_ticket = true                # One container per ticket, not per task
network = "none"                         # Isolated by default
cpu_limit = "2.0"
memory_limit = "4g"
auto_reinstall_deps = true               # Detect package file changes and reinstall between tasks

[runner.local]
allowed_commands = ["npm", "yarn", "pnpm", "cargo", "go", "pytest", "make", "bun"]
forbidden_paths = [".env", ".ssh", ".aws", ".gnupg", "*.key", "*.pem"]

# ─── Database ────────────────────────────────────────────

[database]
driver = "sqlite"                        # sqlite | postgres

[database.sqlite]
path = "~/.foreman/foreman.db"
busy_timeout_ms = 5000                   # PRAGMA busy_timeout
wal_mode = true                          # PRAGMA journal_mode=WAL
event_flush_interval_ms = 100            # Batch non-critical writes (events, metrics)
# NOTE: max_parallel_tickets is capped at 3 for SQLite.
# Use PostgreSQL for higher concurrency.

[database.postgres]
url = "${DATABASE_URL}"                  # postgres://user:pass@host:5432/foreman
max_connections = 10
```

---

## 5. Database Schema

```sql
-- ─── Core Tables ────────────────────────────────────────

CREATE TABLE tickets (
    id TEXT PRIMARY KEY,                        -- UUID
    external_id TEXT NOT NULL UNIQUE,           -- "PROJ-123" or GitHub issue #
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT,
    labels TEXT,                                -- JSON array
    priority TEXT,
    status TEXT NOT NULL DEFAULT 'queued',
    -- Status enum: queued | clarification_needed | planning | plan_validating
    --   | implementing | reviewing | pr_created | done
    --   | partial | failed | blocked
    external_status TEXT,
    repo_url TEXT,
    branch_name TEXT,
    pr_url TEXT,
    pr_number INTEGER,
    is_partial BOOLEAN DEFAULT FALSE,          -- PR created with incomplete tasks
    -- Cost tracking
    cost_usd REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,
    -- Clarification tracking
    clarification_requested_at TIMESTAMP,      -- When clarification was asked
    -- Error context
    error_message TEXT,
    -- Crash recovery
    last_completed_task_seq INTEGER DEFAULT 0,  -- Sequence of last committed task
    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Tasks within a ticket (produced by planner)
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,                        -- UUID
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    sequence INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,          -- JSON array
    files_to_read TEXT,                         -- JSON array of file paths
    files_to_modify TEXT,                       -- JSON array of file paths
    test_assertions TEXT,                       -- JSON array
    estimated_complexity TEXT,                  -- simple | medium | complex
    depends_on TEXT,                            -- JSON array of task IDs (for future DAG)
    status TEXT NOT NULL DEFAULT 'pending',
    -- Status enum: pending | implementing
    --   | tdd_verifying | testing | spec_review | quality_review
    --   | done | failed | skipped
    implementation_attempts INTEGER DEFAULT 0,
    spec_review_attempts INTEGER DEFAULT 0,
    quality_review_attempts INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,         -- Absolute counter for cap enforcement
    commit_sha TEXT,
    cost_usd REAL DEFAULT 0.0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

-- Every LLM call (for auditing, debugging, cost tracking)
CREATE TABLE llm_calls (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),          -- NULL for planner/final reviewer
    role TEXT NOT NULL,                         -- planner | implementer | spec_reviewer | quality_reviewer | final_reviewer | clarifier
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    tokens_input INTEGER NOT NULL,
    tokens_output INTEGER NOT NULL,
    cost_usd REAL NOT NULL,
    duration_ms INTEGER NOT NULL,
    prompt_hash TEXT,                           -- SHA256 (not full prompt)
    response_summary TEXT,                      -- First 500 chars
    status TEXT NOT NULL,                       -- success | error | timeout | budget_exceeded | rate_limited
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Structured handoffs between pipeline steps (also stores progress data)
CREATE TABLE handoffs (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    from_role TEXT NOT NULL,
    to_role TEXT,                               -- NULL = available to all
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Progress patterns discovered during implementation (replaces progress.md in git)
CREATE TABLE progress_patterns (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    pattern_key TEXT NOT NULL,                  -- e.g., "import_style", "error_handling"
    pattern_value TEXT NOT NULL,                -- e.g., "ESM imports, no semicolons"
    directories TEXT,                           -- JSON array of directories where pattern applies
    discovered_by_task TEXT REFERENCES tasks(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- File reservations for parallel ticket conflict prevention
CREATE TABLE file_reservations (
    file_path TEXT NOT NULL,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    reserved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP,
    PRIMARY KEY (file_path, ticket_id)
);

-- Cost tracking aggregates
CREATE TABLE cost_daily (
    date TEXT PRIMARY KEY,                     -- YYYY-MM-DD
    total_usd REAL DEFAULT 0.0,
    total_input_tokens INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    ticket_count INTEGER DEFAULT 0,
    task_count INTEGER DEFAULT 0,
    llm_call_count INTEGER DEFAULT 0
);

-- Pipeline event log
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    ticket_id TEXT REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',      -- info | warn | error
    message TEXT NOT NULL,
    details TEXT,                               -- JSON blob
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Dashboard auth tokens
CREATE TABLE auth_tokens (
    token_hash TEXT PRIMARY KEY,                -- SHA256 of the bearer token
    name TEXT NOT NULL,                         -- Friendly name
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked BOOLEAN DEFAULT FALSE
);

-- ─── Indexes ────────────────────────────────────────────

CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_external_id ON tickets(external_id);
CREATE INDEX idx_tasks_ticket_id ON tasks(ticket_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_llm_calls_ticket_id ON llm_calls(ticket_id);
CREATE INDEX idx_llm_calls_created_at ON llm_calls(created_at);
CREATE INDEX idx_events_ticket_id ON events(ticket_id);
CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_cost_daily_date ON cost_daily(date);
CREATE INDEX idx_file_reservations_ticket ON file_reservations(ticket_id);
CREATE INDEX idx_file_reservations_released ON file_reservations(released_at);
CREATE INDEX idx_progress_patterns_ticket ON progress_patterns(ticket_id);
```

### 5.1 Event Types

```
ticket_picked_up           ticket_clarification_requested   ticket_clarification_received
ticket_clarification_timeout
ticket_planning            ticket_planned                   ticket_plan_validation_failed
ticket_too_large           task_started                     task_tdd_verify_pass
task_tdd_verify_fail       task_tdd_verify_invalid_red      task_lint_pass
task_lint_fail             task_test_pass                   task_test_fail
task_retry                 task_spec_review_pass            task_spec_review_fail
task_quality_pass          task_quality_fail                task_completed
task_failed                task_skipped                     task_llm_call_cap_reached
task_dep_change_detected   full_test_pass                   full_test_fail
rebase_success             rebase_conflict                  rebase_conflict_resolved
pr_created                 pr_created_partial               final_review_pass
final_review_fail          ticket_completed                 ticket_partial
ticket_failed              ticket_blocked                   cost_alert
cost_exceeded              rate_limit_hit                   rate_limit_backoff
provider_down              provider_recovered               secrets_detected
file_reservation_conflict  pipeline_resumed_after_crash
search_block_fuzzy_match   search_block_miss
hook_skill_not_found       hook_skill_failed            hook_skill_completed
```

---

## 6. Core Interfaces

### 6.1 LLM Provider Interface

```go
// LlmProvider is implemented by each LLM backend (Anthropic, OpenAI, etc.)
// Every call is stateless — no conversation memory.
type LlmProvider interface {
    // Complete executes a single stateless LLM call.
    Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error)
    // ProviderName returns the provider identifier for logging/config.
    ProviderName() string
    // HealthCheck verifies the provider is reachable and configured.
    HealthCheck(ctx context.Context) error
}

type LlmRequest struct {
    Model          string   `json:"model"`
    SystemPrompt   string   `json:"system_prompt"`
    UserPrompt     string   `json:"user_prompt"`
    MaxTokens      int      `json:"max_tokens"`
    Temperature    float64  `json:"temperature"`
    StopSequences  []string `json:"stop_sequences,omitempty"`
}

type LlmResponse struct {
    Content      string     `json:"content"`
    TokensInput  int        `json:"tokens_input"`
    TokensOutput int        `json:"tokens_output"`
    Model        string     `json:"model"`
    DurationMs   int64      `json:"duration_ms"`
    StopReason   StopReason `json:"stop_reason"`
}

type StopReason string
const (
    StopReasonEndTurn      StopReason = "end_turn"
    StopReasonMaxTokens    StopReason = "max_tokens"
    StopReasonStopSequence StopReason = "stop_sequence"
)

// LlmError types for structured error handling
type RateLimitError struct {
    RetryAfterSecs int
}
type AuthError struct{ Message string }
type TimeoutError struct{}
type BudgetExceededError struct {
    Current float64
    Limit   float64
}
type ConnectionError struct {
    Attempt int
    Err     error
}
```

### 6.2 Issue Tracker Interface

```go
// IssueTracker abstracts Jira, GitHub Issues, Linear, etc.
type IssueTracker interface {
    FetchReadyTickets(ctx context.Context) ([]Ticket, error)
    GetTicket(ctx context.Context, externalID string) (*Ticket, error)
    UpdateStatus(ctx context.Context, externalID string, status string) error
    AddComment(ctx context.Context, externalID string, comment string) error
    AttachPR(ctx context.Context, externalID string, prURL string) error
    AssignTicket(ctx context.Context, externalID string, assignee string) error
    AddLabel(ctx context.Context, externalID string, label string) error
    RemoveLabel(ctx context.Context, externalID string, label string) error
    // HasLabel checks if a label exists (used for clarification re-entry guard)
    HasLabel(ctx context.Context, externalID string, label string) (bool, error)
    ProviderName() string
}

type Ticket struct {
    ExternalID         string
    Title              string
    Description        string
    AcceptanceCriteria string          // May be empty — triggers clarification
    Labels             []string
    Priority           string
    Assignee           string
    Reporter           string
    Comments           []TicketComment
    CreatedAt          time.Time
    UpdatedAt          time.Time
}

type TicketComment struct {
    Author    string
    Body      string
    CreatedAt time.Time
}
```

### 6.3 Git Provider Interface

```go
// GitProvider abstracts git operations.
// Default implementation: native git CLI via CommandRunner.
// Fallback: go-git/v5 (pure Go) for environments without git installed.
type GitProvider interface {
    EnsureRepo(ctx context.Context, workDir string) error
    CreateBranch(ctx context.Context, workDir, branchName string) error
    Commit(ctx context.Context, workDir, message string) (sha string, err error)
    Diff(ctx context.Context, workDir, base, head string) (string, error)
    DiffWorking(ctx context.Context, workDir string) (string, error)
    Push(ctx context.Context, workDir, branchName string) error
    // RebaseOnto rebases current branch onto the target. Returns conflict info if any.
    RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error)
    CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error)
    FileTree(ctx context.Context, workDir string) ([]FileEntry, error)
    Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error)
    // CheckFileOverlap returns files modified on branchA that overlap with the given file list.
    CheckFileOverlap(ctx context.Context, workDir, branchA string, filesB []string) ([]string, error)
}

type RebaseResult struct {
    Success       bool
    ConflictFiles []string           // Files with conflicts (if !Success)
    ConflictDiff  string             // The conflict markers for LLM resolution attempt
}

type PrRequest struct {
    Title      string
    Body       string
    HeadBranch string
    BaseBranch string
    Draft      bool
    Reviewers  []string
    Labels     []string
}

type PrResponse struct {
    Number  int
    URL     string
    HTMLURL string
}

type FileEntry struct {
    Path      string
    IsDir     bool
    SizeBytes int64
}

type CommitEntry struct {
    SHA     string
    Message string
    Author  string
    Date    time.Time
}
```

### 6.4 Command Runner Interface

```go
type CommandRunner interface {
    Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error)
    CommandExists(ctx context.Context, command string) bool
}

type CommandOutput struct {
    Stdout    string
    Stderr    string
    ExitCode  int
    Duration  time.Duration
    TimedOut  bool
}
```

### 6.5 Database Interface

```go
// Database abstracts SQLite and PostgreSQL.
// SQLite implementation uses a serialized writer (single goroutine + channel)
// to avoid SQLITE_BUSY under concurrent pipeline load.
// Non-critical writes (events, metrics) are batched with a flush interval.
type Database interface {
    // Tickets
    CreateTicket(ctx context.Context, t *Ticket) error
    UpdateTicketStatus(ctx context.Context, id, status string) error
    GetTicket(ctx context.Context, id string) (*TicketRecord, error)
    GetTicketByExternalID(ctx context.Context, externalID string) (*TicketRecord, error)
    ListTickets(ctx context.Context, filter TicketFilter) ([]TicketRecord, error)
    SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error

    // Tasks
    CreateTasks(ctx context.Context, ticketID string, tasks []TaskRecord) error
    UpdateTaskStatus(ctx context.Context, id, status string) error
    IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)  // Returns new count

    // LLM calls
    RecordLlmCall(ctx context.Context, call *LlmCallRecord) error
    GetLlmCallCount(ctx context.Context, ticketID string) (int, error)

    // Handoffs
    SetHandoff(ctx context.Context, h *HandoffRecord) error
    GetHandoffs(ctx context.Context, ticketID, forRole string) ([]HandoffRecord, error)

    // Progress patterns (replaces file-based progress.md)
    SaveProgressPattern(ctx context.Context, p *ProgressPattern) error
    GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]ProgressPattern, error)

    // File reservations
    ReserveFiles(ctx context.Context, ticketID string, paths []string) error
    ReleaseFiles(ctx context.Context, ticketID string) error
    GetReservedFiles(ctx context.Context) (map[string]string, error)  // path → ticketID

    // Cost
    GetTicketCost(ctx context.Context, ticketID string) (float64, error)
    GetDailyCost(ctx context.Context, date string) (float64, error)
    GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
    RecordDailyCost(ctx context.Context, date string, amount float64) error

    // Events (batched for SQLite)
    RecordEvent(ctx context.Context, e *EventRecord) error
    GetEvents(ctx context.Context, ticketID string, limit int) ([]EventRecord, error)

    // Auth
    CreateAuthToken(ctx context.Context, tokenHash, name string) error
    ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error)
}
```

#### 6.5.1 SQLite Serialized Writer with Batched Non-Critical Writes

To prevent `SQLITE_BUSY` errors under concurrent pipeline load, the SQLite implementation uses a dedicated writer goroutine. Non-critical writes (events, metrics) are batched to reduce write pressure.

```go
// SqliteDB wraps sqlx.DB with a serialized write channel and a batch buffer.
type SqliteDB struct {
    db          *sqlx.DB
    writeCh     chan writeOp         // Critical writes go through this channel
    batchCh     chan *EventRecord    // Non-critical events buffered here
    flushTicker *time.Ticker         // Flush batch every N ms
    done        chan struct{}
}

type writeOp struct {
    fn       func(tx *sqlx.Tx) error
    resultCh chan error
}

// The writer goroutine processes critical writes sequentially.
func (s *SqliteDB) writerLoop() {
    for op := range s.writeCh {
        tx, _ := s.db.Beginx()
        err := op.fn(tx)
        if err != nil {
            tx.Rollback()
        } else {
            err = tx.Commit()
        }
        op.resultCh <- err
    }
}

// The batch goroutine flushes non-critical events periodically.
func (s *SqliteDB) batchLoop() {
    var buffer []*EventRecord
    for {
        select {
        case evt := <-s.batchCh:
            buffer = append(buffer, evt)
            if len(buffer) >= 50 { // Also flush when buffer hits 50
                s.flushEvents(buffer)
                buffer = buffer[:0]
            }
        case <-s.flushTicker.C:
            if len(buffer) > 0 {
                s.flushEvents(buffer)
                buffer = buffer[:0]
            }
        case <-s.done:
            if len(buffer) > 0 {
                s.flushEvents(buffer)
            }
            return
        }
    }
}

// Write submits a critical write operation and waits for completion.
func (s *SqliteDB) Write(fn func(tx *sqlx.Tx) error) error {
    resultCh := make(chan error, 1)
    s.writeCh <- writeOp{fn: fn, resultCh: resultCh}
    return <-resultCh
}

// Reads go directly to the DB (WAL mode allows concurrent reads).
func (s *SqliteDB) Read(fn func(db *sqlx.DB) error) error {
    return fn(s.db)
}
```

**Concurrency limits:** When using SQLite, `max_parallel_tickets` is capped at 3 at config validation time. For higher concurrency, PostgreSQL is required.

---

## 7. Pipeline State Machine

### 7.1 Full Pipeline Lifecycle

**V1 executes tasks in strict sequential topological order.** Parallel task execution within a ticket is deferred to post-V1. The `depends_on` field is validated during plan validation (no cycles allowed) and used for ordering, but all tasks run one at a time.

```
                    ┌──────────┐
                    │  QUEUED   │ ◄── Ticket pulled from tracker
                    └────┬─────┘
                         │
                         ▼
              ┌─────────────────────────┐
              │ FILE RESERVATION CHECK   │ ◄── Check planner's intended files
              └────┬──────────┬─────────┘     against active reservations
                   │          │
              free │     conflict → queue ticket, retry on next poll
                   │
                   ▼
              ┌─────────────────────┐
              │ CLARIFICATION CHECK  │ ◄── Is ticket detailed enough?
              └────┬──────────┬─────┘
                   │          │
              enough│     ambiguous
                   │          │
                   │          ▼
                   │   ┌───────────────┐
                   │   │ ASK CLARIFY    │ ◄── Comment on ticket, add label, wait
                   │   └───────┬───────┘     (timeout after clarification_timeout_hours)
                   │           │ (author responds, label removed)
                   │           │
                   ▼           ▼
              ┌──────────┐
              │ PLANNING  │ ◄── One-shot LLM: Planner
              └────┬─────┘
                   │
                   ▼
              ┌──────────────┐
              │ PLAN VALIDATE │ ◄── Deterministic: check file paths, DAG, budget
              └────┬─────────┘
                   │
              valid│    invalid → re-plan (max 1 retry) or escalate
                   │
                   ▼
              ┌──────────────────────┐
              │ RESERVE FILES         │ ◄── Lock planned files in DB
              └────┬─────────────────┘
                   │
                   ▼
        ┌─────────────────────┐
  ┌────►│    TASK (next in     │ ◄── Sequential topological order
  │     │    sequence)         │
  │     └────┬────────────────┘
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ SECRETS SCAN      │ ◄── Scan files before LLM context assembly
  │     └────┬─────────────┘
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ IMPLEMENT (TDD)   │ ◄── One-shot LLM: write test first, then code
  │     └────┬─────────────┘
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ TDD VERIFY        │ ◄── MECHANICAL: apply only test files, run tests,
  │     │ (if enabled)      │     confirm they FAIL for the right reason.
  │     └────┬─────────────┘     Then apply impl, confirm PASS.
  │          │
  │      pass│    fail → retry implementer with specific TDD feedback
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │  LINT + TEST      │ ◄── Deterministic: run linter (auto-fix), run tests
  │     └────┬─────────────┘
  │          │
  │      pass│    fail → retry implementer with error context (max 2)
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ HOOK: post_lint   │ ◄── Run skills (e.g., security-scan)
  │     └────┬─────────────┘     Hook failures logged but don't block pipeline
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ SPEC REVIEW       │ ◄── One-shot LLM: check acceptance criteria
  │     └────┬─────────────┘
  │          │
  │      pass│    fail → retry implementer with spec feedback (max 2 cycles)
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ QUALITY REVIEW    │ ◄── One-shot LLM: check code quality
  │     └────┬─────────────┘
  │          │
  │      pass│    fail → retry implementer with quality feedback (max 1 cycle)
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ COMMIT            │ ◄── Deterministic: git commit, update progress in DB
  │     └────┬─────────────┘
  │          │
  │          ▼
  │     ┌──────────────────┐
  │     │ DEP CHANGE CHECK  │ ◄── Diff package.json/go.mod/Cargo.toml etc.
  │     └────┬─────────────┘     If changed, run install command before next task
  │          │
  │     ┌────┴──────────────────────────────────────┐
  │     │ CHECK: absolute LLM call cap per task (8)  │
  │     │ If exceeded at any point → fail task        │
  │     └───────────────────────────────────────────┘
  │          │
  └──────────┘ (next task in sequence)
                   │ (all tasks done — or partial failure with enable_partial_pr)
                   ▼
              ┌──────────────────┐
              │ REBASE            │ ◄── Rebase onto latest default branch
              └────┬──────┬──────┘
                   │      │
              clean│   conflict
                   │      │
                   │      ▼
                   │   ┌──────────────────┐
                   │   │ AUTO-RESOLVE      │ ◄── LLM attempt to resolve conflicts
                   │   └───┬──────────────┘
                   │       │
                   │   resolved│   failed → create PR anyway with conflict warning
                   │       │
                   ▼       ▼
              ┌──────────────────┐
              │  FULL TESTS       │ ◄── Run entire test suite
              └────┬─────────────┘
                   │
               pass│    fail → mark failed (or partial PR)
                   │
                   ▼
              ┌──────────────────┐
              │ FINAL REVIEW      │ ◄── One-shot LLM: review complete diff
              └────┬─────────────┘
                   │
                   ▼
              ┌──────────────────┐
              │ HOOK: pre_pr      │ ◄── Run skills (e.g., write-changelog)
              └────┬─────────────┘
                   │
                   ▼
              ┌──────────────────┐
              │  CREATE PR        │ ◄── Git push, open PR (draft), sync tracker
              └────┬─────────────┘
                   │
                   ▼
              ┌──────────────────┐
              │ HOOK: post_pr     │ ◄── Run skills (e.g., notify-slack)
              └────┬─────────────┘
                   │
                   ▼
              ┌──────────────────┐
              │ RELEASE FILES     │ ◄── Remove file reservations from DB
              └────┬─────────────┘
                   │
                   ▼
              ┌──────────────────┐
              │   DONE            │
              └──────────────────┘
```

### 7.2 Absolute LLM Call Cap Per Task

Every task tracks `total_llm_calls` as an absolute counter. This counter increments on every LLM call for that task (implementer + spec reviewer + quality reviewer). When it hits `max_llm_calls_per_task` (default 8), the task fails immediately regardless of which review cycle triggered it.

```go
// Before every LLM call for a task:
func (p *Pipeline) checkTaskCallCap(taskID string) error {
    count, err := p.db.IncrementTaskLlmCalls(taskID)
    if err != nil {
        return err
    }
    if count > p.config.Limits.MaxLlmCallsPerTask {
        return &TaskCallCapError{
            TaskID: taskID,
            Count:  count,
            Limit:  p.config.Limits.MaxLlmCallsPerTask,
        }
    }
    return nil
}
```

### 7.3 Clarification Step

When a ticket lacks enough detail for the planner to decompose it (no acceptance criteria, vague description, missing context), the pipeline requests clarification instead of producing a bad plan. A timeout prevents tickets from lingering indefinitely.

```go
func (p *Pipeline) checkTicketClarity(ticket *Ticket) (bool, error) {
    if !p.config.Limits.EnableClarification {
        return true, nil  // Skip check, proceed anyway
    }

    // Quick heuristic checks first (no LLM needed):
    // 1. Description under 50 chars → too vague
    // 2. No acceptance criteria and no checklist in description
    // 3. Title is just a label ("auth", "bug", "fix")
    if len(ticket.Description) < 50 && ticket.AcceptanceCriteria == "" {
        return false, nil
    }

    // If heuristics pass but description is still ambiguous,
    // the planner will indicate this in its output with:
    // CLARIFICATION_NEEDED: <specific question>
    return true, nil
}

// When clarification is needed:
func (p *Pipeline) requestClarification(ticket *Ticket, question string) error {
    comment := fmt.Sprintf(
        "🤖 **Foreman needs clarification before starting:**\n\n%s\n\n"+
        "Please reply to this comment or update the ticket description, "+
        "then remove the `%s` label to resume.",
        question, p.config.Tracker.ClarificationLabel,
    )
    p.tracker.AddComment(ctx, ticket.ExternalID, comment)
    p.tracker.AddLabel(ctx, ticket.ExternalID, p.config.Tracker.ClarificationLabel)
    p.db.UpdateTicketStatus(ctx, ticket.ID, "clarification_needed")
    // Record the time so we can enforce the timeout
    p.db.SetClarificationRequestedAt(ctx, ticket.ID, time.Now())
    return nil
}

// During each poll cycle, check for timed-out clarifications:
func (p *Pipeline) checkClarificationTimeouts() {
    tickets, _ := p.db.ListTickets(ctx, TicketFilter{Status: "clarification_needed"})
    timeout := time.Duration(p.config.Tracker.ClarificationTimeoutHours) * time.Hour

    for _, t := range tickets {
        if t.ClarificationRequestedAt != nil && time.Since(*t.ClarificationRequestedAt) > timeout {
            p.tracker.AddComment(ctx, t.ExternalID, fmt.Sprintf(
                "🤖 No response received after %d hours. Marking as blocked. "+
                "Re-apply the `%s` label to retry after updating the ticket.",
                p.config.Tracker.ClarificationTimeoutHours, p.config.Tracker.PickupLabel,
            ))
            p.tracker.RemoveLabel(ctx, t.ExternalID, p.config.Tracker.ClarificationLabel)
            p.db.UpdateTicketStatus(ctx, t.ID, "blocked")
            p.db.RecordEvent(ctx, &EventRecord{
                TicketID: t.ID, EventType: "ticket_clarification_timeout",
                Severity: "warn", Message: "Clarification timed out",
            })
        }
    }
}

// Re-entry guard: during pickup, skip tickets that are already in clarification_needed
// status in the database, even if the label was accidentally re-added.
func (p *Pipeline) shouldPickUp(externalID string) bool {
    existing, err := p.db.GetTicketByExternalID(ctx, externalID)
    if err != nil {
        return true // New ticket, safe to pick up
    }
    if existing.Status == "clarification_needed" {
        // Only pick up if the clarification label has been removed
        // (meaning the author has responded)
        hasLabel, _ := p.tracker.HasLabel(ctx, externalID, p.config.Tracker.ClarificationLabel)
        return !hasLabel
    }
    return existing.Status == "queued" // Don't double-pick active tickets
}
```

### 7.4 Plan Validation

After the planner produces its output, a deterministic validation step catches bad plans before they cascade into expensive implementation failures. Cost estimation is token-aware, not a flat per-call average.

```go
type PlanValidation struct {
    Valid    bool
    Errors   []string    // Fatal errors that block execution
    Warnings []string    // Non-fatal issues
}

func ValidatePlan(plan *Plan, workDir string, git GitProvider, config *Config) *PlanValidation {
    v := &PlanValidation{Valid: true}

    // 1. Check all referenced file paths exist (or are explicitly marked "new")
    for _, task := range plan.Tasks {
        for _, path := range task.FilesToRead {
            if !fileExists(workDir, path) {
                v.addError("Task '%s' references non-existent file: %s", task.Title, path)
            }
        }
        for _, path := range task.FilesToModify {
            if !strings.HasSuffix(path, "(new)") && !fileExists(workDir, path) {
                v.addError("Task '%s' modifies non-existent file: %s", task.Title, path)
            }
        }
    }

    // 2. Validate task dependency DAG (no cycles)
    if hasCycle(plan.Tasks) {
        v.addError("Task dependencies contain a cycle")
    }

    // 3. Check no two tasks modify the same file without explicit ordering
    fileOwners := map[string][]string{}  // path → [task titles]
    for _, task := range plan.Tasks {
        for _, path := range task.FilesToModify {
            fileOwners[path] = append(fileOwners[path], task.Title)
        }
    }
    for path, owners := range fileOwners {
        if len(owners) > 1 {
            if !hasOrderingBetween(plan.Tasks, owners) {
                v.addWarning("Multiple tasks modify '%s' without explicit ordering: %v", path, owners)
            }
        }
    }

    // 4. Token-aware cost estimation
    //    Instead of a flat per-call average, estimate based on context budget and model pricing.
    estimatedCost := estimateTicketCost(plan, config)
    if estimatedCost > config.Cost.MaxCostPerTicketUSD * 0.5 {
        v.addWarning("Estimated cost $%.2f exceeds 50%% of budget limit $%.2f",
            estimatedCost, config.Cost.MaxCostPerTicketUSD)
        // Emit a cost_alert event (not just a warning)
    }
    if estimatedCost > config.Cost.MaxCostPerTicketUSD * 0.8 {
        v.addError("Estimated cost $%.2f exceeds 80%% of budget limit $%.2f — plan is too expensive",
            estimatedCost, config.Cost.MaxCostPerTicketUSD)
    }

    // 5. Check task count doesn't exceed limit
    if len(plan.Tasks) > config.Limits.MaxTasksPerTicket {
        v.addError("Plan has %d tasks, exceeding limit of %d",
            len(plan.Tasks), config.Limits.MaxTasksPerTicket)
    }

    return v
}

// estimateTicketCost uses token budgets and model pricing for realistic estimates.
func estimateTicketCost(plan *Plan, config *Config) float64 {
    var total float64
    contextBudget := float64(config.Limits.ContextTokenBudget)
    expectedRetryFactor := 1.5  // Empirical: ~50% of tasks need at least one retry

    for _, task := range plan.Tasks {
        // Estimate context size based on complexity
        var inputTokens float64
        switch task.EstimatedComplexity {
        case "simple":
            inputTokens = contextBudget * 0.3
        case "medium":
            inputTokens = contextBudget * 0.5
        default: // complex
            inputTokens = contextBudget * 0.7
        }
        outputTokens := 4000.0 // Average implementer output

        // Calls per task: implementer + spec_reviewer + quality_reviewer
        implPrice := getModelPricing(config, config.Models.Implementer)
        specPrice := getModelPricing(config, config.Models.SpecReviewer)
        qualPrice := getModelPricing(config, config.Models.QualityReviewer)

        taskCost := (inputTokens/1e6)*implPrice.Input + (outputTokens/1e6)*implPrice.Output
        taskCost += (20000.0/1e6)*specPrice.Input + (1000.0/1e6)*specPrice.Output
        taskCost += (20000.0/1e6)*qualPrice.Input + (1000.0/1e6)*qualPrice.Output
        taskCost *= expectedRetryFactor

        total += taskCost
    }

    // Add planner + final reviewer costs
    plannerPrice := getModelPricing(config, config.Models.Planner)
    total += (30000.0/1e6)*plannerPrice.Input + (3000.0/1e6)*plannerPrice.Output
    finalPrice := getModelPricing(config, config.Models.FinalReviewer)
    total += (40000.0/1e6)*finalPrice.Input + (2000.0/1e6)*finalPrice.Output

    return total
}
```

### 7.5 Mechanical TDD Verification with Failure-Type Discrimination

Instead of trusting the LLM to follow TDD, Foreman mechanically verifies it. Critically, the RED phase distinguishes between "test failed because assertions failed" (valid) and "test failed because of import/compile errors" (invalid).

```go
type TDDResult struct {
    Valid  bool
    Reason string
    Phase  string    // "red" or "green" — where it failed
}

type TestFailureType string
const (
    FailureAssertion TestFailureType = "assertion"   // Valid RED
    FailureCompile   TestFailureType = "compile"     // Invalid RED
    FailureImport    TestFailureType = "import"      // Invalid RED
    FailureRuntime   TestFailureType = "runtime"     // Ambiguous — treat as invalid
    FailureUnknown   TestFailureType = "unknown"
)

func (p *Pipeline) verifyTDD(workDir string, changes *ImplementerOutput) (*TDDResult, error) {
    // Step 1: Apply ONLY test files
    for _, file := range changes.Files {
        if isTestFile(file.Path) {
            writeFile(workDir, file.Path, file.Content)
        }
    }

    // Step 2: Run tests — they MUST FAIL (RED phase)
    result := p.runner.Run(ctx, workDir, testCmd, testArgs, 60)
    if result.ExitCode == 0 {
        return &TDDResult{
            Valid: false, Phase: "red",
            Reason: "Tests passed without implementation code. " +
                    "The tests are likely testing themselves, not the feature. " +
                    "Rewrite the tests to assert behavior that requires implementation.",
        }, nil
    }

    // Step 2b: Classify the failure type
    failureType := classifyTestFailure(result.Stdout, result.Stderr)
    if failureType != FailureAssertion {
        return &TDDResult{
            Valid: false, Phase: "red",
            Reason: fmt.Sprintf(
                "Tests failed due to %s errors, not assertion failures. "+
                "The RED phase requires tests that compile and run but fail on assertions. "+
                "Fix import paths and ensure tests can execute before asserting behavior. "+
                "Errors:\n%s",
                failureType, truncate(result.Stderr, 1500)),
        }, nil
    }

    // Step 3: Apply implementation files
    for _, file := range changes.Files {
        if !isTestFile(file.Path) {
            writeFile(workDir, file.Path, file.Content)
        }
    }

    // Step 4: Run tests again — they MUST PASS (GREEN phase)
    result = p.runner.Run(ctx, workDir, testCmd, testArgs, 120)
    if result.ExitCode != 0 {
        return &TDDResult{
            Valid: false, Phase: "green",
            Reason: fmt.Sprintf("Tests fail with implementation: %s", truncate(result.Stderr, 1500)),
        }, nil
    }

    return &TDDResult{Valid: true}, nil
}

// classifyTestFailure parses test output to determine failure type.
func classifyTestFailure(stdout, stderr string) TestFailureType {
    combined := stdout + "\n" + stderr
    lower := strings.ToLower(combined)

    // Check for compile/syntax errors first (language-specific patterns)
    compilePatterns := []string{
        "syntaxerror", "syntax error",
        "cannot find module", "module not found",
        "compilation failed", "build failed",
        "error ts", "type error",  // TypeScript
        "undefined: ",             // Go
        "cannot find symbol",      // Java
        "error[e", "error: ",      // Rust
    }
    for _, p := range compilePatterns {
        if strings.Contains(lower, p) {
            return FailureCompile
        }
    }

    // Check for import errors
    importPatterns := []string{
        "cannot find module", "module not found",
        "importerror", "import error",
        "no such file or directory",
        "could not resolve",
        "cannot resolve",
    }
    for _, p := range importPatterns {
        if strings.Contains(lower, p) {
            return FailureImport
        }
    }

    // Check for assertion failures (valid RED)
    assertionPatterns := []string{
        "assertionerror", "assertion failed",
        "expect(", "expected",
        "assert.", "assertequal",
        "fail:", "failed:",
        "not equal", "not to equal",
        "tobetruthy", "tobefalsy",
        "should have", "should be",
    }
    for _, p := range assertionPatterns {
        if strings.Contains(lower, p) {
            return FailureAssertion
        }
    }

    return FailureUnknown
}
```

### 7.6 Partial PR Support

When a task fails mid-pipeline and `enable_partial_pr` is true:

```go
func (p *Pipeline) handlePartialFailure(ticket *TicketRecord, failedTask *TaskRecord, err error) error {
    completedTasks := p.db.GetCompletedTasks(ctx, ticket.ID)

    if len(completedTasks) == 0 {
        return p.handleFullFailure(ticket, err)
    }

    pr, err := p.git.CreatePR(ctx, PrRequest{
        Title: fmt.Sprintf("[Foreman] [PARTIAL] %s: %s", ticket.ExternalID, ticket.Title),
        Body: formatPartialPRBody(ticket, completedTasks, failedTask, err),
        Draft: true,
        Labels: []string{"foreman-generated", "partial"},
    })

    p.tracker.AddComment(ctx, ticket.ExternalID, fmt.Sprintf(
        "⚠️ PR #%d opened with **partial** implementation (%d/%d tasks complete).\n\n"+
        "**Failed task:** %s\n**Reason:** %s\n\n"+
        "**Remaining tasks:**\n%s\n\n"+
        "A human developer should review the PR and complete the remaining work.",
        pr.Number, len(completedTasks), totalTasks, failedTask.Title, err.Error(),
        formatRemainingTasks(remainingTasks),
    ))

    p.db.UpdateTicketStatus(ctx, ticket.ID, "partial")
    return nil
}
```

### 7.7 Implementer Output Format: Hybrid Diff Strategy with Fuzzy Matching

Instead of requiring complete file contents (which wastes tokens and risks unintended changes), Foreman uses a hybrid output strategy:

```
For NEW files: output complete file contents.
For EXISTING files: output search-and-replace blocks.
```

The implementer prompt instructs the LLM to use this format:

```
=== NEW FILE: path/to/new_file.ts ===
<complete file contents>
=== END FILE ===

=== MODIFY FILE: path/to/existing_file.ts ===
<<<< SEARCH
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
>>>>
<<<< REPLACE
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
import { validateInput } from '../lib/validation';
>>>>
=== END FILE ===
```

**Fuzzy matching for SEARCH blocks:** LLMs frequently introduce minor whitespace, quote, or indentation differences. Each SEARCH block is matched using normalized Levenshtein similarity:

1. **Exact match** (preferred): byte-for-byte match found in file.
2. **Fuzzy match** (threshold ≥ `search_replace_similarity`, default 0.92): normalized against the best-matching region. Log a `search_block_fuzzy_match` event with the original and matched text for debugging.
3. **Miss**: no region meets the threshold. Log a `search_block_miss` event as WARN. Fail the parse for this file.

**Minimum context requirement:** Each SEARCH block must contain at least `search_replace_min_context_lines` (default 3) lines of context. The implementer prompt enforces this. SEARCH blocks with fewer lines are rejected at parse time with a clear error message.

Benefits of the hybrid approach:
- Output tokens scale with *change size*, not file size.
- Unchanged code is never reproduced — no unintended modifications.
- Works for files of any size (no token budget ceiling).
- Fuzzy matching tolerates minor LLM formatting inconsistencies.
- Misses are explicit — no silent skipping of changes.

### 7.8 Robust Output Parser

```go
type ParsedOutput struct {
    Files       []FileChange
    ParseErrors []string        // Non-fatal parse issues
}

type FileChange struct {
    Path      string
    IsNew     bool
    // For new files:
    Content   string
    // For existing files:
    Patches   []SearchReplace
}

type SearchReplace struct {
    Search     string
    Replace    string
    FuzzyMatch bool      // True if matched via fuzzy threshold
    Similarity float64   // Actual similarity score if fuzzy
}

func ParseImplementerOutput(raw string, config *Config) (*ParsedOutput, error) {
    result := &ParsedOutput{}

    // Strategy 1: Strict parsing (preferred)
    files, err := parseStrict(raw, config)
    if err == nil {
        result.Files = files
        return result, nil
    }

    // Strategy 2: Permissive parsing (handles common LLM quirks)
    //   - Handles ~~~ vs ``` fences
    //   - Handles missing END markers
    //   - Handles commentary between files
    //   - Handles inconsistent whitespace
    files, parseErrors, err := parsePermissive(raw, config)
    if err == nil {
        result.Files = files
        result.ParseErrors = parseErrors
        return result, nil
    }

    // Strategy 3: If both fail, try to extract at least the test file
    //   (so TDD verification can still run on partial output)
    testFile, err := extractTestFile(raw)
    if err == nil {
        result.Files = []FileChange{*testFile}
        result.ParseErrors = append(parseErrors, "Only test file could be extracted")
        return result, nil
    }

    // Total failure
    return nil, fmt.Errorf("failed to parse implementer output (all strategies failed). Raw length: %d", len(raw))
}

// applySearchReplace applies a SEARCH block to file contents with fuzzy matching.
func applySearchReplace(content string, sr *SearchReplace, threshold float64) (string, error) {
    // Try exact match first
    if idx := strings.Index(content, sr.Search); idx != -1 {
        return content[:idx] + sr.Replace + content[idx+len(sr.Search):], nil
    }

    // Fuzzy match: slide a window over the content and find the best match
    searchLines := strings.Split(sr.Search, "\n")
    contentLines := strings.Split(content, "\n")
    windowSize := len(searchLines)

    bestSimilarity := 0.0
    bestStart := -1

    for i := 0; i <= len(contentLines)-windowSize; i++ {
        candidate := strings.Join(contentLines[i:i+windowSize], "\n")
        sim := normalizedLevenshtein(sr.Search, candidate)
        if sim > bestSimilarity {
            bestSimilarity = sim
            bestStart = i
        }
    }

    if bestSimilarity >= threshold {
        sr.FuzzyMatch = true
        sr.Similarity = bestSimilarity
        result := make([]string, 0, len(contentLines))
        result = append(result, contentLines[:bestStart]...)
        result = append(result, strings.Split(sr.Replace, "\n")...)
        result = append(result, contentLines[bestStart+windowSize:]...)
        return strings.Join(result, "\n"), nil
    }

    return "", fmt.Errorf("SEARCH block not found (best similarity: %.2f, threshold: %.2f)", bestSimilarity, threshold)
}
```

### 7.9 Robust YAML Parser for Planner Output

LLMs frequently wrap YAML in markdown fences, add explanatory prose, or produce YAML with unquoted special characters. The planner output parser uses the same strict → permissive → partial fallback chain as the implementer parser.

```go
func ParsePlannerOutput(raw string) (*PlannerResult, error) {
    // Strategy 1: Parse as strict YAML
    result, err := parseStrictYAML(raw)
    if err == nil {
        return result, nil
    }

    // Strategy 2: Strip markdown fences and prose
    cleaned := raw
    // Remove ```yaml ... ``` fences
    cleaned = stripMarkdownFences(cleaned)
    // Find the YAML block by locating the first "status:" key
    if idx := strings.Index(cleaned, "status:"); idx != -1 {
        cleaned = cleaned[idx:]
    }
    // Try lenient parsing
    result, err = parseLenientYAML(cleaned)
    if err == nil {
        return result, nil
    }

    // Strategy 3: Extract at minimum the status field
    if strings.Contains(raw, "CLARIFICATION_NEEDED") {
        // Extract the question after CLARIFICATION_NEEDED
        question := extractAfterKey(raw, "CLARIFICATION_NEEDED")
        return &PlannerResult{
            Status:  "CLARIFICATION_NEEDED",
            Message: question,
        }, nil
    }
    if strings.Contains(raw, "TICKET_TOO_LARGE") {
        message := extractAfterKey(raw, "TICKET_TOO_LARGE")
        return &PlannerResult{
            Status:  "TICKET_TOO_LARGE",
            Message: message,
        }, nil
    }

    return nil, fmt.Errorf("failed to parse planner output (all strategies failed). Raw length: %d", len(raw))
}

func parseLenientYAML(raw string) (*PlannerResult, error) {
    var result PlannerResult
    decoder := yaml.NewDecoder(strings.NewReader(raw))
    // KnownFields(false) ignores unexpected fields
    decoder.KnownFields(false)
    if err := decoder.Decode(&result); err != nil {
        return nil, err
    }
    return &result, nil
}
```

### 7.10 File Reservation Layer for Parallel Tickets

When multiple pipelines run concurrently, they can conflict on the same files. The daemon's scheduler prevents this with a reservation layer.

```go
// Before starting a pipeline, the scheduler checks and reserves files.
func (s *Scheduler) tryReserveForTicket(ticketID string, plan *Plan) error {
    allFiles := collectAllFiles(plan)

    // Check against active reservations
    reserved, err := s.db.GetReservedFiles(ctx)
    if err != nil {
        return err
    }

    var conflicts []string
    for _, f := range allFiles {
        if ownerTicket, ok := reserved[f]; ok && ownerTicket != ticketID {
            conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", f, ownerTicket))
        }
    }

    if len(conflicts) > 0 {
        s.db.RecordEvent(ctx, &EventRecord{
            TicketID: ticketID, EventType: "file_reservation_conflict",
            Severity: "info",
            Message: fmt.Sprintf("Queued due to file conflicts: %v", conflicts),
        })
        return &FileConflictError{Conflicts: conflicts}
    }

    // Reserve all files
    return s.db.ReserveFiles(ctx, ticketID, allFiles)
}

// On pipeline completion (success, failure, or partial), release reservations.
func (s *Scheduler) releasePipeline(ticketID string) {
    s.db.ReleaseFiles(ctx, ticketID)
}
```

### 7.11 Crash Recovery

If the daemon process is killed mid-pipeline, it resumes from the last committed task on restart.

```go
func (d *Daemon) recoverInProgressTickets() {
    tickets, _ := d.db.ListTickets(ctx, TicketFilter{
        StatusIn: []string{"planning", "implementing", "reviewing"},
    })

    for _, t := range tickets {
        lastSeq := t.LastCompletedTaskSeq
        tasks, _ := d.db.GetTasks(ctx, t.ID)

        if lastSeq == 0 && t.Status == "planning" {
            // Re-run the planner
            d.db.UpdateTicketStatus(ctx, t.ID, "queued")
            d.db.RecordEvent(ctx, &EventRecord{
                TicketID: t.ID, EventType: "pipeline_resumed_after_crash",
                Message: "Restarting from planning phase",
            })
            continue
        }

        // Mark tasks up to lastSeq as done, reset current task to pending
        for _, task := range tasks {
            if task.Sequence <= lastSeq {
                // Already committed — keep as done
                continue
            }
            if task.Status != "pending" && task.Status != "done" {
                // Was in progress when crash happened — reset
                d.db.UpdateTaskStatus(ctx, task.ID, "pending")
            }
        }

        d.db.UpdateTicketStatus(ctx, t.ID, "implementing")
        d.db.RecordEvent(ctx, &EventRecord{
            TicketID: t.ID, EventType: "pipeline_resumed_after_crash",
            Message: fmt.Sprintf("Resuming from task %d", lastSeq+1),
        })
    }
}

// On successful task commit:
func (p *Pipeline) afterTaskCommit(ticket *TicketRecord, task *TaskRecord) {
    p.db.SetLastCompletedTask(ctx, ticket.ID, task.Sequence)
}
```

### 7.12 Dependency Change Detection Between Tasks

When a task modifies package manifests (package.json, go.mod, Cargo.toml, etc.), the system detects this and reinstalls dependencies before the next task runs.

```go
var depFiles = []string{
    "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
    "go.mod", "go.sum",
    "Cargo.toml", "Cargo.lock",
    "requirements.txt", "pyproject.toml", "poetry.lock",
    "Gemfile", "Gemfile.lock",
}

func (p *Pipeline) checkAndReinstallDeps(workDir string, task *TaskRecord) error {
    if !p.config.Runner.Docker.AutoReinstallDeps {
        return nil
    }

    for _, depFile := range depFiles {
        for _, modified := range task.FilesToModify {
            if filepath.Base(modified) == depFile || modified == depFile {
                p.db.RecordEvent(ctx, &EventRecord{
                    TaskID: task.ID, EventType: "task_dep_change_detected",
                    Message: fmt.Sprintf("Dependency file changed: %s", modified),
                })
                return p.runInstallCommand(workDir, depFile)
            }
        }
    }
    return nil
}

func (p *Pipeline) runInstallCommand(workDir, depFile string) error {
    var cmd string
    var args []string
    switch filepath.Base(depFile) {
    case "package.json", "package-lock.json":
        cmd = "npm"
        args = []string{"install"}
    case "yarn.lock":
        cmd = "yarn"
        args = []string{"install"}
    case "go.mod", "go.sum":
        cmd = "go"
        args = []string{"mod", "download"}
    case "Cargo.toml", "Cargo.lock":
        cmd = "cargo"
        args = []string{"fetch"}
    case "requirements.txt":
        cmd = "pip"
        args = []string{"install", "-r", "requirements.txt"}
    case "pyproject.toml", "poetry.lock":
        cmd = "poetry"
        args = []string{"install"}
    default:
        return nil
    }
    result := p.runner.Run(ctx, workDir, cmd, args, 120)
    if result.ExitCode != 0 {
        return fmt.Errorf("dependency install failed: %s", result.Stderr)
    }
    return nil
}
```

---

## 8. Context Assembly Engine

### 8.1 File Selection Algorithm

The file selector (`file_selector.go`) is the most critical component for context quality. It uses a scored, multi-signal approach to rank files by relevance to a given task.

```go
type ScoredFile struct {
    Path      string
    Score     float64
    Reason    string    // Why this file was selected (for debugging)
    SizeBytes int64
}

// SelectFilesForTask returns the most relevant files within the token budget.
func SelectFilesForTask(task *Task, workDir string, tokenBudget int) ([]ScoredFile, error) {
    candidates := []ScoredFile{}

    // ─── Signal 1: Explicit planner references (highest priority) ───
    for _, path := range task.FilesToRead {
        candidates = append(candidates, ScoredFile{Path: path, Score: 100, Reason: "planner:read"})
    }
    for _, path := range task.FilesToModify {
        if fileExists(workDir, path) {
            candidates = append(candidates, ScoredFile{Path: path, Score: 100, Reason: "planner:modify"})
        }
    }

    // ─── Signal 2: Import graph traversal ───
    // Parse imports from files_to_modify and files_to_read.
    // Follow the dependency chain up to 2 levels deep.
    importedFiles := traceImports(workDir, task.FilesToModify, 2)
    for _, imp := range importedFiles {
        score := 70.0
        if imp.Depth == 1 { score = 80.0 }
        candidates = append(candidates, ScoredFile{Path: imp.Path, Score: score, Reason: fmt.Sprintf("import:depth_%d", imp.Depth)})
    }

    // ─── Signal 3: Directory proximity ───
    // Files in the same directory as files_to_modify get a proximity boost.
    taskDirs := extractDirectories(task.FilesToModify)
    allFiles := listAllSourceFiles(workDir)
    for _, f := range allFiles {
        if inAnyDirectory(f, taskDirs) && !alreadyCandidate(candidates, f) {
            candidates = append(candidates, ScoredFile{Path: f, Score: 30, Reason: "proximity"})
        }
    }

    // ─── Signal 4: Test file adjacency ───
    // If task modifies foo.ts, include foo.test.ts and vice versa.
    for _, path := range task.FilesToModify {
        sibling := findTestSibling(workDir, path)
        if sibling != "" && !alreadyCandidate(candidates, sibling) {
            candidates = append(candidates, ScoredFile{Path: sibling, Score: 60, Reason: "test_sibling"})
        }
    }

    // ─── Signal 5: Type definition and interface files ───
    // Scan for .d.ts, types.go, interfaces/ etc. referenced by task files.
    typeFiles := findTypeDefinitions(workDir, task.FilesToModify)
    for _, tf := range typeFiles {
        if !alreadyCandidate(candidates, tf) {
            candidates = append(candidates, ScoredFile{Path: tf, Score: 50, Reason: "type_def"})
        }
    }

    // ─── Deduplicate and sort by score descending ───
    candidates = deduplicateByPath(candidates) // Keep highest score per path
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Score > candidates[j].Score
    })

    // ─── Token budget cutoff ───
    selected := []ScoredFile{}
    tokensUsed := 0
    for _, c := range candidates {
        fileTokens := estimateFileTokens(workDir, c.Path)
        if tokensUsed + fileTokens > tokenBudget {
            continue // Skip files that would exceed budget
        }
        tokensUsed += fileTokens
        selected = append(selected, c)
    }

    return selected, nil
}

// traceImports parses source files and follows import/require/use statements.
// Supports: JS/TS (import/require), Go (import), Python (import/from), Rust (use/mod).
func traceImports(workDir string, roots []string, maxDepth int) []ImportedFile {
    visited := map[string]bool{}
    var results []ImportedFile

    var trace func(path string, depth int)
    trace = func(path string, depth int) {
        if depth > maxDepth || visited[path] { return }
        visited[path] = true

        content, err := os.ReadFile(filepath.Join(workDir, path))
        if err != nil { return }

        imports := parseImports(path, string(content))
        for _, imp := range imports {
            resolved := resolveImportPath(workDir, path, imp)
            if resolved != "" && !visited[resolved] {
                results = append(results, ImportedFile{Path: resolved, Depth: depth})
                trace(resolved, depth+1)
            }
        }
    }

    for _, root := range roots {
        trace(root, 1)
    }
    return results
}

// parseImports extracts import paths from source code using regex patterns.
// NOT a full parser — designed for speed and 90%+ accuracy on standard import styles.
func parseImports(filePath, content string) []string {
    ext := filepath.Ext(filePath)
    var patterns []*regexp.Regexp

    switch ext {
    case ".ts", ".tsx", ".js", ".jsx", ".mjs":
        patterns = []*regexp.Regexp{
            regexp.MustCompile(`import\s+.*?from\s+['"]([^'"]+)['"]`),
            regexp.MustCompile(`require\(['"]([^'"]+)['"]\)`),
        }
    case ".go":
        patterns = []*regexp.Regexp{
            regexp.MustCompile(`"([^"]+)"`), // Within import blocks
        }
    case ".py":
        patterns = []*regexp.Regexp{
            regexp.MustCompile(`from\s+(\S+)\s+import`),
            regexp.MustCompile(`import\s+(\S+)`),
        }
    case ".rs":
        patterns = []*regexp.Regexp{
            regexp.MustCompile(`use\s+([\w:]+)`),
            regexp.MustCompile(`mod\s+(\w+)`),
        }
    }

    var imports []string
    for _, p := range patterns {
        matches := p.FindAllStringSubmatch(content, -1)
        for _, m := range matches {
            if len(m) > 1 {
                imports = append(imports, m[1])
            }
        }
    }
    return imports
}
```

**Integration test requirement:** `tests/integration/file_selector_test.go` must include a test against the `tests/fixtures/sample_repo/` that measures retrieval precision and recall for known task→file mappings.

### 8.2 Context Per Role

#### Planner Context (~30k tokens)

The planner has access to the **file tree and structural files only**, not source code implementation files. This is sufficient for decomposing tickets into tasks. The planner specifies which files each task should read, and the implementer gets the actual file contents.

```
ALWAYS INCLUDE:
├── Ticket: title + description + acceptance criteria + comments
├── Repo file tree (full, ~2k tokens typically)
├── README.md (truncated to 3k tokens if large)
├── Key config files (package.json, Cargo.toml, go.mod, etc.)
├── CI config (.github/workflows/*.yml)
├── Recent git log (last 20 commits)
└── .foreman-context.md (project-specific rules, if exists)

CONDITIONALLY INCLUDE:
├── Test config (jest.config, vitest.config, etc.)
├── DB schema files (if ticket mentions data/models)
└── API route definitions (if ticket mentions endpoints)

NEVER INCLUDE:
├── Source code implementation files
├── Test file contents
├── Build output, node_modules, vendor/
```

#### Implementer Context (~60k tokens)

```
ALWAYS INCLUDE:
├── Current task spec (title, description, acceptance criteria, test assertions)
├── Files selected by file_selector.go (scored and ranked within budget)
├── Files to modify (current contents if they exist)
├── Progress patterns (from DB, pruned by directory relevance)
├── Build/test/lint commands
└── Codebase patterns (language, framework, style)

IF RETRY:
├── Previous attempt diff (what was tried)
├── Error output (truncated to 2k tokens)
├── Reviewer feedback (spec or quality issues)
├── Attempt number + max attempts (urgency signal)

CONDITIONALLY INCLUDE (via file_selector scoring):
├── Related test files (to understand test patterns)
├── Adjacent files in same directory (import/export patterns)
└── Type definitions/interfaces relevant to task
```

#### Spec Reviewer Context (~20k tokens)

```
ALWAYS:  task acceptance criteria + git diff + test output
NEVER:   full source files, codebase patterns, other tasks
```

#### Quality Reviewer Context (~20k tokens)

```
ALWAYS:  git diff + codebase patterns + style conventions + directory rules
NEVER:   acceptance criteria, ticket description, test output
```

#### Final Reviewer Context (~40k tokens)

```
ALWAYS:  original ticket + full diff + test output + task summaries
```

### 8.3 Progress Data Management (Database-Backed)

Progress patterns are stored in the `progress_patterns` database table, not in git. This prevents noise in PR diffs and allows pruning by directory relevance.

```go
type ProgressPattern struct {
    ID               string
    TicketID         string
    PatternKey       string      // e.g., "import_style", "error_handling"
    PatternValue     string      // e.g., "ESM imports, no semicolons"
    Directories      []string    // Where this pattern applies
    DiscoveredByTask string
    CreatedAt        time.Time
}

// GetPrunedPatterns returns only patterns relevant to the current task's files.
func (p *Pipeline) GetPrunedPatterns(ticketID string, task *Task) ([]ProgressPattern, error) {
    taskDirs := extractDirectories(task.FilesToModify)
    patterns, err := p.db.GetProgressPatterns(ctx, ticketID, taskDirs)
    if err != nil {
        return nil, err
    }
    // Cap at ~2k tokens worth of patterns
    return truncatePatternsByTokens(patterns, 2000), nil
}
```

### 8.4 Pre-Flight Secrets Scanner

Before any file enters the context assembler, it is scanned for known secret patterns. Matching files are redacted or excluded.

```go
var builtinSecretPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),                          // AWS access key
    regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,})`),                       // OpenAI/Anthropic key prefix
    regexp.MustCompile(`(?i)(ghp_[a-zA-Z0-9]{36})`),                       // GitHub PAT
    regexp.MustCompile(`(?i)(glpat-[a-zA-Z0-9\-]{20,})`),                  // GitLab PAT
    regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----`),  // Private key headers
    regexp.MustCompile(`(?i)(xox[bprs]-[a-zA-Z0-9\-]+)`),                  // Slack tokens
    regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`),             // Bearer tokens
    regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|api[_-]?token)\s*[:=]\s*["']?[a-zA-Z0-9\-._]{16,}`),
    regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*["'][^"']{8,}["']`),
}

type SecretsScanner struct {
    patterns      []*regexp.Regexp
    alwaysExclude []string
}

func NewSecretsScanner(config *SecretsConfig) *SecretsScanner {
    patterns := append([]*regexp.Regexp{}, builtinSecretPatterns...)
    for _, extra := range config.ExtraPatterns {
        patterns = append(patterns, regexp.MustCompile(extra))
    }
    return &SecretsScanner{
        patterns:      patterns,
        alwaysExclude: config.AlwaysExclude,
    }
}

type ScanResult struct {
    Path       string
    HasSecrets bool
    Matches    []SecretMatch
}

type SecretMatch struct {
    Line    int
    Pattern string
    Snippet string    // Redacted: first 4 chars + "***"
}

func (s *SecretsScanner) ScanFile(path, content string) *ScanResult {
    // Check always-exclude list first
    for _, pattern := range s.alwaysExclude {
        if matchGlob(path, pattern) {
            return &ScanResult{Path: path, HasSecrets: true,
                Matches: []SecretMatch{{Pattern: "always_exclude", Snippet: path}}}
        }
    }

    result := &ScanResult{Path: path}
    lines := strings.Split(content, "\n")
    for i, line := range lines {
        for _, pat := range s.patterns {
            if pat.MatchString(line) {
                result.HasSecrets = true
                match := pat.FindString(line)
                redacted := match[:min(4, len(match))] + "***"
                result.Matches = append(result.Matches, SecretMatch{
                    Line: i + 1, Pattern: pat.String(), Snippet: redacted,
                })
            }
        }
    }
    return result
}

// ScanAndFilter removes files with secrets from the context file list.
// Returns the filtered list and logs security events.
func (s *SecretsScanner) ScanAndFilter(files []ScoredFile, workDir string, db Database, ticketID string) []ScoredFile {
    filtered := []ScoredFile{}
    for _, f := range files {
        content, err := os.ReadFile(filepath.Join(workDir, f.Path))
        if err != nil {
            continue
        }
        result := s.ScanFile(f.Path, string(content))
        if result.HasSecrets {
            db.RecordEvent(ctx, &EventRecord{
                TicketID: ticketID, EventType: "secrets_detected",
                Severity: "warn",
                Message: fmt.Sprintf("Excluded %s from context: %d secret(s) detected", f.Path, len(result.Matches)),
            })
            continue
        }
        filtered = append(filtered, f)
    }
    return filtered
}
```

### 8.5 `.foreman-context.md` (Per-Repo Custom Context)

Generated via `foreman init --analyze` (auto-detects and pre-populates), then manually reviewed and committed by the team. **This is a one-time human action per repo.** It runs once, generates a template, and the team edits and commits it. The pipeline reads it at runtime but never modifies it.

For monorepos, use one `.foreman-context.md` at the root with sections per package, or place one in each package directory. The context assembler reads the nearest `.foreman-context.md` walking up from the task's primary directory.

```markdown
# Foreman Context

## Project Overview
Next.js 14 e-commerce platform. App Router, Prisma ORM, Stripe payments.

## Architecture
- `src/app/` — Pages and API routes
- `src/lib/` — Shared utilities, DB client, auth
- `src/components/` — React components (shadcn/ui)
- `prisma/` — Schema and migrations

## Conventions
- API routes return `{ data, error }` shape
- Use `zod` for input validation
- Auth: always use `requireAuth()` from `src/lib/auth.ts`
- DB: use `src/lib/db.ts`, never import PrismaClient directly
- Money as integers (cents), not floats

## Commands
- Test: `npm test`
- Lint: `npm run lint`
- Lint fix: `npm run lint:fix`
- Type check: `npm run typecheck`

## Gotchas
- User IDs are cuid2, not UUID
- Email uniqueness is case-insensitive (use toLowerCase)
- Rate limiting on all /api/ routes — tests need to handle this
```

---

## 9. LLM Prompt Templates

### 9.1 Planner Prompt

```
# prompts/planner.md.j2

You are a senior software engineer decomposing a ticket into implementation tasks.

## Your Job
Produce an ordered list of granular tasks. Each task: 2-5 minutes, completable by an
AI agent that has no memory between tasks and follows strict TDD.

## Rules
1. Each task MUST specify exact file paths to read and modify.
2. Each task MUST include test assertions.
3. Each task MUST have acceptance criteria verifiable from the diff alone.
4. Tasks are executed in strict sequential order. Use depends_on to declare ordering
   constraints explicitly.
5. Maximum {{ max_tasks }} tasks. If more needed, output TICKET_TOO_LARGE with explanation.
6. Do NOT include setup tasks (branching, env). The system handles those.
7. Every task includes its own tests — do NOT have separate "write tests" tasks.
8. For EXISTING files, describe what to ADD or CHANGE, not the full file.
9. If the ticket lacks enough detail to decompose, output CLARIFICATION_NEEDED
   with a specific question.
10. If two tasks modify the same file, they MUST have an explicit ordering
    (later task depends_on earlier task).

## Output Format (YAML — strict, parse failure = pipeline failure)

```yaml
status: OK | TICKET_TOO_LARGE | CLARIFICATION_NEEDED
message: "<explanation if not OK>"

codebase_patterns:
  language: "<detected>"
  framework: "<detected>"
  test_runner: "<detected>"
  style_notes: "<key conventions>"

tasks:
  - title: "<short title>"
    description: |
      <Detailed description. Be specific. Function signatures, edge cases.>
    acceptance_criteria:
      - "<verifiable from diff>"
    test_assertions:
      - "<what the test should assert>"
    files_to_read:
      - "<path>"
    files_to_modify:
      - "<path> (new)" or "<existing path>"
    estimated_complexity: "simple|medium|complex"
    depends_on: []
```

Do NOT wrap the YAML in markdown fences. Output ONLY the YAML.

## Ticket
Title: {{ ticket_title }}
Description:
{{ ticket_description }}

{% if acceptance_criteria %}
Acceptance Criteria:
{{ acceptance_criteria }}
{% endif %}

{% if ticket_comments %}
Context from comments:
{% for c in ticket_comments %}
- {{ c.author }}: {{ c.body }}
{% endfor %}
{% endif %}

## Repository
{{ repo_tree }}
{{ readme_content }}
{{ config_files }}
{{ recent_git_log }}

{% if foreman_context %}
## Project-Specific Context
{{ foreman_context }}
{% endif %}
```

### 9.2 Implementer Prompt

```
# prompts/implementer.md.j2

You are implementing a single task using strict Test-Driven Development.

## TDD Rules (MANDATORY)
1. RED: Write a failing test first. The test MUST compile and run, but FAIL on assertions.
   - Do NOT write tests that fail due to import errors or missing modules.
   - Ensure all imports resolve to existing files or files you are creating.
   - The test must execute and produce assertion failures, not compile errors.
2. GREEN: Write MINIMAL code to make the test pass.
3. Do NOT add anything not in the task spec.

## Output Format
For NEW files, use:
=== NEW FILE: path/to/file.ext ===
<complete contents>
=== END FILE ===

For EXISTING files, use search-and-replace blocks:
=== MODIFY FILE: path/to/file.ext ===
<<<< SEARCH
<exact lines to find — include at least 3 lines of context>
>>>>
<<<< REPLACE
<replacement lines>
>>>>
=== END FILE ===

IMPORTANT:
- Each SEARCH block must include at least 3 lines of surrounding context.
- Match the existing file's indentation and whitespace exactly.
- ALWAYS output test files BEFORE implementation files.

{% if previous_error %}
## RETRY (attempt {{ attempt }}/{{ max_attempts }})
Previous error:
```
{{ previous_error }}
```
Fix the specific error. Do not rewrite from scratch unless necessary.
{% endif %}

{% if spec_feedback %}
## SPEC REVIEWER FOUND ISSUES
{{ spec_feedback }}
Address every issue above.
{% endif %}

{% if quality_feedback %}
## QUALITY REVIEWER FOUND ISSUES
{{ quality_feedback }}
Address every issue above.
{% endif %}

{% if tdd_feedback %}
## TDD VERIFICATION FAILED
{{ tdd_feedback }}
{% endif %}

## Task
Title: {{ task_title }}
Description:
{{ task_description }}

Acceptance Criteria:
{% for c in acceptance_criteria %}- {{ c }}
{% endfor %}

Test Assertions:
{% for a in test_assertions %}- {{ a }}
{% endfor %}

## Codebase Patterns
{{ codebase_patterns }}

## Commands
Build: `{{ build_cmd }}`  Test: `{{ test_cmd }}`  Lint: `{{ lint_cmd }}`

{% if progress_patterns %}
## Patterns From Previous Tasks
{{ progress_patterns }}
{% endif %}

## Files
{% for file in files %}
### {{ file.path }}
```{{ file.language }}
{{ file.content }}
```
{% endfor %}
```

### 9.3 Spec Reviewer Prompt

```
# prompts/spec_reviewer.md.j2

You verify that the implementation satisfies every acceptance criterion. Nothing more.

## Rules
1. Check EVERY criterion. Mark ✅ or ❌.
2. Flag any extra functionality not requested (YAGNI).
3. Do NOT comment on code quality or style.
4. Be specific — say exactly what's missing and where.

## Output Format
STATUS: APPROVED | REJECTED

CRITERIA:
{% for c in acceptance_criteria %}
- [ ] {{ c }}
{% endfor %}

ISSUES:
- <what's missing, which file, what's needed>

EXTRAS:
- <anything not requested>

## Task
{{ task_title }}
Criteria:
{% for c in acceptance_criteria %}- {{ c }}
{% endfor %}

## Diff
```diff
{{ diff }}
```

## Test Output
```
{{ test_output }}
```
```

### 9.4 Quality Reviewer Prompt

```
# prompts/quality_reviewer.md.j2

You review code quality only. Do NOT check spec compliance.

## Check
- Style matches codebase patterns
- Naming consistency
- Error handling
- No obvious bugs/edge cases
- DRY
- Tests are meaningful
- No security issues (hardcoded secrets, injection, XSS)
- No performance anti-patterns

## Severity
- CRITICAL: Must fix. Security, data loss, production breakage.
- IMPORTANT: Should fix. Code smell, subtle bug.
- MINOR: Nice to fix. Does NOT block approval.

## Output Format
STATUS: APPROVED | CHANGES_REQUESTED

ISSUES:
- [CRITICAL|IMPORTANT|MINOR] <file, issue, fix suggestion>

STRENGTHS:
- <what was done well>

## Codebase Patterns
{{ codebase_patterns }}

## Diff
```diff
{{ diff }}
```
```

### 9.5 Final Reviewer Prompt

```
# prompts/final_reviewer.md.j2

Final review of the complete changeset before PR creation.

## Check
1. Changes as a whole address the original ticket
2. Integration issues between tasks
3. Cross-cutting concerns (error handling consistency, migrations, etc.)

## Output Format
STATUS: APPROVED | REJECTED
SUMMARY: <2-3 sentences>
CHANGES: <key changes by area>
CONCERNS: <issues if any>
REVIEW_NOTES: <notes for human reviewer>

## Ticket
{{ ticket_title }}
{{ ticket_description }}

## Full Diff
```diff
{{ full_diff }}
```

## Tasks
{% for t in tasks %}
{{ loop.index }}. {{ t.title }} — {{ t.status }}
{% endfor %}

## Tests
```
{{ test_output }}
```
```

---

## 10. Cost Controller & Rate Limiter

### 10.1 Cost Model Reference Table

Empirical cost estimates (Sonnet + Haiku routing):

| Ticket Complexity | Tasks | Retries | Estimated Cost | Duration |
|---|---|---|---|---|
| Simple (3 tasks, no retries) | 3 | 0 | $1.50 – $3.00 | 5-10 min |
| Medium (7 tasks, ~2 retries) | 7 | 2 | $5.00 – $10.00 | 15-30 min |
| Complex (15 tasks, ~5 retries) | 15 | 5 | $12.00 – $25.00 | 30-60 min |
| Very complex (20 tasks, many retries) | 20 | 8+ | $20.00 – $40.00 | 45-90 min |

**Budget recommendation:** Set `max_cost_per_ticket_usd` to 2-3× your average expected cost. Start with $15 and adjust based on observed usage. The plan validator provides a token-aware estimate before execution begins.

### 10.2 Shared Rate Limiter

All pipeline workers share a single rate limiter per LLM provider:

```go
type SharedRateLimiter struct {
    limiters map[string]*rate.Limiter  // provider → limiter
    mu       sync.RWMutex
    config   RateLimitConfig
}

func (r *SharedRateLimiter) Wait(ctx context.Context, provider string) error {
    limiter := r.getOrCreate(provider)
    return limiter.Wait(ctx)
}

func (r *SharedRateLimiter) OnRateLimit(provider string, retryAfter int) {
    limiter := r.getOrCreate(provider)
    limiter.SetLimit(rate.Every(time.Duration(retryAfter) * time.Second / rate.Limit(r.config.BurstSize)))

    go func() {
        jitter := time.Duration(rand.Intn(r.config.JitterPercent)) * time.Millisecond
        time.Sleep(time.Duration(retryAfter)*time.Second + jitter)
        limiter.SetLimit(rate.Every(time.Minute / rate.Limit(r.config.RequestsPerMinute)))
    }()
}
```

### 10.3 Provider Outage Handling

When an LLM provider is fully down (connection refused, DNS failure, persistent 500s — not rate limiting):

1. Retry the connection `max_connection_retries` times (default 3) with `connection_retry_delay_secs` (default 30) between attempts.
2. If a `fallback_provider` is configured and healthy, route the call there.
3. If all retries exhausted and no fallback available: pause the pipeline (not fail it), emit a `provider_down` event, and retry on the next daemon poll cycle.
4. The ticket remains in its current status. No work is lost.
5. When the provider recovers, emit a `provider_recovered` event and resume normally.

---

## 11. Git Operations

### 11.1 Native Git CLI as Default

The default git backend shells out to the native `git` CLI via the CommandRunner. This is significantly faster than go-git for large repos, has complete support for complex rebase scenarios, and handles large object stores correctly.

The go-git pure Go implementation is available as a fallback for environments where the `git` binary is not installed. The `git.backend` config option controls this.

```go
// NewGitProvider creates the appropriate git backend.
func NewGitProvider(config *GitConfig, runner CommandRunner) GitProvider {
    switch config.Backend {
    case "gogit":
        return &GoGitProvider{config: config}
    default: // "native"
        // Verify git is available
        if !runner.CommandExists(ctx, "git") {
            log.Warn().Msg("Native git not found, falling back to go-git")
            return &GoGitProvider{config: config}
        }
        return &NativeGitProvider{config: config, runner: runner}
    }
}
```

---

## 12. Repo Analysis & Auto-Detection

### 12.1 Two-Tier Detection

**Tier 1: `foreman init --analyze` (explicit, recommended)**

Scans the repo, auto-detects the stack, and generates a `.foreman-context.md` template pre-populated with detected values. The team reviews, edits, and commits it. **This is a one-time human action per repository.** It is not automated per pipeline.

**Tier 2: Runtime auto-detection (fallback)**

If no `.foreman-context.md` exists, the pipeline auto-detects from config files. This is less reliable — the detection may misidentify commands, miss environment requirements, or fail on monorepo structures.

```go
func AnalyzeRepo(workDir string, runner CommandRunner) (*RepoInfo, error) {
    info := &RepoInfo{}

    // Priority 1: Read .foreman-context.md (walk up directories for monorepo support)
    if ctx, err := findAndReadForemanContext(workDir); err == nil {
        return parseContextFile(ctx)
    }

    // Priority 2: Detect from config files
    // Check package.json, Cargo.toml, go.mod, pyproject.toml, Makefile, Gemfile
    // Parse scripts section, detect test/lint/build commands

    // Priority 3: Verify detected commands actually work
    if info.TestCmd != "" {
        result := runner.Run(ctx, workDir, info.TestCmd, nil, 60)
        if result.ExitCode != 0 {
            info.TestCmdVerified = false
            info.TestCmdError = result.Stderr
        }
    }

    return info, nil
}
```

---

## 13. Dashboard

### 13.1 Authentication

All dashboard endpoints require a bearer token:

```go
func authMiddleware(db Database) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := extractBearerToken(r)
            if token == "" {
                http.Error(w, "Unauthorized", 401)
                return
            }
            hash := sha256.Sum256([]byte(token))
            valid, _ := db.ValidateAuthToken(ctx, hex.EncodeToString(hash[:]))
            if !valid {
                http.Error(w, "Unauthorized", 401)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Generate tokens via CLI: `foreman token generate --name "my-dashboard"`

### 13.2 Endpoints

```
GET  /                              → Dashboard HTML
GET  /api/status                    → Daemon status, uptime, version
GET  /api/tickets                   → Tickets (filterable by status)
GET  /api/tickets/:id               → Ticket detail
GET  /api/tickets/:id/tasks         → Tasks
GET  /api/tickets/:id/events        → Event log
GET  /api/tickets/:id/llm-calls     → LLM call audit log
GET  /api/pipeline/active           → Running pipelines
GET  /api/costs/today               → Today's cost breakdown
GET  /api/costs/week                → Weekly breakdown
GET  /api/costs/month               → Monthly breakdown
GET  /api/costs/budgets             → Budget status (used / limit)
GET  /api/metrics                   → Prometheus metrics
WS   /ws/events                     → Live event stream
POST /api/tickets/:id/retry         → Retry failed ticket
POST /api/tickets/:id/cancel        → Cancel running pipeline
POST /api/daemon/pause              → Pause polling
POST /api/daemon/resume             → Resume polling
```

---

## 14. CLI Interface

```bash
# Lifecycle
foreman start                       # Start daemon (foreground)
foreman start --daemon              # Background
foreman stop                        # Stop daemon
foreman status                      # Status + uptime + active pipelines

# Run single ticket (no daemon)
foreman run "PROJ-123"              # Run specific ticket by external ID
foreman run --dry-run "PROJ-123"    # Plan only — show tasks, estimated cost, files

# NOTE: --title is intentionally not supported. Use external IDs only.
# Fuzzy title matching risks picking up the wrong ticket.

# Pipeline monitoring
foreman ps                          # Active pipelines
foreman ps --all                    # All including completed
foreman logs PROJ-123               # Event log for ticket
foreman logs --follow               # Tail all events

# Cost
foreman cost today                  # Today's breakdown
foreman cost week                   # Weekly
foreman cost month                  # Monthly
foreman cost ticket PROJ-123        # Per-ticket breakdown

# Dashboard
foreman dashboard                   # Start on configured port
foreman dashboard --port 8080

# Setup
foreman init                        # Create foreman.toml in current dir
foreman init --analyze              # + scan repo, generate .foreman-context.md
foreman doctor                      # Health check all providers
foreman token generate --name "me"  # Generate dashboard auth token
```

---

## 15. Security

```
LOCAL MODE:
├── Command allowlist
├── Forbidden path patterns
├── Working directory scoped to repo clone
├── Dashboard requires bearer token auth
├── Pre-flight secrets scanning on all files before LLM context
└── Files matching secret patterns excluded from LLM calls

DOCKER MODE (recommended for production):
├── One container per TICKET (persists across tasks within ticket)
├── Repo directory mounted read-write
├── Dependencies installed once at start, reinstalled on package file changes
├── No network access (--network=none)
├── Resource limits (CPU, memory)
├── Non-root user
├── Container destroyed on ticket pipeline completion
├── Inter-ticket isolation
└── Orphan container cleanup on daemon restart

SECRETS:
├── API keys from environment variables only
├── Config supports ${ENV_VAR} syntax
├── Dashboard redacts all secrets
├── LLM call logs store prompt hashes, not full prompts
├── .foreman-context.md is git-tracked (never put secrets there)
├── Pre-flight secrets scanner on all context files
├── Known secret patterns: AWS keys, API tokens, private keys, etc.
├── Configurable extra patterns via [secrets] config
└── Always-exclude file list for .env, *.pem, *.key etc.

GIT:
├── Never force-push
├── Never push to default branch
├── Branches prefixed (foreman/)
├── PRs always draft by default
├── Forbidden file patterns checked before commit
├── Rebase before PR to minimize conflicts
├── Progress data stored in DB, never committed to git
```

### 15.1 Docker Container Lifecycle

On daemon startup, check for orphaned containers from a previous crash:

```go
func (d *Daemon) cleanupOrphanContainers() {
    containers := listContainersWithLabel("foreman-ticket")
    activeTickets := d.db.ListTickets(ctx, TicketFilter{StatusIn: activeStatuses})
    activeIDs := toSet(activeTickets)

    for _, c := range containers {
        ticketID := c.Labels["foreman-ticket"]
        if !activeIDs[ticketID] {
            log.Info().Str("container", c.ID).Str("ticket", ticketID).Msg("Removing orphan container")
            removeContainer(c.ID)
        }
    }
}
```

---

## 16. Open Source & Release

### 16.1 License

MIT + Apache 2.0 dual license (Go ecosystem standard).

### 16.2 Release Artifacts

Each release publishes:
- `foreman-x.y.z-linux-amd64` (static binary)
- `foreman-x.y.z-linux-arm64`
- `foreman-x.y.z-darwin-amd64`
- `foreman-x.y.z-darwin-arm64`
- `foreman-x.y.z-windows-amd64.exe`
- Docker: `ghcr.io/org/foreman:x.y.z` and `:latest`

### 16.3 Installation

```bash
# One-liner
curl -fsSL https://foreman.dev/install.sh | bash

# Go install
go install github.com/anthropics/foreman@latest

# Docker
docker run -d --name foreman \
  -v ~/.foreman:/root/.foreman \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  ghcr.io/org/foreman:latest

# From source
git clone https://github.com/anthropics/foreman.git
cd foreman && go build -o foreman . && ./foreman --help
```

---

## 17. Build Order

### Phase 1: Core Execution (Week 1-2)
Build: `llm/provider.go`, `llm/anthropic.go`, `pipeline/implementer.go`, `pipeline/output_parser.go`, `runner/local.go`, `context/token_budget.go`, `context/file_selector.go`, `context/secrets_scanner.go`, implementer prompt.
**Verify:** Give it one task + files → get working TDD code back. File selector returns relevant files. Secrets scanner excludes sensitive files.

### Phase 2: Full Pipeline (Week 2-3)
Build: `pipeline/pipeline.go`, `pipeline/planner.go`, `pipeline/plan_validator.go`, `pipeline/yaml_parser.go`, `pipeline/tdd_verifier.go`, `pipeline/feedback.go`, `pipeline/dep_detector.go`, `context/assembler.go`, `context/repo_analyzer.go`, `git/native.go`, `db/`.
**Verify:** Give ticket description → get multi-task commits. TDD verification catches invalid RED. YAML parser handles fenced output.

### Phase 3: Review + Quality + Skills (Week 3-4)
Build: `pipeline/spec_reviewer.go`, `pipeline/quality_reviewer.go`, `pipeline/final_reviewer.go`, `git/pr.go`, partial PR support, all review prompts, `skills/engine.go`, `skills/loader.go`, `skills/hooks.go`.
**Verify:** Reviews catch issues, trigger re-implementation, partial PR works. Skills execute at hook points.

### Phase 4: Daemon + Tracker (Week 4-5)
Build: `daemon/`, `daemon/file_lock.go`, `daemon/recovery.go`, `tracker/jira.go`, `tracker/github_issues.go`, `telemetry/cost_controller.go`, `llm/ratelimiter.go`, clarification flow with timeout, full CLI.
**Verify:** Runs 24/7, polls Jira, syncs status, respects budgets, file reservations prevent conflicts, crash recovery works.

### Phase 5: Dashboard + Polish (Week 5-6)
Build: `dashboard/`, WebSocket events, `llm/openai.go`, `llm/openrouter.go`, Docker runner with orphan cleanup, Prometheus metrics, cross-platform builds.
**Verify:** Full end-to-end with dashboard visibility.

### Phase 6: Open Source (Week 6-7)
Docs, CI/CD, release automation, Docker publishing, README, CONTRIBUTING, demo.

---

## 18. Metrics

```
foreman_tickets_total{status}                 # Counter
foreman_tickets_active                        # Gauge
foreman_tasks_total{status}                   # Counter
foreman_llm_calls_total{role,model,status}    # Counter
foreman_llm_tokens_total{direction,model}     # Counter
foreman_llm_duration_seconds{role,model}      # Histogram
foreman_cost_usd_total{model}                 # Counter
foreman_pipeline_duration_seconds             # Histogram
foreman_test_runs_total{result}               # Counter
foreman_retries_total{role}                   # Counter
foreman_rate_limits_total{provider}           # Counter
foreman_tdd_verify_total{result}              # Counter (result: pass|fail_assertion|fail_compile|fail_import)
foreman_partial_prs_total                     # Counter
foreman_clarifications_total                  # Counter
foreman_clarification_timeouts_total          # Counter
foreman_secrets_detected_total                # Counter
foreman_file_reservation_conflicts_total      # Counter
foreman_search_block_fuzzy_matches_total      # Counter
foreman_search_block_misses_total             # Counter
foreman_provider_outages_total{provider}      # Counter
foreman_crash_recoveries_total                # Counter
foreman_hook_executions_total{hook}            # Counter (hook: post_lint|pre_pr|post_pr)
foreman_skill_executions_total{skill,status}   # Counter (status: success|failed)
```

---

## 19. YAML Skills & Pipeline Hooks

### 19.1 Skills: Zero-Overhead Extensibility

Skills are YAML workflow files that compose existing pipeline primitives. No subprocess, no protocol, no registry. Contributors submit a `.yml` file via PR — that's it.

```
foreman/
└── skills/
    ├── feature-dev.yml       ← built-in
    ├── bug-fix.yml           ← built-in
    ├── refactor.yml          ← built-in
    └── community/
        ├── write-changelog.yml   ← anyone can add
        └── security-scan.yml
```

A skill file only uses primitives Foreman already has: `llm_call`, `run_command`, `file_write`, `git_diff`. No new runtime needed.

**Example skill:**

```yaml
# skills/community/write-changelog.yml
id: write-changelog
description: "Generate a changelog entry from the PR diff and ticket context"
trigger: pre_pr         # Named hook point where this skill runs

steps:
  - id: generate
    type: llm_call
    prompt_template: "prompts/changelog.md.j2"
    model: "{{ .Models.Clarifier }}"    # Use the cheapest model
    context:
      diff: "{{ .Diff }}"
      ticket: "{{ .Ticket }}"

  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ .Steps.generate.output }}"
    mode: prepend
```

**Another example:**

```yaml
# skills/community/security-scan.yml
id: security-scan
description: "Run a security-focused LLM review after lint"
trigger: post_lint

steps:
  - id: scan
    type: llm_call
    prompt_template: "prompts/security-scan.md.j2"
    model: "{{ .Models.QualityReviewer }}"
    context:
      diff: "{{ .Diff }}"
      file_tree: "{{ .FileTree }}"

  - id: check
    type: run_command
    command: "npm"
    args: ["audit", "--production"]
    allow_failure: true     # Non-zero exit doesn't fail the pipeline

  - id: report
    type: file_write
    path: ".foreman/security-report.md"
    content: |
      ## Security Scan Results
      ### LLM Review
      {{ .Steps.scan.output }}
      ### npm audit
      {{ .Steps.check.stdout }}
    mode: overwrite
```

### 19.2 Skill Step Types

Skills compose from exactly four primitives that map to existing Go functions:

| Step Type | Maps To | Description |
|---|---|---|
| `llm_call` | `LlmProvider.Complete()` | Stateless LLM call with template-rendered prompt |
| `run_command` | `CommandRunner.Run()` | Execute a shell command |
| `file_write` | `os.WriteFile()` / prepend logic | Write or prepend content to a file |
| `git_diff` | `GitProvider.Diff()` | Capture current diff as a context variable |

Each step has access to a template context containing:

```go
type SkillContext struct {
    Ticket    *Ticket              // Current ticket
    Diff      string               // Current branch diff against default
    FileTree  string               // Repo file tree
    Models    map[string]string    // Model routing config
    Config    *Config              // Foreman config
    Steps     map[string]*StepResult  // Results of previous steps in this skill
    PR        *PrResponse          // Only available in post_pr hooks
}
```

### 19.3 Skill Engine (~500 LOC)

The skill engine is a simple YAML interpreter that loads skill files, validates step types, and executes them sequentially:

```go
type Skill struct {
    ID          string     `yaml:"id"`
    Description string     `yaml:"description"`
    Trigger     string     `yaml:"trigger"`   // post_lint | pre_pr | post_pr
    Steps       []SkillStep `yaml:"steps"`
}

type SkillStep struct {
    ID             string            `yaml:"id"`
    Type           string            `yaml:"type"`    // llm_call | run_command | file_write | git_diff
    PromptTemplate string            `yaml:"prompt_template,omitempty"`
    Model          string            `yaml:"model,omitempty"`
    Context        map[string]string `yaml:"context,omitempty"`
    Command        string            `yaml:"command,omitempty"`
    Args           []string          `yaml:"args,omitempty"`
    AllowFailure   bool              `yaml:"allow_failure,omitempty"`
    Path           string            `yaml:"path,omitempty"`
    Content        string            `yaml:"content,omitempty"`
    Mode           string            `yaml:"mode,omitempty"`  // overwrite | prepend | append
}

func (e *SkillEngine) Execute(skill *Skill, sCtx *SkillContext) error {
    for _, step := range skill.Steps {
        result, err := e.executeStep(step, sCtx)
        if err != nil {
            if step.AllowFailure {
                sCtx.Steps[step.ID] = &StepResult{Error: err.Error()}
                continue
            }
            return fmt.Errorf("skill '%s' step '%s' failed: %w", skill.ID, step.ID, err)
        }
        sCtx.Steps[step.ID] = result
    }
    return nil
}

func (e *SkillEngine) executeStep(step SkillStep, sCtx *SkillContext) (*StepResult, error) {
    switch step.Type {
    case "llm_call":
        prompt, err := e.renderTemplate(step.PromptTemplate, sCtx)
        if err != nil { return nil, err }
        model := e.renderString(step.Model, sCtx)
        resp, err := e.llm.Complete(ctx, LlmRequest{
            Model: model, UserPrompt: prompt, MaxTokens: 4096, Temperature: 0.3,
        })
        if err != nil { return nil, err }
        return &StepResult{Output: resp.Content}, nil

    case "run_command":
        cmd := e.renderString(step.Command, sCtx)
        args := e.renderStrings(step.Args, sCtx)
        out := e.runner.Run(ctx, e.workDir, cmd, args, 120)
        if out.ExitCode != 0 && !step.AllowFailure {
            return nil, fmt.Errorf("command failed (exit %d): %s", out.ExitCode, out.Stderr)
        }
        return &StepResult{Output: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode}, nil

    case "file_write":
        content := e.renderString(step.Content, sCtx)
        path := filepath.Join(e.workDir, step.Path)
        switch step.Mode {
        case "prepend":
            existing, _ := os.ReadFile(path)
            content = content + "\n" + string(existing)
        case "append":
            existing, _ := os.ReadFile(path)
            content = string(existing) + "\n" + content
        }
        return &StepResult{Output: path}, os.WriteFile(path, []byte(content), 0644)

    case "git_diff":
        diff, err := e.git.Diff(ctx, e.workDir, e.defaultBranch, "HEAD")
        return &StepResult{Output: diff}, err

    default:
        return nil, fmt.Errorf("unknown step type: %s", step.Type)
    }
}
```

### 19.4 Three Pipeline Hook Points

Instead of proliferating hook points, Foreman exposes exactly three that cover 90% of real extensibility needs:

| Hook | When It Runs | Use Cases |
|---|---|---|
| `post_lint` | After lint passes, before tests | Security scanning, code complexity checks, custom static analysis |
| `pre_pr` | After all tasks complete + tests pass, before PR creation | Changelog generation, documentation updates, summary generation |
| `post_pr` | After PR is created | Slack/Discord notifications, external system updates, metrics reporting |

Each hook runs one or more named skills from the `skills/` directory. Skills execute sequentially within a hook. If any skill fails (and `allow_failure` is not set), the pipeline records the error but continues — hook failures do not block the main pipeline.

```go
func (p *Pipeline) runHook(hookName string, sCtx *SkillContext) {
    skillNames := p.config.Pipeline.Hooks[hookName]
    if len(skillNames) == 0 {
        return
    }

    for _, name := range skillNames {
        skill, err := p.skillEngine.Load(name)
        if err != nil {
            p.db.RecordEvent(ctx, &EventRecord{
                EventType: "hook_skill_not_found",
                Severity: "warn",
                Message: fmt.Sprintf("Hook '%s': skill '%s' not found: %v", hookName, name, err),
            })
            continue
        }

        if err := p.skillEngine.Execute(skill, sCtx); err != nil {
            p.db.RecordEvent(ctx, &EventRecord{
                EventType: "hook_skill_failed",
                Severity: "warn",
                Message: fmt.Sprintf("Hook '%s': skill '%s' failed: %v", hookName, name, err),
            })
            // Hook failures don't block the pipeline
        }
    }
}
```

Hook invocation points in the pipeline:

```
LINT PASS → runHook("post_lint", ...) → TESTS
ALL TASKS DONE + FULL TESTS PASS + FINAL REVIEW → runHook("pre_pr", ...) → CREATE PR
PR CREATED → runHook("post_pr", ...) → DONE
```

### 19.5 Skill Validation

On daemon startup (and on `foreman doctor`), all referenced skills are validated:

```go
func (e *SkillEngine) ValidateAll(hookConfig map[string][]string) []error {
    var errs []error
    for hook, names := range hookConfig {
        for _, name := range names {
            skill, err := e.Load(name)
            if err != nil {
                errs = append(errs, fmt.Errorf("hook '%s': skill '%s' not found", hook, name))
                continue
            }
            // Validate trigger matches hook
            if skill.Trigger != hook {
                errs = append(errs, fmt.Errorf("skill '%s' has trigger '%s' but is used in hook '%s'",
                    name, skill.Trigger, hook))
            }
            // Validate step types are known
            for _, step := range skill.Steps {
                if !isValidStepType(step.Type) {
                    errs = append(errs, fmt.Errorf("skill '%s' step '%s': unknown type '%s'",
                        name, step.ID, step.Type))
                }
            }
            // Validate prompt templates exist
            for _, step := range skill.Steps {
                if step.Type == "llm_call" && step.PromptTemplate != "" {
                    if !templateExists(step.PromptTemplate) {
                        errs = append(errs, fmt.Errorf("skill '%s' step '%s': template '%s' not found",
                            name, step.ID, step.PromptTemplate))
                    }
                }
            }
        }
    }
    return errs
}
```

---

## 20. Key Design Decisions (FAQ)

| # | Question | Answer |
|---|---|---|
| 1 | What is the file selection algorithm? | Multi-signal scored ranking: explicit planner references (100), import graph traversal 2 levels deep (70-80), directory proximity (30), test sibling (60), type definitions (50). Token-budget cutoff. See §8.1. |
| 2 | How does the daemon handle a crash mid-pipeline? | Resumes from the last committed task. The `last_completed_task_seq` field in the tickets table tracks this. On restart, incomplete tasks are reset to pending. See §7.11. |
| 3 | Where is progress data stored? | In the `progress_patterns` database table, not in git. This prevents PR noise and enables directory-scoped pruning. See §8.3. |
| 4 | What happens to Docker containers if the daemon is killed? | On restart, the daemon scans for orphaned containers labeled with `foreman-ticket` and removes any that don't correspond to active tickets. See §15.1. |
| 5 | Who runs `foreman init --analyze`? | A human, once per repo. It generates a `.foreman-context.md` template that the team reviews, edits, and commits. See §8.5. |
| 6 | Does the planner see the full codebase? | No. The planner sees the file tree, README, config files, and CI config — not source code. It specifies which files each task should read, and the implementer gets the actual contents. See §8.2. |
| 7 | What happens when an LLM provider is fully down? | Retry 3 times with 30s delay, then try fallback provider if configured, then pause the pipeline and retry on next poll cycle. The ticket is not failed. See §10.3. |
| 8 | Can `max_parallel_tickets` > 3 with SQLite? | No. Config validation caps it at 3 for SQLite. Use PostgreSQL for higher concurrency. See §4.1 and §6.5.1. |
| 9 | How are monorepos handled? | One `.foreman-context.md` at the root with sections per package, or one per package directory. The context assembler walks up from the task's directory to find the nearest one. See §8.5. |
| 10 | How does the clarification comment get posted? | As the bot account configured in the tracker (e.g., `bot@yourcompany.com` for Jira). The planner can interpret unstructured human responses because it receives all ticket comments as context. The clarification label serves as a machine-readable gate. |
| 11 | Why was `foreman run --title` removed? | Fuzzy title matching risks picking up the wrong ticket silently. Use explicit external IDs only. See §14. |
| 12 | V1 task execution: sequential or parallel? | Strictly sequential (topological order). The `depends_on` field exists for ordering constraints and future parallel execution, but V1 runs one task at a time. See §7.1. |
| 13 | How do skills work? | YAML files that compose 4 existing primitives (llm_call, run_command, file_write, git_diff). No subprocess, no protocol, no registry. ~500 LOC interpreter. See §19. |
| 14 | Can hook failures block the pipeline? | No. Hook skill failures are logged as warnings but do not block the main pipeline. Individual steps within a skill can set `allow_failure: true` to continue on error. See §19.4. |

---

## 21. Future Roadmap (Post-V1)

Not part of initial build. Documented for context only.

1. **Parallel task execution within a ticket** — Run independent tasks concurrently (DAG execution)
2. **Learning from human PR reviews** — Feed review patterns back into prompts
3. **Multi-repo support** — One daemon, multiple repos
4. **Webhook triggers** — Instant pickup instead of polling
5. **Semantic code search** — Embedding-based file discovery alongside import graph
6. **Custom skills** — User-defined skill files for domain knowledge
7. **Self-healing CI** — Auto-fix cycle when full test suite fails
8. **MCP integration** — Connect to external tools via Model Context Protocol
9. **Notification channels** — Slack/Discord/email for completions + failures
10. **Agent marketplace** — Community workflow definitions
11. **IDE extension** — Trigger from VS Code / JetBrains
12. **Observability integrations** — Grafana dashboards, PagerDuty alerts

---

**Estimated total: ~12,500-16,500 lines of Go (including ~500 LOC skill engine) + ~600 lines of prompt templates.**

**This document is the complete specification. Build it.**
