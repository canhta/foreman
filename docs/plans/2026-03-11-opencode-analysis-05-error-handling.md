# OpenCode Analysis 05: Error Handling, Recovery & Observability

**Date:** 2026-03-11  
**Domain:** Error types, retry logic, panic recovery, SSE timeouts, tool repair, structured logging, cost observability  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`

---

## How OpenCode Solves This

### Error Type Definitions

`packages/opencode/src/provider/error.ts` — rich `ParsedAPICallError` union type:
- `context_overflow | api_error`
- `parseStreamError()` maps stream-level error codes to typed values (lines 125–161)
- **13 `OVERFLOW_PATTERNS` regex array** covering Anthropic, OpenAI, Google, Groq, xAI and others — all in one place (lines 8–23)
- HTML gateway error detection with human-readable messages (lines 82–90)
- Special-cased OpenAI 404 as retryable (lines 25–29)

### Retry with Exponential Backoff

`packages/opencode/src/session/retry.ts`:
- `RETRY_INITIAL_DELAY=2000ms`, `RETRY_BACKOFF_FACTOR=2`, `RETRY_MAX_DELAY=30s`
- Reads `Retry-After` and `Retry-After-Ms` HTTP headers — waits the exact server-instructed duration (lines 32–50)
- **Abort-signal-aware `sleep()`** — honors cancellation mid-wait (lines 11–26)
- Context overflow → never retry; rate limit / server overload → retry with backoff

### Per-Chunk SSE Timeout

`packages/opencode/src/provider/provider.ts:90–136` — `wrapSSE()` wraps each SSE response body with a **120s per-chunk abort signal** via `AbortController`. If no chunk arrives within 120s, the stream is aborted.

### Tool Call Repair

`packages/opencode/src/session/llm.ts:178–198` — `experimental_repairToolCall`:
- Auto-lowercases mismatched tool names
- Falls back to `"invalid"` tool name sentinel for unknown tools
- Prevents hard failures when LLM uses wrong case

### Logging

`packages/opencode/src/util/log.ts`:
- Custom logger with `.tag(key, value)` for named context fields
- `.clone()` creates child logger inheriting parent tags
- `.time()` with `Symbol.dispose` for **automatic duration tracking** — no manual stop call
- Relative-time `+Nms` delta between log entries for request-level timeline reconstruction
- Log file rotation: keeps last 10 files, deletes older

---

## Issues in Our Current Approach

### G1 — No Per-Call Exponential Backoff (High)

`internal/agent/builtin.go:238–244` — on `RateLimitError` with a fallback model configured, retries **once** with the fallback model. No exponential backoff. The `SharedRateLimiter` provides forward-looking rate regulation but not per-call retry. A single transient 500/overload fails the entire agent turn.

### G2 — Retry-After Header Hardcoded to 60s (High)

`internal/llm/anthropic.go:226–228` — `RateLimitError{RetryAfterSecs: 60}` is hardcoded. The `Retry-After` HTTP header is not read. If Anthropic instructs a shorter or longer wait, Foreman ignores it.

### G3 — No Per-Chunk SSE Timeout (Medium)

The only timeout is a 5-minute context deadline on the full HTTP call. A stalled provider that keeps the connection open but stops sending data holds the goroutine for up to 5 minutes.

### G4 — No Context Overflow Pattern Detection (Medium)

`internal/llm/provider.go` detects token-count overflows but does not parse API error message text. If an API returns a context overflow in HTTP response body text (not a clean status code), Foreman does not recognize it as overflow and cannot trigger compaction.

### G5 — No Panic Recovery in Per-Ticket Goroutines (High)

`internal/daemon/daemon.go:489–500` — per-ticket goroutines have `defer d.wg.Done()` but **no `defer recover()`**. A panic in `d.orchestrator.ProcessTicket()` crashes the entire daemon process, losing all in-flight work.

### G6 — Doom Loop Warns but Does Not Stop (High)

`internal/agent/builtin.go:310–319` — doom loop detection injects a warning and continues looping. A stuck agent burns all `maxTurns` budget rather than stopping. There is no hard stop or escalation.

### G7 — No Automatic Duration Logging (Low)

Latency data is recorded in `LlmCallRecord.DurationMs` but not emitted as a structured log field at call time, making real-time log-based latency analysis harder.

### G8 — No Tool Name Repair for Case Mismatches (Medium)

If the LLM uses `Read_File` instead of `read_file`, the tool call fails silently with a tool error. Foreman has no case-insensitive fallback lookup.

---

## Specific Improvements to Adopt

### I1: Add Per-Call `RetryingProvider` Wrapper
A `RetryingProvider` decorator that wraps any `LlmProvider` and retries transient errors (rate limits, overloads, connection errors) with exponential backoff. Respects `context.Context` cancellation mid-wait.

**Effort:** Low-Medium.

### I2: Read `Retry-After` Header in Anthropic Provider
Parse `Retry-After` and `Retry-After-Ms` HTTP headers from the API response. Use the header value instead of hardcoded 60s.

**Effort:** Very low.

### I3: Per-Chunk SSE Timeout Reader
Wrap the HTTP response body in `anthropic.go` with a `chunkTimeoutReader` that aborts if no data arrives within 120s.

**Effort:** Low.

### I4: Context Overflow Pattern Detection
Add `IsContextOverflow(err error) bool` with provider-agnostic regex patterns. Wire into the agent loop to trigger compaction rather than crashing.

**Effort:** Low.

### I5: Panic Recovery in Per-Ticket Goroutines
Add `defer recover()` with structured error logging to all per-ticket goroutines in `daemon.go`. Optionally mark the ticket as failed in the DB.

**Effort:** Very low.

### I6: Hard Stop Doom Loop
Change `builtin.go` doom loop handling to break the agent loop and return `ErrDoomLoop` after injection. Optionally provide an `OnDoomLoop` callback for interactive escalation.

**Effort:** Low.

### I7: Add `logTimer` Helper
A small helper that captures start time and emits a structured log line with duration when deferred. Use it around LLM calls and compaction.

**Effort:** Very low.

### I8: Case-Insensitive Tool Lookup
In `tools.Registry.Lookup()`, add a case-insensitive fallback after the exact match fails.

**Effort:** Very low.

---

## Concrete Implementation Suggestions

### I1 — RetryingProvider

```go
// internal/llm/retrying_provider.go
type RetryConfig struct {
    MaxAttempts   int           // e.g. 3
    InitialDelay  time.Duration // e.g. 2s
    BackoffFactor float64       // e.g. 2.0
    MaxDelay      time.Duration // e.g. 30s
}

