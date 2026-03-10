# OpenCode Analysis 02: LLM Interaction Patterns

**Date:** 2026-03-11  
**Domain:** Provider abstraction, streaming, message formatting, retry logic, multi-provider support  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`

---

## How OpenCode Solves This

### Provider Abstraction via Vercel AI SDK

OpenCode delegates all LLM communication to the **Vercel AI SDK v5** (`ai` package), which provides a `LanguageModelV2` interface. Every provider — Anthropic, OpenAI, Google, Bedrock, OpenRouter, Mistral, Groq, and 15+ others — is addressed through this single interface.

`packages/opencode/src/provider/provider.ts`:
- Lines 138–161: `BUNDLED_PROVIDERS` map — lazy-loaded SDK provider packages
- Lines 170–664: `CUSTOM_LOADERS` map — per-provider init logic (credentials, custom fetch interceptors, model ID prefixing for Bedrock regions)
- Lines 90–136: `wrapSSE()` — wraps any provider's streaming response with a **120s per-chunk timeout** via `AbortController`

Model metadata is fetched from `models.dev` at startup, giving capability flags (supports tools, reasoning, vision, cost, context length) per model — no hardcoded capabilities.

### Message Normalization Pipeline

`packages/opencode/src/provider/transform.ts` runs every message array through a normalization pipeline before the API call:

- **Prompt caching**: Injects `cacheControl: {type: "ephemeral"}` on last 2 system parts and last 2 non-system messages for Anthropic/Bedrock/OpenRouter
- **Tool call ID sanitization**: Claude requires IDs matching `[a-zA-Z0-9_-]`; Mistral requires 9-char alphanumeric. Normalizer rewrites IDs and maintains a remap table.
- **Mistral bridging**: Injects `assistant: "Done."` messages between tool sequences where Mistral requires an assistant turn
- **Empty message filtering**: Strips empty Anthropic assistant messages that cause API errors
- **Unsupported modality handling**: Converts image/file parts to error text when model doesn't support them
- **Gemini schema sanitization**: Rewrites integer enums to string enums, normalizes array `items` fields

### Streaming (Always)

OpenCode **only ever streams** — `streamText()` from the Vercel AI SDK, never batch completions. The full stream is consumed as an async iterator over `fullStream`. On abort, any in-flight tool calls are converted to `output-error` parts — preventing dangling `tool_use` blocks in Anthropic message history (which cause API errors on the next turn).

### Retry with Exponential Backoff

`packages/opencode/src/session/retry.ts`:
- Initial delay: 2000ms, backoff factor: 2×, max delay: 30s
- Reads `Retry-After` and `Retry-After-Ms` HTTP headers — waits the exact server-instructed duration
- Abort-signal-aware `sleep()` — honors cancellation mid-wait
- Context overflow → never retry; rate limit / server overload → retry with backoff

---

## Issues in Our Current Approach

### G1 — No Streaming (High Impact)

Every LLM call in Foreman is a blocking HTTP call via `io.ReadAll()` in `internal/llm/anthropic.go`. The 5-minute context timeout is a blunt instrument. Users/callers get no incremental output; errors only surface after the full wait. A stalled provider that keeps the connection open but stops sending data holds the goroutine for up to 5 minutes.

### G2 — OpenAI Provider Discards Multi-Turn History (Critical Bug)

`internal/llm/openai.go` constructs messages as only `[system, user]` — it ignores `req.Messages` entirely. Any agent using OpenAI, OpenRouter, or Ollama (all of which delegate to the OpenAI provider) loses conversation history, tool call results, and accumulated context on every single turn. This is a correctness bug.

### G3 — No Tool Call Support in OpenAI Provider (High)

The `openaiMessage` struct has no `tool_calls` or `tool_call_id` fields. Agentic tasks on OpenAI/OpenRouter/Ollama models cannot use structured tool calling — they fall back to prompt-only interaction.

### G4 — Prompt Caching Only on System Prompt (High — cost)

`internal/llm/anthropic.go` — `CacheSystemPrompt bool` wraps only the system prompt. For long conversations where the conversation history is repeated on every turn, not caching messages wastes significant cost (potential 40-60% savings left on the table).

### G5 — Rate Limiter Not Auto-Wired (Medium)

`internal/llm/ratelimiter.go` provides a working token bucket but must be called manually. It is easy to forget to wire it for a new provider or call site. OpenCode's retry+rate limiting is integrated directly into the processor loop.

### G6 — Single Fallback Model, Not a Fallback Chain (Medium)

`internal/agent/builtin.go:240–244` supports only one fallback model on rate limit. If the fallback is also rate-limited, the agent fails. No retry with exponential backoff — just one-shot attempt at the fallback.

### G7 — No Per-Chunk Timeout for Long Generations (Medium)

The only timeout is a 5-minute context deadline on the full HTTP call. There is no mechanism equivalent to OpenCode's `wrapSSE()` 120s per-chunk deadline.

### G8 — No Context Overflow Pattern Detection (Medium)

`internal/llm/provider.go` detects token-count overflows but does not parse API error message text. If a non-Anthropic provider returns a context overflow in the HTTP response body, it surfaces as an untyped error and crashes the agent loop rather than triggering compaction.

### G9 — No Schema Sanitization for Non-Anthropic Providers (Low)

Tool schemas passed to OpenAI/Gemini/Groq may contain Anthropic-specific constructs that cause silent failures. OpenCode's `transform.ts` has per-provider schema rewriters.

---

## Specific Improvements to Adopt

### I1: Add Streaming to Anthropic Provider
Switch from `io.ReadAll()` to consuming Anthropic's SSE stream incrementally. Enables real-time output forwarding to dashboard, earlier error detection, and per-chunk timeout.

**Effort:** Medium. **Impact:** High — enables real-time dashboard visibility.

### I2: Fix OpenAI Multi-Turn Message History
Rewrite `openai.go`'s message builder to consume `req.Messages` the same way `anthropic.go` does.

**Effort:** Low. **Impact:** Critical — correctness fix. This is a bug.

### I3: Add Tool Call Support to OpenAI Provider
Extend `openaiMessage` to include `tool_calls []openaiToolCall` and `tool_call_id string`. Build tool definitions from `req.Tools`. Parse `tool_calls` from response.

**Effort:** Medium. **Impact:** High — unblocks OpenAI/OpenRouter agentic tool use.

### I4: Extend Prompt Caching to Last 2 Messages
Apply `cache_control: {type: "ephemeral"}` to the last 2 non-system messages in `buildAnthropicMessages()`. Additive — does not break existing behavior.

**Effort:** Low. **Impact:** High — immediate cost reduction.

### I5: Rate Limiter as Transparent Middleware
Create a `RateLimitedProvider` decorator (same pattern as `CircuitBreakerProvider`) that gates every `Complete()` call. Stack decorators at construction time: `RecordingProvider(CircuitBreakerProvider(RateLimitedProvider(AnthropicProvider)))`.

**Effort:** Low.

### I6: Fallback Chain
Replace `fallbackModel string` with `fallbackModels []string`. Iterate the list on rate limit until one succeeds.

**Effort:** Low.

### I7: Add Context Overflow Pattern Detection
Add `ParseContextOverflow(msg string) bool` with provider-specific regex patterns. Wire into agent loop to trigger compaction on overflow errors from any provider.

**Effort:** Low.

### I8: Per-Chunk SSE Timeout
Wrap the HTTP response body in `anthropic.go` with a chunk-level deadline reader (~120s).

**Effort:** Low.

---

## Concrete Implementation Suggestions

### I2 + I3 — OpenAI Multi-Turn + Tool Calls

```go
// internal/llm/openai.go

