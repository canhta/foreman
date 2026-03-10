# System Flow Problems

**Review Date:** 2026-03-11  
**Scope:** `internal/pipeline/`, `internal/daemon/`  
**Method:** Full static analysis by autonomous agent

---

## Summary

19 issues found across the pipeline state machine and daemon orchestration. 2 are CRITICAL (correctness defects that cause tasks to fail or data to be lost silently), 6 are HIGH (reliability gaps causing stuck tickets or silent data corruption), and 11 are MEDIUM/LOW.

---

## CRITICAL

### SF-01 — Call Cap Increments BEFORE Every LLM Call, Including Reviewers

**File:** `internal/pipeline/call_cap.go:27–34`  
**Also:** `internal/pipeline/task_runner.go:255, 690, 722`  
**Severity:** CRITICAL

**Description:**  
`CheckTaskCallCap` increments the DB counter and then checks whether it exceeds the maximum. It is called three times per retry loop iteration: once before the implementer, once before the spec reviewer, and once before the quality reviewer — all against the same `MaxLlmCallsPerTask` budget.

With the default of 8 calls per task and 3 retries (4 total attempts):
- Attempt 1: implementer (1) + spec review (2) + quality review (3)
- Attempt 2: implementer (4) + spec review (5) + quality review (6)
- Attempt 3: implementer (7) → **cap hit on 3rd attempt**, task fails immediately

The task fails even though 1 full retry attempt remains unused. Furthermore, the spec reviewer `CheckTaskCallCap` at line 690 is called after `UpdateTaskStatus(SpecReview)` — if the cap is hit there, the task ends up stuck with status `TaskStatusSpecReview` until the outer loop exits and sets it to `Failed`.

**Impact:** Tasks are killed prematurely; reviewer calls consume the implementation budget; confusing `SpecReview` terminal status in the dashboard.

**Fix Direction:**
- Track implementation and reviewer calls in separate counters, or
- Use `MaxSpecReviewCycles` / `MaxQualityReviewCycles` (which already exist on config but are not wired) as separate reviewer budgets, or
- Increase default `MaxLlmCallsPerTask` to `(MaxImplementationRetries + 1) * 3`.

---

### SF-02 — Spec/Quality Review Rejections Burn Implementation Retry Slots

**File:** `internal/pipeline/task_runner.go:362–379`  
**Severity:** CRITICAL

**Description:**  
When spec reviewer or quality reviewer reject output, the code issues a `continue` that increments the outer `attempt` counter. Review rejections are semantically identical to failed implementation attempts — they burn one retry slot.

With `MaxImplementationRetries=3`:
- Attempt 1: implement → spec reject → attempt++
- Attempt 2: implement → spec reject → attempt++
- Attempt 3: implement → spec reject → attempt++
- Attempt 4 (last): implement → spec reject → **loop ends, task FAILED**

The task fails even though implementation never had a chance to reach the quality reviewer.

**Impact:** Tasks can fail on technically correct implementations because the reviewer retry budget is not budgeted separately. No user-visible distinction between "failed all implementation retries" and "hit spec review limit."

**Fix Direction:**
- Add inner retry loops for spec review (bounded by `MaxSpecReviewCycles`) and quality review (bounded by `MaxQualityReviewCycles`), separate from the outer implementation retry loop.

---

## HIGH

### SF-03 — Agent Path Reviews Are Non-Blocking (No Retry on Rejection)

**File:** `internal/pipeline/task_runner.go:629–649`  
**Severity:** HIGH

**Description:**  
In `runTaskWithAgent`, spec and quality review results are entirely advisory. Even if `runSpecReview` returns `&reviewRejectedError{}`, the code logs a warning and proceeds to commit. There is no `continue` to trigger a retry.

Code produced by Claude Code or Copilot runners bypasses review enforcement entirely. `MaxSpecReviewCycles` and `MaxQualityReviewCycles` have no effect on the agent path.