var DefaultRetryConfig = RetryConfig{
    MaxAttempts:   3,
    InitialDelay:  2 * time.Second,
    BackoffFactor: 2.0,
    MaxDelay:      30 * time.Second,
}

type RetryingProvider struct {
    inner  LlmProvider
    config RetryConfig
}

func NewRetryingProvider(inner LlmProvider, config RetryConfig) *RetryingProvider {
    return &RetryingProvider{inner: inner, config: config}
}

func (r *RetryingProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
    delay := r.config.InitialDelay
    for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
        resp, err := r.inner.Complete(ctx, req)
        if err == nil { return resp, nil }

        var rateLimitErr RateLimitError
        var overloadErr  ServerOverloadError
        var connErr      ConnectionError

        switch {
        case errors.As(err, &rateLimitErr):
            wait := time.Duration(rateLimitErr.RetryAfterSecs) * time.Second
            if wait == 0 { wait = delay }
            log.Warn().Dur("wait", wait).Int("attempt", attempt).Msg("rate limited, retrying")
            if !sleepWithCancel(ctx, wait) { return nil, ctx.Err() }
        case errors.As(err, &overloadErr), errors.As(err, &connErr):
            log.Warn().Dur("wait", delay).Int("attempt", attempt).Msg("transient error, retrying")
            if !sleepWithCancel(ctx, delay) { return nil, ctx.Err() }
            delay = minDuration(time.Duration(float64(delay)*r.config.BackoffFactor), r.config.MaxDelay)
        default:
            return nil, err // non-retryable
        }
    }
    return nil, fmt.Errorf("exhausted %d retry attempts", r.config.MaxAttempts)
}

