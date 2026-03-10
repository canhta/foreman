# Multi-Project Architecture Refactor — Design Spec

**Date:** 2026-03-10
**Status:** Approved

## Overview

Refactor Foreman from a single-project deployment to a multi-project architecture. Remove PostgreSQL support. Redesign the UI for multi-project workflows with project boards, dashboards, and an agent chat interface.

## Goals

1. Support multiple independent projects within a single Foreman instance.
2. Remove PostgreSQL — use one SQLite database per project for natural isolation.
3. Redesign the frontend for multi-project navigation, Jira-style project boards, and agent communication via chat.
4. Maintain CLI and dashboard parity for project management.

## Non-Goals

- Multi-tenancy / multi-user with role-based access control.
- Drag-and-drop board (status is pipeline-driven, not manual).
- Bidirectional sync between dashboard chat and tracker comments.

---

## 1. Project Data Model & Storage

### Directory Structure

```
~/.foreman/
  config.toml                    # global config
  projects.json                  # lightweight index (id, name, created_at, active)
  projects/
    <uuid>/
      config.toml                # project-specific config
      foreman.db                 # project SQLite database
      work/                      # git worktrees for this project
      ssh/                       # project-specific deploy keys
```

### projects.json

A lightweight index so the sidebar and global overview can list projects without opening every SQLite DB.

```json
{
  "projects": [
    {
      "id": "uuid-1",
      "name": "SessionUp",
      "created_at": "2026-01-15T10:00:00Z",
      "active": true
    }
  ]
}
```

Updated when projects are created or deleted. All writes to this file go through the daemon process (CLI commands call the daemon API), avoiding concurrent write races.

### Global Config (`~/.foreman/config.toml`)

Contains only shared infrastructure. The `[database]` section is removed entirely — each project gets its own `foreman.db` automatically.

```toml
[daemon]
poll_interval_secs = 60
idle_poll_interval_secs = 300
log_level = "info"
log_format = "json"
lock_ttl_seconds = 3600

[dashboard]
enabled = true
port = 8080
host = "127.0.0.1"
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"

[llm]
default_provider = "anthropic"
embedding_provider = ""
embedding_model = ""
[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"
[llm.openai]
api_key = "${OPENAI_API_KEY}"
[llm.openrouter]
api_key = "${OPENROUTER_API_KEY}"
[llm.local]
base_url = ""
[llm.outage]
fallback_provider = ""
max_connection_retries = 3
retry_delay_secs = 5

[cost]
max_cost_per_day_usd = 150.0
max_cost_per_month_usd = 3000.0
alert_threshold_percent = 80

[channel]
provider = "whatsapp"
[channel.whatsapp]
session_db = "~/.foreman/whatsapp.db"
dm_policy = "pairing"

[secrets]
enabled = true
always_exclude = [".env", ".env.*", "*.pem", "*.key", "*.p12"]

[mcp]
resource_max_bytes = 524288

[runner]
mode = "local"    # "local" | "docker" — global default, overridable per project

[rate_limit]
# global rate limiting defaults
```

### Project Config (`~/.foreman/projects/<id>/config.toml`)

Contains everything specific to a project. Fields previously under `[daemon]` that are per-project (like `max_parallel_tickets`, `max_parallel_tasks`, `task_timeout_minutes`, `env_files`) move to the project's `[limits]` or relevant section.

