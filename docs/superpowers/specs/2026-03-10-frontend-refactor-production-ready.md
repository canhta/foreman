# Frontend Production-Ready Refactor — Design Spec

**Date:** 2026-03-10
**Status:** Approved
**Supersedes:** Phase 2 plan partial implementation

## Goal

Fix all bugs, typos, dead code, and incomplete components in the Phase 2 frontend. Full refactor — no backward-compat constraints. Keep the dark terminal aesthetic (black/electric-yellow/mono).

---

## Issues to Fix

### Bugs
1. `GlobalOverview.svelte` — raw `window.location.hash` navigation instead of svelte-spa-router `push()`
2. `ProjectSettings.svelte` — raw `window.location.hash = '/'` on delete instead of `push('/')`
3. `ProjectWizard.svelte` — raw `window.location.hash` navigation (×2) instead of `push()`
4. `TaskCard.svelte` — `retryTicket(task.ID)` passes task ID; should pass `task.TicketID`
5. `ProjectBoard.svelte` — `panelExpanded` branch is an empty stub; `expandPanel()` silently does nothing

### Typos & Dead Code
- `tracking-widests` (extra `s`) in `ProjectSettings.svelte` (×2) and `GlobalOverview.svelte` (×1)
- `routes.ts` — unused `wrap` import
- `GlobalState.wsConnected` — set but never surfaced in UI
- Global WebSocket `onmessage` — only fires toasts; never refreshes overview on events

### Type Safety
- `ProjectSettings.svelte` — `config: Record<string, any>` → type with `ProjectConfig` interface
- `ProjectSettings.svelte` — `$props<...>()` legacy syntax → standard `$props()` with typed destructure

---

## Components to Rewrite / Enhance

### TicketPanel.svelte — Full Rewrite
Current state is bare (title, progress bar, PR link, cost, description, tasks).
New design adds tabbed layout:
- **Tasks tab** (default): task list with expand/collapse
- **Events tab**: chronological event log (severity colored)
- **Chat tab**: shows `ChatMessage[]`; inline reply input for `clarification_needed` tickets
- Header: external ID, status badge, PR link, cost, retry/delete actions
- Actions bar: Retry (failed/blocked), Delete (with confirm dialog)

### TicketFullView.svelte — New Shell
Overlay that fills `main` area when `panelExpanded = true` on the board.
Phase 3 (chat) placeholder — renders the full ticket detail without the side panel constraint.
Contains: back button, all TicketPanel content in full-width layout.

### GlobalOverview.svelte — Fix + Enhance
- Fix: `project.active ? 1 : 0` → use `ProjectSummary.active_tickets` from API
- Wire global WS `onmessage` → call `loadOverview()` + `loadProjects()` on relevant event types
- Add skeleton loading state (pulse bars while `overview` is zero-state on first load)
- Add WS indicator dot in header (green/grey)

### ProjectDashboard.svelte — Chart Improvement
- Add day labels + cost value tooltip on hover (pure CSS, no lib)
- Bar minimum height of 2px so zero-cost days are still visible
- Add "No data" empty state when `weekDays.length === 0`

### routes.ts — Add 404
```typescript
'*': NotFound  // simple centered "404 — not found" component
```

---

## Architecture Notes

- All navigation: use `push(path)` from `svelte-spa-router` — no `window.location.hash`
- `ProjectConfig` interface added to `types.ts` to replace `Record<string, any>` in settings
- `TicketPanel` tabs driven by local `$state<'tasks'|'events'|'chat'>('tasks')`
- `TicketFullView` rendered as absolute overlay in `App.svelte` when `panelExpanded && selectedTicketId`
- Chat input only shown when ticket status is `clarification_needed`

---

## File Inventory

| Action | File |
|--------|------|
| Fix | `src/routes.ts` |
| Fix | `src/components/TicketCard.svelte` (no changes needed — clean) |
| Fix bugs | `src/components/TaskCard.svelte` |
| Fix nav | `src/pages/GlobalOverview.svelte` |
| Fix nav + types | `src/pages/ProjectSettings.svelte` |
| Fix nav | `src/pages/ProjectWizard.svelte` |
| Fix panel + board layout | `src/pages/ProjectBoard.svelte` |
| Rewrite | `src/components/TicketPanel.svelte` |
| Create | `src/components/TicketFullView.svelte` |
| Enhance | `src/pages/GlobalOverview.svelte` |
| Enhance | `src/pages/ProjectDashboard.svelte` |
| Update | `src/types.ts` (add `ProjectConfig`) |
| Update | `src/state/global.svelte.ts` (WS refresh) |
| Create | `src/pages/NotFound.svelte` |

---

## Out of Scope

- `GlobalSetup.svelte` — no confirmed backend endpoint
- Chat send UI in `TicketFullView` — Phase 3 (agent chat backend not complete)
- Drag-and-drop board (non-goal per spec)
