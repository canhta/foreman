# Architectural Issues

**Review Date:** 2026-03-11  
**Scope:** `cmd/`, `internal/agent/`, `internal/llm/`, `internal/tracker/`, `internal/git/`, `internal/db/`, `internal/config/`, `internal/runner/`, `internal/skills/`, `internal/prompts/`, `internal/context/`, `main.go`, `go.mod`  
**Method:** Full static analysis by autonomous agent

---

## Summary

27 issues found across initialization/DI, concurrency, interface design, configuration, security, schema, dead code, and build/module concerns. 8 are HIGH, 11 are MEDIUM, and 8 are LOW.

---

## HIGH

### ARCH-01 — Two-Phase Init: `SubagentTool` `runFn` Injection Window

**File:** `internal/agent/tools/registry.go:198`, `internal/agent/builtin.go:70–74`  
**Severity:** HIGH

**Description:**  
`Registry.SetRunFn(fn)` must be called after both `Registry` and `BuiltinRunner` are constructed. There is no compile-time or runtime guard. If `BuiltinRunner.Run()` is called before `SetRunFn` completes (fast integration test, startup order change), `SubagentTool` fires with a nil `runFn` — either panicking or silently no-oping.

**Fix Direction:**
- Merge `Registry` construction and `runFn` injection into a single constructor: `NewRegistry(runFn RunFn) *Registry`.
- Break the circular dependency by extracting a `SubagentExecutor` interface that `BuiltinRunner` satisfies.

---

### ARCH-02 — `skills.Engine` Two-Phase Setter Injection

**File:** `internal/skills/engine.go:65–80`  
**Severity:** HIGH

**Description:**  
`Engine.SetAgentRunner()` and `Engine.SetGitProvider()` are post-construction setters. Steps of type `agentsdk` and `git_diff` will silently fail at runtime if these are not called. In `cmd/start.go`, these setters are not consistently called for all code paths. Failures only surface when a hook fires — potentially hours after startup.

**Fix Direction:**
- Make `AgentRunner` and `GitProvider` required constructor parameters with nil checks in `NewEngine`.

---

### ARCH-03 — Top-Level `daemon.Daemon` Is Never Fully Wired

**File:** `cmd/start.go:272–284, 438`  
**Severity:** HIGH

**Description:**  
A top-level `d := daemon.NewDaemon(...)` is created and `d.SetDB(database)` is called, but `d.SetTracker()` and `d.SetOrchestrator()` are **never called** on this outer daemon. Per-project daemons (created in `setupProjectWorker`) are fully wired. At line 438, `d.Start(ctx)` is called on the outer daemon — which runs the poll loop with no tracker, no orchestrator, and no scheduler. This is either a CPU-consuming no-op or a runtime panic.

**Impact:** In single-project mode (the common case), the outer daemon's poll loop dereferences nil fields, panicking the process.

**Fix Direction:**
- Audit whether the outer daemon is intentional. If single-project mode should use `setupProjectWorker`, do so unconditionally and remove the outer unwired daemon. If intentional, add an explicit no-op guard when tracker/orchestrator are nil.

---

### ARCH-04 — `--dangerously-skip-permissions` Hardcoded in ClaudeCode CLI Args

**File:** `internal/agent/claudecode.go:74`  
**Severity:** HIGH

**Description:**  
`"--dangerously-skip-permissions"` is hardcoded into the ClaudeCode runner arguments — there is no configuration option to disable it. In a production deployment where the worktree contains credentials, `.env` files, or SSH keys, a Claude Code agent running with this flag can read/exfiltrate any file without restriction. Combined with prompt injection in a malicious ticket, this is a full sandbox escape.

**Fix Direction:**
- Make this flag opt-in via `ClaudeCodeConfig.SkipPermissions bool`. Default to permission-checked mode. Document the security trade-off explicitly.

---

### ARCH-05 — `CopilotRunner` Uses `copilot.PermissionHandler.ApproveAll`

**File:** `internal/agent/copilot.go:111`  
**Severity:** HIGH

**Description:**  
All Copilot permission requests are automatically approved (`OnPermissionRequest: copilot.PermissionHandler.ApproveAll`). Like ARCH-04, this means the agent can take any file or system action without restriction. The `Evaluate` function in `internal/agent/permission.go` already exists but is not wired to the Copilot permission handler.

**Fix Direction:**
- Implement a permission handler that evaluates requests against the `Ruleset` defined in `internal/agent/permission.go`.

---

