# Config & Usage Drawer + Real-time Enrichments — Design

## Problem

1. **No config visibility** — Users must SSH in and read the TOML file to verify configuration.
2. **No activity attribution** — Can't see which runner (builtin/claudecode/copilot) or model handled which task in real-time.
3. **No Claude Code CLI usage** — No visibility into external Claude Code session costs alongside Foreman's own tracking.
4. **Gaps in real-time data** — Agent turns, tool calls, and model selection aren't streamed to the dashboard. Only ticket-level events are visible.

## Solution

Two workstreams:

**A. Settings Drawer** — Right-side slide-out drawer with Config + Usage tabs.
**B. Real-time Enrichments** — Stream agent-level events to dashboard, enrich LiveFeed and TaskCard with runner/model info.

## Scope

- Settings drawer with Config tab and Usage tab (dashboard)
- Activity breakdown by runner, model, and role
- Claude Code CLI usage (parse local `~/.claude/` JSONL session files)
- Real-time agent turn/tool events streamed to dashboard WebSocket
- Enriched LiveFeed with runner/model badges
- Enriched TaskCard with live execution status
- CLI `foreman config` command
- No GitHub Copilot integration (separate accounts)
- No external API keys required

---

## A. Settings Drawer

### Backend

**New API Endpoints:**

| Endpoint | Purpose |
|----------|---------|
| `GET /api/config/summary` | Returns redacted operational config summary |
| `GET /api/usage/claude-code` | Parses Claude Code JSONL session files, returns aggregated usage |
| `GET /api/usage/activity` | Aggregated LLM call breakdown by runner, model, and role |

**Config Summary Endpoint:**

Returns a structured JSON object with sections:

```json
{
  "llm": {
    "provider": "anthropic",
    "models": {
      "planner": "claude-sonnet-4-6",
      "implementer": "claude-sonnet-4-6",
      "spec_reviewer": "claude-sonnet-4-6",
      "quality_reviewer": "claude-sonnet-4-6",
      "final_reviewer": "claude-sonnet-4-6"
    },
    "api_key": "sk-ant-...a1b2"
  },
  "tracker": {
    "provider": "github",
    "poll_interval": "30s"
  },
  "git": {
    "provider": "github",
    "clone_url": "git@github.com:org/repo.git",
    "branch_prefix": "foreman/",
    "auto_merge": false
  },
  "agent_runner": {
    "provider": "builtin",
    "turn_limit": 50,
    "token_budget": 100000
  },
  "cost": {
    "daily_budget": 10.0,
    "monthly_budget": 200.0,
    "per_ticket_budget": 5.0,
    "alert_threshold": 0.8
  },
  "daemon": {
    "max_parallel_tickets": 2,
    "max_parallel_tasks": 3,
    "work_dir": "~/.foreman/work",
    "log_level": "info"
  },
  "database": {
    "driver": "sqlite",
    "path": "~/.foreman/foreman.db"
  },
  "mcp": {
    "servers": ["server1", "server2"]
  },
  "rate_limit": {
    "requests_per_minute": 50
  }
}
```

API keys redacted to last 4 characters. Passwords and tokens follow the same pattern.

**Claude Code Usage Endpoint:**

Scans `~/.claude/projects/` for JSONL session files. Each file contains message records with token fields: `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`.

Returns:

```json
{
  "available": true,
  "today": {
    "sessions": 5,
    "input_tokens": 150000,
    "output_tokens": 45000,
    "cache_read_tokens": 80000,
    "estimated_cost_usd": 1.25
  },
  "last_7_days": [
    { "date": "2026-03-09", "input_tokens": 150000, "output_tokens": 45000, "cost_usd": 1.25 },
    { "date": "2026-03-08", "input_tokens": 200000, "output_tokens": 60000, "cost_usd": 1.80 }
  ],
  "total_sessions": 42
}
```

If `~/.claude/` does not exist or has no session files, returns `{ "available": false }`.

Cost estimation uses published Anthropic pricing (default to Sonnet pricing).

**Activity Breakdown Endpoint:**

Aggregates `llm_calls` table data. Supports `?days=7` query param (default 7).

