# Bugs and Edge Cases

**Review Date:** 2026-03-11  
**Scope:** `internal/db/`, `internal/models/`, `internal/context/`, `internal/envloader/`, `internal/snapshot/`, `internal/git/`, `internal/tracker/`, `internal/telemetry/`, `tests/`  
**Method:** Full static analysis by autonomous agent

---

## Summary

32 issues found across the data layer, tracker integrations, git operations, context assembly, and testing gaps. 4 are CRITICAL (data corruption, complete feature breakage), 8 are HIGH (correctness bugs causing incorrect behavior), and 20 are MEDIUM/LOW.

---

## CRITICAL

### BUG-01 — TOCTOU Race in `AcquireLock`

**File:** `internal/db/sqlite.go:1337–1357`  
**Severity:** CRITICAL

**Description:**  
The lock acquisition logic issues two separate SQL statements: first a `DELETE` of expired locks, then an `INSERT OR IGNORE`. These are not wrapped in a `BEGIN IMMEDIATE` transaction. Two concurrent goroutines or processes can both execute the `DELETE` and then both proceed to the `INSERT OR IGNORE`. One will silently succeed; the other silently fails because `OR IGNORE` swallows the conflict. The losing caller incorrectly believes it did not acquire the lock and skips processing, but the true holder can't be reliably determined.

Under high concurrency, two processes can both believe they own the lock and run pipeline stages for the same ticket simultaneously.

**Impact:** Duplicate task execution, corrupted worktrees, double-PR creation.

**Fix Direction:**
- Wrap the `DELETE` + `INSERT` in a single `BEGIN IMMEDIATE` transaction. Alternatively, use a single `INSERT OR REPLACE` that atomically claims unexpired-or-expired slots.

---

### BUG-02 — Migration Destroys `llm_call_details` Table on Existing Deployments

**File:** `internal/db/sqlite.go` (`runSQLiteMigrations`)  
**Severity:** CRITICAL

**Description:**  
The migration logic checks whether `llm_call_details` contains a `REFERENCES` clause in its `CREATE TABLE` statement. If it does (existing deployment with the old FK schema), it drops and recreates the table. This destroys **all historical LLM call observability data** on first upgrade. There is no backup, no `ALTER TABLE`, and no data migration step.

**Impact:** Complete loss of all `llm_call_details` rows on first run of any new binary. Breaks audit trails and cost reconstruction. Happens silently with no warning or confirmation.

**Fix Direction:**
- Use `ALTER TABLE ... ADD COLUMN` for additive changes. For structural changes (removing FK), create a new table, copy data, then rename — never `DROP TABLE`.

---

### BUG-03 — `LinearTracker.AddLabel` / `UpdateStatus` Are Silent No-Ops

**File:** `internal/tracker/linear.go:176–178, 193–200`  
**Severity:** CRITICAL

**Description:**  
Both `UpdateStatus` and `AddLabel` silently return `nil` without performing any API call. The decompose/approval lifecycle and the entire status state machine depend on these methods. A Linear-backed deployment will never transition ticket statuses or add approval labels.

**Impact:** Linear-backed deployments are non-functional for any workflow requiring status updates or label operations. Decompose approval labels are never applied. Tickets are re-processed endlessly because status never updates.

**Fix Direction:**
- Implement the Linear GraphQL mutations for `IssueUpdate` (state) and label association. At minimum, return an explicit error rather than silently succeeding.

---

### BUG-04 — `JiraTracker.UpdateStatus` Is a Silent No-Op

**File:** `internal/tracker/jira.go:264–267`  
**Severity:** CRITICAL

**Description:**  
`UpdateStatus` returns `nil` unconditionally. The comment says "transition IDs required — simplified for now." In practice, every Jira ticket processed by Foreman never has its status updated in Jira. The daemon will re-pick the same ticket on the next poll loop.

**Impact:** Jira-backed deployments show no status changes in Jira. Tickets remain in their initial state forever. The daemon repeatedly re-processes the same tickets.

**Fix Direction:**
- Implement the Jira `POST /rest/api/3/issue/{key}/transitions` call. Fetch transition IDs at startup or lazily cache them per project.

---

## HIGH

### BUG-05 — `ReserveFiles` Silently Ignores Conflicts

**File:** `internal/db/sqlite.go:649–668`  
**Severity:** HIGH

**Description:**  
`ReserveFiles` uses `INSERT OR IGNORE`, which silently ignores any row that already exists. There is no check of `RowsAffected` and no error returned when a file is already reserved by another ticket. Callers believe all files were reserved when some (or all) may have been silently skipped.