### ARCH-06 — `OnRateLimit` Spawns Untracked, Uncancellable Goroutine

**File:** `internal/llm/ratelimiter.go:54–57`  
**Severity:** HIGH

**Description:**  
```go
go func() {
    time.Sleep(time.Duration(retryAfterSecs) * time.Second)
    limiter.SetLimit(...)
}()
```
This goroutine has no context, no cancellation, and no tracking. With a `retryAfterSecs` of 3600 (common in API rate-limit responses), this goroutine can outlive the process shutdown by up to an hour. In tests that create rate limiters, leaked goroutines accumulate.

**Fix Direction:**
- Use `time.AfterFunc` or pass a context to `OnRateLimit` and use `select { case <-time.After(...): case <-ctx.Done(): }`.

---

### ARCH-07 — `db.Database` Is a God Interface (50+ Methods)

**File:** `internal/db/db.go:104–232`  
**Severity:** HIGH

**Description:**  
The `Database` interface contains 50+ methods covering tickets, tasks, LLM calls, auth tokens, pairings, embeddings, file reservations, distributed locks, cost tracking, DAG state, progress patterns, context feedback, chat messages, and dashboard queries. Every component that needs any database access must accept (and in tests, mock) the entire 50-method interface.

**Impact:**
- Unit testing is effectively impossible without a full SQLite instance or an enormous manual mock.
- Every new database method breaks all mock implementations.
- The interface communicates nothing about which subset of functionality a component requires.

**Fix Direction:**
- Decompose into role-specific sub-interfaces (Interface Segregation Principle):
  ```go
  type TicketStore interface { ... }
  type LlmCallRecorder interface { ... }
  type FileReservationStore interface { ... }
  ```
  `SQLiteDB` continues to implement all of them. The large `db.Database` remains as a composed convenience type for wiring.

---

### ARCH-08 — No Database Migration Strategy

**File:** `internal/db/sqlite.go:42`, `internal/db/schema.go`  
**Severity:** HIGH

**Description:**  
The schema is applied via `CREATE TABLE IF NOT EXISTS` statements. There is no migration versioning, no schema version table, no `ALTER TABLE` path. Several columns show evidence of post-hoc additions (`pr_head_sha TEXT NOT NULL DEFAULT ''`, etc.) that were only safe because `DEFAULT ''` was added to avoid `NOT NULL` failures on existing rows. Any future schema change will silently leave existing users on a stale schema.

**Fix Direction:**
- Adopt a migration library (e.g., `golang-migrate/migrate` or `pressly/goose`).
- Add a `schema_version` table. Existing schema becomes migration `0001`.

---

## MEDIUM

### ARCH-09 — `ClaudeCodeRunner.WithRegistry` Is Optional but Silently Skipped

**File:** `internal/agent/claudecode.go:47–50`  
**Severity:** MEDIUM

**Description:**  
If `WithRegistry` is omitted, `.claude/` is never written, meaning the Claude Code runner operates without any agent definitions, skills, or commands. In `cmd/start.go`, `WithRegistry` is called only via a type assertion — if the factory produces a different wrapper type, the registry is silently skipped with no warning.

**Fix Direction:**
- Accept `*prompts.Registry` as a constructor parameter, or emit a `log.Warn` at `Run()` time when the registry was never set.

---

### ARCH-10 — `CopilotRunner` Starts with `context.Background()` at Construction

**File:** `internal/agent/copilot.go:40`  
**Severity:** MEDIUM

**Description:**  
`client.Start(context.Background())` is called in `NewCopilotRunner`. The Copilot subprocess ignores any cancellation signal from the caller. On `SIGTERM`, the subprocess is only terminated if `Close()` is manually called — a responsibility that is easily missed.

**Fix Direction:**
- Pass `ctx` into `NewCopilotRunner` and forward it to `client.Start`. Register `Close()` in the daemon's shutdown sequence.

---

### ARCH-11 — Dashboard `srv.Shutdown` Has No Force-Kill Safety Net

**File:** `cmd/start.go:384–387`  
**Severity:** MEDIUM

**Description:**  
On signal, a 10-second graceful shutdown window is given. There is no `os.Exit(1)` safety net after the timeout. If the dashboard hangs, the process never exits.

**Fix Direction:**
- Add a `time.AfterFunc(15*time.Second, func() { os.Exit(1) })` safety net.

---

### ARCH-12 — `forwardEvents` Goroutines Never Explicitly Stopped

**File:** `cmd/start.go:785–808`  
**Severity:** MEDIUM

