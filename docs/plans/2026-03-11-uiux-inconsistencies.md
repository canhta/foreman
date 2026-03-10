# UI/UX Inconsistencies

**Review Date:** 2026-03-11  
**Scope:** `internal/dashboard/` (Go API + WebSocket), `internal/dashboard/web/src/` (Svelte frontend), `cmd/` (CLI)  
**Method:** Full static analysis by autonomous agent

---

## Summary

38 issues found across dashboard API design, CLI UX, WebSocket handling, and frontend behavior. 3 are CRITICAL (security vulnerabilities or completely broken commands), 9 are HIGH (data/security issues or severe usability gaps), 15 are MEDIUM, and 11 are LOW.

---

## CRITICAL

### UX-01 — Raw Credentials Returned in API Response

**File:** `internal/dashboard/api.go` (`flattenProjectConfig` / `handleGetProject`)  
**Severity:** CRITICAL

**Description:**  
`GET /api/projects/{pid}` returns the full `projectConfigDTO` including `GitToken` and `TrackerToken` as plaintext strings. The frontend's `types.ts` includes `git_token` and `tracker_token` fields that are read back and displayed in the Settings page.

**Impact:** Any authenticated dashboard session can exfiltrate all stored Git and tracker credentials by loading the settings page or calling the GET endpoint. The bearer token is the only barrier — no per-credential redaction.

**Fix Direction:**
- Return masked tokens (e.g., `ghp_***abc123`) from all GET endpoints using the existing `util.RedactKey` function.
- Accept a sentinel value (e.g., `"__unchanged__"`) from PUT requests meaning "do not update this field."

---

### UX-02 — `foreman stop` Is a No-Op Stub

**File:** `cmd/stop.go:13–15`  
**Severity:** CRITICAL

**Description:**  
`foreman stop` prints `"Stopping Foreman daemon..."` and exits without doing anything. The daemon keeps running.

**Impact:** Operators who rely on `foreman stop` in scripts, systemd, or CI believe they stopped the daemon when they have not. Resource leaks and stale locks accumulate.

**Fix Direction:**
- Implement PID-based signaling: read `~/.foreman/foreman.pid`, send `SIGTERM`, and wait for the process to exit.

---

### UX-03 — `foreman run` Is an Unimplemented Stub

**File:** `cmd/run.go:24`  
**Severity:** CRITICAL

**Description:**  
`foreman run <ticket-id>` is documented in `--help` but does not execute the pipeline at all. It prints a message and returns success.

**Impact:** Misleads users expecting manual one-shot execution. Silently returns success with no action.

**Fix Direction:**
- Wire to the actual pipeline runner, or mark `Hidden: true` and return an error, or remove it until implemented.

---

## HIGH

### UX-04 — `os.Exit(1)` in `doctor` Command Bypasses Go Deferred Cleanup

**File:** `cmd/doctor.go:52, 67, 198`  
**Severity:** HIGH

**Description:**  
`doctor` calls `os.Exit(1)` directly inside `RunE` instead of returning an error. This prevents Cobra from calling deferred cleanup (database connections, temp files), skips any `defer` calls in the function, and prevents test harnesses from capturing the exit cleanly.

**Fix Direction:**
- Return `fmt.Errorf(...)` in all cases and let Cobra handle the exit code via `cmd.Execute()`.

---

### UX-05 — WebSocket Auth Logic Duplicated Three Times

**File:** `internal/dashboard/ws.go` (~lines 40–100, 160–220, 240–300)  
**Severity:** HIGH

**Description:**  
All three WebSocket handlers (`handleGlobalWebSocket`, `handleProjectWebSocket`, `handleChatWebSocket`) contain identical ~20-line blocks for token extraction, SHA-256 hashing, and DB lookup.

**Impact:** Any fix to auth logic must be applied in three places. A missed edit creates a security inconsistency between WS endpoints.

**Fix Direction:**
- Extract a single `authenticateWebSocket(w, r, db) (bool, string)` helper.

---

### UX-06 — Deprecated `?token=` Query Parameter Still Accepted on WebSocket

**File:** `internal/dashboard/ws.go`, `internal/dashboard/web/src/state/global.svelte.ts:106`  
**Severity:** HIGH

