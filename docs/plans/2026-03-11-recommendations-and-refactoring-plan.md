# Recommendations and Refactoring Plan

**Review Date:** 2026-03-11  
**Based on:** System Flow Problems, Architectural Issues, UI/UX Inconsistencies, Bugs and Edge Cases  
**Total Issues Identified:** 99 (across all documents)

---

## Overview

This document consolidates findings from all four review areas into actionable refactoring recommendations, grouped by theme. Each recommendation includes rationale, the issues it resolves, and a suggested implementation approach.

---

## Theme 1: Security Hardening

**Issues addressed:** ARCH-04, ARCH-05, ARCH-17, BUG-24, UX-01, UX-06, UX-26, UX-03 (implicit)

### REC-01 — Make Agent Permission Bypass Opt-In

Both ClaudeCode and Copilot runners currently disable all permission checks unconditionally. This is the single highest-risk security issue in the system.

**Recommendation:**
1. Add `SkipPermissions bool` to `ClaudeCodeConfig`. Default to `false`.
2. Wire `internal/agent/permission.go`'s `Evaluate` function to the Copilot permission handler.
3. Document the security implications of enabling `skip_permissions` in `foreman.toml`.
4. Add a `foreman doctor` check that warns when `skip_permissions = true`.

**Files to change:** `internal/agent/claudecode.go`, `internal/agent/copilot.go`, `internal/agent/permission.go`, `cmd/doctor.go`

---

### REC-02 — Redact Credentials in All API Responses

**Recommendation:**
1. In `flattenProjectConfig`, mask `GitToken` and `TrackerToken` using `util.RedactKey` before serializing.
2. In `handleGetProject`, apply the same masking.
3. Define a sentinel constant `"__unchanged__"` — when received in a PUT body, skip updating that field.
4. Update `ProjectSettings.svelte` to not send back masked values.

**Files to change:** `internal/dashboard/api.go`, `internal/dashboard/web/src/pages/ProjectSettings.svelte`, `internal/dashboard/web/src/types.ts`

---

### REC-03 — Fix SSRF Risk in `handleTestConnection`

**Recommendation:**
1. Validate that the Jira URL uses `https://` only.
2. Resolve the host and reject private IP ranges (RFC 1918: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) and loopback.
3. Apply same validation to any other "test connectivity" endpoints.

**Files to change:** `internal/dashboard/api.go`

---

### REC-04 — Remove Deprecated `?token=` WebSocket Auth

**Recommendation:**
1. Remove the `?token=` URL parameter code path from all three WebSocket handlers.
2. Extract a shared `authenticateWebSocket(w, r, db)` helper used by all three handlers.
3. Update any documentation that references the deprecated path.

**Files to change:** `internal/dashboard/ws.go`

---

## Theme 2: Pipeline Correctness

**Issues addressed:** SF-01, SF-02, SF-03, SF-04, SF-05, SF-06, SF-07, SF-08, SF-09, SF-11, SF-12

### REC-05 — Fix LLM Call Cap and Review Retry Budgeting

The call cap mechanism is broken by design — it counts reviewer calls against the implementation budget, and review rejections burn implementation retry slots.

**Recommendation:**
1. **Separate counters**: Add `impl_calls` and `review_calls` columns to the task record, or track them in-memory per `RunTask` invocation.
2. **Separate inner loops**: Add inner retry loops for spec review (bounded by `MaxSpecReviewCycles`) and quality review (bounded by `MaxQualityReviewCycles`), separate from the outer implementation retry loop.
3. **Agent path parity**: Apply the same retry logic to `runTaskWithAgent` — inject feedback on rejection and retry within bounds.
4. **Default recalibration**: Review the default `MaxLlmCallsPerTask = 8` with the new separated accounting in mind.

**Files to change:** `internal/pipeline/task_runner.go`, `internal/pipeline/call_cap.go`

---

### REC-06 — Fix Terminal Status Gaps in Ticket Lifecycle

Several ticket statuses are dead ends with no path forward.

**Recommendation:**
1. `TicketStatusPRUpdated` — Include in `merge_checker.go`'s `checkAll` filter. On detecting an external push, update the stored SHA and continue monitoring rather than transitioning away.
2. `TicketStatusPartial` — Include in `checkAll` filter so partial PRs can be tracked for merge.
3. `checkParentCompletion` — Define a terminal set (`Merged`, `PRClosed`, `Failed`, `Done`) and close the parent when ALL children are terminal.
4. `TicketStatusClarificationNeeded` — Wire `shouldPickUp` into `ingestFromTracker` so clarification resolution triggers re-queuing.

