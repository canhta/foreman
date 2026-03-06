# Architectural Review & Production Hardening Design

**Date:** 2026-03-06
**Scope:** Full codebase audit — pipeline, daemon, agent, LLM, DB, config, skills, dashboard, channels, telemetry
**Approach:** Phase 1 (critical bug fixes) → Phase 2 (architectural refactoring)

---

## Problem Statement

The Foreman codebase has structural issues across the end-to-end workflow that prevent production readiness. A deep scan identified 60+ issues spanning data corruption bugs, race conditions, security vulnerabilities, incomplete implementations, and architectural gaps. This design documents all findings and defines a phased remediation plan.

---

## Phase 1: Critical Bug Fixes

These issues cause data corruption, silent failures, or broken core guarantees. Must be fixed before any production use.

### C1. Wrong Task ID in Quality Review Call Cap

**File:** `internal/pipeline/task_runner.go:244`

**Bug:** `CheckTaskCallCap()` receives `r.config.WorkDir` (a filesystem path) instead of `task.ID`.

**Impact:** LLM call cap is never enforced for quality reviews. Tasks can exceed `MaxLlmCallsPerTask` without triggering the cap, blowing through budget silently.

**Fix:** Change to `CheckTaskCallCap(ctx, r.db, task.ID, r.config.MaxLlmCallsPerTask)`.

---

### C2. File Changes Not Reverted Between Retries

**File:** `internal/pipeline/task_runner.go:89-129`

**Bug:** When a task fails and retries, file changes from the previous attempt remain on disk. No `git checkout`/`git clean` between attempts.

**Impact:** Mixed implementations from multiple attempts. The LLM sees corrupted state, tests run against hybrid code.

**Fix:** Add a `resetWorkingTree()` call at the top of the retry loop that runs `git checkout -- .` and `git clean -fd` for task-relevant paths. Must preserve committed changes from prior tasks.

---

### C3. Non-Atomic Patch Application

**File:** `internal/pipeline/task_runner.go:297-303`

**Bug:** If patch 1 succeeds but patch 2 fails in `applyChanges()`, the file is left half-patched on disk.

**Impact:** Corrupted working tree persists into retry attempts.

**Fix:** Two-phase apply — validate all patches can be applied first (in memory), then write all files atomically. On any failure, revert to pre-apply state.

---

### C4. Feedback Accumulator Never Reset on Retry

**File:** `internal/pipeline/task_runner.go:87-101`

**Bug:** `FeedbackAccumulator` is created once before the retry loop but never reset. Feedback from attempt 1 is included in attempt 2, 3, etc.

**Impact:** LLM receives stale, contradictory feedback. Retry effectiveness degrades with each attempt.

**Fix:** Call `feedback.Reset()` (or create a new accumulator) at the start of each retry iteration. Optionally keep a brief summary of prior attempt failures as context.

---

### C5. Quality Review Approval Logic Bug

**File:** `internal/pipeline/task_runner.go:256-259`

**Bug:** Only rejects if `!Approved AND HasCritical`. Non-critical rejections (IMPORTANT/MINOR) are silently approved.

**Impact:** Quality issues below CRITICAL severity never trigger retries. Inconsistent with spec review behavior (which retries on any rejection).

**Fix:** Align with spec review — reject on `!Approved` regardless of severity level. The quality reviewer prompt already categorizes severity; the gate should respect any rejection.

---

### C6. Double Ticket Pickup Race Condition

**File:** `internal/daemon/daemon.go:322-343`

**Bug:** Ticket status isn't updated to `in_progress` before the goroutine starts. If the poll loop fires again before the goroutine executes, the same ticket is picked up twice.

**Impact:** Same ticket processed concurrently — duplicate branches, duplicate PRs, wasted LLM spend.

**Fix:** Update ticket status to `in_progress` synchronously (in the main loop) before launching the goroutine. Move `d.active.Add(1)` before the goroutine as well.

---

### C7. Non-Atomic File Reservation

**File:** `internal/daemon/scheduler.go:37-54`

**Bug:** `TryReserve()` does read-check-write without a transaction. Two tickets can check, find no conflict, and both reserve the same file.

**Impact:** File conflict invariant broken — concurrent edits to same files.

**Fix:** Wrap the check-and-reserve in a single database transaction. For SQLite, use `BEGIN IMMEDIATE`. For PostgreSQL, use `SELECT ... FOR UPDATE` or an advisory lock.

---

### C8. SQLite IncrementTaskLlmCalls Race

**File:** `internal/db/sqlite.go:207-215`