func sleepWithCancel(ctx context.Context, d time.Duration) bool {
    select {
    case <-time.After(d): return true
    case <-ctx.Done():    return false
    }
}
```

### I2 — Read Retry-After Header

```go
// internal/llm/anthropic.go — in the HTTP error handling block:
// Replace:
//   return nil, RateLimitError{RetryAfterSecs: 60}
// With:
retryAfter := 60
if v := resp.Header.Get("Retry-After-Ms"); v != "" {
    if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
        retryAfter = int(ms / 1000)
    }
} else if v := resp.Header.Get("Retry-After"); v != "" {
    if s, err := strconv.Atoi(v); err == nil {
        retryAfter = s
    }
}
return nil, RateLimitError{RetryAfterSecs: retryAfter}
```

### I3 — Per-Chunk SSE Timeout Reader

```go
// internal/llm/chunk_timeout_reader.go
package llm

import (
    "fmt"
    "io"
    "time"
)

const defaultChunkTimeout = 120 * time.Second

type chunkTimeoutReader struct {
    r       io.ReadCloser
    timeout time.Duration
}

func newChunkTimeoutReader(r io.ReadCloser, timeout time.Duration) io.ReadCloser {
    return &chunkTimeoutReader{r: r, timeout: timeout}
}

func (c *chunkTimeoutReader) Read(p []byte) (int, error) {
    type result struct{ n int; err error }
    ch := make(chan result, 1)
    go func() { n, err := c.r.Read(p); ch <- result{n, err} }()
    select {
    case res := <-ch:
        return res.n, res.err
    case <-time.After(c.timeout):
        return 0, fmt.Errorf("SSE chunk timeout after %s (no data received)", c.timeout)
    }
}

func (c *chunkTimeoutReader) Close() error { return c.r.Close() }
```

Apply in `anthropic.go` streaming handler:
```go
body = newChunkTimeoutReader(resp.Body, defaultChunkTimeout)
```

### I4 — Context Overflow Patterns

```go
// internal/llm/overflow.go
package llm

import "regexp"

var overflowPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)context.{0,20}(length|window|limit|size).{0,30}exceed`),
    regexp.MustCompile(`(?i)prompt.{0,20}too.{0,10}long`),
    regexp.MustCompile(`(?i)maximum context length`),
    regexp.MustCompile(`(?i)reduce.{0,30}(message|input|prompt).{0,20}length`),
    regexp.MustCompile(`(?i)token.{0,20}limit.{0,20}exceed`),
    regexp.MustCompile(`(?i)context_length_exceeded`),
    regexp.MustCompile(`(?i)string too long`),
    regexp.MustCompile(`(?i)input.{0,20}too.{0,10}long`),
    regexp.MustCompile(`(?i)exceeds.{0,20}(model|context).{0,20}limit`),
    regexp.MustCompile(`(?i)max_tokens.{0,20}exceed`),
    regexp.MustCompile(`(?i)insufficient_quota`),
    regexp.MustCompile(`(?i)prompt_too_long`),
    regexp.MustCompile(`(?i)invalid_prompt`),
}

func IsContextOverflow(err error) bool {
    if err == nil { return false }
    msg := err.Error()
    for _, p := range overflowPatterns {
        if p.MatchString(msg) { return true }
    }
    return false
}
```

Wire in `builtin.go`:
```go
if llm.IsContextOverflow(err) {
    log.Warn().Err(err).Int("turn", turn).Msg("context overflow detected, triggering compaction")
    if compactErr := r.triggerCompaction(ctx, &messages, provider); compactErr != nil {
        return AgentResult{}, fmt.Errorf("context overflow and compaction failed: %w", compactErr)
    }
    continue
}
```

