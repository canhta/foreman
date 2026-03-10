# OpenCode Analysis 01: Cache Management & Memory/Context Strategy

**Date:** 2026-03-11  
**Domain:** Cache management, session persistence, context compaction, prompt caching  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`

---

## How OpenCode Solves This

### Session Persistence (SQLite-backed, crash-safe)

OpenCode stores all conversation state in SQLite via two tables: `MessageTable` (metadata) and `PartTable` (granular parts: text, tool calls, tool results, reasoning, compaction markers). Every part has a `time.created`, `time.completed`, and `time.compacted` timestamp, enabling precise compaction boundary tracking.

- **Key files:** `packages/opencode/src/session/session.sql.ts`, `packages/opencode/src/session/message-v2.ts`
- Messages are streamed via generator in chunks of 50 rows with offset pagination (`message-v2.ts:731`)
- `filterCompacted()` at `message-v2.ts:809` loads only post-compaction history on resume
- Compaction state is stored as a `CompactionPart` row — sessions are fully resumable after crash or restart

### Context Compaction

Two distinct mechanisms: **prune** (fast, tool-output-only) and **compact** (full LLM-based summarization).

`packages/opencode/src/session/compaction.ts`:
- `isOverflow()`: checks `tokens.total >= model.limit.input - reserved - COMPACTION_BUFFER(20k)`
- `prune()`: walks backwards, protects last `PRUNE_PROTECT=40k` tokens, marks old tool results as `compacted` in DB. Only acts if `PRUNE_MINIMUM=20k` tokens can be freed. Protects `skill` tool outputs explicitly.
- `process()`: full LLM compaction — sends history to dedicated compaction agent, structured `## Goal / ## Instructions / ## Discoveries / ## Accomplished / ## Relevant files` summary stored as `CompactionPart` DB row
- Compaction LLM call uses a separate named agent with its own model override
- After compaction: injects synthetic `"Continue if you have next steps…"` user message

### Prompt Caching (Anthropic multi-breakpoint)

`packages/opencode/src/provider/transform.ts:174` — `applyCaching()` automatically applies `cacheControl: { type: "ephemeral" }` to:
- All system messages
- The **last 2 non-system messages** in the conversation

Applied per-request via middleware — zero application-level configuration required. Cache hit/write tokens tracked and included in cost calculations at `session/index.ts:803`.

---

## Issues in Our Current Approach

### 1. No Session Persistence (Critical)

`internal/agent/builtin.go:171` — `[]models.Message` slice held entirely in-memory within `BuiltinRunner.Run()`. If the process crashes mid-task, all conversation history is lost. Sessions are not resumable — every `Run()` starts from scratch.

### 2. Context Compaction Uses Character-Based Thresholds (High)

`PruneOldToolOutputs()` in `compaction.go` uses `budget/4` **characters** as its protection threshold. Token count ≠ character count — this produces unpredictable protection windows. Phase 2 has a hardcoded 500-token summary cap (`compaction.go:239`) that truncates discoveries for complex tasks.

### 3. No Protection for Important Tool Outputs (High)

Foreman prunes by age only. OpenCode explicitly protects `skill` tool outputs from pruning. Foreman prunes critical context (rules, skills, key search results) equally with trivial outputs.

### 4. Phase 3 Drops Entire Turn-Pairs (Medium)

`CompactMessages()` removes the oldest assistant+user pairs wholesale, silently discarding critical early context including the original task requirements.

### 5. Only System Prompt is Cached (High — cost impact)

`internal/llm/anthropic.go:144` — `CacheSystemPrompt bool` wraps only the system prompt. Anthropic allows **4 cache breakpoints** per request. For long conversations, conversation history (repeated on every turn) is the ideal cache target — potential 90% cost reduction on repeated prefixes.

### 6. Prompt Caching Requires Manual Opt-in (Medium)

`CacheSystemPrompt` must be set explicitly. If a caller forgets, no caching occurs. OpenCode applies caching automatically to every request.

### 7. Compaction Not Persisted (High)

After a crash, compaction state is lost. The agent has no durable record of what was summarized.

---

## Specific Improvements to Adopt

### I1: Automatic Multi-Breakpoint Anthropic Caching
Apply `cache_control: {type: "ephemeral"}` to the last 2 non-system messages in addition to the system prompt. Make this automatic in every `Complete()` call — no manual opt-in.

**Effort:** Low (~50 lines). **ROI:** Highest — immediate cost reduction on every long conversation.

