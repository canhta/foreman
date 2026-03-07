# Deep Architectural Audit — Foreman

**Date:** 2026-03-07
**Scope:** Agent loops, memory/caching, data sharing, multi-agent coordination, failure handling, observability
**Reference systems:** OpenCode, Claude Code
**Auditor:** Principal Software Architect review

---

## Executive Summary

Foreman is an ambitious autonomous development daemon with solid foundational architecture: clean interface separation, pluggable providers, and a well-designed DAG executor. However, the current implementation has **systemic architectural weaknesses** across six critical dimensions that will cause instability at scale. The most significant issues are:

1. **No conversation memory management** — agent loops accumulate messages without bounds
2. **No shared state between agents and orchestrator** — context is reconstructed from scratch for every LLM call with no caching
3. **Subagent depth explosion** — recursive agents have no depth enforcement
4. **Stateless-by-design is carried too far** — eliminates the possibility of intelligent context reuse
5. **Silent failure propagation** — tool errors, context errors, and hook errors are all swallowed

These are not just bugs (the existing `2026-03-06-architectural-review-design.md` covers specific bugs well). These are **structural patterns** that need architectural redesign.

---

## 1. Agent Execution Loop and Lifecycle

### Current Design

The builtin runner (`internal/agent/builtin.go:116-206`) implements a synchronous turn-based loop:

```
for turn := 0; turn < maxTurns; turn++ {
    resp := provider.Complete(ctx, llmReq)      // LLM call
    if resp.StopReason == EndTurn → return       // Terminal
    tools := parallelExecute(resp.ToolCalls)     // errgroup
    messages = append(messages, resp, results)   // Accumulate
    contextProvider.OnFilesAccessed(paths)        // Reactive inject
}
```

### Issues Found

#### A1. Unbounded Message History (Critical)

**Location:** `builtin.go:113-189`

Messages are **append-only with no pruning, windowing, or summarization**. Each turn adds an assistant message + tool results. With 10 max turns and large tool outputs (file reads, grep results), the message array can easily exceed 100K tokens.

**Impact:** LLM performance degrades, costs explode, and eventually hits provider context limits causing hard failures.

**How OpenCode solves this:** OpenCode has a sophisticated compaction system (`session/compaction.ts`):
- **Overflow detection** (`isOverflow()`): Compares total tokens against `model.limit.input - reserved`
- **Automatic pruning** (`prune()`): Walks backwards through parts, keeping the last 40K tokens of tool calls and erasing older tool outputs
- **Summarization agent**: Spawns a dedicated `compaction` agent that produces a structured summary (Goal, Instructions, Discoveries, Accomplished, Relevant Files)
- **Protected tools**: Certain tools (e.g., `skill`) are never pruned

**How Claude Code solves this:** Claude Code automatically compresses prior messages as conversations approach context limits, preserving critical context while discarding verbose tool outputs.

**Recommendation:** Implement a two-tier context management strategy:
1. **Reactive pruning**: After each turn, estimate total token count. When exceeding 70% of context window, truncate old tool outputs (keep summaries)
2. **Summarization**: When exceeding 85%, pause the loop, generate a summary of completed work, replace history with summary + recent messages

#### A2. No Agent Depth Enforcement (High)

**Location:** `builtin.go:61-73`, `tools/exec.go:168-174`

`AgentRequest` has an `AgentDepth` field (`runner.go:35`) but it is **never checked or incremented** in the builtin runner. The subagent tool clamps turns to 10 but doesn't track nesting depth.

**Impact:** A subagent spawning subagents can create 10 × 10 = 100+ LLM calls. With parallel tool execution, this becomes a cost and latency bomb.

**How OpenCode solves this:** OpenCode's `task` tool (subagent equivalent) operates within the session's step counter (`agent.steps`), which provides a hard upper bound. Subagents are tracked as parts within the parent message, making the hierarchy visible.

