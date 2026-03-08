# Smart Retry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When the RETRY button is clicked on a failed ticket, re-run only the failed/skipped tasks, keeping already-done tasks' commits and the existing feature branch.

**Architecture:** The retry API call seeds `dag_state` with done task IDs (so `TasksForDAGRecovery` can skip them), resets failed/skipped tasks to `pending`, and re-queues the ticket. `ProcessTicket` detects the retry by checking if existing done tasks are present, and skips planning + task creation, jumping straight to DAG execution.

**Tech Stack:** Go, existing `db.DAGState` + `TasksForDAGRecovery` (crash recovery mechanism), `NativeGitProvider.CreateBranch` (already handles existing branches via `git checkout`).

---

### Task 1: Add `SaveDAGState` to `DashboardDB` interface + test mock

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/api_test.go`

**Step 1: Add `SaveDAGState` to the `DashboardDB` interface**

In `internal/dashboard/api.go`, add one line to the `DashboardDB` interface (after `UpdateTicketStatus`):

```go
// DashboardDB is a subset of db.Database needed by the dashboard.
type DashboardDB interface {
    // ... existing methods ...
    UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
    SaveDAGState(ctx context.Context, ticketID string, state db.DAGState) error  // ADD THIS
    // ... rest of methods ...
}
```

**Step 2: Build to confirm the interface is broken (expected)**

```bash
go build ./internal/dashboard/...
```

Expected: compile error — `mockDashboardDB does not implement DashboardDB (missing SaveDAGState method)`

**Step 3: Add `SaveDAGState` to `mockDashboardDB` in `api_test.go`**

Add tracking fields to the struct:

```go
type mockDashboardDB struct {
    tickets           []models.Ticket
    events            []models.EventRecord
    teamStats         []models.TeamStat
    summaries         []models.TicketSummary
    contextStats      map[string]TaskContextStatsDB
    savedDAGState     *db.DAGState          // ADD
    taskStatusUpdates []struct {            // ADD
        id     string
        status models.TaskStatus
    }
}
```

Add the method:

```go
func (m *mockDashboardDB) SaveDAGState(_ context.Context, ticketID string, state db.DAGState) error {
    m.savedDAGState = &state
    return nil
}
```

Also update `UpdateTaskStatus` to track calls (needed for Task 2 tests):

```go
func (m *mockDashboardDB) UpdateTaskStatus(_ context.Context, id string, status models.TaskStatus) error {
    m.taskStatusUpdates = append(m.taskStatusUpdates, struct {
        id     string
        status models.TaskStatus
    }{id, status})
    return nil
}
```

**Step 4: Build to confirm it compiles**

```bash
go build ./internal/dashboard/...
```

Expected: clean build.

**Step 5: Run existing dashboard tests to confirm nothing broken**

```bash
go test ./internal/dashboard/... -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat: add SaveDAGState to DashboardDB interface"
```

---

### Task 2: Implement `smartRetrier` (TDD)

**Files:**
- Modify: `internal/dashboard/server.go`
- Modify: `internal/dashboard/api_test.go` (add test)

**Step 1: Write the failing test in `api_test.go`**

Add this test at the end of `api_test.go`:

```go
func TestSmartRetrier_ResetsTasksAndSavesDagState(t *testing.T) {
    tasks := []models.Task{
        {ID: "t-done", TicketID: "ticket-1", Status: models.TaskStatusDone},
        {ID: "t-failed", TicketID: "ticket-1", Status: models.TaskStatusFailed},
        {ID: "t-skip1", TicketID: "ticket-1", Status: models.TaskStatusSkipped},
        {ID: "t-skip2", TicketID: "ticket-1", Status: models.TaskStatusSkipped},
    }

    type listTasksDB interface {
        DashboardDB
    }
    mdb := &mockDashboardDB{}

    // Override ListTasks to return our tasks.
    // Can't override methods on struct — use the retryTestDB wrapper below.
    retryDB := &retryTestDB{mockDashboardDB: mdb, tasks: tasks}
    retrier := &smartRetrier{db: retryDB}

    err := retrier.RetryTicket(context.Background(), "ticket-1")
    require.NoError(t, err)

    // dag_state saved with only the done task ID.
    require.NotNil(t, mdb.savedDAGState)
    assert.Equal(t, []string{"t-done"}, mdb.savedDAGState.CompletedTasks)

    // failed and skipped tasks reset to pending.
    pendingIDs := map[string]bool{}
    for _, u := range mdb.taskStatusUpdates {
        if u.status == models.TaskStatusPending {
            pendingIDs[u.id] = true
        }
    }
    assert.True(t, pendingIDs["t-failed"], "t-failed should be reset to pending")
    assert.True(t, pendingIDs["t-skip1"], "t-skip1 should be reset to pending")
    assert.True(t, pendingIDs["t-skip2"], "t-skip2 should be reset to pending")
    assert.False(t, pendingIDs["t-done"], "t-done should NOT be reset")

    // ticket re-queued.
    var lastTicketStatus models.TicketStatus
    for _, u := range mdb.taskStatusUpdates {
        _ = u // task updates, not ticket
    }
    assert.Equal(t, models.TicketStatusQueued, retryDB.ticketStatus)
}