### I5 — Panic Recovery in Daemon Goroutines

```go
// internal/daemon/daemon.go — inside processQueuedTickets goroutine
go func(t models.Ticket, lk string) {
    defer d.wg.Done()
    defer func() { <-d.tickets }()
    defer func() {
        if r := recover(); r != nil {
            log.Error().
                Interface("panic", r).
                Str("ticket_id", t.ID).
                Bytes("stack", debug.Stack()).
                Msg("panic recovered in ticket pipeline goroutine")
            // Mark ticket as failed to prevent it hanging in "running" state
            if err := d.store.UpdateTicketStatus(context.Background(), t.ID, models.TicketStatusFailed); err != nil {
                log.Error().Err(err).Str("ticket_id", t.ID).Msg("failed to mark panicked ticket as failed")
            }
        }
    }()
    defer func() {
        if err := database.ReleaseLock(ctx, lk); err != nil {
            log.Error().Err(err).Str("lock", lk).Msg("failed to release ticket lock")
        }
    }()
    if err := d.orchestrator.ProcessTicket(ctx, t); err != nil { ... }
}(ticket, lockName)
```

### I6 — Hard Stop Doom Loop

```go
// internal/agent/builtin.go
var ErrDoomLoop = errors.New("doom loop detected: agent repeated the same tool call too many times")

// In the tool execution loop:
if r.doomLoop.Check(normalizedFingerprint) {
    log.Error().
        Str("tool", toolCall.Name).
        Int("turn", turn).
        Msg("doom loop detected, aborting agent run")
    return AgentResult{}, ErrDoomLoop
}
```

### I7 — Log Timer Helper

```go
// internal/telemetry/logtimer.go
package telemetry

import (
    "time"
    "github.com/rs/zerolog"
)

// StartTimer returns a completion function that emits a structured log line with duration.
// Usage: defer StartTimer(logger, "llm_complete")()
func StartTimer(logger zerolog.Logger, op string) func(...func(*zerolog.Event) *zerolog.Event) {
    start := time.Now()
    return func(fields ...func(*zerolog.Event) *zerolog.Event) {
        e := logger.Debug().
            Str("op", op).
            Dur("duration_ms", time.Since(start))
        for _, f := range fields { e = f(e) }
        e.Msg("operation completed")
    }
}

// Usage in anthropic.go:
// done := telemetry.StartTimer(log.Logger, "anthropic_complete")
// resp, err := p.doRequest(ctx, req)
// done(func(e *zerolog.Event) *zerolog.Event {
//     return e.Str("model", req.Model).Int("tokens", resp.Usage.InputTokens)
// })
```

### I8 — Case-Insensitive Tool Lookup

```go
// internal/agent/tools/registry.go
func (r *Registry) Lookup(name string) (Tool, bool) {
    if t, ok := r.tools[name]; ok {
        return t, true
    }
    // Case-insensitive fallback — handles LLM case normalization issues
    lower := strings.ToLower(name)
    for k, t := range r.tools {
        if strings.ToLower(k) == lower {
            log.Warn().Str("requested", name).Str("resolved", k).Msg("tool: case-insensitive lookup fallback")
            return t, true
        }
    }
    return nil, false
}
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Priority |
|---|-------------|--------|----------|
| 1 | Panic recovery in daemon goroutines (I5) | Very low | **Critical** — prevents daemon crash on any ticket |
| 2 | Read `Retry-After` header (I2) | Very low | High — correctness fix |
| 3 | Hard stop doom loop (I6) | Low | High — prevents budget waste |
| 4 | Case-insensitive tool lookup (I8) | Very low | High — prevents silent tool failures |
| 5 | Context overflow pattern detection (I4) | Low | Medium |
| 6 | Per-call `RetryingProvider` (I1) | Medium | Medium |
| 7 | Per-chunk SSE timeout reader (I3) | Low | Medium |
| 8 | Log timer helper (I7) | Very low | Low |