**Recommendation:**
```go
// In AgentRequest, enforce:
if req.AgentDepth >= MaxAgentDepth {
    return AgentResult{}, fmt.Errorf("max agent depth %d exceeded", MaxAgentDepth)
}
// In subagentRunFn, increment:
result, err := r.Run(ctx, AgentRequest{
    AgentDepth: parentDepth + 1,
    ...
})
```

#### A3. No Token Budget Enforcement in Builtin Runner (High)

**Location:** `builtin.go:140-142`

Token counts are aggregated (`usage.InputTokens += resp.TokensInput`) but **never compared against any budget**. The ClaudeCode runner delegates budget to the CLI (`--max-budget-usd`), but the builtin runner — which is the primary runner — has no equivalent.

**Impact:** Builtin agent runs until max turns regardless of cost. Combined with A2 (depth explosion), a single ticket can consume unbounded LLM spend.

**Recommendation:** Add `MaxBudgetTokens` and `MaxBudgetUSD` to `BuiltinConfig`. Check after each turn:
```go
if usage.InputTokens + usage.OutputTokens > r.config.MaxBudgetTokens {
    return result, &llm.BudgetExceededError{...}
}
```

#### A4. Silent Tool Error Suppression (Medium)

**Location:** `builtin.go:169-174`

All tool errors become ToolResult strings with `IsError: true`. There's no circuit breaker — if all tools in every turn fail, the agent loops until max turns, wasting LLM calls on contexts that will never produce working code.

**How OpenCode solves this:** OpenCode's `SessionProcessor` has a **doom loop detector** (`processor.ts:152-177`): if the last 3 tool calls have identical name and input, it triggers a permission check (`doom_loop`), which can halt the loop. OpenCode also distinguishes `PermissionNext.RejectedError` from tool errors, setting a `blocked` flag that terminates the loop cleanly.

**Recommendation:** Add a consecutive-failure counter. After N consecutive turns where all tools fail, terminate with a structured error rather than continuing.

#### A5. Parallel Tool Write Conflicts (Medium)

**Location:** `builtin.go:156-179`

All tool calls in a turn execute concurrently via `errgroup`. If two Write/Edit tools target the same file, they race. No lock or ordering is enforced.

**Impact:** Corrupted file writes during parallel execution.

**Recommendation:** Pre-scan tool inputs for file paths. If any paths overlap across concurrent tools, serialize those tools while keeping non-conflicting ones parallel.

---

## 2. Memory Architecture and Caching Strategy

### Current Design

Foreman's core design principle is **"Stateless LLM Calls"** — every call reconstructs context from scratch using:
- Database handoffs (`internal/db` → `GetHandoffs()`)
- Git repository state (`FileTree()`, file reads)
- Context assembly (`internal/context/assembler.go`)
- Token budgets (`internal/context/token_budget.go`)

### Issues Found

#### M1. No Context Caching Between Pipeline Stages (Critical)

**Location:** `internal/context/assembler.go`, `internal/pipeline/task_runner.go`

Every pipeline stage (planner, implementer, spec reviewer, quality reviewer, final reviewer) calls the full context assembler independently. The assembler:
1. Rebuilds the file tree (`GitProvider.FileTree`)
2. Re-scores all files for relevance
3. Re-scans for secrets
4. Re-loads rules from `.foreman-rules.md` files
5. Re-queries progress patterns from DB

For a ticket with 5 tasks, each with implement + TDD verify + spec review + quality review = ~20 full context assemblies. Each involves multiple disk reads and git operations.

**Impact:** Massive redundant I/O. File tree, secret patterns, and rules don't change between stages — only the working tree content changes.

**How OpenCode solves this:** OpenCode uses `Instance.state()` — a per-project singleton pattern that caches expensive computations. Agent definitions, configs, and file indices are computed once and cached until invalidated. The `Snapshot` system (`snapshot/index.ts`) tracks file changes efficiently rather than re-scanning everything.

**Recommendation:** Introduce a `ContextCache` per-pipeline that:
- Caches file tree, rules, and secret patterns at pipeline start
- Invalidates only after git operations (commit, checkout)
- Shares scored file relevance data between stages (a file relevant for implementation is also relevant for review)

