# Remove Single-Project Legacy Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove all single-project (legacy) code paths so Foreman exclusively supports multi-project mode, eliminating dead code, misleading API surface, and the migration scaffolding.

**Architecture:** Every running project now has its own daemon, database, tracker, git provider, and orchestrator managed by `setupProjectWorker`. The global daemon (`d` in `newStartCmd`) becomes a thin process-lifetime anchor for auth, PR checking, and channel routing only — it never touches tickets. `migrate.go` and all legacy API endpoints are deleted.

**Tech Stack:** Go, zerolog, net/http stdlib mux, SQLite (go-sqlite3)

---

## Task 1: Delete migration code

**Files:**
- Delete: `internal/project/migrate.go`
- Delete: `internal/project/migrate_test.go`
- Modify: `cmd/start.go`

**Step 1: Delete migrate.go and its test**

```bash
rm internal/project/migrate.go
rm internal/project/migrate_test.go
```

**Step 2: Remove the auto-migration call from `cmd/start.go` lines 189–195**

Remove this block:
```go
// Auto-migrate single-project setup if needed.
if project.NeedsMigration(foremanDir) {
    cfgPath := filepath.Join(foremanDir, "foreman.toml")
    if _, migErr := project.MigrateFromSingleProject(foremanDir, cfgPath); migErr != nil {
        log.Warn().Err(migErr).Msg("single-project migration failed; continuing with legacy mode")
    }
}
```

**Step 3: Verify it compiles**

```bash
go build ./...
```
Expected: no errors. If `migrate.go` exported symbols used elsewhere they will error here — fix those import references.

**Step 4: Run tests**

```bash
go test ./internal/project/... ./cmd/...
```
Expected: all pass (migrate_test.go is gone, nothing should depend on it).

**Step 5: Commit**

```bash
git add -A
git commit -m "remove single-project migration scaffolding"
```

---

## Task 2: Strip legacy global daemon wiring from `cmd/start.go`

**Files:**
- Modify: `cmd/start.go`

The global daemon `d` (in `newStartCmd`) should never do ticket work. Remove the dead construction paths for tracker, git, orchestrator, planner, and pipeline that were only used in legacy mode.

**What to remove** (all lines in the global startup path of `newStartCmd`, not inside `setupProjectWorker`):

| Lines | What | Reason |
|---|---|---|
| 250–254 | `buildTracker(cfg)` call + error check | project workers call this themselves |
| 256–259 | `buildGitProvider` + `repoReady` computation | `WorkDir` is always `""` in multi-project mode; project workers handle this |
| 270–272 | `buildPRCreator(cfg)` / `buildPRChecker(cfg)` calls | project workers call these; `prChecker` is also attached to `d` below — keep `buildPRChecker` only if the global `prChecker` is still used |
| 291–310 | Pipeline agent runner construction | only needed per-project |
| 314–377 | `ticketPlanner`, `pipelineObj`, `orch` construction | only needed per-project |
| 396–401 | `if cfg.Daemon.WorkDir != ""` guard block | was the legacy gate; whole block goes away |

> **Important:** `buildPRChecker(cfg)` at line 272 feeds into `d.SetPRChecker(prChecker)` at line 402 — this runs the merge-checker goroutine on the **global** DB. Check whether per-project workers already have their own PR checker (they do, in `setupProjectWorker` line 781). If so, the global `prChecker` wiring on `d` can be removed too. If the global merge checker is needed for any purpose, keep only `buildPRChecker` and `d.SetPRChecker`.

**Step 1: Remove `buildTracker` call (lines 250–254)**

```go
// DELETE:
// 3. Initialize tracker.
tr, err := buildTracker(cfg)
if err != nil {
    return fmt.Errorf("tracker: %w", err)
}
```

**Step 2: Remove `buildGitProvider` + `repoReady` (lines 256–259)**

```go
// DELETE:
// 4. Initialize git provider and ensure the work repo is ready.
// WorkDir is empty in multi-project mode; per-project workers use their own dir.
gitProv := buildGitProvider(cfg)
repoReady := cfg.Daemon.WorkDir != "" && gitProv.EnsureRepo(context.Background(), cfg.Daemon.WorkDir) == nil
```

**Step 3: Remove `buildPRCreator` (line 271) and evaluate `buildPRChecker` (line 272)**

Keep `prChecker` only if it's used. Check line 402 — `d.SetPRChecker(prChecker)` is inside the route "if prChecker != nil" block. Since per-project workers manage their own PR checking, remove the global prChecker too:

```go
// DELETE:
// 5. Initialize PR creator and checker.
prCreator := buildPRCreator(cfg)
prChecker := buildPRChecker(cfg)
```

And remove `d.SetPRChecker(prChecker)` at line ~402.

**Step 4: Remove pipeline agent runner construction (lines 291–310)**

Delete the block:
```go
// 8b. Build pipeline agent runner ...
var pipelineAgentRunner agent.AgentRunner
agentRunnerName := cfg.AgentRunner.Provider
if agentRunnerName != "" && agentRunnerName != "builtin" {
    ...
}
```

