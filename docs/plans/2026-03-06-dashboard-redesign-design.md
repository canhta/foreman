# Dashboard Redesign Design

**Date:** 2026-03-06
**Status:** Approved

## Context

The current dashboard is a minimal two-panel layout (ticket list + live event log) that provides limited functionality for a small team monitoring a Foreman instance. This redesign adds ticket deep-dives, operational controls, team visibility, and richer data presentation while preserving the brutalist terminal aesthetic.

## Design Decisions

- **Frontend:** Alpine.js + htmx via CDN. No build step, no npm. Assets embedded in Go binary.
- **Aesthetic:** Brutalist terminal — monospace font, dark palette (#0a0a0a/#111111), yellow accent (#FFE600), hard 2px borders, box shadows, no border-radius, text-character icons.
- **Primary users:** Small team sharing one Foreman instance.

---

## 1. Layout: Three-Zone

```
+------------------------------------------------------------------+
| FOREMAN | . RUNNING | . WA: OK | COST $4.21/$50 | ACTIVE 3 | [] | <- sticky header + daemon controls
+------------+---------------------------+-------------------------+
|            |                           |                         |
|  TICKETS   |     TICKET DETAIL /       |   LIVE FEED             |
|  (sidebar) |     TEAM SUMMARY          |   (collapsible)         |
|            |                           |                         |
|  filters   |                           |                         |
|  search    |                           |                         |
+------------+---------------------------+-------------------------+
| COST: $4.21 today | $18.90 this week | $52.31 this month        | <- summary footer
+------------------------------------------------------------------+
```

- **Left sidebar** (~250px): Ticket list with filter tabs, search, progress bars.
- **Center panel** (flex): Ticket detail view OR team summary when no ticket selected.
- **Right panel** (~300px, collapsible to 40px icon strip): Live event feed.
- **Header:** Daemon status, WhatsApp health, cost, active count, pause/resume controls.
- **Footer:** Cost summary bar (today, this week, this month).

---

## 2. Ticket List Panel (Left Sidebar)

**Filter tabs:**
- `ALL` -- everything
- `ACTIVE` -- planning, implementing, reviewing, pr_created, awaiting_merge, clarification_needed
- `DONE` -- done, merged
- `FAIL` -- failed, blocked, partial

**Each ticket card shows:**
- Title (truncated)
- Status badge (color-coded)
- Submitter name (from WhatsApp contacts config, phone number fallback), muted color
- Cost so far (`CostUSD`)
- Mini task progress bar with count (e.g., `4/6`)
- Markers: red X for failed/blocked, ? for clarification_needed

**Search:** Client-side, filters by title and submitter name.

**Sort:** Most recent activity first (`UpdatedAt`), failed tickets pinned to top.

**Performance:** Task counts (`tasks_total`, `tasks_done`) and `cost_usd`, `submitter_name`, `last_event_at` aggregated in the ticket list API response. No N+1 queries.

---

## 3. Ticket Detail Panel (Center, ticket selected)

### Header Section
- Title, status, submitter, relative time since started
- PR link (clickable, opens new tab) when `PRURL` is set
- **Retry button** -- visible on failed/blocked/partial. POSTs to `/api/tickets/{id}/retry`. Confirmation prompt before send.

### Clarification Section (conditional)
- Shown only if ticket passed through `clarification_needed`
- Displays the question asked and the answer received, with timing

### Tasks Section
- Ordered by sequence number
- Icons: checkmark (done), gear (in-progress), X (failed), circle (pending), slash (skipped)
- Shows estimated complexity (simple/medium/complex)
- **Task-level retry:** `[retry task]` button on failed tasks, POSTs to `/api/tasks/{id}/retry`
- **Expandable task detail:** Click to expand inline showing:
  - Status with attempt count (e.g., "attempt 2/3")
  - Files to modify
  - Error message (for failed tasks)
  - `[Show diff]` -- collapsible, fetches from `/api/tasks/{id}/diff` on demand
  - Assertion counts (spec, quality)
  - Cost and token usage

### Cost Breakdown Section
- Cost by LLM role (planner, implementer, spec_reviewer, quality_reviewer) with horizontal bars
- Summary: model used, total tokens, total LLM calls with success/retry counts
- Budget consumed indicator: `$1.20 / $15.00 used (8%)` with progress bar (`max_cost_per_ticket_usd` from config)
- Data from `/api/tickets/{id}/llm-calls`, aggregated client-side

### Events Section
- Filtered to this ticket (from `/api/tickets/{id}/events`)
- Shows human-readable message content, not just event type

---

## 4. Team Summary View (Center, no ticket selected)

### Today
- Ticket count by outcome (merged, failed, active)
- Daily cost vs budget with progress bar
- Monthly cost vs budget with progress bar

### This Week
- Horizontal text-based bar chart of daily cost + ticket count
- Bars normalized to highest-cost day (max 16 block chars)

### Team
- Submitters ranked by ticket count this week
- Shows: ticket count, cost, failure count per submitter

### Recent PRs
- Last 5 PRs with status (merged/open/failed) and relative time
- Clickable to open in git provider

### Needs Attention
- Failed tickets
- Clarification needed tickets
- Stuck tickets (no event in >30 min, configurable threshold)
- `[view]` link selects ticket in left panel

---

## 5. Live Feed Panel (Right)

### Expanded State (~300px)
- Event entry: timestamp + severity icon + event type + human-readable message + ticket title + submitter
- Severity icons: dot (info/muted), checkmark (success/green), X (failure/red), ? (clarification/yellow)
- Clicking ticket title selects it in left panel
- Max 50 events in DOM, oldest scroll off

### Collapsed State (~40px icon strip)
- Severity-colored dots only, vertically stacked, most recent on top
- Max 50 dots, same cap as expanded
- Click expand button to return to full width
- Collapse state persisted to `localStorage`

### Toggle
- `[<]` button to collapse, `[>]` to expand

### Animation
- New events fade in: background from muted accent to transparent, 1.2s ease-out
- No slide/translate motion (brutalist static aesthetic)

### WebSocket Payload (enriched server-side)
```json
{
  "event_type": "task_spec_review_pass",
  "ticket_id": "abc-123",
  "ticket_title": "Add dark mode",
  "submitter": "Canh",
  "message": "All 3 spec assertions passed",
  "severity": "success"
}
```

Note: `ticket_title` is a snapshot at event time. Titles rarely change; stale title in live feed is acceptable. Document this contract in `ws.go`.

---

## 6. Header Bar & Daemon Controls

```
FOREMAN | . RUNNING | . WA: OK | COST $4.21/$50 | ACTIVE 3 | [pause] [resume]
```

| Element | Source | Poll Interval |
|---|---|---|
| Daemon status | `/api/status` | 15s |
| WhatsApp health | `/api/status` (extended) | 15s (same call) |
| Daily cost | `/api/costs/today` + `/api/costs/budgets` | 60s |
| Active pipelines | `/api/pipeline/active` | 30s |

**WhatsApp indicator:**
- Green dot + "WA: OK" when connected
- Red dot + "WA: DOWN" when disconnected
- Hidden if WhatsApp channel not configured
- Data from extended `/api/status` response:
  ```json
  { "channels": { "whatsapp": { "connected": true, "last_seen": "..." } } }
  ```

**Daemon controls:**
- Pause button visible when running, resume button visible when paused/stopped
- Confirmation prompt before action
- Loading state until next status poll confirms change
- Backend wiring needed: connect to daemon pause/resume mechanism

---

## 7. Backend API Changes

### Modified Endpoints

| Endpoint | Change |
|---|---|
| `GET /api/status` | Add `channels.whatsapp.connected` and `last_seen` |
| `GET /api/tickets` | Add `tasks_total`, `tasks_done`, `cost_usd`, `submitter_name`, `last_event_at` |
| `POST /api/daemon/pause` | Wire to daemon pause mechanism |
| `POST /api/daemon/resume` | Wire to daemon resume mechanism |
| `POST /api/tickets/{id}/retry` | Wire to pipeline state machine |
| `WS /ws/events` | Enrich with `ticket_title`, `submitter`, `message`, `severity` |

### New Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /api/stats/team` | Tickets by submitter for current week |
| `GET /api/stats/recent-prs` | Last 5 tickets with PRURL set |
| `POST /api/tasks/{id}/retry` | Re-queue single failed task |
| `GET /api/tasks/{id}/diff` | Git diff from task CommitSHA |
| `GET /api/events` | Global events, paginated, for live feed scroll-back |

### New DB Interface Methods

| Method | Purpose |
|---|---|
| `GetTeamStats(ctx, since) []TeamStat` | Aggregate tickets by ChannelSenderID |
| `GetRecentPRs(ctx, limit) []Ticket` | Tickets with PR, ordered by recency |
| `GetTaskDiff(ctx, taskID) string` | Diff from commit SHA |
| `GetGlobalEvents(ctx, limit, offset) []EventRecord` | Paginated global events |
| `RetryTask(ctx, taskID) error` | Reset task status to pending |

No new database tables. All data from existing tables with aggregate queries.

---

## 8. Technology & Implementation Constraints

- **Alpine.js** (CDN): reactivity for filters, detail view, search, expand/collapse
- **htmx** (CDN): retry buttons, daemon controls (POST with loading states)
- All assets embedded in Go binary via `embed.FS`, no build step
- Single `app.js` file, Alpine components via `x-data` inline
- Progress bars: styled `<span>` elements with block characters
- Icons: text characters (checkmark, X, dot, circle, gear, ?, clock), no icon library
- Auth: all new endpoints behind existing bearer token middleware
- Polling intervals: 15s status, 10s tickets, 60s costs, 30s active, 60s team stats
- Task diff: fetched on-demand only (lazy on expand click)
- Live feed: max 50 DOM nodes in both expanded and collapsed states