**Impact:** Two parallel tasks can both believe they have exclusive access to the same file, leading to conflicting edits, lost writes, and broken commits.

**Fix Direction:**
- Remove `ReserveFiles` and force all callers to use `TryReserveFiles`, or change `ReserveFiles` to check `RowsAffected` and return an error on conflict.

---

### BUG-06 — `DeleteTicket` Does Not Cascade-Delete Child Records

**File:** `internal/db/sqlite.go` (~line 1166)  
**Severity:** HIGH

**Description:**  
`DeleteTicket` deletes the ticket and tasks, but orphans rows in `llm_call_details`, `context_feedback`, `dag_states`, `embeddings`, and `chat_messages`. None of these tables have `ON DELETE CASCADE` foreign keys. These orphaned rows accumulate indefinitely.

**Impact:** Unbounded database growth. Incorrect cost aggregation if ticket IDs are reused. Degraded query performance over time.

**Fix Direction:**
- Add `ON DELETE CASCADE` to all foreign keys referencing `tickets.id`, or explicitly delete child rows inside `DeleteTicket` within a transaction.

---

### BUG-07 — `QueryContextFeedback` Has No Scope Filter

**File:** `internal/db/sqlite.go` (~line 1463)  
**Severity:** HIGH

**Description:**  
The method loads the last 500 rows from `context_feedback` without filtering by repository, project, or ticket. In a multi-repo deployment, every ticket gets feedback contaminated by data from entirely different codebases, causing the file-selector to score files from unrelated projects highly.

**Impact:** In multi-repo setups, file selection degrades significantly — wrong files are selected, token budgets wasted on irrelevant context, implementation quality drops.

**Fix Direction:**
- Add a `repo_id` or `work_dir` column to `context_feedback` and filter by it.

---

### BUG-08 — `WriteContextFeedback` Stores File Paths as Comma-Separated String

**File:** `internal/db/sqlite.go` (~line 1447)  
**Severity:** HIGH

**Description:**  
File paths are joined with commas into a single string column. File paths containing commas (legal on all platforms except Windows) will be split incorrectly on read-back, producing garbled paths and corrupted file-selector scoring.

**Fix Direction:**
- Store file paths as a JSON array in the column, or use a normalized join table (`context_feedback_files`).

---

### BUG-09 — `GetRecentLlmCalls` Timestamp Silently Zeroed on Unknown Formats

**File:** `internal/db/sqlite.go` (~line 984)  
**Severity:** HIGH

**Description:**  
Timestamp parsing uses two hardcoded format strings. If SQLite returns a timestamp with fractional seconds or UTC offset notation, both formats fail and `CreatedAt` is silently set to Go's zero time (`0001-01-01`). No error is logged or returned.

**Impact:** Dashboard shows incorrect timestamps. Cost-per-period queries misattribute calls. Duration and ordering calculations produce wrong results.

**Fix Direction:**
- Add a fractional-second format variant. Log a warning on complete parse failure rather than silently zero-ing.

---

### BUG-10 — SSH Key Path Not Quoted in Shell Command String

**File:** `internal/git/native.go:286–291`  
**Severity:** HIGH

**Description:**  
The SSH key path is interpolated directly into a shell command string: `"ssh -i " + g.sshKeyPath + " ..."`. This string is set as `GIT_SSH_COMMAND`. If `sshKeyPath` contains spaces (e.g., `/home/user/My Keys/id_rsa`) or shell metacharacters, the resulting command is malformed and git operations fail.

**Fix Direction:**
- Quote the path: `fmt.Sprintf("ssh -i %q -o ...", g.sshKeyPath)`. Validate that the path is absolute on construction.

---

### BUG-11 — `Snapshot` Uses `context.Background()` Ignoring Caller Context

**File:** `internal/snapshot/snapshot.go:136, 151`  
**Severity:** HIGH

**Description:**  
Both `ensureInit` and `git()` construct their `exec.CommandContext` with a hardcoded `context.Background()`. Snapshot operations cannot be cancelled, will not respect task timeouts, and will hang indefinitely if git hangs (e.g., network stall on a misconfigured git-dir path).

**Impact:** A stuck snapshot operation blocks the entire pipeline task goroutine indefinitely. With all worker slots occupied, the daemon deadlocks.

**Fix Direction:**
- Thread the caller's context through to all `exec.CommandContext` calls.

---

### BUG-12 — `observationLog.ReadFrom` Cursor Position Wrong After Scanner Buffering