```toml
[project]
name = "SessionUp"
description = "Mobile app backend"

[tracker]
provider = "github"
pickup_label = "foreman-ready"
clarification_label = "foreman-needs-info"
clarification_timeout_hours = 72
[tracker.github]
token = "${GITHUB_TOKEN}"
owner = "myorg"
repo = "sessionup"

[git]
provider = "github"
clone_url = "git@github.com:myorg/sessionup.git"
default_branch = "main"
auto_push = true
pr_draft = true
pr_reviewers = []
branch_prefix = "foreman"
rebase_before_pr = true
[git.github]
token = "${GITHUB_TOKEN}"
[git.worktree]
start_command = ""

[models]
planner = "anthropic:claude-sonnet-4-6"
implementer = "anthropic:claude-sonnet-4-6"
spec_reviewer = "anthropic:claude-haiku-4-5"
quality_reviewer = "anthropic:claude-haiku-4-5"
final_reviewer = "anthropic:claude-sonnet-4-6"
clarifier = "anthropic:claude-haiku-4-5"

# Optional per-project LLM API key overrides (falls back to global if unset)
[llm]
[llm.anthropic]
api_key = ""
[llm.openai]
api_key = ""

[cost]
max_cost_per_ticket_usd = 15.0

[limits]
max_tasks_per_ticket = 20
max_parallel_tickets = 3       # moved from [daemon]; max 3 (SQLite constraint)
max_parallel_tasks = 3         # moved from [daemon]
task_timeout_minutes = 15      # moved from [daemon]
max_implementation_retries = 2
max_spec_review_cycles = 2
max_quality_review_cycles = 1
max_llm_calls_per_task = 8
max_task_duration_secs = 600
max_total_duration_secs = 7200
context_token_budget = 80000
enable_partial_pr = true
enable_clarification = true
enable_tdd_verification = true
search_replace_similarity = 0.92
search_replace_min_context_lines = 3
plan_confidence_threshold = 0.60
intermediate_review_interval = 3
conflict_resolution_token_budget = 40000

[agent_runner]
provider = "builtin"
[agent_runner.builtin]
max_turns = 30
max_context_tokens = 100000

[skills.agent_runner]
provider = "builtin"
max_cost_per_ticket_usd = 2.0
max_turns_default = 10

[decompose]
enabled = false
max_ticket_words = 150
max_scope_keywords = 2
approval_label = "foreman-ready"
parent_label = "foreman-decomposed"
llm_assist = false

[context]
context_feedback_boost = 1.5
context_generate_max_tokens = 32000

[runner]
mode = "local"    # override global if needed

[env_files]
# map worktree paths to external .env files for this project
```

### Config Resolution

Project config values take precedence over global defaults. LLM API keys check project config first, then fall back to global. This allows different projects to bill to different accounts.

### Config Field Migration Reference

Fields that move from the current monolithic config to project-level:

| Current Location | New Location | Notes |
|---|---|---|
| `[daemon].max_parallel_tickets` | Project `[limits]` | Max 3 (SQLite). Remove the "use PostgreSQL" validation message. |
| `[daemon].max_parallel_tasks` | Project `[limits]` | |
| `[daemon].task_timeout_minutes` | Project `[limits]` | |
| `[daemon].work_dir` | Automatic (`projects/<id>/work/`) | No longer configurable |
| `[daemon].env_files` | Project `[env_files]` | Per-project env file mapping |
| `[tracker]` (entire section) | Project `[tracker]` | |
| `[git]` (entire section) | Project `[git]` | |
| `[models]` (entire section) | Project `[models]` | |
| `[agent_runner]` (entire section) | Project `[agent_runner]` | |
| `[skills.agent_runner]` | Project `[skills.agent_runner]` | |
| `[decompose]` (entire section) | Project `[decompose]` | |
| `[context]` (entire section) | Project `[context]` | |
| `[limits]` (entire section) | Project `[limits]` | |
| `[runner]` | Project `[runner]` | Overridable, falls back to global |
| `[rate_limit]` | Project `[rate_limit]` | Overridable, falls back to global |
| `[database]` | Removed | Each project auto-creates `foreman.db` |

Fields that stay global: `[daemon]` (poll intervals, log settings, lock TTL), `[dashboard]`, `[llm]` (API keys — default), `[cost]` (daily/monthly limits), `[channel]`, `[secrets]`, `[mcp]`.

### Database Schema

No changes to existing table schemas. Each project's `foreman.db` contains the same tables (tickets, tasks, llm_calls, etc.). The only new table is `chat_messages` (see Section 8).

---

## 2. Backend Architecture

### New Package: `internal/project/`

#### ProjectManager

Top-level coordinator that manages all projects:

- `LoadProjects()` — scans `~/.foreman/projects/`, builds project registry
- `CreateProject(config)` — creates directory, writes config, initializes DB, starts worker
- `DeleteProject(id)` — stops worker, removes directory, updates index
- `GetProject(id)` — returns project instance
- `ListProjects()` — returns from `projects.json` index

#### ProjectWorker

One per active project, runs independently as a goroutine group:

- Owns: merged config, SQLite database connection, orchestrator, tracker client, git provider, cost controller
- Lifecycle: `Start()`, `Stop()`, `Pause()`, `Resume()`, `Status()`
- Each worker runs its own poll/execute loop
- Workers are isolated — one crashing logs the error and does not affect others

#### Daemon Startup Flow

1. Daemon starts, loads global config, creates `ProjectManager`
2. `ProjectManager.LoadProjects()` scans `~/.foreman/projects/`
3. For each project directory, creates a `ProjectWorker` with merged config (global + project)
4. Each worker starts its own goroutine loop
5. Adding a project (via CLI or dashboard) creates a new worker at runtime
6. Deleting a project stops the worker and removes the directory

#### Global Cost Controller

Sits above workers. Before each LLM call, the worker checks:
1. Project budget OK? (per-ticket limit from project config)
2. Global budget OK? (daily/monthly limit from global config)

Global controller aggregation strategy:
- Each `ProjectWorker` reports its LLM call costs to the `GlobalCostController` via an in-process channel (push model, not pull).
- The controller maintains a cached running total for the current day/month.
- On startup, the controller queries each project DB once to seed the cache.
- This avoids repeatedly opening N SQLite databases and keeps the hot path lock-free.

#### Daemon Subsystem Assignment

The current `Daemon` manages several subsystems beyond the orchestrator. Assignment for multi-project:

| Subsystem | Scope | Rationale |
|---|---|---|
| Orchestrator (poll + execute) | Per-project | Different trackers, repos, configs |
| MergeChecker | Per-project | Different git repos and PR APIs |
| Scheduler / task prioritization | Per-project | Independent task queues |
| Clarification timeout checker | Per-project | Different timeout configs |
| WhatsApp channel listener | Global | One phone number, routes messages to correct project |
| Docker cleanup | Global | Shared container runtime |
| Prometheus metrics | Global | Single metrics endpoint, labeled by project |
| Cost controller (daily/monthly) | Global | Aggregate budget enforcement |

#### Key Changes to Existing Code

- `daemon.Start()` creates a `ProjectManager` instead of a single orchestrator
- `Orchestrator` needs minimal changes — it already takes config, DB, tracker, git as dependencies; it gets instantiated per project now
- Remove `internal/db/postgres.go` (~1,500 lines) and `postgres_test.go`
- Remove `pgx` and `sqlx` dependencies from `go.mod`
- Remove PostgreSQL config options from config structs and validation
- Update config validation: remove "use PostgreSQL for higher concurrency" message for `max_parallel_tickets > 3`; enforce max 3 as a hard limit
- Update `AGENTS.md` to remove pgx/sqlx references
- Update `foreman.example.toml` to remove PostgreSQL sections and references
- Update `internal/config/config.go`: remove `expandEnvVars` handling for `cfg.Database.Postgres.URL`
- Leverage existing `internal/config/persist.go` for runtime config updates from the dashboard

---

## 3. API Layer

### Global Endpoints

```
GET  /api/projects                         — list all projects
POST /api/projects                         — create project
GET  /api/overview                         — global dashboard metrics
GET  /api/overview/cost                    — total spend across projects
GET  /api/overview/activity                — recent activity across projects
GET  /api/config                           — read global config
PUT  /api/config                           — update global config
```

### Project-Scoped Endpoints

```
GET    /api/projects/:pid                  — project details + config
PUT    /api/projects/:pid                  — update project config
DELETE /api/projects/:pid                  — delete project

GET    /api/projects/:pid/tickets          — list tickets (with filters)
GET    /api/projects/:pid/tickets/:id      — ticket detail
POST   /api/projects/:pid/tickets/:id/retry — retry failed ticket
DELETE /api/projects/:pid/tickets/:id      — delete ticket

GET    /api/projects/:pid/tickets/:id/tasks      — task list
GET    /api/projects/:pid/tickets/:id/llm-calls  — LLM call history
GET    /api/projects/:pid/tickets/:id/events     — activity log

GET    /api/projects/:pid/cost/daily/:date       — daily cost
GET    /api/projects/:pid/cost/monthly/:yearMonth — monthly cost
GET    /api/projects/:pid/cost/breakdown         — by runner/model/role

POST   /api/projects/:pid/sync             — trigger tracker sync
POST   /api/projects/:pid/pause            — pause project
POST   /api/projects/:pid/resume           — resume project
GET    /api/projects/:pid/health           — project health

GET    /api/projects/:pid/dashboard        — project dashboard metrics
```