**Impact:** Agent-produced code is never blocked by review. Acceptance criteria may be silently violated. Reviews are called (costs incurred) but results are ignored. The two code paths (builtin vs. agent) behave fundamentally differently.

**Fix Direction:**
- On rejection in the agent path, inject the feedback and issue a `continue` bounded by `MaxImplementationRetries`, mirroring the builtin path.

---

### SF-04 — Worktree Merge Loop Has No Rollback on Mid-Sequence Failure

**File:** `internal/daemon/orchestrator.go:593–610`  
**Severity:** HIGH

**Description:**  
After all DAG tasks complete, task branches are merged one-by-one into the ticket branch via `MergeNoFF`. When a merge fails, the code logs the error and continues. The worktree and branch are then deleted regardless, **destroying evidence needed for manual recovery**. The ticket continues to PR creation with a partially-merged, potentially broken branch.

**Impact:** Silent data loss. The PR is created from a partially-merged branch that may not compile. The merge error is logged but doesn't surface as a ticket failure. The deleted worktree branch means the failed merge cannot be retried.

**Fix Direction:**
- On `MergeNoFF` failure: set `returnErr`, break the loop, and do NOT delete the worktree branch.
- Attempt `git merge --abort` / rollback of partial merges before failing the ticket.

---

### SF-05 — No Rollback on Partial Child Ticket Creation in Decompose

**File:** `internal/pipeline/decompose.go:215–229`  
**Severity:** HIGH

**Description:**  
Child tickets are created one-by-one in a loop. If `CreateTicket` fails mid-loop (e.g., network timeout), the function returns with partial `childIDs` containing already-created orphaned tickets. No cleanup is attempted. The parent ticket's state is inconsistent — not labeled as decomposed — so it may be re-decomposed on the next daemon cycle, creating another set of children.

**Impact:** Orphaned child tickets appear in the tracker as ready tickets and are picked up by Foreman. The parent is re-decomposed on the next cycle, creating duplicate children.

**Fix Direction:**
- On failure, attempt to delete already-created children via `d.tracker.DeleteTicket(...)`, or use a two-phase create-then-commit approach.

---

### SF-06 — `TicketStatusPartial` Tickets Are Never Polled for Merge

**File:** `internal/daemon/merge_checker.go:83–86`  
**Severity:** HIGH

**Description:**  
`checkAll` only queries for `TicketStatusAwaitingMerge`. When some tasks fail and `EnablePartialPR` is true, the ticket is set to `TicketStatusPartial` and a partial PR is opened. `MergeChecker` will never poll it. If the partial PR is merged manually, the ticket status will never update, post-merge hooks never fire, and parent ticket completion never triggers.

**Impact:** Partial-PR tickets are permanently stuck. Post-merge hooks never fire. Decomposed parents with one `partial` child and other `merged` children never auto-close.

**Fix Direction:**
- Include `TicketStatusPartial` in the `StatusIn` filter in `checkAll`.

---

### SF-07 — `pr_updated` Is a Dead-End Terminal Status

**File:** `internal/daemon/merge_checker.go:183–194`  
**Severity:** HIGH

**Description:**  
When `handleOpen` detects an external push (SHA changed), it transitions to `TicketStatusPRUpdated`. From that point, `checkAll` never sees the ticket again (not in the `awaiting_merge` filter). No daemon poll path re-queues a `pr_updated` ticket. It is permanently abandoned.

