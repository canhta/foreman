# Prioritized Action Plan

**Review Date:** 2026-03-11  
**Total Issues:** 99 across 4 review areas  
**Total Recommendations:** 28

This document organizes all work into execution phases, ordered by severity and dependency. Each phase is designed to be executable independently, with P0 being the minimum viable "safe to run in production" baseline.

---

## Phase 0 — Critical Safety Fixes (Do Immediately)

These issues cause data loss, silent feature breakage, or security vulnerabilities. They should be addressed before any new features are developed.

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-01 | Make agent permission bypass opt-in | Medium | ARCH-04, ARCH-05 |
| REC-02 | Redact credentials in API responses | Small | UX-01, MED-14 |
| REC-03 | Fix SSRF risk in `handleTestConnection` | Small | UX-26 |
| REC-07 | Preserve worktree branches on merge failure | Small | SF-04 |
| REC-09 | Fix lock release on graceful shutdown | Small | SF-09 |
| REC-10 | Fix `AcquireLock` TOCTOU race | Small | BUG-01, BUG-19 |
| REC-11 | Fix `ReserveFiles` conflict detection | Small | BUG-05 |
| REC-15 (Linear) | Implement `LinearTracker.UpdateStatus` + `AddLabel` | Medium | BUG-03 |
| REC-15 (Jira) | Implement `JiraTracker.UpdateStatus` | Medium | BUG-04 |

**Estimated effort:** ~3–4 days  
**Risk if deferred:** Duplicate task execution, data loss on shutdown, credential exfiltration, Linear/Jira deployments completely non-functional.

---

## Phase 1 — Core Correctness (High Priority)

These issues cause incorrect behavior, stuck tickets, or significant quality degradation. Address in the first sprint after Phase 0.

### 1a — Pipeline State Machine

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-05 | Fix LLM call cap and review retry budgeting | Large | SF-01, SF-02, SF-03 |
| REC-06 | Fix terminal status gaps (partial, pr_updated, clarification) | Medium | SF-06, SF-07, SF-11, SF-13 |
| REC-08 | Wire rebase conflict resolver into orchestration | Medium | SF-12 |

### 1b — Data Layer

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-12 | Fix data orphaning, cascade deletes, PRNumber NULL handling | Medium | BUG-06, BUG-13, BUG-14 |
| REC-13 | Fix context feedback scoping and file path storage | Medium | BUG-07, BUG-08, BUG-17 |
| BUG-09 fix | Fix timestamp parsing for fractional-second SQLite timestamps | Small | BUG-09 |

### 1c — Git and Context

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-25 | Quote SSH key paths, fix PR creator timeout, fix gogit FileTree | Small | BUG-10, BUG-11, BUG-24, BUG-25 |
| REC-26 | Fix context assembly: sort files, binary skip, cursor, token budget | Medium | BUG-12, BUG-17, BUG-18, BUG-27, BUG-30 |

### 1d — Architecture

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-19 | Fix rate limiter goroutine leak | Small | ARCH-06 |
| REC-20 | Fix LLM call ID collision under concurrency | Small | ARCH-18 |
| REC-18 | Fix config env var expansion (`LoadDefaults`, `$VAR`, atomic write) | Medium | ARCH-13, ARCH-14, ARCH-15, ARCH-16 |

**Estimated effort:** ~2 weeks  
**Risk if deferred:** Tasks fail prematurely, tickets get permanently stuck, context assembly produces wrong/insecure results, goroutine leaks accumulate over time.

---

## Phase 2 — Usability and Developer Experience (Medium Priority)

These issues don't cause data loss but significantly hurt usability, debuggability, and correctness of deployed Foreman.

### 2a — CLI Completeness

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-21a | Implement `foreman stop` (PID signaling) | Small | UX-02 |
| REC-21b | Implement or hide `foreman run` | Small | UX-03 |
| REC-21c | Implement `foreman logs --follow` | Small | UX-14 |
| REC-21d | Fix `foreman doctor` to return errors not `os.Exit` | Small | UX-04 |
| REC-21e | Add `--yes` flag to `foreman project delete` | Small | UX-21 |
| REC-21f | Fix hardcoded version string | Small | UX-10 |
| REC-21g | Add `--config` flag to root command | Small | LOW-07 |

### 2b — Dashboard API Quality

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-23 | Standardize error response format (all JSON) | Small | UX-13 |
| REC-22a | Add pagination to ticket list endpoints | Medium | UX-07 |
| REC-22b | Implement rate limiter middleware | Medium | UX-08 |
| REC-22c | Parallelize `handleOverview` queries | Small | UX-09 |
| REC-04 | Remove deprecated `?token=` WS auth path | Small | UX-05, UX-06 |

### 2c — Frontend Fixes

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-24a | Fix WebSocket reconnect with exponential backoff | Small | LOW-11 |
| REC-24b | Fix silent error swallowing in WS onmessage | Small | UX-25 |
| REC-24c | Show "Chat unavailable" toast on fetch error | Small | UX-24 |
| REC-24d | Refactor raw `fetch` calls to use `api.ts` helpers | Small | LOW-01 |
| UX-35 fix | Show token provided/not-provided in ProjectWizard review | Small | LOW-08 |