**Description:**  
WebSocket auth via `?token=...` URL query parameter is still accepted server-side (with a deprecation log warning). `?token=` in the URL is logged by proxies, browser history, and server access logs — a token exposure risk.

**Fix Direction:**
- Remove the `?token=` path entirely. Require the `Sec-WebSocket-Protocol: bearer.<token>` approach only.

---

### UX-07 — No Pagination on Ticket List Endpoints

**File:** `internal/dashboard/api.go` (lines 258–273, 1113)  
**Severity:** HIGH

**Description:**  
`GET /api/projects/{pid}/tickets` and `GET /api/projects/{pid}/ticket-summaries` return all tickets with no `limit`/`offset`. The frontend polls ticket summaries every 10 seconds. A project with thousands of tickets returns the full set on every poll.

**Impact:** Memory pressure, slow responses, and heavy frontend re-renders.

**Fix Direction:**
- Add `?limit=` and `?offset=` pagination with sensible defaults (e.g., 100). Return a `total` count in the response envelope.

---

### UX-08 — No Rate Limiting on Any API Endpoint

**File:** `internal/dashboard/server.go` (route registration)  
**Severity:** HIGH

**Description:**  
`RateLimit.RequestsPerMinute` config key exists but no middleware actually enforces it on incoming HTTP requests. The dashboard API is trivially brute-forceable for tokens.

**Fix Direction:**
- Implement token bucket or sliding-window rate limiter middleware respecting the configured `requests_per_minute`.

---

### UX-09 — `handleOverview` Makes Unbounded N×4 Database Queries Per Request

**File:** `internal/dashboard/api.go:284–320`  
**Severity:** HIGH

**Description:**  
`GET /api/overview` iterates over every project and makes 4 separate DB queries per project (active tickets, PR tickets, clarification tickets, daily cost). The frontend polls this every 15 seconds. With 10 projects this is 40 synchronous DB calls per overview request.

**Fix Direction:**
- Run per-project queries in parallel with `errgroup`, or maintain a cached summary invalidated on ticket status change events.

---

### UX-10 — Hardcoded Version String `"0.1.0"` in Two CLI Files

**File:** `cmd/start.go:370`, `cmd/dashboard.go:55`  
**Severity:** HIGH

**Description:**  
Both files pass the literal string `"0.1.0"` to `dashboard.NewServer`. The dashboard always displays `0.1.0` regardless of actual build version.

**Fix Direction:**
- Read from the package-level `version` variable set by `SetVersion()`, or pass via `cobra.Command.Root().Version`.

---

### UX-11 — `cmd/ps.go` N+1 Database Query Pattern

**File:** `cmd/ps.go:65`  
**Severity:** HIGH

**Description:**  
For each ticket in the list, `ps` calls `ListTasks(ctx, ticket.ID)` individually in a loop. With 50 tickets this executes 51 sequential SQLite queries.

**Fix Direction:**
- Add a `ListTaskCounts(ctx)` DB method, or reuse the `GetTicketSummaries` path which already includes task counts.

---

### UX-12 — `cmd/cost.go` Silently Discards All Database Errors

**File:** `cmd/cost.go:31, 42, 59, 70`  
**Severity:** HIGH

**Description:**  
All four cost-querying calls use `val, _ := db.Get...()` and proceed with `val = 0` on error. No warning is printed. Cost data appears as all-zeros when the DB has a schema issue, with no indication that data is missing.

**Fix Direction:**
- Check and print errors or at minimum a `[warning: could not read X]` annotation for each call.

---

## MEDIUM

### UX-13 — Inconsistent Error Response Format (Plain Text vs. JSON)

**File:** `internal/dashboard/api.go` (scattered; e.g., lines 1022, 1046, 1069, 1215, 1241)  
**Severity:** MEDIUM

**Description:**  
Some error responses use `http.Error(w, "message", code)` (Content-Type `text/plain`) while others use `writeJSON(...)`. The frontend's `handleResponse` reads `res.statusText` for non-2xx responses, so plain-text error bodies are lost.