**Description:**  
`forwardEvents` correctly exits when `ctx.Done()` fires. However, `src.Unsubscribe(ch)` is deferred — if `EventEmitter.Unsubscribe` blocks (waiting for a mutex), the goroutine may not exit promptly during shutdown. No `WaitGroup` tracks these goroutines for drain verification.

**Fix Direction:**
- Verify that `Unsubscribe` is non-blocking or bounded. Add a `WaitGroup` to track all forward goroutines and wait during drain.

---

### ARCH-13 — `expandEnvVars` Not Called in `LoadDefaults`

**File:** `internal/config/config.go:24–41` vs `12–22`  
**Severity:** MEDIUM

**Description:**  
`LoadFromFile` calls `expandEnvVars` to substitute `${VAR}` patterns. `LoadDefaults` does **not**. In tests or any code path that calls `LoadDefaults()` directly, `${ANTHROPIC_API_KEY}` and similar patterns are returned as literal strings to LLM provider constructors, causing authentication failures with no clear error message.

**Fix Direction:**
- Apply `expandEnvVars` in `LoadDefaults` as well, or use a shared `applyEnvExpansion(cfg *models.Config)` function called by both paths.

---

### ARCH-14 — Only Two Settings Are Viper-Bindable via Environment

**File:** `internal/config/config.go:178–181`  
**Severity:** MEDIUM

**Description:**  
`bindEnvOverrides` only binds `FOREMAN_DASHBOARD_PORT` and `FOREMAN_DASHBOARD_HOST`. All other settings can only be overridden via the custom `${VAR}` syntax in TOML. Makes containerized/12-factor deployments awkward.

**Fix Direction:**
- Use `viper.AutomaticEnv()` with `viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))` and `viper.SetEnvPrefix("FOREMAN")`.

---

### ARCH-15 — `expandEnv` Silently Ignores `$VAR` Syntax (Without Braces)

**File:** `internal/config/config.go:212–218`  
**Severity:** MEDIUM

**Description:**  
The custom `expandEnv` function only handles `${VAR}` syntax. If a user writes `api_key = "$ANTHROPIC_API_KEY"`, the literal string `"$ANTHROPIC_API_KEY"` is passed to the LLM provider, causing a `401 Unauthorized` error with no indication that the env var was not expanded.

**Fix Direction:**
- Use `os.Expand` (which handles both `$VAR` and `${VAR}`) or add a validation pass that warns when a config value starts with `$` but was not expanded.

---

### ARCH-16 — `config/persist.go` Writes TOML Without Atomic Replace

**File:** `internal/config/persist.go:61–68`  
**Severity:** MEDIUM

**Description:**  
`writeTree` calls `os.WriteFile(path, out, 0o644)` directly on the config file. If the process is killed mid-write, the config file is partially written and corrupted.

**Fix Direction:**
- Write to a temp file in the same directory, then `os.Rename` to the target path (atomic on POSIX).

---

### ARCH-17 — GitHub Token Fallback Leaks Tracker Token Scope into Git Operations

**File:** `cmd/start.go:529–535`  
**Severity:** MEDIUM

**Description:**  
In `buildPRCreator` and `buildPRChecker`:
```go
if token == "" {
    token = cfg.Tracker.GitHub.Token // reuse tracker token if git token not set
}
```
The tracker token (used for issue tracker API calls) is silently reused for PR creation/checking — two distinct OAuth scopes. If the tracker token has a narrow scope, this causes confusing permission errors. If it has a broad scope, it over-privileges git operations.

**Fix Direction:**
- Do not cross-use tracker and git tokens. Emit a `log.Warn` when falling back. Require explicit configuration for PR creation token.

---

### ARCH-18 — `RecordingProvider.storeDetails` Uses `time.Now().UnixNano()` for Call IDs

**File:** `internal/llm/recording_provider.go:106`  
**Severity:** MEDIUM

**Description:**  
```go
callID := fmt.Sprintf("llm-%d", time.Now().UnixNano())
```
Under concurrent LLM calls (the builtin runner uses `errgroup` for parallel tool execution), two goroutines can call `storeDetails` within the same nanosecond, generating colliding primary keys. SQLite rejects the second insert with a primary key conflict, silently losing the record.

**Fix Direction:**
- Use `github.com/google/uuid` or a global atomic counter.

---

### ARCH-19 — `holderID` Is a Process-Global Package Variable

**File:** `internal/db/lock.go:10–13`  
**Severity:** MEDIUM