**Files to change:** `internal/daemon/merge_checker.go`, `internal/daemon/pickup.go`

---

### REC-07 — Fix Worktree Merge Rollback and Branch Preservation

**Recommendation:**
1. On `MergeNoFF` failure in the merge loop: set `returnErr`, break the loop.
2. Do NOT delete `WorktreeDir` / `WorktreeBranch` when the merge failed — preserve them for debugging and manual recovery.
3. Optionally: attempt `git merge --abort` / `git reset --hard` rollback of partial merges before failing.

**Files to change:** `internal/daemon/orchestrator.go`

---

### REC-08 — Wire Rebase Conflict Resolver Into Orchestration Path

The `rebase_resolver.go` module exists but is never called automatically.

**Recommendation:**
1. On `!rebaseResult.Success`: call `AttemptConflictResolution` from `rebase_resolver.go` before giving up.
2. Always call `git rebase --abort` before returning a rebase failure to restore a clean worktree state.
3. Only mark the ticket `failed` if the LLM-assisted resolution also fails.

**Files to change:** `internal/daemon/orchestrator.go`, `internal/pipeline/rebase_resolver.go`

---

### REC-09 — Fix Graceful Shutdown Lock Release

**Recommendation:**
- In the ticket-processing goroutine's deferred `ReleaseLock`, use a fresh `context.Background()` with a short timeout (5s) instead of the parent shutdown context:
  ```go
  defer func() {
      releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
      defer cancel()
      database.ReleaseLock(releaseCtx, lk)
  }()
  ```

**Files to change:** `internal/daemon/daemon.go`

---

## Theme 3: Data Layer Correctness

**Issues addressed:** BUG-01, BUG-02, BUG-05, BUG-06, BUG-07, BUG-08, BUG-09, BUG-13, BUG-14, BUG-19, BUG-20

### REC-10 — Fix Database Lock Atomicity

**Recommendation:**
1. Wrap `AcquireLock`'s `DELETE` + `INSERT` in a single `BEGIN IMMEDIATE` transaction.
2. Add a random UUID component to `holderID` to handle PID reuse after crash:
   ```go
   var holderID = fmt.Sprintf("%s:%d:%s", hostname, os.Getpid(), uuid.New().String()[:8])
   ```
3. Inject `holderID` as a constructor parameter of `SQLiteDB` to enable proper multi-process testing.

**Files to change:** `internal/db/sqlite.go`, `internal/db/lock.go`

---

### REC-11 — Fix `ReserveFiles` Conflict Detection

**Recommendation:**
1. Remove `ReserveFiles` from the `Database` interface (force callers to use `TryReserveFiles`), or
2. Change `ReserveFiles` to check `RowsAffected` for each row and return an explicit conflict error.

**Files to change:** `internal/db/sqlite.go`, `internal/db/db.go`

---

### REC-12 — Fix Data Orphaning and Cascade Deletes

**Recommendation:**
1. Add `ON DELETE CASCADE` to all foreign keys referencing `tickets.id` in the schema: `llm_call_details`, `context_feedback`, `dag_states`, `embeddings`, `chat_messages`.
2. Add a partial index: `CREATE INDEX idx_file_reservations_active ON file_reservations(file_path) WHERE released_at IS NULL;`
3. Fix `GetChildTickets` to guard `prNumber.Valid` before assigning.

**Files to change:** `internal/db/schema.go`, `internal/db/sqlite.go`

---

### REC-13 — Fix Context Feedback Scoping and Storage

**Recommendation:**
1. Add a `work_dir TEXT` column to `context_feedback` and scope all queries to the current worktree.
2. Store `files_touched` as a JSON array rather than a comma-separated string to handle paths with commas.
3. Sort file sets deterministically before storing (also fix BUG-17 non-deterministic file selection).

**Files to change:** `internal/db/schema.go`, `internal/db/sqlite.go`, `internal/context/file_scanner.go`

---

### REC-14 — Adopt a Database Migration Strategy