```go
type ContextCache struct {
    fileTree     []git.FileEntry
    rules        map[string]string
    secretPaths  []string
    invalidated  bool
    mu           sync.RWMutex
}
```

#### M2. Handoff System is Write-Only, Not Queryable (Medium)

**Location:** `internal/db/db.go:33-34`

The handoff system (`SetHandoff` / `GetHandoffs`) stores key-value pairs passed between pipeline roles. However:
- Handoffs are **append-only** — no update or delete
- Query is only by `(ticketID, forRole)` — can't query by key
- No structured schema — values are opaque strings

**Impact:** As a ticket progresses through stages, handoffs accumulate but can never be pruned or updated. If the planner outputs a plan and the plan is later revised, both the old and new plan exist as handoffs.

**How OpenCode solves this:** OpenCode uses structured `MessageV2.Part` types with explicit state machines (pending → running → completed/error). Parts can be updated in place. The `updatePart()` function replaces part state rather than appending.

**Recommendation:** Add `UpdateHandoff()` to the Database interface. Use structured handoff types with versioning:
```go
type HandoffRecord struct {
    // ... existing fields
    Version   int    // Increment on update
    Supersedes string // ID of handoff this replaces
}
```

#### M3. Progress Patterns Never Used for Context Scoring (Medium)

**Location:** `internal/context/progress.go`, `internal/context/file_selector.go`

Progress patterns are saved (`SaveProgressPattern`) and loaded (`GetProgressPatterns`) but the file selector's scoring doesn't incorporate them. Patterns discovered by task 1 (e.g., "this project uses repository pattern") could improve file selection for task 2-5, but this cross-task learning isn't wired up.

**Recommendation:** Feed progress patterns into the file selector's scoring algorithm as bonus weight for files matching discovered patterns.

#### M4. Feedback Accumulator Reset Destroys Cross-Attempt Learning (High)

**Location:** `internal/pipeline/task_runner.go:87-92`, `internal/pipeline/feedback.go:82-84`

The `FeedbackAccumulator` is reset at the start of each retry attempt (`feedback.Reset()` at line 92). Feedback from attempt N-1 is **completely discarded** before attempt N runs. This means:

- Attempt 1 fails with test error "assertion on line 42 failed"
- Attempt 2 starts with **zero knowledge** of attempt 1's failure
- The implementer may reproduce the exact same bug

This is distinct from the existing review's C4 (which notes the accumulator is created once). The deeper issue is that even when feedback IS accumulated within an attempt (lint → test → review), the entire history is wiped before the next retry.

**Impact:** Retry effectiveness degrades. The implementer has no persistent error journal across attempts.

**Recommendation:** Keep a cumulative error summary across attempts. Reset the detailed feedback but preserve a brief summary:
```go
feedback.ResetKeepingSummary() // Clears entries but keeps 1-line summary of prior attempt failures
```

#### M5. File Selection is Static Per Task (High)

**Location:** `internal/context/file_selector.go:22-77`, `internal/pipeline/task_runner.go:107`

File selection is driven entirely by the planner's task metadata (`FilesToRead`, `FilesToModify`). The implementer **cannot request additional files** during execution. If the implementer fails because a required file was missing from context, the next retry attempt still has the **same file set**.

Three-tier scoring: Explicit references (score 100), test siblings (score 60), directory proximity (score 30). No feedback loop from implementer errors to file selection.

**Impact:** LLM hallucinates about files it has never seen. Retries don't adapt to "file not found" errors.

**Recommendation:** After a failed implementation attempt, parse the LLM's output for file references not in the current context set. Add them as high-priority candidates for the next attempt.

#### M6. Token Budget Allocation Imbalance (Medium)

**Location:** `internal/context/assembler.go:188`