### 2d — Tracker Completeness

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-15 (GitHub) | Implement GitHub Issues pagination | Medium | BUG-21 |
| REC-15 (Linear) | Fix GraphQL variable escaping | Small | BUG-22 |
| REC-15 (rate limiting) | Add tracker HTTP rate limiting with backoff | Medium | BUG-32 |

**Estimated effort:** ~1.5 weeks  
**Risk if deferred:** Poor operator experience, users unable to reliably stop the daemon, continued silent failures that are hard to debug.

---

## Phase 3 — Architecture Refactoring (Lower Priority, High Impact Long-Term)

These are structural improvements that make the system maintainable and properly testable. They don't fix immediate bugs but are critical for long-term health.

| ID | Description | Effort | Reference |
|----|-------------|--------|-----------|
| REC-14 | Adopt database migration strategy (goose/migrate) | Large | ARCH-08, BUG-02 |
| REC-16 | Eliminate two-phase init (SubagentTool, Skills Engine, outer daemon) | Large | ARCH-01, ARCH-02, ARCH-03 |
| REC-17 | Decompose god `Database` interface into role-specific sub-interfaces | Large | ARCH-07 |
| REC-27 | Remove/implement dead code (TaskManager, stubs, loadForemanContext dup) | Small | ARCH-20, ARCH-24, ARCH-27 |
| REC-28 | Fix `go.mod` version declaration | Small | ARCH-21 |
| ARCH-09 fix | Make `ClaudeCodeRunner.WithRegistry` a constructor param | Small | ARCH-09 |
| ARCH-10 fix | Fix `CopilotRunner` context propagation | Small | ARCH-10 |
| BUG-33 fix | Add DB contract test coverage for concurrency edge cases | Medium | BUG-33 |

**Estimated effort:** ~2–3 weeks  
**Why defer:** These require broader coordination and carry refactoring risk. Phase 0–2 fixes are self-contained and safe to apply without waiting for these.

---

## Execution Checklist by File

For teams working through this fix-by-fix, here is the consolidated file-to-issues mapping:

### `internal/pipeline/task_runner.go`
- [ ] SF-01: Fix call cap to not count reviewers against implementation budget
- [ ] SF-02: Add separate inner loops for spec/quality review retries
- [ ] SF-03: Make agent path reviews blocking with retry

### `internal/pipeline/call_cap.go`
- [ ] SF-01: Separate impl and reviewer counters

### `internal/daemon/merge_checker.go`
- [ ] SF-06: Include `TicketStatusPartial` in `checkAll` filter
- [ ] SF-07: Include `TicketStatusPRUpdated` in `checkAll` filter
- [ ] SF-13: Fix `checkParentCompletion` to use terminal-set semantics

### `internal/daemon/orchestrator.go`
- [ ] SF-04: Fix merge loop to not delete worktree on failure
- [ ] SF-12: Wire rebase resolver, always call `git rebase --abort`
- [ ] SF-15: Add `RetryAfter` backoff to smart retry

### `internal/daemon/daemon.go`
- [ ] SF-09: Fix `ReleaseLock` in deferred to use `context.Background()` with timeout
- [ ] SF-10: Remove redundant `UpdateTicketStatus` in orchestrator

### `internal/daemon/pickup.go`
- [ ] SF-11: Wire `shouldPickUp` into `ingestFromTracker`

### `internal/daemon/recovery.go`
- [ ] SF-16: Check DB task status in `TasksForDAGRecovery`

### `internal/daemon/dag_executor.go`
- [ ] SF-08: Fix goroutine leak under context cancellation

### `internal/pipeline/decompose.go`
- [ ] SF-05: Add rollback on partial child creation failure
- [ ] SF-17: Strip markdown fences before JSON unmarshal

### `internal/db/sqlite.go`
- [ ] BUG-01: Wrap `AcquireLock` in `BEGIN IMMEDIATE` transaction
- [ ] BUG-02: Fix destructive `llm_call_details` migration
- [ ] BUG-05: Fix `ReserveFiles` conflict detection
- [ ] BUG-06: Add cascade deletes or explicit cleanup in `DeleteTicket`
- [ ] BUG-07: Add `work_dir` scope to `QueryContextFeedback`
- [ ] BUG-08: Store file paths as JSON array in `context_feedback`
- [ ] BUG-09: Fix timestamp parsing for fractional-second formats
- [ ] BUG-13: Guard `prNumber.Valid` in `GetChildTickets`

### `internal/db/schema.go`
- [ ] BUG-14: Add partial index on `file_reservations`
- [ ] ARCH-08: Begin migration strategy work

### `internal/db/lock.go`
- [ ] BUG-19: Add UUID component to `holderID`
- [ ] ARCH-21: Inject `holderID` as constructor param

### `internal/tracker/linear.go`
- [ ] BUG-03: Implement `UpdateStatus` and `AddLabel`
- [ ] BUG-22: Fix GraphQL variable escaping