### Chat Endpoints

```
GET    /api/projects/:pid/tickets/:id/chat — chat history
POST   /api/projects/:pid/tickets/:id/chat — send message (user reply)
```

### WebSocket

```
WS     /ws/projects/:pid                   — project-scoped events
WS     /ws/global                          — global activity stream
```

### WebSocket Multiplexing

The existing `EventSubscriber` / `telemetry.EventEmitter` pattern becomes project-aware:
- Each `ProjectWorker` has its own event emitter
- Events are tagged with project ID
- `/ws/projects/:pid` subscribes to a single project's emitter
- `/ws/global` subscribes to all project emitters (fan-in)
- The API layer manages subscription routing based on WebSocket path

### Routing

API handler receives project ID, looks up the corresponding `ProjectWorker` from `ProjectManager`, routes to that worker's database/services. Returns 404 if project not found or stopped.

### Authentication

Unchanged — bearer token on all endpoints.

---

## 4. Frontend Architecture

### Tech Stack

- Svelte (stay on current framework)
- Add client-side routing
- Component-based layout with shared shell

### Layout

```
┌──────────────────────────────────────────────────┐
│  Sidebar             │  Main Content Area         │
│                      │                            │
│  [Logo / Foreman]    │  (changes per route)       │
│                      │                            │
│  Overview            │                            │
│                      │                            │
│  ── Projects ──      │                            │
│  > SessionUp    ●    │                            │
│  > MyApp        ⚠    │                            │
│  > ClientAPI    ●    │                            │
│                      │                            │
│  [+ Add Project]     │                            │
│                      │                            │
│  ── bottom ──        │                            │
│  Settings            │                            │
│  User / Logout       │                            │
└──────────────────────────────────────────────────┘
```

Sidebar is collapsible. Project list shows status indicators (running/blocked/error).

### Routes

```
/                              → Global overview dashboard
/projects/new                  → New project wizard
/projects/:pid/board           → Project board (ticket columns)
/projects/:pid/dashboard       → Project metrics dashboard
/projects/:pid/settings        → Project config editor
```

Within a project, secondary tabs (Board / Dashboard / Settings) at the top of the content area.

### Key Components

- `Sidebar.svelte` — project list, navigation
- `GlobalOverview.svelte` — aggregated dashboard
- `ProjectBoard.svelte` — kanban ticket columns
- `ProjectDashboard.svelte` — project metrics
- `ProjectSettings.svelte` — config editor with test buttons
- `TicketPanel.svelte` — side panel detail
- `TicketFullView.svelte` — full page detail with chat
- `ChatInterface.svelte` — agent communication
- `ProjectWizard.svelte` — onboarding / new project form

---

## 5. Project Board

### Columns

| Queued | Planning | In Progress | In Review | Awaiting Merge | Merged | Failed |

Full status-to-column mapping:

| Ticket Status | Board Column |
|---|---|
| `queued` | Queued |
| `clarification_pending` | Queued (with "needs input" badge) |
| `planning` | Planning |
| `implementing` | In Progress |
| `in_review` (spec/quality/final) | In Review |
| `awaiting_merge` | Awaiting Merge |
| `merged` | Merged |
| `failed` | Failed |

Internal pipeline states (`spec_reviewing`, `quality_reviewing`, `testing`, `committing`) are visible in the ticket detail view, not as separate columns.

### Ticket Card

Each card shows:
- Ticket ID + title
- Task progress bar (e.g., "5/8 tasks")
- Agent runner + cost so far
- PR status (if exists)
- Active agent count
- Visual indicator if agent is **waiting for input** — critical for PM visibility

### Board Behavior

- Click card → opens side panel
- Cards are read-only (status is pipeline-driven, not manual drag-and-drop)
- Filter bar: by status, by label, search by title
- Sort: by created date, by cost, by progress

The board is an **observation tool**, not a planning tool. User actions: retry failed tickets, respond to clarifications, review PRs.

---