**Step 5: Remove planner, pipeline object, and orchestrator construction (lines 314–377)**

Delete the blocks:
```go
var ticketPlanner daemon.TicketPlanner
if pipelineAgentRunner != nil { ... } else { ... }
pipelineObj := pipeline.NewPipeline(...)
orch := daemon.NewOrchestrator(...)
```

**Step 6: Remove `if cfg.Daemon.WorkDir != ""` guard (lines 396–401) and all uses of removed vars**

```go
// DELETE the entire block:
if cfg.Daemon.WorkDir != "" {
    d.SetTracker(tr)
    d.SetOrchestrator(orch)
    d.SetScheduler(scheduler)
    d.SetRepoReady(repoReady)
}
```

Also remove `scheduler` construction if it's only used here.

**Step 7: Check `skills.NewEngine` call (line ~466)**

```go
engine := skills.NewEngine(llmProv, cmdRunner, cfg.Daemon.WorkDir, cfg.Git.DefaultBranch)
```

This engine is wired to the global daemon's hook runner. In multi-project mode `WorkDir` is `""` and no tickets are processed, so this engine is never invoked. Either remove the hook runner from the global daemon, or keep it for any global-level skill hooks that don't require a repo. Verify by checking if any skill hook is ever triggered on the global daemon — if not, remove it.

**Step 8: Clean up unused imports in `cmd/start.go`**

```bash
go build ./...
```

Fix any "declared and not used" errors from removed variables. Remove unused imports.

**Step 9: Run all tests**

```bash
go test ./...
```
Expected: all pass.

**Step 10: Commit**

```bash
git add cmd/start.go
git commit -m "remove legacy single-project daemon wiring from global startup"
```

---

## Task 3: Remove `daemon.work_dir` from the system config example and add startup warning

**Files:**
- Modify: `foreman.system.toml` (project root, used as working example)
- Modify: `foreman.system.example.toml`
- Modify: `cmd/start.go` — add a startup log warning if `cfg.Daemon.WorkDir` is non-empty (means someone manually set it in the system config)

**Step 1: Remove `work_dir` from system config examples**

In `foreman.system.toml` and `foreman.system.example.toml`, remove any `work_dir` field under `[daemon]` if present.

**Step 2: Add startup warning in `cmd/start.go`**

After loading `cfg`, add near the top of `newStartCmd`'s RunE:

```go
if cfg.Daemon.WorkDir != "" {
    log.Warn().Str("work_dir", cfg.Daemon.WorkDir).
        Msg("daemon.work_dir set in system config is ignored in multi-project mode; use 'foreman project create' to manage projects")
}
```

**Step 3: Compile and test**

```bash
go build ./... && go test ./...
```

**Step 4: Commit**

```bash
git add cmd/start.go foreman.system.toml foreman.system.example.toml
git commit -m "warn on legacy work_dir in system config; multi-project mode only"
```

---

## Task 4: Remove legacy flat API endpoints from dashboard

**Files:**
- Modify: `internal/dashboard/api.go` (remove handler functions + `configDaemon.WorkDir` field)
- Modify: `internal/dashboard/server.go` (remove route registrations)

**Handlers to remove** (functions + their route registrations):

| Handler | Route | Lines in api.go |
|---|---|---|
| `handleListTickets` | `GET /api/tickets` | 633–651 |
| `handleGetTicket` | `GET /api/tickets/{id}` | 653–674 |
| `handleGetEvents` | `GET /api/tickets/{id}/events` | 676–691 |
| `handleGetTasks` | `GET /api/tickets/{id}/tasks` | 706–718 |
| `handleGetLlmCalls` | `GET /api/tickets/{id}/llm-calls` | 720–732 |
| `handleRetryTicket` (global) | `POST /api/tickets/{id}/retry` | 780–799 |
| `handleReplyToTicket` | `POST /api/tickets/{id}/reply` | 908–955 |
| `handleDeleteTicket` (global) | `DELETE /api/tickets/{id}` | 957–972 |
| `handleGetChat` | `GET /api/tickets/{id}/chat` | 1088–1114 |
| `handlePostChat` | `POST /api/tickets/{id}/chat` | 1117–1162 |
| `handleCostsToday` | `GET /api/costs/today` | 693–704 |
| `handleCostsWeek` | `GET /api/costs/week` | 747–759 |
| `handleCostsMonth` | `GET /api/costs/month` | 761–769 |
| `handleActivePipelines` | `GET /api/pipeline/active` | 734–745 |
| `handleTeamStats` | `GET /api/stats/team` | 801–809 |
| `handleRecentPRs` | `GET /api/stats/recent-prs` | 811–818 |
| `handleTicketSummaries` | `GET /api/ticket-summaries` | 820–828 |
| `handleGlobalEvents` | `GET /api/events` | 830–853 |
| `handleActivityBreakdown` | `GET /api/usage/activity` | 1033–1079 |
| `handleRetryTask` (global) | `POST /api/tasks/{id}/retry` | 855–871 |
| `handleTaskContext` | `GET /api/tasks/{id}/context` | 873–906 |

