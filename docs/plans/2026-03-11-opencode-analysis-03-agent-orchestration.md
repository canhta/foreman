# OpenCode Analysis 03: Agent Orchestration & Task Delegation

**Date:** 2026-03-11  
**Domain:** Agent loop architecture, sub-agent spawning, parallel execution, doom loop detection, session hierarchy  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`

---

## How OpenCode Solves This

### The Agent Loop

`packages/opencode/src/session/prompt.ts`, `SessionPrompt.loop()` (line 275) — event-driven streaming loop:

1. Calls `LLM.stream()` which wraps `streamText` from the Vercel AI SDK
2. Feeds each event to `SessionProcessor.create()` — writes parts to DB in real-time, accumulates tool calls
3. After stream ends, checks `processor.message.finish`:
   - `"tool-calls"` or `"unknown"` → execute tools, loop again
   - Otherwise → break (natural end-of-turn)
4. Checks for pending `subtask` parts first on each iteration

**Termination conditions:** explicit `"stop"` signal, non-tool finish reason, structured output captured, compaction overflow cascade. Step limit is `agent.steps ?? Infinity` — configurable per-agent.

### Sub-Agent Architecture

`packages/opencode/src/tool/task.ts` — sub-agents are **full child sessions** with their own message history, DB rows, and session ID:

```typescript
return await Session.create({
    parentID: ctx.sessionID,
    title: params.description + ` (@${agent.name} subagent)`,
    permission: [ /* per-subagent rules */ ],
})
```

Key properties:
- The child runs a full recursive `SessionPrompt.prompt()` call (task.ts:128)
- Each child has its own turn counter and step limit
- `todowrite`/`todoread` forcibly disabled for sub-agents (task.ts:78–84)
- The `task` tool can be blocked recursively unless the agent explicitly has `hasTaskPermission`
- A `task_id` can be passed to **resume** a prior child session (task.ts:67–69) — long-running sub-agent continuations
- The UI renders a full session tree (parent → child → grandchild)

**Parallelism:** The AI SDK emits multiple tool call parts in a single assistant turn. The main loop processes them concurrently. Orchestration prompts instruct "Launch up to 3 explore agents IN PARALLEL" (prompt.ts:1400 in plan mode).

### Agent Registry

`packages/opencode/src/agent/agent.ts` — named agents with:
- `mode: "primary" | "subagent" | "all"` — controls visibility to `task` tool
- Per-agent `permission: Rule[]` — fine-grained allow/deny rules
- Optional model override and custom system prompt

### Doom Loop Detection

`packages/opencode/src/session/processor.ts` (lines 152–175) — checks last 3 tool parts for identical `tool + input`. On detection: **asks the user** via `PermissionNext.ask({ permission: "doom_loop" })`. The session pauses until the user decides. This is a **human-in-the-loop intervention** — the agent cannot silently continue.

---

## Issues in Our Current Approach

### G1 — No Parallel Sub-Agent Execution (High)

`internal/agent/tools/exec.go:199` — `runFn` is a blocking synchronous call. Sub-agents execute sequentially because the LLM must wait for each `Subagent` tool call to complete before issuing the next one. The `Batch` tool covers parallel *tool* calls but not parallel sub-agent dispatch.

### G2 — No Sub-Agent Session Persistence or Resumability (High)

Sub-agent results are an ephemeral `string`. There is no DB record, no hierarchical session tree, no `task_id` concept, no resumability. `TaskManager` exists in `internal/agent/task_manager.go` but is not wired into the agent loop at all.

### G3 — Doom Loop Detection Does Not Escalate to Human (Medium)

`internal/agent/builtin.go:310–319` and `internal/agent/doomloop.go` — on detection, injects a warning message and **continues looping**. A stuck agent burns all `maxTurns` budget without pausing for guidance. Additionally, JSON key-ordering instability means semantically identical inputs with different key order produce different hashes, missing detections.

### G4 — No Named Agent Registry with Model Overrides (Medium)

Foreman's `modes.go` provides functional modes (`plan`, `explore`, `build`) but no named agent registry with per-agent model overrides, descriptions, or persona definitions. All modes use the same LLM provider.

### G5 — Compaction Has No Post-Compaction Re-Prompt (Low)

After compaction in `builtin.go`, the loop continues but there is no synthetic user message re-prompting the agent to continue or clarify. OpenCode injects `"Continue if you have next steps..."` after every compaction event.

### G6 — TaskManager Is Dead Code (Low)

`internal/agent/task_manager.go` provides an in-memory concurrent-safe store for `ManagedTask` objects (pending/running/completed/failed) but is not connected to the `Subagent` tool or the agent loop in any way.

---

## Specific Improvements to Adopt

### I1: Parallel Sub-Agent Dispatch
Add a `parallel: bool` flag to the `Subagent` tool input, or implement a `ParallelSubagents` tool that fans out multiple `runFn` calls via `errgroup`. Budget should be divided equally among parallel sub-agents.

**Effort:** Medium.

### I2: Wire TaskManager into the Agent Loop
When a `Subagent` tool call is dispatched, create a `ManagedTask` record. Surface the `task_id` in the tool output so the LLM can reference it. This also enables sub-agent progress visibility in the dashboard.

**Effort:** Low-Medium.

### I3: Doom Loop Escalation Path
Add an `OnDoomLoop func(ctx, toolName, turn) bool` callback to `AgentRequest`. The daemon/pipeline layer wires an interactive or policy-based handler. If no callback is set, fall back to current warning behavior.

Also fix JSON key-ordering: normalize tool inputs by canonicalizing JSON keys before hashing (unmarshal to `map[string]interface{}` then re-marshal with sorted keys).

**Effort:** Low.

### I4: Post-Compaction Re-Prompt
After `SummarizeHistory()` succeeds, inject: `"Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed."` as a user message before the next LLM call.

**Effort:** Very low.

### I5: Session Hierarchy in DB
Add `ParentRunID *string` to `AgentUsage` or a new `AgentRun` DB model. Record parent-child relationships when `Subagent` dispatches a child. Enable the dashboard to render a tree view per ticket.

**Effort:** Medium.

---

## Concrete Implementation Suggestions

### I1 — Parallel Sub-Agent Fan-out

```go
// internal/agent/tools/exec.go

