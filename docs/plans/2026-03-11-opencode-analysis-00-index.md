# OpenCode Analysis: Master Index & Priority Matrix

**Date:** 2026-03-11  
**Purpose:** Synthesized findings from deep comparative analysis of OpenCode (`opencode/`) against Foreman (`Foreman/`). Six sub-agents investigated six technical domains independently.

---

## Document Index

| # | Document | Domain |
|---|----------|--------|
| 01 | [Cache Management & Memory](./2026-03-11-opencode-analysis-01-cache-and-memory.md) | Session persistence, compaction, prompt caching |
| 02 | [LLM Interaction Patterns](./2026-03-11-opencode-analysis-02-llm-interaction.md) | Provider abstraction, streaming, retry, message formatting |
| 03 | [Agent Orchestration](./2026-03-11-opencode-analysis-03-agent-orchestration.md) | Agent loop, sub-agents, parallel execution, doom loops |
| 04 | [Tool Usage & Capability Routing](./2026-03-11-opencode-analysis-04-tool-usage.md) | Tool definition, validation, permissions, MCP, truncation |
| 05 | [Error Handling & Recovery](./2026-03-11-opencode-analysis-05-error-handling.md) | Retry logic, panic recovery, overflow detection, observability |
| 06 | [Prompt Architecture](./2026-03-11-opencode-analysis-06-prompt-architecture.md) | System prompt composition, env injection, model-specific prompts |

---

## Where Foreman Is Ahead

Before listing gaps, acknowledge areas where Foreman is already superior to OpenCode:

| Area | Foreman's Advantage |
|------|---------------------|
| **Cost enforcement** | Hard per-ticket/per-day/per-month budget limits with `BudgetExceededError`. OpenCode only tracks, never enforces. |
| **Prometheus metrics** | 50+ operational metrics covering LLM calls, DAG execution, MCP tools, retries, event drops. OpenCode has none. |
| **Crash recovery** | Explicit `ClassifyRecovery()` → `RecoveryAction` with DAG-aware dependency stripping. OpenCode's recovery is implicit. |
| **Event drop detection** | Buffered WebSocket fan-out with drop counters and self-reporting meta-events. OpenCode has no equivalent. |
| **Circuit breaker** | `CircuitBreakerProvider` decorator with full Closed→Open→Half-Open→Closed cycle. |
| **Reactive context injection** | Explicit `ContextProvider` interface — more controllable than OpenCode's implicit tool-result-in-context approach. |
| **Recording provider** | Full prompt/response JSON stored to SQLite per call — more comprehensive than OpenCode's telemetry. |

---

## Priority Matrix (All Improvements)

### Critical — Fix Immediately

| ID | Improvement | File | Effort |
|----|-------------|------|--------|
| LLM-G2 | Fix OpenAI provider multi-turn history (currently discarded) | `internal/llm/openai.go` | Low |
| ERR-G5 | Add panic recovery to per-ticket goroutines | `internal/daemon/daemon.go:489–500` | Very low |
| LLM-G2-R | Read `Retry-After` header instead of hardcoded 60s | `internal/llm/anthropic.go:226–228` | Very low |
| TOOL-G3 | Fix Bash argument splitting (`shlex.Split` vs `strings.Fields`) | `internal/agent/tools/exec.go` | Very low |

### High — Next Sprint

| ID | Improvement | File | Effort |
|----|-------------|------|--------|
| LLM-G3 | Add tool call support to OpenAI provider | `internal/llm/openai.go` | Medium |
| CACHE-I1 | Automatic multi-breakpoint Anthropic prompt caching (system + last 2 messages) | `internal/llm/anthropic.go` | Low |
| TOOL-G2 | Wire `savedPath` into truncation hint (currently always empty string) | `internal/agent/tools/registry.go` | Low |
| TOOL-G1 | Full input validation — type, enum, constraints in `ValidateInput()` | `internal/agent/tools/registry.go` | Medium |
| ERR-G6 | Hard stop doom loop instead of warn-and-continue | `internal/agent/builtin.go:310–319` | Low |
| ERR-G8 | Case-insensitive tool name lookup fallback | `internal/agent/tools/registry.go` | Very low |
| PROMPT-I2 | Inject `<env>` block into system prompt (working dir, git, platform, date) | `internal/context/builder.go` | Very low |
| PROMPT-I3 | Max-steps warning injection at N≤3 turns remaining | `internal/agent/builtin.go` | Very low |

### Medium — Following Sprint