## 6. Global Overview Dashboard

Answers "what's happening across all my projects?"

### Summary Cards (Top)

- Total cost today
- Active tickets (total across projects)
- Open PRs (total)
- **Tickets needing input** (most important — blocked agents)

### Sections

- **Recent Activity** — timeline of events across all projects (project name, ticket ID, event, timestamp)
- **Project Summary Table** — per-project row: active tickets, PRs, cost today, status. Click to navigate.
- **Cost Trend** — 7-day bar chart of spend across all projects

### Implementation

Queries each project's SQLite DB. `ProjectManager` caches summary stats, refreshed periodically or on WebSocket events. No global database needed.

---

## 7. Project Dashboard

Per-project metrics page. Answers "how is this project performing?"

### Metrics

- **Cost:** total spend, by model, by pipeline stage, per ticket. Daily/monthly trends.
- **Ticket throughput:** completed per day/week, average queued-to-merged time, success rate.
- **PR metrics:** created, average review time, merge rate.
- **Model usage:** calls per model, tokens per model, cache hit rates.
- **Agent activity:** tasks executed, average per ticket, retry rates, failure classifications.

### Design Note

Keep this simple initially. Basic tables and a couple of charts. The board is the primary view; the dashboard is a "check weekly" view. Enhance iteratively.

---

## 8. Chat Interface & Agent Communication

### New Table: `chat_messages`

Added to each project's SQLite database:

```sql
CREATE TABLE IF NOT EXISTS chat_messages (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL,
    sender TEXT NOT NULL,            -- 'agent' | 'user' | 'system'
    message_type TEXT NOT NULL,      -- 'clarification' | 'action_request' | 'info' | 'error' | 'reply'
    content TEXT NOT NULL,
    metadata TEXT,                   -- JSON: task_id, options for action requests, etc.
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_ticket ON chat_messages(ticket_id, created_at);
```

### Agent-to-User Flow

1. Agent hits a blocker (unclear requirement, needs confirmation, etc.)
2. Pipeline writes a `chat_message` with `sender=agent`, appropriate `message_type`
3. WebSocket pushes the message to the dashboard
4. Notification forwarded via WhatsApp: "SessionUp / TICKET-45: Agent needs input" with dashboard link
5. User opens dashboard, sees chat in side panel or full view
6. User replies via `POST /api/projects/:pid/tickets/:id/chat`
7. Message stored; agent's next iteration picks it up
8. Ticket stays `clarification_pending` until user replies

### Action Request Types

- **Clarification** — "Which endpoint should I optimize?"
- **Confirmation** — "I need to delete the legacy auth module. Proceed?"
- **Retry guidance** — "Tests failed twice. Try different strategy or skip?"
- **Choice** — "Approach X or Y?" (with structured options in metadata)

### Integration

The existing pipeline clarification logic writes to `chat_messages` instead of tracker comments. The WhatsApp channel becomes a notification forwarder (summary + link) rather than a full conversation channel. Clarification timeouts and confidence thresholds remain unchanged.

---

## 9. Onboarding & Project Creation

### Dashboard — New Project Wizard

Step-by-step form at `/projects/new`:

1. **Project basics** — name, description
2. **Repository** — clone URL, default branch, git provider, access token. "Test connection" button.
3. **Issue tracker** — provider, credentials, project key/labels. "Test connection" button.
4. **Agent configuration** — runner, model selection per role. Defaults pre-filled from global config.
5. **Budget** — per-ticket cost limit.
6. **Review & Create** — summary, create button.

On create: creates directory, writes config, initializes DB, clones repo, updates index, starts worker, redirects to board.

### CLI

```
foreman project create --name "MyApp" --clone-url "git@github.com:org/repo.git" --tracker github
foreman project list
foreman project delete <id>
```

Interactive mode asks missing fields. Non-interactive requires all flags.

### Project Settings

`/projects/:pid/settings` — same fields as wizard, editable. Changes written to project `config.toml`. Worker restart warning for breaking changes. "Test connection" buttons. "Delete project" with confirmation.

### First-Time Global Setup

If `~/.foreman/config.toml` doesn't exist:
- Dashboard shows one-time global setup screen (LLM API keys, dashboard auth)
- CLI: `foreman init`

---