type ParallelSubagentInput struct {
    Tasks []struct {
        Task    string   `json:"task"`
        Mode    string   `json:"mode"`
        Tools   []string `json:"tools,omitempty"`
        MaxTurns int     `json:"max_turns,omitempty"`
    } `json:"tasks"`
}

func (t *subagentTool) executeParallel(
    ctx context.Context,
    tasks []SubagentTask,
    workDir string,
    parentBudget, parentDepth int,
) []string {
    g, ctx := errgroup.WithContext(ctx)
    results := make([]string, len(tasks))
    budgetPerTask := parentBudget / len(tasks)

    for i, task := range tasks {
        i, task := i, task
        g.Go(func() error {
            out, err := runFn(
                ctx, task.Task, workDir, task.Mode,
                task.Tools, task.MaxTurns,
                budgetPerTask, parentDepth+1,
            )
            if err != nil {
                results[i] = fmt.Sprintf("[error: %v]", err)
                return nil // don't fail the group on individual agent failure
            }
            results[i] = out
            return nil
        })
    }
    _ = g.Wait()
    return results
}
```

### I3 — Doom Loop Escalation + JSON Normalization

```go
// internal/agent/builtin.go — add to AgentRequest
type AgentRequest struct {
    // ...
    OnDoomLoop func(ctx context.Context, toolName string, turn int) (continueLoop bool)
}

// In agent loop after doom loop detection:
if r.doomLoop.Check(toolCall.Name, normalizeJSON(inputJSON)) {
    if req.OnDoomLoop != nil {
        if !req.OnDoomLoop(ctx, toolCall.Name, turn) {
            return AgentResult{}, ErrDoomLoopAborted
        }
    } else {
        messages = append(messages, warningMsg)
    }
}

// internal/agent/doomloop.go — add normalization
func normalizeJSON(input []byte) string {
    var m map[string]interface{}
    if err := json.Unmarshal(input, &m); err != nil {
        return string(input)
    }
    // Go's json.Marshal uses sorted keys for map types
    b, _ := json.Marshal(m)
    return string(b)
}
```

### I3 — Post-Compaction Re-Prompt

```go
// internal/agent/builtin.go — after any compaction phase completes:
messages = append(messages, summaryMessage)
messages = append(messages, models.Message{
    Role:    "user",
    Content: "Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed.",
})
```

### I2 — Wire TaskManager for Sub-Agent Visibility

```go
// internal/agent/tools/exec.go — in subagentTool.Execute()
taskID := uuid.New().String()
if tm := t.registry.TaskManager(); tm != nil {
    tm.AddTask(taskID, in.Task, "running")
    defer func() {
        if err != nil {
            tm.UpdateTask(taskID, "failed", err.Error())
        } else {
            tm.UpdateTask(taskID, "completed", result)
        }
    }()
}
result, err := runFn(ctx, in.Task, workDir, in.Mode, in.Tools, maxTurns, parentBudget, parentDepth+1)
return fmt.Sprintf("[task_id: %s]\n%s", taskID, result), err
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Priority |
|---|-------------|--------|----------|
| 1 | Post-compaction re-prompt (I4) | Very low | High — agent momentum |
| 2 | Doom loop escalation + JSON normalization (I3) | Low | High — prevents budget waste |
| 3 | Wire TaskManager to Subagent tool (I2) | Low | Medium |
| 4 | Parallel sub-agent fan-out (I1) | Medium | Medium |
| 5 | DB session hierarchy for sub-agents (I5) | Medium | Low |