**Bug:** `UPDATE` + separate `SELECT` without transaction. Concurrent calls return inconsistent counts.

**Impact:** Call cap enforcement unreliable under parallel task execution.

**Fix:** Wrap in `BEGIN IMMEDIATE` transaction, or use a CTE: `UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = ? RETURNING total_llm_calls` (SQLite 3.35+). If targeting older SQLite, wrap in transaction.

---

### C9. PostgreSQL vs SQLite GetTicketCost Mismatch

**File:** `sqlite.go:337` vs `postgres.go:331`

**Bug:** SQLite reads `cost_usd` from `tickets` table; PostgreSQL SUMs from `llm_calls` table. Different data sources.

**Impact:** Different cost values depending on database backend. Budget enforcement broken on one or both.

**Fix:** Align both implementations. Use `SUM(cost_usd) FROM llm_calls WHERE ticket_id = ?` as the canonical source in both backends.

---

### C10. Hardcoded API Keys in Tracked Config

**File:** `foreman.toml:38-39`

**Bug:** Real OpenAI API key and personal phone number committed to git.

**Impact:** Credential exposure.

**Fix:** Remove secrets from `foreman.toml`. Use `${ENV_VAR}` placeholders exclusively. Add `foreman.toml` to `.gitignore` (keep only `foreman.example.toml` tracked). Rotate the exposed key.

---

## Phase 2: High Severity (Concurrency / Lifecycle / Security)

### H1. MergeChecker Not in WaitGroup

**File:** `internal/daemon/daemon.go:182-186`

**Issue:** MergeChecker goroutine not tracked in `d.wg`. On shutdown, it keeps running — queries DB after cleanup.

**Fix:** Add `d.wg.Add(1)` before spawning, `defer d.wg.Done()` inside the goroutine.

---

### H2. DAG Executor Goroutine Leak

**File:** `internal/daemon/dag_executor.go:80-96`

**Issue:** No explicit wait for workers after context cancellation. Workers continue running in background.

**Fix:** Add a `sync.WaitGroup` for worker goroutines. After `workerCancel()`, call `wg.Wait()` with a bounded timeout before returning results.

---

### H3. MaxParallelTickets Can Be Exceeded

**File:** `internal/daemon/daemon.go:331-335`

**Issue:** `atomic.Load()` + comparison + `atomic.Add()` is not atomic. Race window.

**Fix:** Use a semaphore (`chan struct{}` with capacity `MaxParallelTickets`) or a mutex-guarded counter. Semaphore is idiomatic Go.

---

### H4. DAG Adapter Task Lookup Passes Empty TicketID

**File:** `internal/pipeline/dag_adapter.go:64-82`

**Issue:** `ListTasks(ctx, "")` passes empty string. Fallback returns task stub with only ID.

**Fix:** Store the ticket ID in the adapter struct. Pass it to `ListTasks()`. Remove the empty-fallback path — if a task isn't found, return an error.

---

### H5. WebSocket CORS Allow-All

**File:** `internal/dashboard/auth.go:14`

**Issue:** `CheckOrigin` always returns `true`.

**Fix:** Validate `Origin` header against a configurable allowlist (default: same-origin). Add `dashboard.allowed_origins` config field.

---

### H6. Bash Command Validation Uses Prefix Match

**File:** `internal/agent/tools/exec.go:81-82`

**Issue:** `strings.HasPrefix()` means allowed `"go"` also allows `"go_malicious"`.

**Fix:** Parse command into binary + args. Compare the binary name exactly against the allowlist. Use `exec.LookPath()` to resolve the binary.

---

### H7. Skills File Write Path Traversal

**File:** `internal/skills/engine.go:142-171`

**Issue:** `filepath.Join(workDir, step.Path)` — `step.Path` can contain `../` to escape sandbox.

**Fix:** After `filepath.Join`, call `filepath.Abs()` and verify the result starts with `workDir`. Reject if it escapes.

---

### H8. Prompt Injection in Channel Classifier

**File:** `internal/channel/classifier.go:56-77`

**Issue:** User message body injected directly into LLM prompt.

**Fix:** Wrap user input in clear delimiters (e.g., `<user_message>...</user_message>`). Add instruction to the system prompt to ignore instructions within delimiters. Consider JSON-encoding the input.

---

### H9. Metrics Endpoint Unauthenticated

**File:** `internal/dashboard/server.go:89-92`

**Fix:** Apply the same auth middleware used for other API endpoints. Add config option `dashboard.metrics_auth = true` (default).

---

### H10. Config Validation Missing for Required Fields

**File:** `internal/config/config.go:169-184`