**File:** `internal/context/observations.go:94–101`  
**Severity:** HIGH

**Description:**  
After `bufio.Scanner` has consumed all content, `f.Seek(0, io.SeekCurrent)` returns the pre-read buffer position, not the logical position after all scanner bytes. The scanner's internal buffering reads ahead, so `f.Seek` reports a position inconsistent with the last successfully scanned line. Lines are re-read or skipped on subsequent calls.

**Impact:** Observation events are duplicated or silently dropped in the context assembly pipeline on long-running tasks.

**Fix Direction:**
- Track the cursor manually by summing `len(line) + 1` for each successfully scanned line, starting from the initial seek position.

---

## MEDIUM

### BUG-13 — `GetChildTickets` Silently Returns Zero for NULL `pr_number`

**File:** `internal/db/sqlite.go` (~line 294)  
**Severity:** MEDIUM

**Description:**  
The query scans `prNumber sql.NullInt64` but unconditionally assigns `t.PRNumber = int(prNumber.Int64)`. When `pr_number` IS NULL, `prNumber.Int64 = 0`. Callers cannot distinguish "no PR assigned" from `PRNumber = 0`.

**Impact:** `MergeChecker` may attempt to check the status of PR #0, causing spurious GitHub API 404 errors or incorrect "merged" status transitions.

**Fix Direction:**
- Check `prNumber.Valid` before assigning.

---

### BUG-14 — Missing Partial Index on `file_reservations`

**File:** `internal/db/schema.go`  
**Severity:** MEDIUM

**Description:**  
Hot-path queries in `TryReserveFiles` and `GetReservedFiles` filter on `released_at IS NULL`. There is no partial index for this condition. As the table grows with historical (released) reservations, every query performs a full table scan.

**Fix Direction:**
- Add `CREATE INDEX idx_file_reservations_active ON file_reservations(file_path) WHERE released_at IS NULL;`

---

### BUG-15 — Token Count Integer Overflow Risk on 32-bit Platforms

**File:** `internal/models/ticket.go`, `internal/db/sqlite.go`  
**Severity:** MEDIUM

**Description:**  
`Ticket.TokensInput` and `Ticket.TokensOutput` are `int` (32-bit on 32-bit platforms, ~2 billion tokens max). Accumulated per-ticket token sums can overflow for large or long-running tickets. The DB stores them as unbounded `INTEGER`. On 32-bit hosts, the Go struct silently wraps at ~2B tokens, producing negative token counts and incorrect cost calculations.

**Fix Direction:**
- Change model fields to `int64`. This is backward-compatible since SQLite `INTEGER` is already 64-bit.

---

### BUG-16 — `DurationMs` Type Mismatch Between Model and DB Struct

**File:** `internal/models/ticket.go` vs `internal/db/sqlite.go`  
**Severity:** MEDIUM

**Description:**  
`LlmCallRecord.DurationMs` is declared as `int64` in the models package but the local `RecentLlmCall` struct in `db.go` uses `int`. On 32-bit platforms this truncates durations over ~24 days of milliseconds.

**Fix Direction:**
- Align both to `int64`.

---

### BUG-17 — Non-Deterministic File Selection Order in `file_scanner.go`

**File:** `internal/context/file_scanner.go`  
**Severity:** MEDIUM

**Description:**  
Tier-1 (highest-priority) files are collected by iterating over a `map[string]bool`. Go map iteration order is explicitly randomized per run. The set of files included in any given LLM prompt changes between runs for the same task, making prompt content non-reproducible.

**Impact:** Debugging is harder; prompt hashes differ across runs for identical inputs; test flakiness.

**Fix Direction:**
- Collect map keys into a slice and `sort.Strings()` before iterating.

---

### BUG-18 — Context Budget and Actual Token Use Calculated With Different Estimators

**File:** `internal/context/file_selector.go` (~line 146)  
**Severity:** MEDIUM

**Description:**  
The token budget check uses `estimateFileTokens(c.SizeBytes)` (a bytes/4 heuristic), but the actual token count when the file is included uses `EstimateTokens(string(content))` (actual tiktoken). For highly non-ASCII content (Japanese comments, base64 strings), the byte-based estimator can be off by 3–10x.

**Impact:** LLM calls fail with context-too-large errors, or the context window is used inefficiently.

**Fix Direction:**
- Use the tiktoken estimator at both the budget check and inclusion stages, or apply a conservative multiplier at the budget stage.

---

### BUG-19 — Lock Holder ID Vulnerable to PID Reuse After Crash

**File:** `internal/db/lock.go`  
**Severity:** MEDIUM