### I2: Token-Based Pruning Thresholds
Replace character math in `PruneOldToolOutputs()` with actual token counting using the existing `CountTokens()` function in the codebase. Protect the last 40,000 tokens of content (matching OpenCode's `PRUNE_PROTECT` constant).

**Effort:** Low.

### I3: Dynamic Summary Token Budget
Scale the compaction summary budget proportionally to the model's context window size (e.g., 2% of window, capped 500–4000 tokens). Remove the hardcoded 500-token cap.

**Effort:** Low.

### I4: Protected Tool Result Categories
Maintain a set of tool names whose outputs should never be pruned (e.g., `read_rules`, `get_context`, `list_skills`). These provide orientation context that the LLM needs throughout the entire task.

**Effort:** Low.

### I5: SQLite Session Persistence
Store conversation parts in SQLite. Track a compaction boundary row. On session resume, load only post-compaction history. This enables crash recovery and session resumability.

**Effort:** Medium.

### I6: Post-Compaction Re-Prompt
After any compaction event, inject the synthetic message: `"Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed."` This maintains agent momentum.

**Effort:** Very low.

### I7: Binary Content Stripping Before Compaction
Strip large binary/media tool outputs before feeding history to the compaction LLM call. They inflate the compaction prompt unnecessarily.

**Effort:** Low.

---

## Concrete Implementation Suggestions

### I1 — Automatic Anthropic Multi-Breakpoint Caching

```go
// internal/llm/anthropic.go — add after buildAnthropicMessages() returns

func applyAnthropicCaching(system []anthropicSystemBlock, msgs []anthropicMessage) {
    // Breakpoint 1: system prompt (last block)
    if len(system) > 0 {
        system[len(system)-1].CacheControl = &CacheControl{Type: "ephemeral"}
    }
    // Breakpoints 2 & 3: last 2 non-system messages
    cacheApplied := 0
    for i := len(msgs) - 1; i >= 0 && cacheApplied < 2; i-- {
        if msgs[i].Role == "user" || msgs[i].Role == "assistant" {
            if blocks, ok := msgs[i].Content.([]contentBlock); ok && len(blocks) > 0 {
                blocks[len(blocks)-1].CacheControl = &CacheControl{Type: "ephemeral"}
            }
            cacheApplied++
        }
    }
}

// Call in StreamCompletion/Complete before sending request:
applyAnthropicCaching(anthropicReq.System, anthropicReq.Messages)
```

### I2 — Token-Based Pruning

```go
// internal/agent/compaction.go
const (
    pruneProtectTokens  = 40_000
    pruneMinimumFree    = 20_000
)

func PruneOldToolOutputs(messages []models.Message, countTokens func(string) int) []models.Message {
    // Walk backwards to find the protection boundary
    total := 0
    protectIdx := len(messages)
    for i := len(messages) - 1; i >= 0; i-- {
        total += countTokens(messages[i].TextContent())
        if total >= pruneProtectTokens {
            protectIdx = i + 1
            break
        }
    }

    freed := 0
    result := deepCopy(messages)
    for i := 0; i < protectIdx; i++ {
        for j, part := range result[i].Parts {
            if part.Kind == PartKindToolResult && !part.Compacted && !isProtectedTool(part.Tool.Name) {
                freed += countTokens(part.Tool.Content)
                result[i].Parts[j].Tool.Content = "[tool output pruned by compaction]"
                result[i].Parts[j].Compacted = true
            }
        }
    }
    if freed < pruneMinimumFree {
        return messages // not worth it
    }
    return result
}

var protectedToolNames = map[string]bool{
    "read_rules":    true,
    "get_context":   true,
    "list_skills":   true,
}

func isProtectedTool(name string) bool { return protectedToolNames[name] }
```

### I3 — Dynamic Summary Budget

```go
// internal/agent/compaction.go — SummarizeHistory
func SummarizeHistory(ctx context.Context, provider llm.Provider, messages []models.Message, modelCtxWindow int) ([]models.Message, error) {
    // Scale: 2% of context window, capped 500–4000 tokens
    summaryBudget := modelCtxWindow / 50
    if summaryBudget < 500  { summaryBudget = 500 }
    if summaryBudget > 4000 { summaryBudget = 4000 }
    // ... rest unchanged
}
```

### I5 — SQLite Session Persistence (minimal schema)

```go
// internal/session/store.go
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    ticket_id TEXT,
    task_id TEXT,
    created_at INTEGER,
    updated_at INTEGER,
    status TEXT DEFAULT 'running'
);
CREATE TABLE IF NOT EXISTS parts (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    kind TEXT NOT NULL,      -- 'text' | 'tool-call' | 'tool-result' | 'compaction'
    content TEXT,            -- JSON payload
    created_at INTEGER,
    compacted_at INTEGER,    -- NULL = active, non-NULL = pruned
    FOREIGN KEY(session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_parts_session ON parts(session_id, created_at);
CREATE TABLE IF NOT EXISTS compaction_boundaries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    summary TEXT NOT NULL,   -- structured summary text
    created_at INTEGER
);
`

// LoadActiveHistory returns all non-compacted parts after the last compaction boundary
func (s *Store) LoadActiveHistory(sessionID string) ([]Part, error) {
    var cutoff int64
    row := s.db.QueryRow(
        `SELECT created_at FROM compaction_boundaries WHERE session_id=? ORDER BY created_at DESC LIMIT 1`,
        sessionID,
    )
    row.Scan(&cutoff) // if no row, cutoff=0 loads everything

    return s.queryParts(`
        SELECT id, message_id, kind, content, created_at
        FROM parts
        WHERE session_id=? AND created_at > ? AND compacted_at IS NULL
        ORDER BY created_at
    `, sessionID, cutoff)
}
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Impact |
|---|-------------|--------|--------|
| 1 | Automatic multi-breakpoint Anthropic caching (I1) | Low | Immediate ~40-60% cost reduction on long sessions |
| 2 | Token-based pruning thresholds (I2) | Low | Correctness fix; prevents over/under pruning |
| 3 | Dynamic summary token budget (I3) | Low | Better summaries for complex tasks |
| 4 | Protected tool result categories (I4) | Low | Prevents losing orientation context |
| 5 | Post-compaction re-prompt (I6) | Very low | Agent momentum after compaction |
| 6 | Binary content stripping (I7) | Low | Reduces compaction LLM cost |
| 7 | SQLite session persistence (I5) | Medium | Crash safety, session resumability |