**Recommendation:**
1. Adopt `pressly/goose` or `golang-migrate/migrate` for schema migrations.
2. Convert existing `CREATE TABLE IF NOT EXISTS` schema to migration `0001`.
3. Add a `schema_version` table.
4. Fix the destructive `llm_call_details` migration to use `ALTER TABLE` + data copy instead of `DROP TABLE`.

**Files to change:** `internal/db/sqlite.go`, `internal/db/schema.go` (new migration files)

---

## Theme 4: Tracker Completeness

**Issues addressed:** BUG-03, BUG-04, BUG-21, BUG-22, BUG-32

### REC-15 — Implement Missing Tracker Operations

**Recommendation:**
1. **Linear**: Implement `UpdateStatus` (GraphQL `IssueUpdate` mutation for state) and `AddLabel` (label association mutation). Replace `fmt.Sprintf` GraphQL construction with variable-based requests.
2. **Jira**: Implement `UpdateStatus` using the `POST /rest/api/3/issue/{key}/transitions` endpoint. Cache transition IDs per project.
3. **GitHub Issues**: Implement pagination via the `Link` header in `FetchReadyTickets`.
4. **All trackers**: Add rate-limited HTTP wrappers with exponential backoff on 429/503, respecting `RateLimitConfig`.

**Files to change:** `internal/tracker/linear.go`, `internal/tracker/jira.go`, `internal/tracker/github_issues.go`

---

## Theme 5: Architecture and Initialization

**Issues addressed:** ARCH-01, ARCH-02, ARCH-03, ARCH-06, ARCH-07, ARCH-08, ARCH-13, ARCH-14, ARCH-15, ARCH-16

### REC-16 — Eliminate Two-Phase Init Anti-Patterns

**Recommendation:**
1. **SubagentTool**: Break the circular dependency by extracting a `SubagentExecutor` interface. Construct `Registry` with `runFn` in a single call: `NewRegistry(executor SubagentExecutor) *Registry`.
2. **Skills Engine**: Make `AgentRunner` and `GitProvider` required constructor parameters with nil-checks in `NewEngine`.
3. **Outer daemon in `cmd/start.go`**: Audit whether the unwired outer daemon is intentional. If not, remove it. If single-project mode should use `setupProjectWorker`, do so unconditionally.

**Files to change:** `internal/agent/tools/registry.go`, `internal/agent/builtin.go`, `internal/skills/engine.go`, `cmd/start.go`

---

### REC-17 — Decompose the God `Database` Interface

**Recommendation:**
1. Define role-specific sub-interfaces:
   ```go
   type TicketStore interface { GetTicket; UpdateTicketStatus; ListTickets; ... }
   type LlmCallRecorder interface { RecordLlmCall; StoreCallDetails; ... }
   type FileReservationStore interface { TryReserveFiles; ReleaseFiles; ... }
   type LockStore interface { AcquireLock; ReleaseLock; ... }
   type CostStore interface { RecordDailyCost; GetDailyCost; ... }
   ```
2. Update callers to accept only the sub-interfaces they need.
3. `SQLiteDB` continues to implement all interfaces.
4. The large `db.Database` remains as a composed convenience type for the wiring layer.

**Files to change:** `internal/db/db.go`, all callers of `db.Database`

---

### REC-18 — Fix Configuration Environment Variable Handling

**Recommendation:**
1. Apply `expandEnvVars` in `LoadDefaults` as well as `LoadFromFile`.
2. Use `os.Expand` (handles both `$VAR` and `${VAR}`) instead of the custom `expandEnv` implementation.
3. Enable `viper.AutomaticEnv()` with `FOREMAN_` prefix for all config keys.
4. Use atomic temp-file + rename pattern in `config/persist.go`.
5. Add a validation pass that warns when a config value starts with `$` but was not expanded.

**Files to change:** `internal/config/config.go`, `internal/config/persist.go`

---

### REC-19 — Fix Rate Limiter Goroutine Leak

**Recommendation:**
- In `OnRateLimit`, use a context-aware sleep:
  ```go
  func (r *RateLimiter) OnRateLimit(ctx context.Context, retryAfterSecs int) {
      select {
      case <-time.After(time.Duration(retryAfterSecs) * time.Second):
          r.resetLimit()
      case <-ctx.Done():
      }
  }
  ```

**Files to change:** `internal/llm/ratelimiter.go`

---

### REC-20 — Fix LLM Call ID Generation to Avoid Collisions