**Description:**  
The lock holder ID is `hostname + PID`. After a crash and restart on the same host, the OS may reuse the same PID. The new process appears to already hold locks that the crashed process held, bypassing lock acquisition and allowing two logical processes to both believe they are the lock holder.

**Fix Direction:**
- Include a random component (e.g., a UUID generated at process start) in the holder ID.

---

### BUG-20 — `envloader` Map Iteration Order Makes "Last File Wins" Non-Deterministic

**File:** `internal/envloader/envloader.go`  
**Severity:** MEDIUM

**Description:**  
The function iterates over `map[string]string` (`EnvFiles` config map) to determine which `.env` file values override others. Go map iteration order is randomized per run, so when multiple env files define the same key, which value "wins" is non-deterministic.

**Impact:** Env var values are inconsistent between runs when multiple env files are configured. Leads to intermittent build/test failures that are extremely hard to reproduce.

**Fix Direction:**
- Accept env files as a `[]struct{ dest, src string }` (ordered slice) in the config, or document and enforce explicit priority order.

---

### BUG-21 — GitHub Issues Tracker Fetches Max 100 Issues With No Pagination

**File:** `internal/tracker/github_issues.go` (~line 57)  
**Severity:** MEDIUM

**Description:**  
Comment explicitly states "pagination not implemented." For active projects with more than 100 open issues labeled `foreman-ready`, the daemon silently misses tickets beyond the first page.

**Fix Direction:**
- Implement GitHub API pagination via the `Link` header.

---

### BUG-22 — Linear GraphQL Mutation Built With `fmt.Sprintf` (Invalid Escaping)

**File:** `internal/tracker/linear.go:67–69`  
**Severity:** MEDIUM

**Description:**  
The GraphQL mutation is built with `fmt.Sprintf` using `%q` formatting for title and description. `%q` adds Go-style quotes, not GraphQL-style escaping. Characters like null bytes, multi-line content with raw `\n`, and certain Unicode sequences may not be escaped in a way the Linear GraphQL parser accepts.

**Impact:** Ticket creation silently fails or creates malformed tickets for descriptions containing raw newlines, tabs, or control characters.

**Fix Direction:**
- Properly escape strings for GraphQL or use variable-based requests where title/description are passed as JSON-encoded variables.

---

### BUG-23 — `LocalFileTracker.updateField` Is Not Atomic

**File:** `internal/tracker/local_file.go:190–202`  
**Severity:** MEDIUM

**Description:**  
`updateField` does a read-modify-write cycle with no file locking between the read and write. Two concurrent goroutines (possible when the daemon processes two tickets with the same local ticket ID) can both read the same file, modify it independently, and the last writer wins — silently discarding the first writer's change.

**Fix Direction:**
- Use advisory file locking (`flock`), or serialize local file tracker operations through a mutex.

---

### BUG-24 — `gogit FileTree` Includes Untracked and Gitignored Files

**File:** `internal/git/gogit.go:119–138`  
**Severity:** MEDIUM

**Description:**  
The go-git `FileTree` implementation uses `filepath.Walk`, which lists all files on disk including untracked and gitignored files. The native git implementation uses `git ls-files -z`, which only lists tracked files. The two backends produce different file sets.

**Impact:** When using the go-git fallback, LLM context includes build artifacts, `node_modules`, `.env` files, and other untracked content — potential secret leakage and token waste.

**Fix Direction:**
- Use go-git's worktree status or index to only list tracked files, or explicitly skip `.gitignore`-matched paths.

---

### BUG-25 — `GitHubPRCreator` HTTP Client Has No Timeout

**File:** `internal/git/github_pr.go:39`  
**Severity:** MEDIUM

**Description:**  
`GitHubPRCreator` is created with `client: &http.Client{}` — no timeout. `GitHubPRChecker` correctly uses `Timeout: 30 * time.Second`, but the creator does not. A hung GitHub API response blocks the PR creation goroutine indefinitely.

**Fix Direction:**
- Add `Timeout: 30 * time.Second` to the `http.Client` in `NewGitHubPRCreator`.

---

## LOW

### BUG-26 — `envloader` Quote Handling Does Not Support Escape Sequences

**File:** `internal/envloader/envloader.go`  
**Severity:** LOW

**Description:**  
Quote stripping only removes matching outer `"` or `'` characters. Values like `KEY="has \"quotes\""` will produce `has \"quotes\"` rather than `has "quotes"`.

**Fix Direction:**
- Implement proper `.env` quote and escape-sequence parsing, or adopt an existing well-tested `.env` parser library.