// retryTestDB wraps mockDashboardDB to override ListTasks.
type retryTestDB struct {
    *mockDashboardDB
    tasks        []models.Task
    ticketStatus models.TicketStatus
}

func (r *retryTestDB) ListTasks(_ context.Context, _ string) ([]models.Task, error) {
    return r.tasks, nil
}

func (r *retryTestDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
    r.ticketStatus = status
    return r.mockDashboardDB.UpdateTicketStatus(ctx, id, status)
}
```

**Step 2: Run the test to confirm it fails**

```bash
go test ./internal/dashboard/... -run TestSmartRetrier -v
```

Expected: compile error — `smartRetrier undefined`

**Step 3: Implement `smartRetrier` in `server.go`**

Replace `dbTicketRetrier` with `smartRetrier`. In `internal/dashboard/server.go`:

Remove:
```go
// dbTicketRetrier resets a failed ticket to queued so the daemon picks it up.
type dbTicketRetrier struct{ db DashboardDB }

func (r *dbTicketRetrier) RetryTicket(ctx context.Context, id string) error {
    return r.db.UpdateTicketStatus(ctx, id, models.TicketStatusQueued)
}
```

Add:
```go
// smartRetrier performs a partial retry: it preserves already-done tasks by
// seeding dag_state with their IDs, resets failed/skipped tasks to pending,
// and re-queues the ticket so the daemon resumes from the failure point.
type smartRetrier struct{ db DashboardDB }

func (r *smartRetrier) RetryTicket(ctx context.Context, ticketID string) error {
    tasks, err := r.db.ListTasks(ctx, ticketID)
    if err != nil {
        return fmt.Errorf("list tasks: %w", err)
    }

    // Collect done task IDs to preserve in dag_state.
    var doneIDs []string
    for _, t := range tasks {
        if t.Status == models.TaskStatusDone {
            doneIDs = append(doneIDs, t.ID)
        }
    }

    // Seed dag_state so TasksForDAGRecovery skips already-done tasks.
    if len(doneIDs) > 0 {
        state := db.DAGState{TicketID: ticketID, CompletedTasks: doneIDs}
        if err := r.db.SaveDAGState(ctx, ticketID, state); err != nil {
            return fmt.Errorf("save dag state: %w", err)
        }
    }

    // Reset failed and skipped tasks to pending.
    for _, t := range tasks {
        if t.Status == models.TaskStatusFailed || t.Status == models.TaskStatusSkipped {
            if err := r.db.UpdateTaskStatus(ctx, t.ID, models.TaskStatusPending); err != nil {
                return fmt.Errorf("reset task %s: %w", t.ID, err)
            }
        }
    }

    return r.db.UpdateTicketStatus(ctx, ticketID, models.TicketStatusQueued)
}
```

Also add `"fmt"` to imports if not already present.

Update `NewServer` to use `smartRetrier`:
```go
// Wire ticket retrier — smart retry preserves completed tasks (ARCH-F05).
api.SetTicketRetrier(&smartRetrier{db: db})
```

Also add `db` package import to `server.go`:
```go
import (
    // existing imports ...
    "github.com/canhta/foreman/internal/db"
)
```

**Step 4: Run the test to confirm it passes**

```bash
go test ./internal/dashboard/... -run TestSmartRetrier -v
```

Expected: PASS.

**Step 5: Run all dashboard tests**

```bash
go test ./internal/dashboard/... -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/dashboard/server.go internal/dashboard/api_test.go
git commit -m "feat: implement smartRetrier for partial DAG retry"
```

---

### Task 3: Orchestrator retry detection — restructure `ProcessTicket` (TDD)

**Files:**
- Modify: `internal/daemon/orchestrator.go`
- Modify: `internal/daemon/orchestrator_test.go`

**Step 1: Add tracking fields to test mocks in `orchestrator_test.go`**

Add `called bool` to `orchMockPlanner`:
```go
type orchMockPlanner struct {
    result *PlanResult
    err    error
    called bool  // ADD
}