**Recommendation:**
- Replace `time.Now().UnixNano()` with `github.com/google/uuid` or a global atomic counter:
  ```go
  callID := fmt.Sprintf("llm-%s", uuid.New().String())
  ```

**Files to change:** `internal/llm/recording_provider.go`

---

## Theme 6: CLI and Dashboard UX

**Issues addressed:** UX-02, UX-03, UX-04, UX-07, UX-08, UX-09, UX-10, UX-11, UX-13, UX-14, UX-21

### REC-21 — Implement Stubs and Fix CLI Correctness

**Recommendation:**
1. **`foreman stop`**: Implement PID-based signaling (write PID on `start`, signal on `stop`).
2. **`foreman run`**: Implement or hide with `Hidden: true` + error return.
3. **`foreman logs --follow`**: Implement `tail -f` semantics or emit a clear "not yet implemented" error.
4. **`foreman doctor`**: Replace `os.Exit(1)` calls with `return fmt.Errorf(...)`.
5. **`foreman project delete`**: Add `--yes` / `-y` confirmation flag.
6. **Version string**: Replace hardcoded `"0.1.0"` with the package-level version variable.
7. **`--config` flag**: Add persistent `--config` flag on `rootCmd`.

**Files to change:** `cmd/stop.go`, `cmd/run.go`, `cmd/logs.go`, `cmd/doctor.go`, `cmd/project.go`, `cmd/start.go`, `cmd/dashboard.go`, `cmd/config.go`, `cmd/root.go`

---

### REC-22 — Add API Pagination and Rate Limiting

**Recommendation:**
1. Add `?limit=` and `?offset=` to `handleProjectTickets` and `handleProjectTicketSummaries`.
2. Add `?limit=` and `?offset=` to `handleProjectEvents` (matching `handleProjectGlobalEvents`).
3. Implement a rate limiter middleware on the HTTP router respecting `RateLimit.RequestsPerMinute`.
4. Parallelize `handleOverview`'s N×4 DB queries with `errgroup`.
5. Add a `GetCostRange` DB method to replace 7 serial queries in `handleProjectCostsWeek`.

**Files to change:** `internal/dashboard/api.go`, `internal/dashboard/server.go`

---

### REC-23 — Standardize API Error Responses

**Recommendation:**
- Replace all `http.Error(w, "message", code)` calls with `writeJSON(w, code, map[string]string{"error": "message"})` across the entire `api.go` file.
- Enforce this consistently for WebSocket authentication failures too.

**Files to change:** `internal/dashboard/api.go`, `internal/dashboard/ws.go`

---

### REC-24 — Fix Frontend Error Handling and Reconnect Logic

**Recommendation:**
1. Replace empty `catch {}` blocks in WebSocket `onmessage` with `console.warn(...)`.
2. Show a toast notification when chat history fails to load.
3. Implement exponential backoff for WebSocket reconnect (1s, 2s, 4s... up to 60s).
4. Refactor `createProject`, `deleteProject`, and Settings save to use `api.ts` helpers.

**Files to change:** `internal/dashboard/web/src/state/project.svelte.ts`, `internal/dashboard/web/src/state/global.svelte.ts`, `internal/dashboard/web/src/pages/ProjectSettings.svelte`

---

## Theme 7: Git and Context Assembly

**Issues addressed:** BUG-10, BUG-11, BUG-12, BUG-17, BUG-18, BUG-24, BUG-27, BUG-30

### REC-25 — Fix Git Operation Correctness

**Recommendation:**
1. Quote SSH key paths in `GIT_SSH_COMMAND`: `fmt.Sprintf("ssh -i %q -o ...", g.sshKeyPath)`.
2. Add `Timeout: 30 * time.Second` to `GitHubPRCreator`'s HTTP client.
3. Fix `gogit FileTree` to use the git index rather than `filepath.Walk`.
4. Thread caller context through `snapshot.go`'s `exec.CommandContext` calls.
5. Fix `WalkContextFiles` to normalize paths with `filepath.Clean` before comparison.

**Files to change:** `internal/git/native.go`, `internal/git/github_pr.go`, `internal/git/gogit.go`, `internal/snapshot/snapshot.go`, `internal/context/walk_context_files.go`

---

### REC-26 — Fix Context Assembly Determinism and Token Budgeting