**Description:**  
```go
var holderID = func() string {
    host, _ := os.Hostname()
    return fmt.Sprintf("%s:%d", host, os.Getpid())
}()
```
Initialized once at package load. In tests, all `SQLiteDB` instances share the same lock holder identity (the test binary's PID). Tests cannot simulate two competing processes acquiring locks.

**Fix Direction:**
- Inject `holderID` as a constructor parameter of `SQLiteDB`. Default to the current behavior when not specified.

---

### ARCH-20 — `cmd/run.go` Is a No-Op Stub Shipped in the Binary

**File:** `cmd/run.go:22–25`  
**Severity:** MEDIUM

**Description:**  
The `run` command is registered in the binary, appears in `--help`, but does nothing. Users who try `foreman run` see a success message with no action taken.

**Fix Direction:**
- Implement the command, mark it `Hidden: true`, or remove it entirely until implemented.

---

### ARCH-21 — `go.mod` Declares `go 1.25.0` Which Does Not Exist

**File:** `go.mod:3`  
**Severity:** MEDIUM

**Description:**  
Go 1.25 does not exist as of 2026-03-11 (current stable is 1.24.x). This causes issues with CI systems that install exactly the declared version, and with `go mod tidy` on older toolchains.

**Fix Direction:**
- Change to `go 1.24` or the actual minimum tested Go version.

---

## LOW

### ARCH-22 — `permission.Evaluate` Default-Deny Semantics Undocumented

**File:** `internal/agent/permission.go:33–46`  
**Severity:** LOW

**Description:**  
`Evaluate` returns `ActionDeny` when no rule matches (default-deny). The last-rule-wins evaluation (vs. first-match-wins) is also non-obvious. Developers adding rules may create rule sets that accidentally grant broader permissions than intended.

**Fix Direction:**
- Document the evaluation strategy (`last-wins, default-deny`) explicitly on `Evaluate` and `Merge`.

---

### ARCH-23 — `prompts.SkillStep` Leaked into `skills` Package as Type Dependency

**File:** `internal/prompts/registry.go:314–353`  
**Severity:** LOW

**Description:**  
`SkillStep` is defined in the `prompts` package but is only used as an intermediate type consumed by the `skills` package. This creates an unnecessary `skills → prompts` type dependency for a data-transfer type.

**Fix Direction:**
- Move `SkillStep` to a shared `models` package, or have `prompts.Registry.SkillSteps()` return `[]skills.Step` directly.

---

### ARCH-24 — `TaskManager` Has No Callers — Dead Code

**File:** `internal/agent/task_manager.go`  
**Severity:** LOW

**Description:**  
`TaskManager` is a well-implemented in-memory task registry with full CRUD and status transitions. However, it is not referenced from any other file in the codebase. Dead code adds maintenance burden and confuses readers about intended architecture.

**Fix Direction:**
- Wire `TaskManager` into dashboard agent task tracking (if that is the intent), or delete it.

---

### ARCH-25 — WhatsApp (`go.mau.fi/whatsmeow`) Is Statically Linked Unconditionally

**File:** `cmd/start.go:20–21`, `go.mod`  
**Severity:** LOW

**Description:**  
`go.mau.fi/whatsmeow` is a heavy CGo-heavy dependency linked into the binary even when `cfg.Channel.Provider != "whatsapp"`. Most deployments will never use WhatsApp. Increases binary size, CGo toolchain requirements, and attack surface from `whatsmeow` CVEs.

**Fix Direction:**
- Use Go build tags (`//go:build whatsapp`) to conditionally compile the WhatsApp channel, or use a plugin architecture.

---

### ARCH-26 — `NewSQLiteDB` Schema Init Uses `context.Background()`

**File:** `internal/db/sqlite.go:42`  
**Severity:** LOW

**Description:**  
Schema creation is run with `context.Background()`, ignoring any cancellation signal from the caller. In a test that times out or a startup that is interrupted, the schema init will continue running.

**Fix Direction:**
- Accept a `ctx context.Context` parameter in `NewSQLiteDB` and pass it to schema execution.

---

### ARCH-27 — `loadForemanContext` Duplicated Across `agent` and `skills`

**File:** `internal/agent/builtin.go:574–585`, `internal/skills/engine.go:390–402`  
**Severity:** LOW

**Description:**  
Nearly identical logic for loading `AGENTS.md` / `.foreman/context.md` as project context exists in two separate packages. Any change to context loading must be applied in both places.

**Fix Direction:**
- Extract to a shared helper in `internal/context` or `internal/project`, imported from both callers.