```json
{
  "by_runner": [
    { "runner": "builtin", "calls": 120, "tokens_in": 500000, "tokens_out": 150000, "cost_usd": 4.50 },
    { "runner": "claudecode", "calls": 45, "tokens_in": 800000, "tokens_out": 200000, "cost_usd": 7.20 }
  ],
  "by_model": [
    { "model": "claude-sonnet-4-6", "calls": 150, "tokens_in": 1200000, "tokens_out": 320000, "cost_usd": 10.50 },
    { "model": "claude-haiku-4-5", "calls": 15, "tokens_in": 100000, "tokens_out": 30000, "cost_usd": 1.20 }
  ],
  "by_role": [
    { "role": "planner", "runner": "builtin", "model": "claude-sonnet-4-6", "calls": 30, "cost_usd": 2.10 },
    { "role": "implementer", "runner": "claudecode", "model": "claude-sonnet-4-6", "calls": 45, "cost_usd": 7.20 },
    { "role": "quality_reviewer", "runner": "builtin", "model": "claude-sonnet-4-6", "calls": 40, "cost_usd": 1.80 }
  ],
  "recent_calls": [
    {
      "ticket_id": "PROJ-42",
      "ticket_title": "Add user validation",
      "task_title": "Add validation to user input",
      "role": "implementer",
      "runner": "claudecode",
      "model": "claude-sonnet-4-6",
      "tokens_in": 45000,
      "tokens_out": 12000,
      "cost_usd": 0.45,
      "status": "success",
      "duration_ms": 12000,
      "timestamp": "2026-03-09T14:30:00Z"
    }
  ]
}
```

### Frontend — SettingsDrawer.svelte

**Trigger:** Gear icon button in `Header.svelte`, positioned between SYNC and SYS buttons.

```
[FOREMAN] [●RUNNING · $4.20/$10 · 2 ACTIVE] [PAUSE] [SYNC] [⚙] [SYS]
```

**Drawer mechanics:**
- Slides in from right edge, overlays content (does not push layout)
- Full viewport height, 420px wide (360px on tablet)
- `z-60` to layer above header and feed
- Backdrop: semi-transparent `bg-bg/60` click-to-dismiss
- Keyboard: `Escape` closes, `1`/`2` switches tabs when drawer is open
- Transition: `translateX(100%) -> translateX(0)` over 150ms ease-out

**Layout structure:**

```
+------------------------------------------+
| [X]  SETTINGS                            |  <- Header bar (40px, matches main header height)
+------------------------------------------+
| [  CONFIG  ] [  USAGE  ]                 |  <- Tab bar, inverted accent for active
+------------------------------------------+
|                                          |
|  (scrollable content area)               |
|                                          |
+------------------------------------------+
```

#### Config Tab

Sections rendered as bordered panels (matching SystemHealth pattern):

```
+------------------------------------------+
| ■ LLM                                   |  <- border-l-4 border-l-accent, bg-surface-active
+------------------------------------------+
|  PROVIDER      anthropic                 |
|  PLANNER       claude-sonnet-4-6         |
|  IMPLEMENTER   claude-sonnet-4-6         |
|  REVIEWERS     claude-sonnet-4-6         |
|  API KEY       sk-ant-...a1b2            |
+------------------------------------------+

+------------------------------------------+
| ■ TRACKER                                |
+------------------------------------------+
|  PROVIDER      github                    |
|  POLL INTERVAL 30s                       |
+------------------------------------------+

+------------------------------------------+
| ■ GIT                                    |
+------------------------------------------+
|  PROVIDER      github                    |
|  CLONE URL     git@github.com:org/repo   |
|  BRANCH PREFIX foreman/                  |
|  AUTO MERGE    OFF                       |
+------------------------------------------+

+------------------------------------------+
| ■ AGENT RUNNER                           |
+------------------------------------------+
|  PROVIDER      builtin                   |
|  TURN LIMIT    50                        |
|  TOKEN BUDGET  100,000                   |
+------------------------------------------+

+------------------------------------------+
| ■ COST BUDGETS                           |
+------------------------------------------+
|  DAILY         $10.00                    |
|  MONTHLY       $200.00                   |
|  PER TICKET    $5.00                     |
|  ALERT AT      80%                       |
+------------------------------------------+

+------------------------------------------+
| ■ DAEMON                                 |
+------------------------------------------+
|  PARALLEL TIX  2                         |
|  PARALLEL TASKS 3                        |
|  WORK DIR      ~/.foreman/work           |
|  LOG LEVEL     info                      |
+------------------------------------------+

+------------------------------------------+
| ■ DATABASE                               |
+------------------------------------------+
|  DRIVER        sqlite                    |
|  PATH          ~/.foreman/foreman.db     |
+------------------------------------------+

+------------------------------------------+
| ■ MCP SERVERS                            |
+------------------------------------------+
|  · server1                               |
|  · server2                               |
+------------------------------------------+

+------------------------------------------+
| ■ RATE LIMIT                             |
+------------------------------------------+
|  REQUESTS/MIN  50                        |
+------------------------------------------+
```