| ID | Improvement | File | Effort |
|----|-------------|------|--------|
| CACHE-I2 | Token-based pruning thresholds (replace char math) | `internal/agent/compaction.go` | Low |
| CACHE-I3 | Dynamic summary token budget (scale with model context window) | `internal/agent/compaction.go` | Low |
| CACHE-I4 | Protected tool result categories (rules, skills never pruned) | `internal/agent/compaction.go` | Low |
| CACHE-I6 | Post-compaction re-prompt injection | `internal/agent/builtin.go` | Very low |
| LLM-G5 | Rate limiter as transparent middleware decorator | New `internal/llm/ratelimited_provider.go` | Low |
| LLM-G6 | Fallback model chain (replace single `fallbackModel`) | `internal/agent/builtin.go` | Low |
| LLM-G8 | Context overflow pattern detection (regex-based) | New `internal/llm/overflow.go` | Low |
| ORCH-G3 | Doom loop escalation callback + JSON key normalization | `internal/agent/builtin.go`, `internal/agent/doomloop.go` | Low |
| ORCH-I4 | Wire TaskManager into Subagent tool dispatch | `internal/agent/tools/exec.go` | Low |
| ERR-I1 | `RetryingProvider` decorator with exponential backoff | New `internal/llm/retrying_provider.go` | Medium |
| ERR-G3 | Per-chunk SSE timeout reader (120s per chunk) | `internal/llm/anthropic.go` | Low |
| TOOL-G8 | MCP tool list change handler | `internal/mcp/stdio_client.go`, `manager.go` | Low |
| TOOL-G7 | Truncation cleanup scheduler (7-day retention) | `internal/agent/tools/truncation.go` + daemon | Low |
| PROMPT-I5 | Two-block Anthropic system prompt for caching optimization | `internal/llm/anthropic.go` | Low |
| PROMPT-I1 | Consolidate to single prompt system (`ContextBuilder`) | `internal/context/builder.go` | Medium |

### Low — Backlog

| ID | Improvement | File | Effort |
|----|-------------|------|--------|
| CACHE-I5 | SQLite session persistence for crash recovery | New `internal/session/store.go` | Medium |
| CACHE-I7 | Binary content stripping before compaction LLM call | `internal/agent/compaction.go` | Low |
| ORCH-I1 | Parallel sub-agent fan-out via `errgroup` | `internal/agent/tools/exec.go` | Medium |
| ORCH-I5 | DB session hierarchy (ParentRunID) for sub-agents | DB schema + models | Medium |
| TOOL-G4 | `ActionAsk` permission escalation with `PermissionAsker` interface | `internal/agent/permission.go` | Medium |
| TOOL-G5 | `DeniedError`/`CorrectedError` structured types | `internal/agent/tools/errors.go` | Low |
| PROMPT-I4 | Model-variant prompt selection | `internal/prompt/registry.go` | Low |
| ERR-I7 | `logTimer` helper for structured duration logging | `internal/telemetry/logtimer.go` | Very low |
| LLM-I1 | Full Anthropic SSE streaming | `internal/llm/anthropic.go` | Medium-High |

---

## Key Patterns to Learn From OpenCode

### 1. Everything Has an Abort Signal
OpenCode threads `AbortSignal` through every layer: the HTTP call, each SSE chunk, every tool execution, and the agent loop itself. Foreman threads `context.Context` for cancellation but lacks per-chunk SSE cancellation and tool-level abort signals beyond context cancellation.

### 2. Default Permissions Are Interactive, Not Blocking
OpenCode's default permission action is `ask` — the user is shown a prompt and can choose. Foreman defaults to `deny` which silently blocks valid tool calls. The `ask` model leads to better agent UX and clearer user control.

### 3. Structured Parts, Not Monolithic Strings
OpenCode stores conversation as typed `Part` records (text, tool-call, tool-result, compaction, reasoning). Each part can be independently compacted, protected, or filtered. Foreman uses monolithic `Content` strings, making fine-grained compaction harder.

### 4. Compaction Is a First-Class Concept
OpenCode stores compaction events as DB rows, enabling crash recovery, session resumability, and compaction history inspection. Foreman's compaction is ephemeral and in-memory.

### 5. The Prompt Includes the Environment
Injecting `<env>` (working directory, date, platform, git status) into the system prompt eliminates orientation tool calls. This is a simple, high-value pattern.

### 6. Tools Have LLM-Actionable Error Messages
OpenCode's tool validation errors include specific guidance: "the `X` tool was called with invalid arguments: `Y`. Please rewrite the input so it satisfies the expected schema." Foreman's errors are terse and non-actionable.

### 7. Prompt Caching Is Applied Automatically
OpenCode applies Anthropic cache breakpoints to every request automatically. No manual opt-in. Foreman requires `CacheSystemPrompt: true` and only caches the system prompt, missing 40-60% of achievable cache savings.

---

## Estimated Cost Impact of Top Improvements

| Improvement | Mechanism | Estimated Savings |
|-------------|-----------|-------------------|
| Multi-breakpoint prompt caching (CACHE-I1) | Anthropic cache reads at 10% of input cost | ~40-60% on long conversations |
| Two-block system prompt caching (PROMPT-I5) | Better cache hit rate on stable system content | ~10-20% additional on top of above |
| Token-based compaction (CACHE-I2, I3) | More accurate compaction = fewer wasted turns | Indirect: fewer turns per task |
| Protected tool results (CACHE-I4) | LLM retains orientation context longer | Indirect: fewer context-lost recovery turns |
| Fix doom loop hard stop (ERR-G6) | Eliminates wasted turns on stuck agents | Up to full `maxTurns` saved per stuck task |
| Per-call retry with backoff (ERR-I1) | Recovers from transient errors automatically | Eliminates task failures from transient issues |