type openaiMessage struct {
    Role       string           `json:"role"`
    Content    interface{}      `json:"content"` // string or []openaiContentPart
    ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
    ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
    ID       string             `json:"id"`
    Type     string             `json:"type"` // "function"
    Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON-encoded
}

func buildOpenAIMessages(req *pipeline.LlmRequest) []openaiMessage {
    var msgs []openaiMessage
    if req.SystemPrompt != "" {
        msgs = append(msgs, openaiMessage{Role: "system", Content: req.SystemPrompt})
    }
    for _, m := range req.Messages {
        switch m.Role {
        case "user":
            msgs = append(msgs, openaiMessage{Role: "user", Content: m.Content})
        case "assistant":
            msg := openaiMessage{Role: "assistant", Content: m.Content}
            for _, tc := range m.ToolCalls {
                msg.ToolCalls = append(msg.ToolCalls, openaiToolCall{
                    ID:   tc.ID,
                    Type: "function",
                    Function: openaiToolFunction{
                        Name:      tc.Name,
                        Arguments: tc.InputJSON,
                    },
                })
            }
            msgs = append(msgs, msg)
        case "tool":
            msgs = append(msgs, openaiMessage{
                Role:       "tool",
                Content:    m.Content,
                ToolCallID: m.ToolCallID,
            })
        }
    }
    if req.UserPrompt != "" {
        msgs = append(msgs, openaiMessage{Role: "user", Content: req.UserPrompt})
    }
    return msgs
}
```

### I4 — Message-Level Prompt Caching

```go
// internal/llm/anthropic.go — call after buildAnthropicMessages()
func applyCacheControlToLastN(msgs []anthropicMessage, n int) {
    count := 0
    for i := len(msgs) - 1; i >= 0 && count < n; i-- {
        if msgs[i].Role == "user" || msgs[i].Role == "assistant" {
            if parts, ok := msgs[i].Content.([]interface{}); ok && len(parts) > 0 {
                if lastPart, ok := parts[len(parts)-1].(map[string]interface{}); ok {
                    lastPart["cache_control"] = map[string]string{"type": "ephemeral"}
                }
            }
            count++
        }
    }
}
// applyCacheControlToLastN(messages, 2)
```

### I5 — Rate Limiter Decorator

```go
// internal/llm/ratelimited_provider.go
type RateLimitedProvider struct {
    inner   LlmProvider
    limiter *RateLimiter
}