Each panel:
- `border-2 border-border` outer
- Header: `px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent`
- Header text: `text-[10px] font-bold tracking-[0.2em] text-text`
- Row: `flex justify-between px-3 py-1` with label `text-[10px] text-muted tracking-wider` and value `text-xs text-text`
- Boolean values: `ON` in `text-success` or `OFF` in `text-muted`
- Redacted keys: dimmed with `text-muted-bright`, last 4 chars in `text-text`

#### Usage Tab

Three sections stacked vertically:

**Section 1: Foreman Costs**

Reuses existing `appState` data (dailyCost, monthlyBudget, weekDays etc). Same visual pattern as SystemHealth cost budgets section:

```
+------------------------------------------+
| ■ FOREMAN COSTS                          |
+------------------------------------------+
|  DAILY    $4.20 / $10     [========  ]   |
|  MONTHLY  $89.50 / $200   [====      ]   |
|  WEEKLY   $28.40                         |
+------------------------------------------+
|  DAILY | $4.2 | $3.8 | $5.1 | ... | ... |  <- 7-day mini bar chart
+------------------------------------------+
```

Budget bars follow SystemHealth pattern: `h-2 bg-border` track, `bg-accent` fill, `bg-warning` at 75%, `bg-danger` at 90%.

7-day mini chart: vertical bars per day, height proportional to max day cost, accent fill, `text-[9px]` date labels below. Each bar is a `div` with dynamic height inside a fixed-height container (48px). Hover shows exact cost.

**Section 2: Activity Breakdown**

```
+------------------------------------------+
| ■ ACTIVITY BREAKDOWN                     |
+------------------------------------------+
|  BY RUNNER                               |
|  builtin     120 calls  [======    ] $4.50|
|  claudecode   45 calls  [========  ] $7.20|
|                                          |
|  BY MODEL                                |
|  claude-sonnet-4-6  150 [=========] $10.50|
|  claude-haiku-4-5    15 [=        ]  $1.20|
+------------------------------------------+
|  ROLE MAPPING                            |
|  planner          builtin   sonnet  $2.10 |
|  implementer      claudecode sonnet $7.20 |
|  spec_reviewer    builtin   sonnet  $0.90 |
|  quality_reviewer builtin   sonnet  $1.80 |
|  final_reviewer   builtin   sonnet  $0.60 |
+------------------------------------------+
|  RECENT CALLS                            |
|  14:30  implementer  claudecode  sonnet   |
|         "Add validation..." PROJ-42 $0.45 |
|  14:25  quality_rev  builtin     sonnet   |
|         "Fix auth flow" PROJ-41   $0.22   |
|  ...                                      |
+------------------------------------------+
```

Runner bars: color-coded (`bg-accent` for claudecode, `bg-muted-bright` for builtin, `bg-purple-400` for copilot). Proportional width based on cost share.

Model bars: `bg-accent` fill, width proportional to call count share.

Role mapping: compact table with runner badge (same `runnerBadgeCls` pattern from TaskCard) and truncated model name. Shows the configured pipeline at a glance.

Recent calls: last 10, each showing timestamp, role, runner badge, model, ticket title (clickable), and cost. Matches LiveFeed density.