The implementer context allocates only **50% of the token budget** to file content (`tokenBudget / 2`). The other 50% is reserved for system prompt, task description, and feedback. But as feedback grows across stages (lint errors + test errors + review feedback), the feedback section consumes tokens that were budgeted for files.

Files either fit completely or are skipped entirely — no per-file truncation. Large high-priority files can crowd out many smaller relevant files.

**Recommendation:** Dynamic budget allocation: measure actual system prompt + feedback size first, then give remaining budget to files.

#### M7. Token Budget Uses Heuristic Counting (Low)

**Location:** Architecture doc: "Token counting: Built-in heuristic (len/4 approximation)"

The `len/4` heuristic can be off by 30-50% for code (which has more tokens per character than prose). This means the token budget can overflow or underutilize the context window.

**How OpenCode solves this:** OpenCode uses `Token.estimate()` with provider-specific adjustments and tracks actual token counts from API responses.

**Recommendation:** Use the actual `TokensInput` from the first LLM response to calibrate the heuristic for subsequent calls.

---

## 3. State and Data Sharing Between Agents and Orchestrator

### Current Design

The orchestrator (`internal/daemon/orchestrator.go`) owns the pipeline lifecycle. Agents run within pipeline stages via `AgentRunner.Run()`. Communication flows through:
- Database (handoffs, tasks, events)
- Git (file changes, commits)
- Function return values (AgentResult)

### Issues Found

#### S1. Orchestrator Has No Visibility Into Agent Progress (Critical)

**Location:** `internal/agent/runner.go:9-13`

`AgentRunner.Run()` is a black box — it takes `AgentRequest` and returns `AgentResult`. The orchestrator cannot:
- Monitor agent progress during execution
- Cancel an agent based on cost threshold
- Report partial progress to the dashboard
- Know which tools the agent is using

**How OpenCode solves this:** OpenCode's `SessionProcessor` emits granular events via the `Bus` system throughout execution:
- `start-step` / `finish-step` with token counts and cost
- `tool-call` / `tool-result` / `tool-error` with timing
- `text-start` / `text-delta` / `text-end` for streaming
- These events are consumed by the UI, WebSocket subscribers, and the compaction system

**How Claude Code solves this:** Claude Code uses a streaming architecture where every tool use, text output, and cost increment is emitted as an event. The status line, permission system, and hooks all consume this stream.

**Recommendation:** Add a `ProgressCallback` to `AgentRequest`:
```go
type AgentRequest struct {
    // ... existing fields
    OnProgress func(event AgentEvent)
}

type AgentEvent struct {
    Type      string // "tool_start", "tool_end", "turn_complete", "token_update"
    ToolName  string
    TokensUsed int
    CostUSD   float64
    Turn      int
}
```

Wire this to the EventEmitter so the dashboard can show real-time agent activity.

#### S2. No Data Sharing Between Parallel Tasks (High)

**Location:** `internal/daemon/dag_executor.go`

The DAG executor runs tasks in parallel via a coordinator/worker pattern. Workers are completely isolated — they share only the git working directory. If task A discovers that the project uses a specific pattern (e.g., "all services implement the `Repository` interface"), task B running in parallel has no way to benefit from this discovery.

**Impact:** Parallel tasks redundantly explore the codebase. Each task independently reads the same files, discovers the same patterns, and makes the same contextual inferences.

**How OpenCode solves this:** OpenCode doesn't have parallel tasks in the same way, but its `Bus` system provides a mechanism for cross-concern communication. Any component can publish events that others subscribe to.

**Recommendation:** Introduce a shared `DiscoveryBoard` per ticket:
```go
type DiscoveryBoard struct {
    mu       sync.RWMutex
    patterns map[string]string  // key → value
    files    map[string]float64 // path → relevance score
}
```
Tasks write discoveries to the board during execution. Subsequent tasks (and parallel tasks on their next LLM turn) read from it.

#### S3. AgentResult Loses Structured Data (Medium)

**Location:** `internal/agent/runner.go:15-27`

```go
type AgentResult struct {
    Output  string
    Usage   AgentUsage
}
```