func (r *RateLimitedProvider) Complete(ctx context.Context, req *pipeline.LlmRequest) (*pipeline.LlmResponse, error) {
    if err := r.limiter.Wait(ctx); err != nil {
        return nil, err
    }
    resp, err := r.inner.Complete(ctx, req)
    if err != nil {
        var rlErr *RateLimitError
        if errors.As(err, &rlErr) {
            r.limiter.OnRateLimit(rlErr.RetryAfterSecs)
        }
    }
    return resp, err
}
```

### I7 — Context Overflow Detection

```go
// internal/llm/overflow.go
var overflowPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)context.{0,20}(length|window|limit|size).{0,30}exceed`),
    regexp.MustCompile(`(?i)prompt.{0,20}too.{0,10}long`),
    regexp.MustCompile(`(?i)maximum context length`),
    regexp.MustCompile(`(?i)reduce.{0,30}(message|input|prompt).{0,20}length`),
    regexp.MustCompile(`(?i)token.{0,20}limit.{0,20}exceed`),
    regexp.MustCompile(`(?i)context_length_exceeded`),
    regexp.MustCompile(`(?i)string too long`),
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

Wire in `builtin.go` agent loop alongside rate limit check:
```go
if llm.IsContextOverflow(err) {
    if compactErr := agent.compactContext(ctx); compactErr != nil {
        return nil, fmt.Errorf("context overflow and compaction failed: %w", compactErr)
    }
    continue
}
```

### I8 — Per-Chunk Timeout Reader

```go
// internal/llm/chunk_timeout_reader.go
type chunkTimeoutReader struct {
    r       io.ReadCloser
    timeout time.Duration
}

func (c *chunkTimeoutReader) Read(p []byte) (int, error) {
    type result struct{ n int; err error }
    ch := make(chan result, 1)
    go func() { n, err := c.r.Read(p); ch <- result{n, err} }()
    select {
    case res := <-ch:
        return res.n, res.err
    case <-time.After(c.timeout):
        return 0, fmt.Errorf("SSE chunk timeout after %s", c.timeout)
    }
}

func (c *chunkTimeoutReader) Close() error { return c.r.Close() }
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Priority |
|---|-------------|--------|----------|
| 1 | Fix OpenAI multi-turn history (I2) | Low | **Critical** — correctness bug |
| 2 | Add OpenAI tool call support (I3) | Medium | High |
| 3 | Extend prompt caching to messages (I4) | Low | High — cost reduction |
| 4 | Rate limiter as middleware decorator (I5) | Low | Medium |
| 5 | Context overflow pattern detection (I7) | Low | Medium |
| 6 | Per-chunk SSE timeout (I8) | Low | Medium |
| 7 | Fallback model chain (I6) | Low | Medium |
| 8 | Full Anthropic streaming (I1) | Medium | Low (requires larger refactor) |