**Section 3: Claude Code CLI**

```
+------------------------------------------+
| ■ CLAUDE CODE                            |
+------------------------------------------+
|  TODAY                                   |
|  5 sessions   150K in / 45K out   $1.25  |
|                                          |
|  LAST 7 DAYS                             |
|  Mar 9   150K in  45K out   $1.25        |
|  Mar 8   200K in  60K out   $1.80        |
|  Mar 7   180K in  52K out   $1.55        |
|  ...                                     |
|                                          |
|  42 total sessions                       |
+------------------------------------------+
```

Or if unavailable:

```
+------------------------------------------+
| ■ CLAUDE CODE                            |
+------------------------------------------+
|  Claude Code CLI data not found.         |
|  Install: npm i -g @anthropic-ai/claude-code |
+------------------------------------------+
```

### Frontend — State & Types

**New types in `types.ts`:**

```typescript
export interface ConfigSummary {
  llm: { provider: string; models: Record<string, string>; api_key: string };
  tracker: { provider: string; poll_interval: string };
  git: { provider: string; clone_url: string; branch_prefix: string; auto_merge: boolean };
  agent_runner: { provider: string; turn_limit: number; token_budget: number };
  cost: { daily_budget: number; monthly_budget: number; per_ticket_budget: number; alert_threshold: number };
  daemon: { max_parallel_tickets: number; max_parallel_tasks: number; work_dir: string; log_level: string };
  database: { driver: string; path: string };
  mcp: { servers: string[] };
  rate_limit: { requests_per_minute: number };
}

export interface ClaudeCodeUsage {
  available: boolean;
  today?: { sessions: number; input_tokens: number; output_tokens: number; cache_read_tokens: number; estimated_cost_usd: number };
  last_7_days?: { date: string; input_tokens: number; output_tokens: number; cost_usd: number }[];
  total_sessions?: number;
}

export interface ActivityBreakdown {
  by_runner: { runner: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_model: { model: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_role: { role: string; runner: string; model: string; calls: number; cost_usd: number }[];
  recent_calls: {
    ticket_id: string; ticket_title: string; task_title: string;
    role: string; runner: string; model: string;
    tokens_in: number; tokens_out: number; cost_usd: number;
    status: string; duration_ms: number; timestamp: string;
  }[];
}
```

**State additions to `AppState` class:**

```typescript
settingsOpen = $state(false);
settingsTab = $state<'config' | 'usage'>('config');
configSummary = $state<ConfigSummary | null>(null);
claudeCodeUsage = $state<ClaudeCodeUsage | null>(null);
activityBreakdown = $state<ActivityBreakdown | null>(null);
```

**New functions in `state.svelte.ts`:**

```typescript
export async function fetchConfigSummary(): Promise<void> {
  try {
    appState.configSummary = await fetchJSON<ConfigSummary>('/api/config/summary');
  } catch { /* ignore */ }
}

export async function fetchClaudeCodeUsage(): Promise<void> {
  try {
    appState.claudeCodeUsage = await fetchJSON<ClaudeCodeUsage>('/api/usage/claude-code');
  } catch { /* ignore */ }
}

export async function fetchActivityBreakdown(): Promise<void> {
  try {
    appState.activityBreakdown = await fetchJSON<ActivityBreakdown>('/api/usage/activity');
  } catch { /* ignore */ }
}

export function openSettings() {
  appState.settingsOpen = true;
  // Fetch all data in parallel on open
  fetchConfigSummary();
  fetchClaudeCodeUsage();
  fetchActivityBreakdown();
}

export function closeSettings() {
  appState.settingsOpen = false;
}
```

Data is fetched fresh each time the drawer opens (not polled).

### CLI Command

```
foreman config [--json]
```

Prints formatted operational config summary. Same data as the API endpoint. `--json` outputs raw JSON.