**Fix:** Extend `Validate()` to check:
- Non-empty API key for the configured LLM provider
- Non-empty git token if git host is configured
- Non-empty tracker credentials if tracker is configured
- Valid model name format
- Port ranges (1-65535) for dashboard
- Positive values for timeouts and budgets

---

## Phase 3: Medium Severity (State / Error Handling / Observability)

| ID | Issue | Fix |
|----|-------|-----|
| M1 | Task status stuck on escalation | Update status to `task_escalated` before returning `EscalationError` |
| M2 | File reservation release errors non-fatal | Make release errors fatal — propagate to caller, log at ERROR level |
| M3 | Clarification timeout infinite loop | Track label removal failure in DB; skip ticket on next iteration |
| M4 | Crash recovery no bounds check on `LastCompletedTaskSeq` | Validate against actual task count; clamp to valid range |
| M5 | No overall DAG timeout | Add `ticket_timeout_minutes` config; wrap DAG execution in `context.WithTimeout` |
| M6 | MergeChecker hook errors discarded | Log at WARN level; record event in DB; don't fail the merge transition |
| M7 | JSON unmarshal errors swallowed (Anthropic) | Log at WARN; return partial error info rather than empty |
| M8 | Context provider errors dropped | Log at WARN with paths that triggered the error |
| M9 | `errgroup.Wait()` discarded | Replace `_ =` with explicit error check + log |
| M10 | Dashboard status filter unvalidated | Validate against `models.ValidStatuses` enum; return 400 on invalid |
| M11 | WhatsApp rate limiter resets on restart | Move rate limit state to database |
| M12 | Hardcoded fallback pricing | Log WARNING for unknown models; use configurable default |
| M13 | No daemon precondition validation | Add `validateDeps()` in `Start()` — panic early on nil orchestrator/DB |
| M14 | Hardcoded prompts in reviewers | Wire template engine (pongo2) to spec/quality/final reviewers |
| M15 | No distributed locking | Add optional distributed lock via DB (`SELECT ... FOR UPDATE SKIP LOCKED`) for multi-instance deployments |

---

## Phase 4: Low Severity (Code Quality / Best Practices)

| ID | Issue | Fix |
|----|-------|-----|
| L1 | Unused `minContextLines` parameter | Remove parameter from signature and call sites |
| L2 | Inconsistent error wrapping in DB | Audit all DB methods; wrap with `fmt.Errorf("method: %w", err)` |
| L3 | Double `workerCancel()` | Remove explicit call; rely on defer |
| L4 | `resultChan` never closed | Close after coordinator loop exits |
| L5 | Transaction rollback pattern | Standardize to `defer tx.Rollback()` + `return tx.Commit()` with comment |
| L6 | `listSourceFiles` swallows Walk error | Return error; caller decides severity |
| L7 | SHA256 token hashing (no salt) | Migrate to bcrypt for stored tokens |
| L8 | WebSocket auth token in query param | Support `Sec-WebSocket-Protocol` header for token transport |
| L9 | No input size limits | Add `max_description_bytes` config; validate in tracker and dashboard |
| L10 | Missing integration tests | Add test suites for dashboard auth, channel routing, skills execution, concurrent ticket processing |

---

## Implementation Strategy

### Phase 1 — Critical Bugs (10 items)
- All can be fixed independently
- Each fix is small and localized (1-2 files)
- Should be done first, sequentially, with tests for each fix
- Estimated: 10 focused PRs or 1 large PR with clear commit boundaries

### Phase 2 — High Severity (10 items)
- Security fixes (H5-H9) can be parallelized
- Concurrency fixes (H1-H3) should be done together
- H4 (DAG adapter) is independent
- H10 (config validation) is independent

### Phase 3 — Medium Severity (15 items)
- Group by subsystem: pipeline (M1, M14), daemon (M2-M6, M13, M15), agent (M8-M9), dashboard (M10), channel (M11), telemetry (M12), LLM (M7)
- Can be worked in parallel by subsystem

### Phase 4 — Low Severity (10 items)
- Cleanup pass — can be batched into a single PR per subsystem
- L10 (integration tests) is ongoing work throughout all phases

---

## Success Criteria

1. All C1-C10 fixes verified by unit tests that reproduce the original bug
2. No race conditions detected by `go test -race ./...`
3. All security issues (H5-H9) verified by targeted security tests
4. End-to-end pipeline test passes: ticket pickup → plan → implement → PR creation
5. Graceful shutdown completes within configured timeout with no goroutine leaks
6. Cost tracking consistent between SQLite and PostgreSQL backends