---

### BUG-27 — Context Assembler Does Not Skip Binary Files

**File:** `internal/context/assembler.go` (~line 214)  
**Severity:** LOW

**Description:**  
File contents are read and appended to the LLM prompt without checking whether the file is binary. Binary files (compiled artifacts, images, `.wasm`, lock files with binary sections) produce garbled, token-heavy prompt content and occasionally trigger safety filters.

**Fix Direction:**
- Check for null bytes in the first N bytes of the file and skip if detected, or check file extension against an allowlist.

---

### BUG-28 — `snapshot.Patch` Swallows `git diff` Errors

**File:** `internal/snapshot/snapshot.go:55–57`  
**Severity:** LOW

**Description:**  
When `s.git("diff", ...)` returns an error, `Patch` returns `&Patch{Hash: hash}` with an empty file list and no error. Callers cannot distinguish between "no files changed" and "diff failed." A corrupt snapshot state is silently treated as "no changes."

**Fix Direction:**
- Return the error from `git diff` rather than swallowing it.

---

### BUG-29 — `forbidden.go` Full-Path Match for Wildcard Patterns Is Dead Code

**File:** `internal/git/forbidden.go:24–47`  
**Severity:** LOW

**Description:**  
`filepath.Match("*.key", f)` is tested against the full path. `filepath.Match` treats `/` as a literal separator — `*.key` will never match `certs/server.key` via full-path check. The code correctly falls back to `filepath.Base`, but the first check is dead code and confuses the intent.

**Fix Direction:**
- Document that wildcard-only patterns are matched against `filepath.Base` and remove the dead full-path match for non-`/`-containing patterns.

---

### BUG-30 — `WalkContextFiles` May Escape Repo Root if Paths Are Not Normalized

**File:** `internal/context/walk_context_files.go:18–42`  
**Severity:** LOW

**Description:**  
The stop condition is `dir == workDir` (string equality). If `workDir` has a trailing slash or `..` components, the loop may never hit the stop condition and walks all the way to the filesystem root, potentially collecting context files from parent directories (e.g., a user's home `AGENTS.md`).

**Fix Direction:**
- Normalize both `startDir` and `workDir` with `filepath.Clean` before starting the walk. Add a hard-limit on iterations.

---

### BUG-31 — `telemetry/events.go` `Emit` Assigns `Seq` After Persisting to Store

**File:** `internal/telemetry/events.go:83–87`  
**Severity:** LOW

**Description:**  
`evt.Seq` is assigned via `atomic.AddInt64` **after** the event is persisted to the store with `RecordEvent`. The sequence number stored in the DB is therefore always 0. WebSocket clients use `Seq` for gap detection, but DB-recovered events after restart have no useful sequence number.

**Fix Direction:**
- Assign `Seq` before calling `RecordEvent`, or persist `Seq` as a separate DB column.

---

### BUG-32 — No Rate Limiting or Retry on 429/503 in Any Tracker HTTP Client

**Files:** `internal/tracker/jira.go`, `github_issues.go`, `linear.go`  
**Severity:** LOW

**Description:**  
All three tracker implementations make HTTP calls with no rate limiting, no retry on 429/503, and no exponential backoff. `RateLimitConfig` exists in the config model but is not wired to tracker HTTP calls.

**Impact:** Under high polling frequency or many concurrent tickets, the daemon hits API rate limits, receives 429 responses, and surfaces them as hard errors rather than backing off gracefully.

**Fix Direction:**
- Wrap HTTP calls in a rate-limited wrapper respecting `RateLimitConfig`, with exponential backoff and jitter on 429/503 responses.

---

## Testing Gaps

### BUG-33 — DB Contract Test Suite Missing Key Edge Cases

**File:** `tests/integration/db_contract_test.go`  
**Severity:** MEDIUM

**Description:**  
The DB contract test suite does not cover:
- `TryReserveFiles` under concurrent access (the atomicity guarantee from BUG-01 and BUG-05)
- `AcquireLock` / `ReleaseLock` full lifecycle
- Orphan cleanup after `DeleteTicket` (BUG-06)
- `QueryContextFeedback` cross-ticket contamination (BUG-07)
- Timestamp round-trip fidelity (the fractional-second parsing bug BUG-09 would be caught here)

**Impact:** The most serious data-layer bugs (BUG-01, BUG-05, BUG-07, BUG-09) have no automated regression guard. They can be silently reintroduced.

**Fix Direction:**
- Add subtests for each scenario to `runDBContractSuite`.