The agent returns a flat string. The pipeline then parses this string (`internal/pipeline/output_parser.go`) to extract structured data (file changes, review decisions, etc.). This creates a fragile parse-unparse cycle.

**Impact:** If the LLM output format varies slightly, the parser fails. The implementer's SEARCH/REPLACE blocks, the reviewer's STATUS/CRITICAL markers — all depend on exact formatting.

**Recommendation:** Extend `AgentResult` with structured fields:
```go
type AgentResult struct {
    Output       string
    FileChanges  []FileChange  // Parsed SEARCH/REPLACE or diff blocks
    ReviewResult *ReviewResult // Parsed review decision
    Usage        AgentUsage
    Metadata     map[string]string
}
```

---

## 4. Multi-Agent Coordination and Task Execution Flow

### Current Design

Foreman's multi-agent coordination happens at two levels:
1. **Pipeline level**: Sequential stages (planner → implementer → reviewer) with serial handoffs
2. **DAG level**: Parallel task execution within a ticket, bounded by `max_parallel_tasks`

### Issues Found

#### C1. DAG Executor Goroutine Leak (High — also in existing review as H2)

**Location:** `internal/daemon/dag_executor.go:80-96`

Workers are spawned but not waited on after context cancellation. If the coordinator cancels (due to failure or timeout), workers continue running in the background.

**How OpenCode solves this:** OpenCode uses structured concurrency via AbortSignal propagation. Every operation checks `input.abort.throwIfAborted()` at the start of each stream iteration (`processor.ts:56`). When the signal fires, all downstream operations clean up synchronously.

**Recommendation:** Add `sync.WaitGroup` for workers with a bounded drain timeout after cancellation.

#### C2. No Coordination Protocol Between Pipeline Stages (Medium)

Pipeline stages communicate only through:
1. Database handoffs (unstructured key-value strings)
2. Git state (file content on disk)
3. Implicit conventions (output format expectations)

There's no explicit contract between stages. The planner produces a YAML plan that the plan validator parses. The implementer produces SEARCH/REPLACE blocks that the output parser handles. But these contracts are defined only in prompt templates and parser implementations — they're not enforced by the type system.

**How OpenCode solves this:** OpenCode uses typed Zod schemas for all inter-component data. Messages have explicit `Part` types (text, tool, reasoning, step-start, step-finish, patch, compaction) that are machine-parseable. The Bus system uses typed BusEvent definitions.

**Recommendation:** Define Go types for inter-stage data:
```go
type PlanOutput struct {
    Tasks     []TaskSpec    `json:"tasks"`
    Rationale string        `json:"rationale"`
}

type ImplementOutput struct {
    Changes  []FileChange  `json:"changes"`
    Tests    []TestResult  `json:"tests"`
}

type ReviewOutput struct {
    Approved bool          `json:"approved"`
    Severity string        `json:"severity"`
    Issues   []ReviewIssue `json:"issues"`
}
```
Use `OutputSchema` in `LlmRequest` to enforce structured output from the LLM.

#### C3. Ticket Decomposition Creates Orphan Coordination Problems (Medium)

**Location:** `internal/pipeline/decompose.go`, `internal/daemon/merge_checker.go`

When a ticket is decomposed into children:
- Children are created as independent tickets in the tracker
- Parent waits for all children to reach `merged` status
- MergeChecker polls for this asynchronously

But there's no coordination if children have conflicting file modifications. Two children could both modify the same file, creating merge conflicts that neither child can resolve.

**Recommendation:** Before creating child tickets, analyze `FilesToModify` across children for overlap. If overlap exists, either add dependency edges between children or warn the user.

---

## 5. Failure Handling and System Reliability

### Current Design

Foreman has several failure handling mechanisms:
- Retry loops in task runner (max 3 attempts)
- Rate limit fallback in agent loop
- Crash recovery from `last_completed_task_seq`
- Partial PR creation for partially-successful tickets
- Graceful degradation on provider outage

### Issues Found