func (m *orchMockPlanner) Plan(_ context.Context, _ string, _ *models.Ticket) (*PlanResult, error) {
    m.called = true  // ADD
    if m.err != nil {
        return nil, m.err
    }
    return m.result, nil
}
```

Add `ranTaskIDs []string` to `orchMockTaskRunner`:
```go
type orchMockTaskRunner struct {
    results    map[string]TaskResult
    ranTaskIDs []string  // ADD
}

func (m *orchMockTaskRunner) Run(_ context.Context, taskID string) TaskResult {
    m.ranTaskIDs = append(m.ranTaskIDs, taskID)  // ADD
    if res, ok := m.results[taskID]; ok {
        return res
    }
    return TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}
```

**Step 2: Write the failing test**

Add this test in `orchestrator_test.go`:

```go
// TestProcessTicket_SmartRetry verifies that when a ticket already has done tasks,
// ProcessTicket skips planning and runs only the pending/failed tasks.
func TestProcessTicket_SmartRetry(t *testing.T) {
    f := newOrchFixture()

    // Seed DB with one done task and one pending task (the failed task was reset to pending by smartRetrier).
    f.db.tasks = []models.Task{
        {ID: "task-done", Title: "Task Done", Status: models.TaskStatusDone, Sequence: 1, FilesToModify: []string{"a.go"}},
        {ID: "task-retry", Title: "Task Retry", Status: models.TaskStatusPending, Sequence: 2, FilesToModify: []string{"b.go"}},
    }
    // Simulate dag_state seeded by smartRetrier.
    f.db.dagState = &db.DAGState{
        TicketID:       "t-retry",
        CompletedTasks: []string{"task-done"},
    }
    // task-retry runner result.
    f.runner.results["task-retry"] = TaskResult{TaskID: "task-retry", Status: models.TaskStatusDone}

    ticket := models.Ticket{
        ID:         "t-retry",
        ExternalID: "GH-99",
        Title:      "Smart retry ticket",
    }

    err := f.orch.ProcessTicket(context.Background(), ticket)
    require.NoError(t, err)

    // Planner must NOT have been called (no new tasks created).
    assert.False(t, f.planner.called, "planner should not run in retry mode")
    assert.Empty(t, f.db.createdTasks, "CreateTasks should not be called in retry mode")

    // Only task-retry should have been executed (task-done was skipped by dag recovery).
    assert.Contains(t, f.runner.ranTaskIDs, "task-retry")
    assert.NotContains(t, f.runner.ranTaskIDs, "task-done")

    // PR still created.
    assert.True(t, f.prCreator.called, "PR should still be created after retry")

    // Ticket should reach awaiting_merge.
    seq := f.db.statusSequence("t-retry")
    assert.Equal(t, models.TicketStatusAwaitingMerge, seq[len(seq)-1])
}
```

**Step 3: Run the test to confirm it fails**

```bash
go test ./internal/daemon/... -run TestProcessTicket_SmartRetry -v
```

Expected: FAIL — either the planner IS called, or task-done IS in ranTaskIDs.

**Step 4: Add helpers to `orchestrator.go`**

Add these two helpers at the bottom of `orchestrator.go` (before the compile-time check):

```go
// hasDoneTasks reports whether any task in the slice has status Done.
func hasDoneTasks(tasks []models.Task) bool {
    for _, t := range tasks {
        if t.Status == models.TaskStatusDone {
            return true
        }
    }
    return false
}

// collectTaskFilesToModify collects unique files to modify from existing DB tasks.
// Used in the retry path where no PlanResult is available.
func collectTaskFilesToModify(tasks []models.Task) []string {
    seen := make(map[string]bool)
    var files []string
    for _, t := range tasks {
        for _, f := range t.FilesToModify {
            if !seen[f] {
                seen[f] = true
                files = append(files, f)
            }
        }
    }
    return files
}
```

**Step 5: Restructure `ProcessTicket` to detect retry and skip planning**

In `ProcessTicket`, locate this block (after `EnsureRepo` / around line 320-430):

```go
// Check ticket clarity (if enabled).
if o.config.EnableClarification {
    ...
}