> **Note:** `handleRetryTicket` is used by BOTH the global route (`POST /api/tickets/{id}/retry`) AND the project route (`POST /api/projects/{pid}/tickets/{id}/retry`). Check `server.go` dispatcher — the project route calls it through `projectDB(r)`. When removing, keep only the handler signature but route it only via the project path, OR keep the function and only remove the flat route registration.

**Also remove:**
- `configDaemon.WorkDir` field from `configDaemon` struct (line 384) and `handleConfigSummary` assignment (line ~551)
- Route registrations in `server.go`: `/api/tickets`, `/api/tickets/`, `/api/costs/today`, `/api/costs/week`, `/api/costs/month`, `/api/pipeline/active`, `/api/stats/team`, `/api/stats/recent-prs`, `/api/ticket-summaries`, `/api/events`, `/api/usage/activity`, `/api/tasks/`

**Step 1: Remove route registrations from `server.go`**

In `NewServer`, delete:
```go
mux.Handle("/api/tickets",            auth(api.handleListTickets))
mux.Handle("/api/tickets/",           auth(/* dispatcher ... */))
mux.Handle("/api/costs/today",        auth(api.handleCostsToday))
mux.Handle("/api/pipeline/active",    auth(api.handleActivePipelines))
mux.Handle("/api/costs/week",         auth(api.handleCostsWeek))
mux.Handle("/api/costs/month",        auth(api.handleCostsMonth))
mux.Handle("/api/stats/team",         auth(api.handleTeamStats))
mux.Handle("/api/stats/recent-prs",   auth(api.handleRecentPRs))
mux.Handle("/api/ticket-summaries",   auth(api.handleTicketSummaries))
mux.Handle("/api/events",             auth(api.handleGlobalEvents))
mux.Handle("/api/usage/activity",     auth(api.handleActivityBreakdown))
mux.Handle("/api/tasks/",             auth(/* ... */))
```

**Step 2: Remove legacy handler functions from `api.go`**

Remove each function listed in the table above. Be careful with `handleRetryTicket` — verify if the project-scoped dispatcher still needs it after the restructure.

**Step 3: Remove `configDaemon.WorkDir` field and its assignment**

In `configDaemon` struct:
```go
// REMOVE this field:
WorkDir string `json:"work_dir"`
```

In `handleConfigSummary`:
```go
// REMOVE this line:
WorkDir: cfg.Daemon.WorkDir,
```

**Step 4: Trim `DashboardDB` interface to remove methods no longer called by any retained handler**

After removing the handlers, some methods in `DashboardDB` may no longer be needed at the global `a.db` level. Check each method:
- `ListTickets` — only called by removed handlers → remove from interface
- `GetTicket` — only called by removed handlers → remove
- `GetEvents` — only called by removed handlers → remove
- `ListTasks` / `ListLlmCalls` — only called by removed handlers → remove
- `GetDailyCost` / `GetMonthlyCost` — check if `handleCostsBudgets` still needs them on the global DB; `handleCostsBudgets` reads from `a.costCfg` (config, not DB), so can remove
- `GetTeamStats` / `GetRecentPRs` / `GetTicketSummaries` → remove
- `GetGlobalEvents` — used by `handleGlobalEvents` (legacy, removed) but also possibly by WebSocket; check `ws.go` usage before removing
- `DeleteTicket` / `AppendTicketDescription` — only called by removed handlers → remove
- `GetTaskContextStats` / `UpdateTaskContextStats` — only called by removed `handleTaskContext` → remove
- `GetLlmCallAggregates` / `GetRecentLlmCalls` — only called by removed `handleActivityBreakdown` → remove
- `CreateChatMessage` / `GetChatMessages` — only called by removed chat handlers → remove
- `GetTicketCost` — check all usages before removing
- `UpdateTaskStatus` / `UpdateTicketStatus` / `SaveDAGState` — check if still needed by `smartRetrier`

Keep: `AuthValidator`, `PromptSnapshotQuerier` (used by `handlePromptVersions`), anything used by WebSocket handlers or remaining project-scoped routes.

**Step 5: Compile**

```bash
go build ./...
```
Fix any "undefined" errors from interface mismatches.

**Step 6: Run dashboard tests**

```bash
go test ./internal/dashboard/...
```
Expected: all pass (tests for removed handlers will also be removed).

**Step 7: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go
git commit -m "remove legacy flat API endpoints; dashboard is multi-project only"
```

---

## Task 5: Final verification

**Step 1: Full test suite**

```bash
go test ./...
```
Expected: all pass.

**Step 2: Build the binary**

```bash
make build
```
Expected: clean build, no warnings.

**Step 3: Verify `go vet`**

```bash
go vet ./...
```
Expected: no issues.

**Step 4: Final commit**

```bash
git add -A
git commit -m "chore: complete removal of single-project legacy mode"
```