#### F1. Retry Loop Doesn't Reset State (Critical — also C2 in existing review)

**Location:** `internal/pipeline/task_runner.go:89-129`

When a task fails and retries, file changes from the previous attempt remain on disk. The feedback accumulator isn't reset (C4 in existing review). The combination means:
- Retry 2 sees corrupt file state from retry 1
- Retry 2 receives contradictory feedback from retry 1
- LLM is asked to fix code that's a hybrid of two failed attempts

**How OpenCode solves this:** OpenCode's `Snapshot` system (`snapshot/index.ts`) takes snapshots before each step (`start-step` event). On failure, it can compute a patch to revert to the pre-step state. The `revert` field on sessions enables clean rollback.

**Recommendation:** Take a git snapshot (`git stash` or commit to a temp ref) before each implementation attempt. On retry, restore the snapshot first.

#### F2. No Circuit Breaker for Provider Failures (Medium)

**Location:** `internal/llm/provider.go`, `internal/llm/anthropic.go`

If the LLM provider returns repeated 500s, each pipeline stage retries independently. There's no global circuit breaker that would pause all pipelines when the provider is degraded.

**How OpenCode solves this:** OpenCode's `SessionRetry` (`session/retry.ts`) implements exponential backoff with jitter, and distinguishes retriable errors from terminal ones. The retry delay increases across attempts, and provider metadata is used to inform delay duration.

**Recommendation:** Add a provider-level circuit breaker:
```go
type CircuitBreaker struct {
    failures    int32
    lastFailure time.Time
    state       string // "closed", "open", "half-open"
}
```
When the circuit opens (e.g., 5 failures in 60 seconds), all pipelines pause LLM calls and wait for the cooldown.

#### F3. Crash Recovery Skips DAG-Scheduled Tasks (Medium)

**Location:** `internal/daemon/recovery.go`

Recovery uses `last_completed_task_seq` to resume after the last completed task. But DAG execution doesn't follow sequence numbers — tasks execute based on dependency graph order. A crash during DAG execution could skip tasks that were in-flight but not completed.

**Recommendation:** Store DAG execution state (which tasks are completed, which are in-flight) in the database. On recovery, reconstruct the DAG and re-execute only pending/failed tasks.

#### F4. File Reservation Orphaning After PR (Medium)

**Location:** `internal/daemon/orchestrator.go:484-487`

If `scheduler.Release()` fails **after** a PR has already been successfully created, the ticket is marked `AWAITING_MERGE` but file reservations remain held. On the next ticket that touches the same files, `TryReserve()` will report false conflicts, blocking work indefinitely.

The orchestrator specifically avoids using `returnErr` for this error (to prevent the deferred handler from incorrectly marking the ticket as failed), but the reservation leak is still dangerous.

**Recommendation:** Make reservation release idempotent and retriable. Add a periodic cleanup goroutine that releases reservations for tickets in terminal states (`merged`, `pr_closed`, `done`, `failed`).

#### F5. Silent File Skip in Context Loading (Medium)

**Location:** `internal/pipeline/task_runner.go:275-286`

When loading context files for the implementer, missing files are silently skipped (`continue` on `os.ReadFile` error). If the planner specified `FilesToRead: ["internal/auth/handler.go"]` but the file doesn't exist (typo, moved, or deleted), the implementer never learns this -- it simply doesn't see the file, and may hallucinate its contents.

**Recommendation:** Log a warning and include a note in the context: "File `internal/auth/handler.go` was referenced but not found."

#### F6. MergeChecker Parent Completion Race (Low)

**Location:** `internal/daemon/merge_checker.go:115-147`

When two child tickets merge simultaneously, both trigger `checkParentCompletion()`. Both goroutines check "are all children merged?" -- both find yes -- both call `UpdateTicketStatus(parent, Done)`. The database update is idempotent (setting status to `done` twice is harmless), but the external tracker update and hook execution may fire twice.