**Fix Direction:**
- Standardize all error responses through `writeJSON(w, code, map[string]string{"error": "..."})`. Replace all `http.Error(...)` calls.

---

### UX-14 — `foreman logs --follow` Flag Is Documented but Not Implemented

**File:** `cmd/logs.go:61–65`  
**Severity:** MEDIUM

**Description:**  
The `--follow` / `-f` flag is registered and described but the implementation is explicitly deferred. It exits immediately after printing existing logs. Users who expect `foreman logs -f` to stream live output receive a static snapshot.

**Fix Direction:**
- Implement `tail -f` semantics, or print a notice when `--follow` is passed without implementation.

---

### UX-15 — Duplicate `dashboardPort` Package-Level Variable

**File:** `cmd/start.go:173`, `cmd/dashboard.go:19`  
**Severity:** MEDIUM

**Description:**  
Both files declare `var dashboardPort int` in `package cmd`. This is confusing and will fail to compile if both declarations are ever in scope simultaneously.

**Fix Direction:**
- Move to a single location in `cmd/root.go` or a shared `cmd/flags.go`.

---

### UX-16 — `handleProjectEvents` Hardcodes Limit of 100 with No Query Override

**File:** `internal/dashboard/api.go:968`  
**Severity:** MEDIUM

**Description:**  
`GET /api/projects/{pid}/tickets/{id}/events` fetches exactly 100 events with no `?limit=` or `?offset=` override, unlike `handleProjectGlobalEvents` which has both. Inconsistent API behavior; tickets with heavy event histories are silently truncated.

**Fix Direction:**
- Add `?limit=` and `?offset=` query params matching the pattern in `handleProjectGlobalEvents`.

---

### UX-17 — `handleProjectPostChat` Returns Plain Text for Validation Errors

**File:** `internal/dashboard/api.go:1316, 1321`  
**Severity:** MEDIUM

**Description:**  
Validation failures in the chat POST handler return `http.Error(w, "...", 400)` (text/plain), while the success path returns JSON. The frontend shows `res.statusText` ("Bad Request") rather than the specific validation message.

**Fix Direction:**
- Use `writeJSON` for all responses in this handler.

---

### UX-18 — `handleProjectWebSocket` Dead Code: `emitter` Assigned and Immediately Discarded

**File:** `internal/dashboard/ws.go:267–271`  
**Severity:** MEDIUM

**Description:**  
A per-project `emitter` is resolved and assigned to a local variable, then immediately set to `_ = emitter`. All WS events are filtered by `ProjectID` at the application layer rather than using a dedicated per-project emitter. The dead assignment indicates an incomplete feature.

**Fix Direction:**
- Either wire the per-project emitter correctly or delete the dead assignment and document the intentional fallback.

---

### UX-19 — `channel status` Uses File Existence as Proxy for Connection State

**File:** `cmd/channel.go:89–94`  
**Severity:** MEDIUM

**Description:**  
`channel status` reports WhatsApp as connected if a session database file exists. A disconnected, expired, or corrupt session file shows as "session exists" without any actual connectivity check.

**Fix Direction:**
- Perform an actual connection check, or label output as "session file exists (connectivity unverified)".

---

### UX-20 — `pairing list` Hardcodes `"whatsapp"` Channel Name

**File:** `cmd/pairing.go:34`  
**Severity:** MEDIUM

**Description:**  
The `pairing list` command always prints `"whatsapp"` regardless of which channels are configured. When a second channel provider is added, this will be incorrect.

**Fix Direction:**
- Read `cfg.Channel.Provider` and iterate over configured channels dynamically.

---

### UX-21 — `project delete` CLI Has No Confirmation Prompt

**File:** `cmd/project.go` (delete subcommand)  
**Severity:** MEDIUM

**Description:**  
`foreman project delete <id>` executes immediately with no `--yes`/`-y` confirmation flag or interactive prompt. The operation is described as "cannot be undone."

**Fix Direction:**
- Add a `--yes` / `-y` flag. Without it, print a warning and prompt `Are you sure? [y/N]`.

---

### UX-22 — `cmd/init.go` Has No `--force` Flag to Overwrite Existing Config

**File:** `cmd/init.go:32`  
**Severity:** MEDIUM

