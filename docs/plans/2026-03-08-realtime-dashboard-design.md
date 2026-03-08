# Real-Time Dashboard Redesign

**Date:** 2026-03-08
**Status:** Approved
**Stack:** Svelte 5 + TypeScript + Vite + Tailwind CSS 4

## Problem

The dashboard task detail view is static. WebSocket events flow to the live feed but the detail panel only loads on click and never refreshes. Operators monitoring long-running tasks have no real-time visibility into progress, and the flat task list hides dependency structure.

## Approach

Migrate the frontend from Alpine.js to Svelte 5. Keep the Go backend and `//go:embed` pattern. Add structured activity streams, DAG visualization, and system observability views. All styling via Tailwind utility classes — no separate CSS files.

## Architecture

### Build Pipeline

- Svelte 5 + Vite builds to `internal/dashboard/web/dist/`
- Go server embeds `web/dist/` via `//go:embed`
- `go:generate` directive in `server.go` runs `npm run build`
- Dev mode: `vite dev` with proxy to Go API server

### Project Layout

```
internal/dashboard/
├── web/
│   ├── src/
│   │   ├── App.svelte              # Shell, layout, auth, WS connection
│   │   ├── main.ts                 # Mount point (~5 lines)
│   │   ├── state.svelte.ts         # All shared reactive state (runes)
│   │   ├── api.ts                  # fetchJSON, postJSON with auth
│   │   ├── format.ts               # Time/cost formatters
│   │   ├── types.ts                # Ticket, Task, Event interfaces
│   │   └── components/
│   │       ├── Header.svelte
│   │       ├── TicketList.svelte
│   │       ├── TicketDetail.svelte
│   │       ├── TaskCard.svelte
│   │       ├── ActivityStream.svelte
│   │       ├── DagView.svelte
│   │       ├── LiveFeed.svelte
│   │       ├── CostBreakdown.svelte
│   │       └── SystemHealth.svelte
│   ├── app.css                     # Tailwind directives + theme extensions
│   ├── index.html
│   ├── package.json
│   ├── vite.config.ts
│   └── tsconfig.json
├── dist/                           # Build output (go:embed target)
├── server.go
├── api.go
├── ws.go
└── auth.go
```

### Shared State (`state.svelte.ts`)

All reactive state managed via Svelte 5 runes in a single file:
- Ticket list, selected ticket, detail data
- WebSocket connection state and event buffer
- Daemon status, costs, active count
- No separate store files — one source of truth

## Real-Time Data Flow

### Event-Driven Refresh

```
WebSocket event arrives
  → state.svelte.ts receives, appends to events
  → If event.ticket_id === selected ticket:
    ├── Debounced re-fetch detail (300ms)
    ├── Append to ticket events in-place
    └── If event.task_id matches expanded task:
        └── Update task status + activity stream
  → If status change event:
    └── Optimistic local patch, background API confirm
```

### Key Behaviors

- **Debounced refresh**: Rapid events coalesce into one API call
- **Optimistic patching**: Status updates instantly from WebSocket payload
- **Stable scroll**: Auto-scroll only when user is at bottom
- **Connection resilience**: Exponential backoff (1s → 2s → 4s → max 30s), visual indicator, catch-up via API on reconnect

### New Backend Support

- `GET /api/tickets/{id}/activity` — merged chronological stream of events + task status changes + LLM call summaries
- `GET /api/llm-calls/{id}/details` — stored prompt/response for a specific LLM call
- WebSocket enhancement: `seq` field on events for gap detection after reconnect

## Activity Stream

Per-ticket chronological feed in the detail view showing real-time progress.

### Item Format

Each item has: icon (stage/status), headline (what), sub-lines (details), relative timestamp.

- Active tasks: pulsing indicator, live-updating sub-lines as events stream in
- Failed items: red accent, error message immediately visible without clicking
- Completed items: collapsed by default
- "Expand raw detail" link reveals full event JSON for debugging

### Example

```
● Planning                                    2m ago
  Decomposed into 4 tasks

⚙ Task 2: Add auth middleware              just now
  Editing src/auth/middleware.go
  ├── Reading 3 dependency files
  └── Applied diff (47 lines changed)

✓ Task 1: Create user model                  5m ago
  Tests passed (8/8) · $0.12 · 14s

✗ Task 3: Update routes                      1m ago
  Test failure: TestRouteAuth — assertion error on line 42
  [Expand raw detail ▸]
```

## DAG Visualization

Horizontal left-to-right SVG flow diagram. No external library — DAGs are typically 3-10 tasks.

- **Nodes**: Rounded rect, task title, status icon, stage label
- **Edges**: SVG paths with arrowheads showing dependencies
- **Color-coded borders**: Gray (pending), pulsing yellow (active), green (done), red (failed)
- **Interactive**: Click node → scroll to task detail card
- **Layout**: Rank-based topological sort by dependency depth
- **Responsive**: Vertical layout on mobile
- **Conditional**: Only shown when ticket has 2+ tasks with dependencies

## System Observability

### System Health Panel (`SystemHealth.svelte`)

Accessible via header tab:

- **Agent health**: Status of each runner (builtin, claude-code, copilot) with health check result
- **MCP servers**: Connection status
- **Queue depth**: Queued vs actively processing
- **Cost gauges**: Daily/weekly/monthly with budget progress bars and threshold warnings
- **Throughput**: Tickets completed today, average time-to-merge, success rate

### Debugging in Task Detail

- **Error context**: Error message, failure stage, files involved, last activity items before failure
- **Retry history**: Collapsible timeline of each attempt with outcome (when attempts > 1)
- **LLM call inspector**: Per-task expandable list — model, tokens, cost, duration, status. Click for full prompt/response.
- **Context budget bar**: Visual token budget utilization per task

## Usability & Interaction

### Keyboard Navigation

- `j/k` — Move through ticket list
- `Enter` — Open selected ticket
- `Escape` — Back to ticket list
- `1/2/3/4` — Switch filter tabs
- `?` — Keyboard shortcut overlay

### Toast Notifications

Brief notifications when tickets complete/fail while viewing a different ticket. Auto-dismiss after 5s, stack up to 3, click to navigate.

### Responsive Layout

- **Desktop**: Three-panel (ticket list | detail | live feed), collapsible, drag-to-resize
- **Tablet**: Two-panel (list + detail), feed as overlay
- **Mobile**: Single panel with bottom tab bar, swipe gestures

### Loading & Empty States

- Skeleton loaders (not spinners)
- Helpful empty state messages
- Optimistic UI: actions reflect immediately, revert on error

### Search & URL State

- Filter by status, date range, cost threshold
- URL reflects state (`?ticket=42&filter=active`) for sharing/bookmarks
- Browser back/forward navigates ticket selections

### Accessibility

- ARIA labels on all interactive elements
- Focus management with Escape to return
- Color never the only status indicator — icons + text always present
- `prefers-reduced-motion` respected
- Semantic HTML: headings, landmarks, lists

### Visual

Dark theme only. Monospace terminal aesthetic. Tailwind dark palette with yellow/gold accents from existing design.
