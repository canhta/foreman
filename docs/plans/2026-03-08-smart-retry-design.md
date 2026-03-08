# Smart Retry Design

**Date:** 2026-03-08
**Status:** Approved

## Problem

When a ticket's DAG execution fails (one task fails → dependents skip), the RETRY button
re-queues the ticket for a full re-run from scratch: re-plan, new branch, all tasks re-executed.
This wastes LLM calls and discards already-committed work from tasks that succeeded.

## Goal

Retry only the failed task(s) and their skipped dependents, keeping already-done tasks and
the existing feature branch.

## Design

### Trigger (API layer)

`POST /api/tickets/{id}/retry` routes to a new `smartRetrier` (replaces `dbTicketRetrier`).

`smartRetrier.RetryTicket(ctx, ticketID)`:
1. `ListTasks` → separate done tasks from failed/skipped tasks
2. `SaveDAGState({TicketID: id, CompletedTasks: [done task IDs]})` — seeds crash-recovery state
3. For each failed/skipped task: `UpdateTaskStatus(id, pending)`
4. `UpdateTicketStatus(ticketID, queued)` — daemon picks it up on next poll

### Orchestrator (resume path)

In `ProcessTicket`, immediately after `EnsureRepo`:

```
existingTasks = ListTasks(ticket.ID)
if any existingTasks are done → resumePartialRun(ctx, ticket, existingTasks)
```

`resumePartialRun`:
1. `CheckoutBranch(branchPrefix + externalID)` — land on existing feature branch
2. Re-reserve files via `scheduler.TryReserve`
3. `UpdateTicketStatus → implementing`
4. `GetDAGState` → `TasksForDAGRecovery` filters out done tasks
5. `executor.Execute` → runs only remaining tasks
6. Same post-DAG logic (analyze results, PR, status update)

### Interface changes

| Location | Change |
|---|---|
| `DashboardDB` (`api.go`) | Add `SaveDAGState` method |
| `TicketRetrier` / `server.go` | Replace `dbTicketRetrier` with `smartRetrier` |
| `Orchestrator` (`orchestrator.go`) | Add retry detection + `resumePartialRun` |
| Git interface | Add `CheckoutBranch` (or tolerate existing branch in `CreateBranch`) |

## Data flow

```
User clicks RETRY
  → smartRetrier seeds dag_state with done IDs, resets failed/skipped → pending, queues ticket

Daemon next poll
  → ProcessTicket detects existing done tasks → retry path
  → CheckoutBranch (existing), re-reserve, set implementing
  → GetDAGState → TasksForDAGRecovery skips done tasks
  → Execute only remaining tasks
  → PR / status update as normal
```

## What stays the same

- Full ticket retry (re-plan from scratch) is still possible by deleting the ticket and
  re-submitting it.
- `TasksForDAGRecovery` and `dag_state` persistence are unchanged — this design reuses them.
- Post-DAG logic (PR creation, status update, notifications) is unchanged.

## Out of scope

- Per-task retry button behavior (individual task retry remains a manual override that
  resets one task status; ticket-level RETRY is the primary retry path).
- Retry limit / backoff (not needed for dashboard-triggered retries).