**Description:**  
`foreman init` returns an error if the config file already exists, with no way to overwrite or re-scaffold it. Users must manually delete the file to reset to defaults.

**Fix Direction:**
- Add `--force` flag to overwrite, or `--update` to merge new defaults into existing config.

---

### UX-23 — `handleProjectCostsWeek` Makes 7 Sequential DB Queries

**File:** `internal/dashboard/api.go:1171–1180`  
**Severity:** MEDIUM

**Description:**  
The week cost endpoint calls `GetDailyCost` 7 times in a serial loop. Each is a separate SQLite query.

**Fix Direction:**
- Add a `GetCostRange(ctx, startDate, endDate)` DB method, or parallelize with `errgroup`.

---

### UX-24 — Frontend `loadTicketDetail` Silently Ignores Chat Errors

**File:** `internal/dashboard/web/src/state/project.svelte.ts:80`  
**Severity:** MEDIUM

**Description:**  
Chat fetch is wrapped in `.catch(() => [] as ChatMessage[])` — errors are swallowed with no toast or log. A backend chat endpoint failure is invisible to the user; the UI shows an empty chat panel.

**Fix Direction:**
- Log the error at minimum; ideally show a non-blocking toast: "Chat history unavailable."

---

### UX-25 — Frontend WebSocket `onmessage` Swallows All Processing Errors

**File:** `internal/dashboard/web/src/state/project.svelte.ts:202`, `global.svelte.ts:123`  
**Severity:** MEDIUM

**Description:**  
Both WebSocket `onmessage` handlers have empty `catch {}` blocks. A malformed JSON message or runtime error in event processing is silently discarded with no diagnostic signal.

**Fix Direction:**
- At minimum `console.warn('WS message error', e)` in the catch block.

---

### UX-26 — `handleTestConnection` (Jira) Has No URL Validation — SSRF Risk

**File:** `internal/dashboard/api.go:1532–1553`  
**Severity:** MEDIUM

**Description:**  
The `test-connection` endpoint for Jira accepts any string as `url` and makes an HTTP request to it without validating that it's a valid HTTPS URL. An authenticated user could probe internal services (`http://169.254.169.254/`, `http://localhost:6379/`, etc.).

**Fix Direction:**
- Validate that the URL uses `https://` and is not a private/loopback IP range before making the outbound request.

---

### UX-27 — `ProjectSettings` Sends Raw Tokens Back on Every Save — Compounds CRIT-01

**File:** `internal/dashboard/web/src/pages/ProjectSettings.svelte:38`  
**Severity:** MEDIUM

**Description:**  
Settings page fetches `GET /api/projects/{pid}` and binds the full response (including raw tokens) to the form. When saved, raw tokens are sent back in the PUT body. This creates a plaintext token round-trip on every settings save, even when the user did not change the token.

**Fix Direction:**
- Implement server-side token masking (see UX-01) and treat masked sentinel values as "no change" on the frontend.

---

## LOW

### UX-28 — `GlobalState.createProject` Bypasses Shared `api.ts` Fetch Helpers

**File:** `internal/dashboard/web/src/state/global.svelte.ts:47–59`  
**Severity:** LOW

**Description:**  
`createProject`, `deleteProject`, and several places in `ProjectSettings.svelte` manually construct `fetch` calls instead of using `postJSONBody` from `api.ts`. If auth logic changes, these raw calls won't pick up the change.

**Fix Direction:**
- Refactor to use `postJSONBody` / `deleteJSON` from `api.ts`.

---

### UX-29 — `api.ts` `postJSON` Does Not Set `Content-Type: application/json`

**File:** `internal/dashboard/web/src/api.ts:51–54`  
**Severity:** LOW

**Description:**  
`postJSON` sends a POST with no body and no `Content-Type` header. Some proxy/WAF configurations may reject bodyless POSTs without the content type header.

**Fix Direction:**
- Add `Content-Type: application/json` to the `postJSON` helper for consistency.

---

### UX-30 — `handleStatus` Always Returns `"status": "running"` — Redundant with `daemon_state`

**File:** `internal/dashboard/api.go:511`  
**Severity:** LOW