## 10. Ticket Detail View

### Side Panel (Default)

Opens from right (~40% width) when clicking a ticket on the board. Shows:
- Status badge, ticket ID, title
- Task progress bar
- Linked PR with status
- Cost so far
- Recent chat messages (last 3-5)
- "Expand" button to full page

### Full Page View

```
┌──────────────────────────────────────────────┐
│  Ticket Title                  [Status] [PR] │
│──────────────────────────────────────────────│
│  Left Column (60%)    │  Right Column (40%)  │
│                       │                      │
│  Description          │  Chat Interface      │
│  Subtasks list        │                      │
│  (status, cost,       │  Agent messages      │
│   agent runner        │  User replies        │
│   per task)           │  Action requests     │
│                       │  [Input box]         │
│  PR Status            │                      │
│  Cost Breakdown       │                      │
│  Events / Activity    │                      │
└──────────────────────────────────────────────┘
```

---

## 11. Migration Strategy

For existing single-project Foreman deployments:

1. Detect existing `foreman.toml` at startup
2. Auto-migrate: create `~/.foreman/projects/<uuid>/` directory
3. Split config into global + project
4. Move existing `foreman.db` (SQLite) into the project directory
5. Update `projects.json` index
6. Log the migration and continue

This ensures zero manual migration effort for existing SQLite users.

### PostgreSQL Migration

Users currently running with `database.driver = "postgres"` cannot be auto-migrated. On detecting a PostgreSQL config:
1. Log a clear warning with instructions
2. Provide a CLI command: `foreman migrate-from-postgres` that exports PostgreSQL data to a SQLite `foreman.db`
3. Refuse to start until migration is completed or the user explicitly opts for a fresh start

This is expected to affect very few deployments.

---

## 12. Implementation Phases

### Phase 1: Multi-Project Backend

Sub-phase 1a — PostgreSQL removal (independent, can land first):
- Delete `internal/db/postgres.go` and `postgres_test.go`
- Remove `pgx` and `sqlx` from `go.mod`
- Remove PostgreSQL config options from structs and validation
- Update `foreman.example.toml`, `AGENTS.md`, docs
- Remove `expandEnvVars` handling for `cfg.Database.Postgres.URL`
- Update `max_parallel_tickets` validation (hard limit at 3, remove "use PostgreSQL" message)

Sub-phase 1b — Multi-project backend:
- Create `internal/project/` package (ProjectManager, ProjectWorker)
- Split config system (global + per-project, with fallback resolution)
- Config field migration (see Section 1 reference table)
- Directory-per-project structure with per-project SQLite
- `projects.json` index management (daemon-serialized writes)
- Global cost controller with push-based aggregation
- Refactor daemon startup: ProjectManager replaces single orchestrator
- Per-project subsystems: orchestrator, merge checker, clarification timeout
- Global subsystems: WhatsApp channel, Docker cleanup, Prometheus metrics
- WebSocket event multiplexing (project-scoped + global fan-in)
- CLI commands: `foreman project create/list/delete`
- Auto-migration from single-project setup (SQLite path)
- `foreman migrate-from-postgres` command for PostgreSQL users
- Update API endpoints to be project-scoped
- Add global endpoints (`/api/projects`, `/api/overview`)
- Add `chat_messages` table to schema
- Leverage `internal/config/persist.go` for dashboard config editing

### Phase 2: Frontend Redesign

- Add client-side routing
- Sidebar navigation with project list
- Global overview page
- Project board (kanban columns with ticket cards)
- Ticket side panel (expandable to full page)
- Project dashboard (metrics)
- Project settings page (config editor with test buttons)
- New project wizard
- Update WebSocket for project-scoped channels

### Phase 3: Chat & Notifications

- Chat interface component in ticket detail view
- Chat API endpoints
- Modify pipeline clarification logic to write to chat_messages
- WhatsApp channel as notification forwarder (summary + dashboard link)
- Action request UI (structured choices, confirmations)

### Phase Dependencies

- Phase 2 depends on Phase 1 (frontend needs project-scoped API)
- Phase 3 depends on Phase 2 (chat UI) and Phase 1 (chat table)
- Phase 1a (PostgreSQL removal) is independent and can land first
- Phase 1b depends on 1a being complete