**Recommendation:** Use a database-level `UPDATE ... WHERE status = 'decomposed'` with row-count check. Only proceed with hooks/tracker if exactly 1 row was updated.

---

## 6. Logging, Tracing, and Debugging Capability

### Current Design

Foreman has:
- **Structured logging**: zerolog with JSON output and contextual fields
- **Prometheus metrics**: 25+ counters/histograms/gauges for tickets, tasks, LLM calls, cost, etc.
- **Database events**: EventEmitter writes events to DB and fans out via WebSocket
- **Dashboard**: Web UI with REST API and WebSocket for real-time updates

### Issues Found

#### O1. No Request-Level Tracing (High)

There is no trace ID that follows a request through the full pipeline. When debugging why a ticket failed, you must correlate:
- zerolog entries (by ticket_id field)
- Database events (by ticket_id)
- Prometheus metrics (by label, but not per-request)
- LLM call records (by ticket_id)

But there's no way to trace a single LLM call back to the pipeline stage that triggered it, or to see the exact prompt that was sent.

**How OpenCode solves this:** OpenCode assigns ascending IDs to every entity (`Identifier.ascending("part")`, `Identifier.ascending("message")`). Parts are nested within messages, messages within sessions. The full execution trace is reconstructable from the database. Each step records start/finish timestamps, token counts, cost, and snapshot hashes.

**How Claude Code solves this:** Claude Code has `experimental_telemetry` with OpenTelemetry integration, user ID tracking, and full request metadata.

**Recommendation:** Add a `TraceID` to pipeline context:
```go
type PipelineContext struct {
    TraceID   string
    TicketID  string
    TaskID    string
    Stage     string  // "planning", "implementing", "reviewing"
    Attempt   int
}
```
Pass this through to LLM calls, tool executions, and events. Store the full LLM prompt/response (or a hash + summary) in `llm_calls` for debugging.

#### O2. LLM Prompts Not Stored for Debugging (High)

**Location:** `internal/db/sqlite.go:263-277`

`RecordLlmCall` stores a `prompt_hash` and `response_summary` but not the full prompt or response. When an LLM call produces unexpected output, there's no way to see what prompt was actually sent.

**Recommendation:** Store the full prompt and response in a separate table (or file-backed storage) linked by the `llm_calls.id`. Use compression for storage efficiency.

#### O3. EventEmitter Drops Events Silently (Medium)

**Location:** `internal/telemetry/events.go:69-73`

```go
select {
case ch <- evt:
default:
    // Drop if subscriber is slow — backpressure is caller's responsibility.
}
```

If a WebSocket subscriber is slow, events are silently dropped. The dashboard can show incomplete or inconsistent state.

**How OpenCode solves this:** OpenCode's Bus system uses synchronous `Promise.all(pending)` for event delivery (`bus/index.ts:63`). All subscribers process the event before the publisher continues. This guarantees delivery but risks blocking.

**Recommendation:** Use a bounded buffer with overflow detection. When events are dropped, emit a meta-event "events_dropped" with a count so the dashboard can show a "reconnect" indicator.

#### O4. No Cost-Per-Stage Breakdown (Low)

The cost controller (`internal/telemetry/cost_controller.go`) tracks cost per ticket and per day, but not per pipeline stage. You can see "this ticket cost $2.40" but not "planning cost $0.30, implementation cost $1.50, review cost $0.60."

**Recommendation:** Add `stage` as a field in `llm_calls` and provide `GetTicketCostByStage()` in the database interface.

---

## Comparative Architecture Summary