**Description:**  
`GET /api/status` always sets `"status": "running"` unconditionally, while separately returning `"daemon_state"`. The top-level `status` field is meaningless.

**Fix Direction:**
- Remove the redundant `status` field or have it mirror `daemon_state`.

---

### UX-31 — `handleProjectSync` Does Not Actually Trigger a Sync

**File:** `internal/dashboard/api.go:1019–1041`  
**Severity:** LOW

**Description:**  
`POST /api/projects/{pid}/sync` returns `{"status":"sync triggered"}` but no signal is sent to the worker. The actual sync happens on the next scheduled poll.

**Fix Direction:**
- Implement an actual notification channel (e.g., a `chan struct{}` in the worker) that `handleProjectSync` sends to.

---

### UX-32 — `telemetry.EventEmitter.newID()` Generates Non-Standard UUID

**File:** `internal/telemetry/events.go:50–54`  
**Severity:** LOW

**Description:**  
The ID format resembles UUID v4 but is not RFC 4122-compliant (version/variant bits are not set). IDs may collide with actual UUIDs from external systems, and tools that validate UUID format will reject these IDs.

**Fix Direction:**
- Use `github.com/google/uuid` or the standard library's `crypto/rand` with proper UUID v4 bit-twiddling.

---

### UX-33 — `setup-ssh` Instructions Are GitHub-Only

**File:** `cmd/setup_ssh.go:41–45`  
**Severity:** LOW

**Description:**  
Instructions hardcode `https://github.com/settings/ssh/new` despite Foreman supporting GitLab and Gitea.

**Fix Direction:**
- Make instructions conditional on `cfg.git.provider`, or keep them generic ("Add the following public key as a deploy key in your Git provider").

---

### UX-34 — No `--config` / `-c` Flag on Root Command

**File:** `cmd/config.go:116`  
**Severity:** LOW

**Description:**  
Config lookup path is hardcoded to `"foreman.toml"`. Users cannot point `foreman` at a non-default config path, making multi-project or CI usage harder.

**Fix Direction:**
- Add a persistent `--config` flag on `rootCmd` and pass it through to `config.LoadFromFile`.

---

### UX-35 — `ProjectWizard` Review Step Doesn't Indicate Whether Tokens Were Provided

**File:** `internal/dashboard/web/src/pages/ProjectWizard.svelte:358–411`  
**Severity:** LOW

**Description:**  
The review screen (step 5) omits git/tracker token fields entirely. A user who forgot to enter a token has no feedback before hitting "Create."

**Fix Direction:**
- Add rows like `"Git Token: ✓ provided"` / `"✗ not provided"` using a boolean check, without showing the actual value.

---

### UX-36 — `handleProjectGlobalEvents` Max Limit Capped Silently

**File:** `internal/dashboard/api.go:1193`  
**Severity:** LOW

**Description:**  
If `?limit=` exceeds 100, the value is silently ignored and 50 (the default) is used. Callers requesting `?limit=200` receive only 50 results with no indication.

**Fix Direction:**
- Return an error for out-of-range `limit`, or include `"applied_limit"` in the response body.

---

### UX-37 — WebSocket Reconnect Has No Backoff or Max-Retry Limit

**File:** `internal/dashboard/web/src/state/project.svelte.ts:206`, `global.svelte.ts:111`  
**Severity:** LOW

**Description:**  
On WebSocket close, both state modules reconnect after a fixed 5-second delay with no exponential backoff and no limit. If the server is down, the client hammers it every 5 seconds indefinitely.

**Fix Direction:**
- Implement exponential backoff (1s, 2s, 4s, 8s… up to 60s) with an optional max retry count before showing a "disconnected" banner.

---

### UX-38 — Claude Code Usage Panel Uses Hardcoded Sonnet Pricing

**File:** `internal/dashboard/claudecode.go:71–75`  
**Severity:** LOW

**Description:**  
Claude Code usage estimates use hardcoded per-token pricing for Claude Sonnet, separate from the `pricing.toml` embedded table used by `CostController`.

**Fix Direction:**
- Look up Claude Sonnet pricing from `CostController`'s pricing table rather than hardcoding.