**Impact:** Any ticket whose PR branch is pushed to externally (CI bot, co-author, GitHub's auto-update) transitions to an unrecoverable state. For teams using squash-merge workflows, this could affect every ticket.

**Fix Direction:**
- Include `TicketStatusPRUpdated` in `checkAll` to continue monitoring, or
- On detecting `pr_updated`, update the stored SHA and continue monitoring without status change.

---

### SF-08 — DAG Worker Goroutines Can Block Forever After Cancellation

**File:** `internal/daemon/dag_executor.go:98–188`  
**Severity:** HIGH

**Description:**  
When `ctx` is cancelled, the coordinator exits its loop and calls `close(readyChan)`. Workers mid-task finish and send to `resultChan`. If `resultChan`'s buffer (sized `len(tasks)`) is full (because many tasks completed simultaneously during shutdown), those sends block indefinitely. `workerCancel()` is deferred on `Execute` return, but `Execute` can't return if workers are blocked on `resultChan <- result`.

**Impact:** Goroutine leak under context cancellation with high concurrency. On a busy daemon with many parallel tickets, leaked goroutines accumulate over time.

**Fix Direction:**
- Add a drain goroutine that reads `resultChan` until closed after the coordinator exits, or use a `select` with `ctx.Done()` on the send side.

---

## MEDIUM

### SF-09 — Lock Released With Possibly-Cancelled Context on Shutdown

**File:** `internal/daemon/daemon.go:489–500`  
**Severity:** MEDIUM

**Description:**  
The deferred `ReleaseLock` in the ticket-processing goroutine uses the outer `ctx` (which is the daemon's shutdown context). On graceful shutdown, when `ctx` is cancelled, `ProcessTicket` returns and the deferred `ReleaseLock` is called with a cancelled context. The DB call fails, and the lock is not released.

**Impact:** On graceful shutdown, all in-flight ticket locks are leaked for up to 1 hour (TTL), during which those tickets cannot be picked up by any Foreman instance including the restarted one.

**Fix Direction:**
```go
defer func() {
    releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    database.ReleaseLock(releaseCtx, lk)
}()
```

---

### SF-10 — Double-Write of `planning` Status Creates Racy Window

**File:** `internal/daemon/daemon.go:479` and `internal/daemon/orchestrator.go:338`  
**Severity:** MEDIUM

**Description:**  
The daemon writes `planning` status before launching the goroutine, then `ProcessTicket` writes it again as its first action. In multi-instance deployments or under DB contention, if the second write fails, the deferred error handler marks the ticket `failed` — even though no work was done.

**Fix Direction:**
- Remove the redundant `UpdateTicketStatus` at `orchestrator.go:338`.

---

### SF-11 — `shouldPickUp` Is Dead Code — Clarification Resolution Never Triggers

**File:** `internal/daemon/pickup.go:20–37`  
**Severity:** MEDIUM

**Description:**  
`shouldPickUp` implements correct logic to re-queue tickets when a user resolves a clarification request (by removing the clarification label). However, `ingestFromTracker` only checks `if existing != nil { continue }` — it never calls `shouldPickUp`. A user who removes the clarification label will see Foreman do nothing. The ticket stays in `ClarificationNeeded` until the clarification timeout fires.

**Fix Direction:**
- Replace the `if existing != nil { continue }` check with a call to `shouldPickUp`.

---

### SF-12 — Rebase Failure Is Terminal — LLM Resolver Is Never Called

**File:** `internal/daemon/orchestrator.go:673–683`  
**Severity:** MEDIUM

**Description:**  
When a rebase conflict occurs, the ticket is immediately marked `failed`. No attempt is made to call `AttemptConflictResolution` from `rebase_resolver.go`, which exists specifically for this purpose. Furthermore, `git rebase --abort` is never called before returning, leaving the working directory in a mid-rebase state.

**Impact:**
1. The LLM-powered conflict resolver is dead code in the orchestration path.
2. The working directory is left in an inconsistent mid-rebase state, breaking the next operation on the same worktree.

**Fix Direction:**
- On `!rebaseResult.Success`, call `AttemptConflictResolution` before giving up.
- Always call `git rebase --abort` before returning a rebase failure.

---

### SF-13 — `checkParentCompletion` Ignores `TicketStatusPartial` Children

**File:** `internal/daemon/merge_checker.go:231–235`  
**Severity:** MEDIUM

**Description:**  
Parent auto-close requires ALL children to be `TicketStatusMerged`. If any child is `partial`, `failed`, or `pr_closed`, the parent stays `Decomposed` forever with no timeout or alternative completion path.

**Fix Direction:**
- Define a terminal set (`Merged`, `Done`, `PRClosed`, `Failed`) and close the parent when ALL children reach any terminal state.

---

### SF-14 — `resetWorkingTree` Does Not Clean New Files Created During an Attempt

**File:** `internal/pipeline/task_runner.go:248–251` and `755–775`  
**Severity:** MEDIUM

**Description:**  
`resetWorkingTree` runs `git checkout -- <file>` only for files in `task.FilesToModify`. New files created by the implementer (not in `FilesToModify`) are left on disk. On retry, the implementer sees stale untracked files, potentially producing incorrect context or apply failures.

**Fix Direction:**
- After resetting tracked files, explicitly `os.Remove` new files from the previous attempt, or run `git clean -fdx` on affected directories.

---

### SF-15 — Smart Retry Re-Queues Ticket With No Backoff

**File:** `internal/daemon/orchestrator.go:383–395`  
**Severity:** MEDIUM

**Description:**  
When a file conflict is detected, the ticket is set back to `Queued` and returned immediately. On the next poll cycle, it is re-processed with no delay. Conflicting tickets trigger tight polling loops and wasted DB queries.

**Fix Direction:**
- Add a `RetryAfter` timestamp to the ticket model and only pick it up after a configurable delay (e.g., 5 minutes).

---

### SF-16 — Crash Recovery May Re-Execute Already-Done Tasks

**File:** `internal/daemon/recovery.go:95–114`  
**Severity:** MEDIUM

**Description:**  
`TasksForDAGRecovery` uses DAG state to skip completed tasks. If DAG state is missing (state corruption, pre-DAG-state-persistence data), tasks with DB status `Done` remain in the `tasksToRun` list and are re-executed, resulting in duplicate commits.

**Fix Direction:**
- In `TasksForDAGRecovery`, also check the DB task status and skip any task with `TaskStatusDone` regardless of DAG state.

---

## LOW

### SF-17 — JSON Unmarshal on Raw LLM Content Fails on Markdown-Wrapped JSON

**File:** `internal/pipeline/decompose.go:265–268`  
**Severity:** LOW

**Description:**  
LLMs commonly wrap JSON in markdown code blocks (` ```json ... ``` `). `json.Unmarshal` fails with a syntax error on this format. The decomposer does not strip markdown fences before parsing.

**Fix Direction:**
- Strip markdown code fences from `resp.Content` before unmarshaling, using a regex or an existing `extractJSONBlock` helper.

---

### SF-18 — Snapshot `preImplHash` Error Is Silently Swallowed

**File:** `internal/pipeline/task_runner.go:204–206`  
**Severity:** LOW

**Description:**  
```go
preImplHash, _ = r.snap.Track()  // error discarded
```
If `Track()` fails, `preImplHash` is empty. The restore guard at line 409 skips restoration when `preImplHash == ""`, leaving uncommitted debris from failed attempts.

**Fix Direction:**
- Log the `Track()` error as a warning so operators are aware that snapshot-based rollback will be unavailable.

---

### SF-19 — `codebasePatterns` Is Empty in the Smart-Retry Path

**File:** `internal/daemon/orchestrator.go:369–396` vs `518`  
**Severity:** LOW

**Description:**  
In the normal path, `codebasePatterns` is set from `planResult.CodebasePatterns`. In the smart-retry path (no re-planning), it remains its zero value `""` and is passed to `TaskRunnerFactoryInput.CodebasePatterns`, giving the task runner no language/framework context.

**Fix Direction:**
- On smart retry, load `codebasePatterns` from the DB ticket model, or store it during initial planning for later retrieval.