// Create feature branch.
branchName := o.config.BranchPrefix + ticket.ExternalID
if err := o.git.CreateBranch(...); err != nil { ... }

// Plan the ticket.
planResult, err := o.planner.Plan(...)
...

// Convert PlannedTask -> models.Task and persist.
tasks := make([]models.Task, ...)
...
if createErr := o.db.CreateTasks(...); createErr != nil { ... }

// Reserve files via scheduler.
filesToReserve := collectFilesToModify(planResult.Tasks)
if reserveErr := o.scheduler.TryReserve(...); reserveErr != nil { ... }

// Status: planning -> implementing
if implErr := o.db.UpdateTicketStatus(..., models.TicketStatusImplementing); ... { ... }

// Reload tasks from DB to get assigned IDs.
dbTasks, err := o.db.ListTasks(ctx, ticket.ID)
```

Replace that entire block with the following restructured version:

```go
// Detect smart retry: if this ticket already has committed (done) tasks,
// skip planning and reuse the existing branch and task set (ARCH-F05).
priorTasks, priorErr := o.db.ListTasks(ctx, ticket.ID)
isRetry := priorErr == nil && hasDoneTasks(priorTasks)

var dbTasks []models.Task
var codebasePatterns string
branchName := o.config.BranchPrefix + ticket.ExternalID

if isRetry {
    log.Info().Msg("smart retry detected: skipping planning, resuming from existing tasks")
    dbTasks = priorTasks

    // CreateBranch checks out the existing branch if it already exists.
    if err := o.git.CreateBranch(ctx, o.config.WorkDir, branchName); err != nil {
        returnErr = fmt.Errorf("checkout branch %s: %w", branchName, err)
        return returnErr
    }

    // Re-reserve files from existing task list.
    filesToReserve := collectTaskFilesToModify(dbTasks)
    if reserveErr := o.scheduler.TryReserve(ctx, ticket.ID, filesToReserve); reserveErr != nil {
        var conflictErr *FileConflictError
        if errors.As(reserveErr, &conflictErr) {
            log.Info().Str("conflicts", conflictErr.Error()).Msg("file conflict on retry, requeueing ticket")
            if dbErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); dbErr != nil {
                returnErr = fmt.Errorf("requeue after conflict: %w", dbErr)
                return returnErr
            }
            returnErr = nil
            return nil
        }
        returnErr = fmt.Errorf("reserve files: %w", reserveErr)
        return returnErr
    }
} else {
    // Normal path: check clarity, create branch, plan, create tasks.

    // Check ticket clarity (if enabled).
    if o.config.EnableClarification {
        clear, err := o.clarityChecker.CheckTicketClarity(&ticket)
        if err != nil {
            returnErr = fmt.Errorf("check ticket clarity: %w", err)
            return returnErr
        }
        if !clear {
            log.Info().Msg("ticket needs clarification")
            if err := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusClarificationNeeded); err != nil {
                returnErr = fmt.Errorf("update status to clarification_needed: %w", err)
                return returnErr
            }
            if o.config.ClarificationLabel != "" {
                if err := o.tracker.AddLabel(ctx, ticket.ExternalID, o.config.ClarificationLabel); err != nil {
                    log.Warn().Err(err).Msg("failed to add clarification label")
                }
            }
            if err := o.tracker.AddComment(ctx, ticket.ExternalID,
                "Foreman needs more detail to proceed. Please add a clearer description or acceptance criteria."); err != nil {
                log.Warn().Err(err).Msg("failed to add clarification comment")
            }
            o.notify(ctx, ticket, fmt.Sprintf("Question about ticket #%s: needs more detail to proceed.", ticket.ID))
            returnErr = nil
            return nil
        }
    }

    // Create feature branch.
    if err := o.git.CreateBranch(ctx, o.config.WorkDir, branchName); err != nil {
        returnErr = fmt.Errorf("create branch %s: %w", branchName, err)
        return returnErr
    }

    // Plan the ticket.
    o.emitEvent(ctx, ticket.ID, "planning_started", "info", "Planning ticket...")
    planResult, err := o.planner.Plan(ctx, o.config.WorkDir, &ticket)
    if err != nil {
        returnErr = fmt.Errorf("planning: %w", err)
        return returnErr
    }
    if planResult.Status != "OK" {
        returnErr = fmt.Errorf("planner returned non-OK status: %s — %s", planResult.Status, planResult.Message)
        return returnErr
    }
    o.emitEvent(ctx, ticket.ID, "planning_done", "info", fmt.Sprintf("Plan ready: %d tasks", len(planResult.Tasks)))

    // Convert PlannedTask -> models.Task and persist.
    tasks := make([]models.Task, len(planResult.Tasks))
    for i, pt := range planResult.Tasks {
        tasks[i] = models.Task{
            Title:               pt.Title,
            Description:         pt.Description,
            FilesToModify:       pt.FilesToModify,
            FilesToRead:         pt.FilesToRead,
            TestAssertions:      pt.TestAssertions,
            AcceptanceCriteria:  pt.AcceptanceCriteria,
            DependsOn:           pt.DependsOn,
            EstimatedComplexity: pt.EstimatedComplexity,
            Status:              models.TaskStatusPending,
            Sequence:            i + 1,
        }
    }
    if createErr := o.db.CreateTasks(ctx, ticket.ID, tasks); createErr != nil {
        returnErr = fmt.Errorf("create tasks: %w", createErr)
        return returnErr
    }

    // Reserve files via scheduler.
    filesToReserve := collectFilesToModify(planResult.Tasks)
    if reserveErr := o.scheduler.TryReserve(ctx, ticket.ID, filesToReserve); reserveErr != nil {
        var conflictErr *FileConflictError
        if errors.As(reserveErr, &conflictErr) {
            log.Info().Str("conflicts", conflictErr.Error()).Msg("file conflict, requeueing ticket")
            if dbErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); dbErr != nil {
                returnErr = fmt.Errorf("requeue after conflict: %w", dbErr)
                return returnErr
            }
            returnErr = nil
            return nil
        }
        returnErr = fmt.Errorf("reserve files: %w", reserveErr)
        return returnErr
    }

    codebasePatterns = formatCodebasePatterns(planResult.CodebasePatterns)

    // Reload tasks from DB to get assigned IDs.
    var reloadErr error
    dbTasks, reloadErr = o.db.ListTasks(ctx, ticket.ID)
    if reloadErr != nil {
        returnErr = fmt.Errorf("list tasks: %w", reloadErr)
        return returnErr
    }
}