**Recommendation:**
1. Sort Tier-1 file map keys before iterating in `file_scanner.go`.
2. Use the tiktoken estimator (not byte heuristic) at the budget check stage in `file_selector.go`.
3. Skip binary files in the context assembler (null-byte check on first N bytes).
4. Fix `observationLog.ReadFrom` cursor to track position as sum of scanned line lengths.

**Files to change:** `internal/context/file_scanner.go`, `internal/context/file_selector.go`, `internal/context/assembler.go`, `internal/context/observations.go`

---

## Theme 8: Code Quality and Dead Code Removal

**Issues addressed:** ARCH-20, ARCH-24, ARCH-25, ARCH-27, UX-14, SF-19

### REC-27 — Remove or Implement Dead Code

| Dead Code | Action |
|-----------|--------|
| `internal/agent/task_manager.go` | Wire into dashboard agent tracking or delete |
| `cmd/run.go` (stub) | Implement or hide |
| `cmd/stop.go` (stub) | Implement |
| `cmd/logs.go --follow` (stub) | Implement or emit error |
| `rebase_resolver.go` (uncalled) | Wire into orchestrator (REC-08) |
| `shouldPickUp` in `pickup.go` (uncalled) | Wire into `ingestFromTracker` (REC-06) |
| `loadForemanContext` duplication | Extract to shared helper (REC-27a) |

---

### REC-28 — Fix Build Module Declaration

**Recommendation:**
- Change `go 1.25.0` in `go.mod` to `go 1.24.0` (or the actual minimum tested version).
- Consider using build tags for the WhatsApp dependency to reduce binary size for deployments that don't need it.

**Files to change:** `go.mod`

---

## Summary of Recommendations

| ID | Theme | Priority | Effort | Issues Resolved |
|----|-------|----------|--------|-----------------|
| REC-01 | Security | P0 | Medium | ARCH-04, ARCH-05 |
| REC-02 | Security | P0 | Small | UX-01, UX-27, MED-14 |
| REC-03 | Security | P0 | Small | UX-26 |
| REC-04 | Security | P1 | Small | UX-05, UX-06, HIGH-02, HIGH-03 |
| REC-05 | Pipeline | P0 | Large | SF-01, SF-02, SF-03 |
| REC-06 | Pipeline | P0 | Medium | SF-06, SF-07, SF-11, SF-13 |
| REC-07 | Pipeline | P0 | Small | SF-04 |
| REC-08 | Pipeline | P1 | Medium | SF-12 |
| REC-09 | Pipeline | P1 | Small | SF-09 |
| REC-10 | Data | P0 | Small | BUG-01, BUG-19 |
| REC-11 | Data | P0 | Small | BUG-05, HIGH-01 |
| REC-12 | Data | P1 | Medium | BUG-06, BUG-13, BUG-14 |
| REC-13 | Data | P1 | Medium | BUG-07, BUG-08, BUG-17 |
| REC-14 | Data | P1 | Large | ARCH-08, BUG-02 |
| REC-15 | Trackers | P0 | Large | BUG-03, BUG-04, BUG-21, BUG-32 |
| REC-16 | Architecture | P1 | Large | ARCH-01, ARCH-02, ARCH-03 |
| REC-17 | Architecture | P2 | Large | ARCH-07 |
| REC-18 | Architecture | P1 | Medium | ARCH-13, ARCH-14, ARCH-15, ARCH-16 |
| REC-19 | Architecture | P1 | Small | ARCH-06 |
| REC-20 | Architecture | P1 | Small | ARCH-18 |
| REC-21 | CLI/UX | P0 | Medium | UX-02, UX-03, UX-04, UX-10, UX-14, UX-21 |
| REC-22 | Dashboard | P1 | Medium | UX-07, UX-08, UX-09, UX-11, UX-23 |
| REC-23 | Dashboard | P1 | Small | UX-13, MED-01, MED-05 |
| REC-24 | Frontend | P1 | Small | UX-24, UX-25, LOW-11 |
| REC-25 | Git | P1 | Small | BUG-10, BUG-11, BUG-24, BUG-25 |
| REC-26 | Context | P1 | Medium | BUG-12, BUG-17, BUG-18, BUG-27, BUG-30 |
| REC-27 | Code Quality | P2 | Small | ARCH-20, ARCH-24, ARCH-27, SF-11 |
| REC-28 | Build | P2 | Small | ARCH-21, ARCH-25 |
