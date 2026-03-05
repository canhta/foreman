# Dashboard Dark Brutalism Redesign

**Date:** 2026-03-05
**Status:** Approved

## Goal

Redesign the Foreman dashboard frontend with Dark Brutalism aesthetics: black background, electric yellow (`#FFE600`) accent, system monospace, hard borders, zero softness. Expand the header strip to show daemon status (three states) and cost-vs-budget.

## Visual Language

| Property | Value |
|---|---|
| Background | `#0a0a0a` |
| Surface | `#111111` |
| Border | `2px solid #FFE600` |
| Card shadow | `4px 4px 0 #FFE600` |
| Accent | `#FFE600` (electric yellow) |
| Text | `#F0F0F0` |
| Muted text | `#888888` |
| Danger | `#FF4444` |
| Font | `monospace` (system) |
| Border radius | `0` everywhere |
| Letter spacing | `0.05em` on labels |
| Labels | UPPERCASE |

## Layout

```
┌──────────────────────────────────────────────────────────────┐
│ [FOREMAN]    ● RUNNING    COST: $12.40 / $150    ACTIVE: 3  │  ← header strip
├───────────────────────────────┬──────────────────────────────┤
│ TICKETS (N)                   │ LIVE EVENTS                  │
│ ┌───────────────────────────┐ │ 12:34:01 task_started        │
│ │ > Add login feature       │ │          [ticket-abc123]     │
│ │   [IMPLEMENTING]          │ │ 12:33:59 plan_created        │
│ └───────────────────────────┘ │          [ticket-xyz456]     │
│ ...                           │ ...                          │
└───────────────────────────────┴──────────────────────────────┘
```

## Components

### Header Strip
Single row, full width, `background: #0a0a0a`, `border-bottom: 2px solid #FFE600`.
Left: `[FOREMAN]` wordmark in yellow. Right cluster: status dot, cost, active count — separated by `|` dividers.

**Three-state status dot:**
- `●` `#FFE600` `RUNNING` — WebSocket connected + daemon state is `running`
- `●` `#888888` `PAUSED` — WebSocket connected + daemon state is `paused`
- `●` `#FF4444` `DISCONNECTED` — WebSocket not connected

Frontend derives state by combining WebSocket connection + polling `/api/status` (which returns `{status: "running"|"paused"|"stopped"}`). Polling interval: every 15s.

**Cost indicator:**
Fetches `/api/costs/today` for current spend and `/api/costs/budgets` for `max_daily_usd` and `alert_threshold_pct`.
- Display: `COST: $X.XX / $Y` (whole number for budget)
- When `current / max_daily >= alert_threshold_pct / 100`: flip cost text to `#FF4444`
- If budget endpoint returns no config data, display `COST: $X.XX` without the `/ $Y` part

**Active pipelines:** `ACTIVE: N` — fetches `/api/pipeline/active`, shows count.

### Ticket Cards
- Container: `border: 2px solid #FFE600`, `box-shadow: 4px 4px 0 #FFE600`, `background: #111`, `padding: 0.75rem`
- Title: `> TICKET TITLE` — yellow `>` prefix, title in white, monospace
- Status tag: `[IMPLEMENTING]` — yellow background, black text, uppercase, no border-radius

Status tag colors by state:
| Status | Background | Text |
|---|---|---|
| done | `#FFE600` | `#0a0a0a` |
| implementing, planning, reviewing | `#FFE600` | `#0a0a0a` |
| failed, blocked | `#FF4444` | `#F0F0F0` |
| queued, pending | `#333` | `#F0F0F0` |

### Event Log
- Alternating row backgrounds: `#0a0a0a` / `#0f0f0f`
- Layout per entry: `[HH:MM:SS]` in yellow, space, event type in white, newline-indent ticket ID in `#888`
- Max 200 entries, newest first
- Font size: `0.8rem`

## Backend Changes Required

Two wiring tasks needed before the frontend can fully use both features:

### 1. Daemon State in `/api/status`
The `API` struct needs access to daemon state. Add a `DaemonStatus` interface to the dashboard package and wire it from `cmd/dashboard.go`.

```go
// internal/dashboard/api.go
type DaemonStatusProvider interface {
    Status() daemon.DaemonStatus  // or a local struct
}
```

`handleStatus` returns `{status, version, uptime, daemon_state}` where `daemon_state` is `"running"`, `"paused"`, or `"stopped"`.

### 2. Budget Config in `/api/costs/budgets`
The `API` struct needs access to `models.CostConfig`. Pass it in `NewAPI`. `handleCostsBudgets` returns:
```json
{ "max_daily_usd": 150, "alert_threshold_pct": 80 }
```

## JS Changes

- `loadStatus()` — poll every 15s, update dot state and uptime
- `loadCosts()` — poll every 60s, fetch both `/api/costs/today` and `/api/costs/budgets`, render with threshold logic
- `loadActive()` — poll every 30s, fetch `/api/pipeline/active`, render count
- `connectWS()` — on open: set `wsConnected = true` and re-evaluate dot; on close: set `wsConnected = false`
- Dot rendering: pure function of `{wsConnected, daemonState}` → color + label

## Files Changed

| File | Change |
|---|---|
| `internal/dashboard/web/index.html` | Full rewrite — brutalist structure, status bar |
| `internal/dashboard/web/style.css` | Full rewrite — dark brutalism tokens |
| `internal/dashboard/web/app.js` | Full rewrite — add cost/budget/active/status polling |
| `internal/dashboard/api.go` | Add `DaemonStatusProvider` interface, `CostConfig` to API struct, wire `handleCostsBudgets` and `handleStatus` |
| `internal/dashboard/api_test.go` | Update tests for new `NewAPI` signature |
| `cmd/dashboard.go` | Pass daemon + cost config to `NewServer` |