// Status: planning -> implementing (same for both paths).
if implErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusImplementing); implErr != nil {
    returnErr = fmt.Errorf("update status to implementing: %w", implErr)
    return returnErr
}

o.notify(ctx, ticket, fmt.Sprintf("Implementing %d tasks for ticket #%s...", len(dbTasks), ticket.ID))
o.emitEvent(ctx, ticket.ID, "implementing_started", "info", fmt.Sprintf("Starting implementation of %d tasks", len(dbTasks)))
```

Then update the existing code below (that currently uses `dbTasks` and `codebasePatterns`) — it already uses `dbTasks`, so only the `codebasePatterns` variable reference needs updating:

Change the runner factory call:
```go
// Before:
codebasePatterns := formatCodebasePatterns(planResult.CodebasePatterns)
dagRunner := o.runnerFactory.Create(TaskRunnerFactoryInput{
    ...
    CodebasePatterns: codebasePatterns,
    ...
})

// After (codebasePatterns is already set above):
dagRunner := o.runnerFactory.Create(TaskRunnerFactoryInput{
    ...
    CodebasePatterns: codebasePatterns,
    ...
})
```

Also remove the old `dbTasks, err := o.db.ListTasks(...)` line that previously appeared before the runner factory call — it's now handled inside the if-else.

**Step 6: Run the failing test to confirm it now passes**

```bash
go test ./internal/daemon/... -run TestProcessTicket_SmartRetry -v
```

Expected: PASS.

**Step 7: Run all daemon tests**

```bash
go test ./internal/daemon/... -v
```

Expected: all pass. Fix any compile errors from the restructure (e.g., variable redeclarations).

**Step 8: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

**Step 9: Commit**

```bash
git add internal/daemon/orchestrator.go internal/daemon/orchestrator_test.go
git commit -m "feat: skip planning on retry when done tasks already exist (ARCH-F05)"
```

---

## Verification

After all tasks complete, verify end-to-end:

1. Start daemon: `make dev`
2. Submit a ticket with 6 tasks
3. Wait for it to fail (or force a failure by using an invalid model)
4. Click RETRY in the dashboard
5. Confirm in logs:
   - `"smart retry detected: skipping planning, resuming from existing tasks"`
   - `"starting DAG execution"` with `task_count` = number of failed/skipped tasks (not 6)
   - Ticket status transitions: `failed` → `queued` → `planning` → `implementing` → `awaiting_merge`