| Concern | Foreman | OpenCode | Claude Code |
|---------|---------|----------|-------------|
| **Agent loop** | Fixed turn counter, no windowing | Step-based with compaction | Context compression near limits |
| **Context management** | Full rebuild each call | Instance.state() caching + pruning | Automatic prior message compression |
| **Memory/history** | Append-only, unbounded | Part-based with typed state machine | Sliding window with summarization |
| **Event system** | Fire-and-forget, lossy | Typed Bus with sync delivery | Stream-based with hooks |
| **Error recovery** | Single fallback model | Exponential backoff + abort signal | Retry with delay calibration |
| **Multi-agent** | Subagent tool (no depth limit) | Task tool with step budget | Agent tool with scoped capabilities |
| **Observability** | Prometheus + zerolog + events | Structured parts with timestamps | OpenTelemetry integration |
| **State persistence** | DB handoffs + git | SQLite with Drizzle ORM | Conversation transcript |
| **Cost control** | Per-ticket/day/month budgets | Per-model token tracking | Per-session budget flag |
| **Doom loop detection** | None | Last-3 identical calls check | Hook-based validation |

---

## Priority Remediation Roadmap

### Tier 1 — Architectural (do before production)

| ID | Issue | Effort | Impact |
|----|-------|--------|--------|
| A1 | Message history windowing/compaction | L | Prevents context overflow and cost explosion |
| S1 | Agent progress callbacks | M | Enables monitoring, cancellation, dashboard updates |
| M1 | Context caching per pipeline | M | 80%+ reduction in redundant I/O |
| M4 | Feedback accumulator cross-attempt learning | S | Prevents repeated identical failures |
| M5 | Adaptive file selection on retry | M | Fixes hallucination from missing files |
| F1 | Retry state reset (git snapshot) | S | Prevents corrupted retry attempts |
| A2 | Agent depth enforcement | S | Prevents exponential LLM call explosion |
| A3 | Token/cost budget in builtin runner | S | Prevents unbounded spend |

### Tier 2 — Reliability (do before scaling)

| ID | Issue | Effort | Impact |
|----|-------|--------|--------|
| O1 | Request-level tracing | M | Debuggability for production issues |
| F2 | Provider circuit breaker | S | Prevents cascading failures |
| A4 | Tool failure circuit breaker | S | Prevents wasteful doom loops |
| F4 | File reservation orphan cleanup | S | Prevents false conflicts blocking work |
| F5 | Log missing context files instead of silent skip | S | Prevents implementer hallucination |
| S2 | Shared discovery board | M | Improves parallel task efficiency |
| O2 | Full prompt/response storage | M | Enables LLM call debugging |
| M6 | Dynamic token budget allocation | S | Prevents feedback crowding out file context |

### Tier 3 — Quality (do for maturity)

| ID | Issue | Effort | Impact |
|----|-------|--------|--------|
| C2 | Typed inter-stage contracts | L | Eliminates fragile string parsing |
| S3 | Structured AgentResult | M | Reduces parse failures |
| M2 | Handoff versioning | S | Cleaner state management |
| M3 | Progress patterns in scoring | S | Better file selection over time |
| O3 | Event delivery guarantees | S | Dashboard reliability |
| O4 | Cost-per-stage breakdown | S | Cost optimization insights |
| C3 | Decomposition conflict detection | M | Prevents merge conflicts |
| F3 | DAG-aware crash recovery | M | Correct recovery after mid-DAG crash |
| F6 | MergeChecker parent completion race | S | Prevents duplicate hook/tracker calls |
| M7 | Calibrate token estimation from API responses | S | More accurate context budget usage |

**Effort key:** S = small (< 1 day), M = medium (1-3 days), L = large (3-5 days)

---

## Conclusion

Foreman's interface-first design and pluggable architecture are genuine strengths. The DAG executor's mutex-free coordinator pattern is well-engineered. The problem is not the foundation — it's the **absence of key systems** that production agent frameworks need:

1. **Context lifecycle management** (OpenCode's compaction/pruning system)
2. **Execution observability** (OpenCode's typed event bus, Claude Code's streaming)
3. **Defensive agent control** (depth limits, doom loop detection, budget enforcement)
4. **Intelligent state reuse** (caching, shared discovery, structured handoffs)

The existing bug-fix plan (`2026-03-06-architectural-review-design.md`) should be executed first — it addresses correctness. This audit addresses **scalability and stability** — the issues that emerge when Foreman processes real workloads at scale.