```
FOREMAN CONFIGURATION
=====================

LLM
  Provider:          anthropic
  Planner:           claude-sonnet-4-6
  Implementer:       claude-sonnet-4-6
  API Key:           sk-ant-...a1b2

TRACKER
  Provider:          github
  Poll Interval:     30s

GIT
  Provider:          github
  Clone URL:         git@github.com:org/repo.git
  Branch Prefix:     foreman/

COST BUDGETS
  Daily:             $10.00
  Monthly:           $200.00
  Per Ticket:        $5.00
  Alert Threshold:   80%

DAEMON
  Parallel Tickets:  2
  Parallel Tasks:    3
  Work Dir:          ~/.foreman/work

DATABASE
  Driver:            sqlite
  Path:              ~/.foreman/foreman.db
```

---

## B. Real-time Enrichments

### Problem

Currently the dashboard shows only ticket-level events via WebSocket: `planning_started`, `planning_done`, `ticket_completed`, `ticket_failed`, etc. Agent-level events (`agent_turn_start`, `agent_turn_end`, `agent_tool_start`, `agent_tool_end`) are emitted by the engine but may not reach the dashboard's EventEmitter.

### Backend Changes

**1. Ensure agent events reach dashboard WebSocket**

The engine emits fine-grained events (turn starts, tool calls, LLM call completions). Verify these flow through to the dashboard's `EventPublisher` → `EventEmitter` → WebSocket broadcast.

Relevant event types to surface:

| Event Type | When | Key Details Field Data |
|------------|------|----------------------|
| `agent_turn_start` | Agent begins a turn | `{ "task_id", "runner", "turn_number", "model" }` |
| `agent_turn_end` | Agent completes a turn | `{ "task_id", "runner", "turn_number", "tokens_used" }` |
| `agent_tool_start` | Tool execution begins | `{ "task_id", "tool_name", "runner" }` |
| `agent_tool_end` | Tool execution completes | `{ "task_id", "tool_name", "runner", "duration_ms", "success" }` |
| `llm_call_completed` | LLM API call finishes | `{ "task_id", "model", "runner", "role", "tokens_in", "tokens_out", "cost_usd", "duration_ms" }` |
| `task_runner_assigned` | Runner selected for task | `{ "task_id", "runner", "model" }` |

**2. Enrich WebSocket event payload**

Add `runner` and `model` fields to the `enrichedEvent` struct in `ws.go`:

```go
type enrichedEvent struct {
    models.EventRecord
    TicketTitle string `json:"ticket_title,omitempty"`
    Submitter   string `json:"submitter,omitempty"`
    Runner      string `json:"runner,omitempty"`   // NEW
    Model       string `json:"model,omitempty"`    // NEW
}
```

The `enrichEvent` function extracts `runner` and `model` from the event's `Details` JSON field when present.

### Frontend Changes

**1. LiveFeed enrichment**

Currently shows: severity icon, event type, message, ticket title.

Add runner + model badges when present in the event:

```
● agent_turn_start                    14:30
  Turn 5 starting...
  [claudecode] [sonnet-4-6]              <- NEW: runner + model badges
  Add user validation                     <- ticket title link
```

Badge styling: reuse `runnerBadgeCls` pattern from TaskCard:
- `claudecode`: `text-accent border-accent/40`
- `copilot`: `text-purple-400 border-purple-400/40`
- `builtin`: `text-muted border-border-strong`

Model shown as short name (strip `claude-` prefix): `sonnet-4-6`, `haiku-4-5`, `opus-4-6`.

Only show badges when `runner` or `model` fields are present in the event (not all events have them).

**2. TaskCard live execution status**

When a task is in an active status (`implementing`, `tdd_verifying`, `testing`, `spec_review`, `quality_review`), show a live execution indicator in the expanded stats row:

```
[Expanded TaskCard for active task]
+------------------------------------------+
| ◉ 3. Add user validation      IMPLEMENTING|
+------------------------------------------+
| Attempt 1 · $0.32 · 8 LLM calls         |
| [claudecode] [sonnet-4-6]               |
| ► TURN 5/50 · 45K tokens used           |  <- NEW: live progress
| Last tool: edit_file (main.go) 2s ago    |  <- NEW: last tool call
+------------------------------------------+
```

This data comes from WebSocket events. The frontend tracks the latest `agent_turn_start`/`agent_tool_end` events for each active task:

```typescript
// In AppState class
activeTaskProgress = $state<Record<string, {
  turn: number;
  maxTurns: number;
  tokensUsed: number;
  runner: string;
  model: string;
  lastTool?: string;
  lastToolTime?: string;
}>>({});
```

Updated on each WebSocket event in `ws.onmessage`:

```typescript
if (evt.EventType === 'agent_turn_start' && evt.TaskID) {
  const details = JSON.parse(evt.Details || '{}');
  appState.activeTaskProgress[evt.TaskID] = {
    turn: details.turn_number || 0,
    maxTurns: details.max_turns || 50,
    tokensUsed: details.tokens_used || 0,
    runner: details.runner || '',
    model: details.model || '',
    ...appState.activeTaskProgress[evt.TaskID],
  };
}
if (evt.EventType === 'agent_tool_end' && evt.TaskID) {
  const details = JSON.parse(evt.Details || '{}');
  const existing = appState.activeTaskProgress[evt.TaskID];
  if (existing) {
    existing.lastTool = details.tool_name;
    existing.lastToolTime = evt.CreatedAt;
  }
}
```

Cleared when task transitions to done/failed.

**3. ActivityStream enrichment**

The ActivityStream already handles event types by name. Add handling for the new agent-level events with appropriate icons:

| Event Type | Icon | Color |
|------------|------|-------|
| `agent_turn_start` | `►` | `text-accent` |
| `agent_turn_end` | `■` | `text-muted-bright` |
| `agent_tool_start` | `⚡` | `text-accent` |
| `agent_tool_end` | `✓` | `text-success` (or `text-danger` on failure) |
| `llm_call_completed` | `◆` | `text-accent` |
| `task_runner_assigned` | `→` | `text-accent` |

Show runner + model inline in the event message when present, matching LiveFeed badge pattern.

**4. Updated EventRecord type**

```typescript
export interface EventRecord {
  ID: string;
  TicketID: string;
  TaskID: string;
  EventType: string;
  Severity: 'info' | 'success' | 'warning' | 'error';
  Message: string;
  Details: string;
  CreatedAt: string;
  // Enriched by WebSocket
  ticket_title?: string;
  submitter?: string;
  runner?: string;    // NEW
  model?: string;     // NEW
  seq?: number;
  isNew?: boolean;
}
```

---

## Design Tokens (Brutalism)

All new UI follows the existing design system exactly:

| Token | Value | Usage |
|-------|-------|-------|
| `bg-surface` | `#121212` | Drawer background |
| `bg-surface-active` | `#242424` | Section headers |
| `bg-bg` | `#0a0a0a` | Backdrop, bar tracks |
| `border-2 border-border` | `#2a2a2a` | Panel borders |
| `border-l-4 border-l-accent` | `#FFE600` | Section header accent |
| `text-[10px] font-bold tracking-[0.2em]` | — | Section header text |
| `text-[10px] text-muted tracking-wider` | — | Field labels |
| `text-xs text-text` | — | Field values |
| `bg-accent text-bg` | — | Active tab (inverted) |
| `text-muted hover:text-text` | — | Inactive tab |
| No `rounded-*` | — | Square corners everywhere |
| `font-mono` | JetBrains Mono | All text |

Runner badge colors (consistent across all components):
- `builtin`: `text-muted border-border-strong`
- `claudecode`: `text-accent border-accent/40`
- `copilot`: `text-purple-400 border-purple-400/40`

## Constraints

- Claude Code JSONL parsing depends on file format stability (not a public API)
- Session files can be large — scan only last 7 days by modification time
- No Copilot integration due to separate account/token requirements
- Config endpoint is read-only — no editing from dashboard
- Drawer data fetched on open (not polled)
- Agent-level events may be high volume — LiveFeed already caps at 50 events and drops slow consumers
- `activeTaskProgress` state cleared on task completion to prevent stale data

## Future Considerations

- Copilot integration if token scoping is resolved
- Historical usage storage in DB for trends
- Config diff view (show what changed since last restart)
- Export config as TOML from dashboard
- Streaming cost accumulator (show running cost total during task execution)
- Planning phase detail view (intermediate steps, task dependency reasoning)