### `internal/tracker/jira.go`
- [ ] BUG-04: Implement `UpdateStatus` with transition ID fetch

### `internal/tracker/github_issues.go`
- [ ] BUG-21: Implement pagination

### `internal/agent/claudecode.go`
- [ ] ARCH-04: Make `--dangerously-skip-permissions` opt-in

### `internal/agent/copilot.go`
- [ ] ARCH-05: Wire permission Evaluate to handler instead of ApproveAll
- [ ] ARCH-10: Fix context propagation in constructor

### `internal/llm/ratelimiter.go`
- [ ] ARCH-06: Fix goroutine leak with context-aware sleep

### `internal/llm/recording_provider.go`
- [ ] ARCH-18: Use UUID for call IDs

### `internal/config/config.go`
- [ ] ARCH-13: Apply `expandEnvVars` in `LoadDefaults`
- [ ] ARCH-15: Use `os.Expand` for both `$VAR` and `${VAR}`
- [ ] ARCH-14: Add `viper.AutomaticEnv()` with `FOREMAN_` prefix

### `internal/config/persist.go`
- [ ] ARCH-16: Use atomic temp-file + rename pattern

### `internal/git/native.go`
- [ ] BUG-10: Quote SSH key path in `GIT_SSH_COMMAND`

### `internal/git/github_pr.go`
- [ ] BUG-25: Add HTTP timeout to `GitHubPRCreator`

### `internal/git/gogit.go`
- [ ] BUG-24: Use git index instead of `filepath.Walk`

### `internal/snapshot/snapshot.go`
- [ ] BUG-11: Thread caller context through `exec.CommandContext`
- [ ] BUG-28: Return error from `git diff` instead of swallowing

### `internal/context/file_scanner.go`
- [ ] BUG-17: Sort Tier-1 file keys before iterating

### `internal/context/file_selector.go`
- [ ] BUG-18: Use tiktoken estimator at budget-check stage

### `internal/context/assembler.go`
- [ ] BUG-27: Skip binary files

### `internal/context/observations.go`
- [ ] BUG-12: Fix cursor tracking after scanner buffering

### `internal/context/walk_context_files.go`
- [ ] BUG-30: Normalize paths with `filepath.Clean`

### `internal/dashboard/api.go`
- [ ] UX-01: Redact credentials in GET responses
- [ ] UX-09: Parallelize `handleOverview` queries
- [ ] UX-13: Standardize all errors to JSON format
- [ ] UX-26: Validate Jira URL (SSRF fix)
- [ ] REC-22: Add pagination to ticket list and events endpoints
- [ ] UX-23: Parallelize week cost queries

### `internal/dashboard/ws.go`
- [ ] UX-05: Extract shared `authenticateWebSocket` helper
- [ ] UX-06: Remove deprecated `?token=` path

### `internal/dashboard/server.go`
- [ ] UX-08: Add rate limiter middleware

### `internal/dashboard/web/src/state/project.svelte.ts`
- [ ] UX-24: Show toast on chat fetch error
- [ ] UX-25: Log WS message processing errors
- [ ] LOW-11: Add WS reconnect exponential backoff

### `internal/dashboard/web/src/state/global.svelte.ts`
- [ ] LOW-01: Use `api.ts` helpers for `createProject`, `deleteProject`
- [ ] LOW-11: Add WS reconnect exponential backoff

### `internal/dashboard/web/src/pages/ProjectSettings.svelte`
- [ ] UX-27: Handle masked token sentinel on save

### `cmd/stop.go`
- [ ] UX-02: Implement PID-based signaling

### `cmd/run.go`
- [ ] UX-03: Implement or hide

### `cmd/logs.go`
- [ ] UX-14: Implement `--follow` or emit error

### `cmd/doctor.go`
- [ ] UX-04: Replace `os.Exit(1)` with `return fmt.Errorf(...)`

### `cmd/project.go`
- [ ] UX-21: Add `--yes` confirmation to delete

### `cmd/start.go`
- [ ] UX-10: Fix hardcoded version string
- [ ] ARCH-03: Audit outer unwired daemon
- [ ] ARCH-17: Remove tracker→git token cross-use

### `go.mod`
- [ ] ARCH-21: Fix `go 1.25.0` → `go 1.24.0`

---

## Risk Matrix

| Phase | Items | Estimated Effort | Regression Risk | Production Risk (if skipped) |
|-------|-------|-----------------|----------------|-------------------------------|
| Phase 0 | 9 | ~3–4 days | Low (targeted fixes) | CRITICAL — data loss, security |
| Phase 1 | ~20 | ~2 weeks | Medium (pipeline changes) | HIGH — stuck tickets, wrong results |
| Phase 2 | ~20 | ~1.5 weeks | Low (UI/CLI changes) | MEDIUM — poor UX, hidden failures |
| Phase 3 | ~10 | ~2–3 weeks | High (structural refactor) | LOW (long-term maintainability) |

**Total estimated effort: 6–9 weeks** for a single developer, or 2–3 weeks for a team of 3 working in parallel across phases.
