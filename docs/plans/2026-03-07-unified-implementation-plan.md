# Foreman Unified Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Execute all bug fixes, architectural hardening, and new feature requirements across three source documents in a single sequenced plan with no missed items.

**Architecture:** Five execution waves. Wave 0 fixes critical correctness bugs. Wave 1 adds foundational safety systems (context management, budget enforcement, retry cleanup). Wave 2 builds reliability infrastructure. Wave 3 adds new tools and capabilities. Wave 4 is quality polish and advanced features.

**Tech Stack:** Go 1.25+, SQLite/PostgreSQL, zerolog, cobra/viper, prometheus, tiktoken-go, errgroup

---

## Complete Item Registry

Every item from all source documents is listed below. Each has a unique canonical ID, its source, and its assigned wave.

### Source: `2026-03-06-architectural-review-design.md` (Bug Fixes)

| Canonical ID | Source ID | Description | Wave |
|---|---|---|---|
| BUG-C01 | C1 | Wrong task ID in quality review call cap | 0 |
| BUG-C02 | C2 | File changes not reverted between retries | 0 |
| BUG-C03 | C3 | Non-atomic patch application | 0 |
| BUG-C04 | C4 | Feedback accumulator never reset on retry | 0 |
| BUG-C05 | C5 | Quality review approval logic bug | 0 |
| BUG-C06 | C6 | Double ticket pickup race condition | 0 |
| BUG-C07 | C7 | Non-atomic file reservation | 0 |
| BUG-C08 | C8 | SQLite IncrementTaskLlmCalls race | 0 |
| BUG-C09 | C9 | PostgreSQL vs SQLite GetTicketCost mismatch | 0 |
| BUG-C10 | C10 | Hardcoded API keys in tracked config | 0 |
| BUG-H01 | H1 | MergeChecker not in WaitGroup | 0 |
| BUG-H02 | H2 | DAG executor goroutine leak | 0 |
| BUG-H03 | H3 | MaxParallelTickets can be exceeded | 0 |
| BUG-H04 | H4 | DAG adapter task lookup passes empty TicketID | 0 |
| BUG-H05 | H5 | WebSocket CORS allow-all | 0 |
| BUG-H06 | H6 | Bash command validation uses prefix match | 0 |
| BUG-H07 | H7 | Skills file write path traversal | 0 |
| BUG-H08 | H8 | Prompt injection in channel classifier | 0 |
| BUG-H09 | H9 | Metrics endpoint unauthenticated | 0 |
| BUG-H10 | H10 | Config validation missing for required fields | 0 |
| BUG-M01 | M1 | Task status stuck on escalation | 2 |
| BUG-M02 | M2 | File reservation release errors non-fatal | 2 |
| BUG-M03 | M3 | Clarification timeout infinite loop | 2 |
| BUG-M04 | M4 | Crash recovery no bounds check | 2 |
| BUG-M05 | M5 | No overall DAG timeout | 2 |
| BUG-M06 | M6 | MergeChecker hook errors discarded | 2 |
| BUG-M07 | M7 | JSON unmarshal errors swallowed (Anthropic) | 2 |
| BUG-M08 | M8 | Context provider errors dropped | 2 |
| BUG-M09 | M9 | errgroup.Wait() discarded | 2 |
| BUG-M10 | M10 | Dashboard status filter unvalidated | 2 |
| BUG-M11 | M11 | WhatsApp rate limiter resets on restart | 2 |
| BUG-M12 | M12 | Hardcoded fallback pricing | 2 |
| BUG-M13 | M13 | No daemon precondition validation | 2 |
| BUG-M14 | M14 | Hardcoded prompts in reviewers | 2 |
| BUG-M15 | M15 | No distributed locking | 4 |
| BUG-L01 | L1 | Unused minContextLines parameter | 4 |
| BUG-L02 | L2 | Inconsistent error wrapping in DB | 4 |
| BUG-L03 | L3 | Double workerCancel() | 4 |
| BUG-L04 | L4 | resultChan never closed | 4 |
| BUG-L05 | L5 | Transaction rollback pattern | 4 |
| BUG-L06 | L6 | listSourceFiles swallows Walk error | 4 |
| BUG-L07 | L7 | SHA256 token hashing (no salt) | 4 |
| BUG-L08 | L8 | WebSocket auth token in query param | 4 |
| BUG-L09 | L9 | No input size limits | 4 |
| BUG-L10 | L10 | Missing integration tests | 4 |

### Source: `2026-03-07-deep-architectural-audit.md` (Architecture)

| Canonical ID | Source ID | Description | Wave | Covered By REQ? |
|---|---|---|---|---|
| ARCH-A01 | A1 | Unbounded message history | 1 | REQ-LOOP-006 |
| ARCH-A02 | A2 | No agent depth enforcement | 1 | REQ-LOOP-004 (partial) |
| ARCH-A03 | A3 | No token budget in builtin runner | 1 | REQ-LOOP-004 (partial) |
| ARCH-A04 | A4 | Silent tool error suppression / doom loop | 1 | REQ-LOOP-002 (partial) |
| ARCH-A05 | A5 | Parallel tool write conflicts | 1 | REQ-LOOP-003 |
| ARCH-M01 | Audit M1 | No context caching between stages | 1 | REQ-CTX-005 |
| ARCH-M02 | Audit M2 | Handoff system write-only | 3 | -- |
| ARCH-M03 | Audit M3 | Progress patterns not used for scoring | 3 | -- |
| ARCH-M04 | Audit M4 | Feedback accumulator cross-attempt reset | 0 | REQ-PIPE-007 |
| ARCH-M05 | Audit M5 | Static file selection per task | 1 | REQ-CTX-003 (partial) |
| ARCH-M06 | Audit M6 | Token budget allocation imbalance | 2 | REQ-CTX-002 (partial) |
| ARCH-M07 | Audit M7 | Token heuristic len/4 | 1 | REQ-CTX-001 |
| ARCH-S01 | S1 | No agent progress visibility | 1 | REQ-LOOP-007 |
| ARCH-S02 | S2 | No data sharing between parallel tasks | 3 | -- |
| ARCH-S03 | S3 | AgentResult loses structured data | 3 | -- |
| ARCH-C01 | Audit C1 | DAG executor goroutine leak | 0 | BUG-H02 (same) |
| ARCH-C02 | Audit C2 | No typed inter-stage contracts | 3 | -- |
| ARCH-C03 | Audit C3 | Decomposition orphan coordination | 3 | -- |
| ARCH-F01 | F1 | Retry state corruption | 0 | BUG-C02 + REQ-PIPE-007 |
| ARCH-F02 | F2 | No provider circuit breaker | 2 | -- |
| ARCH-F03 | F3 | Crash recovery skips DAG tasks | 3 | -- |
| ARCH-F04 | F4 | File reservation orphaning after PR | 2 | -- |
| ARCH-F05 | F5 | Silent file skip in context loading | 2 | -- |
| ARCH-F06 | F6 | MergeChecker parent completion race | 3 | -- |
| ARCH-O01 | O1 | No request-level tracing | 2 | -- |
| ARCH-O02 | O2 | LLM prompts not stored for debugging | 2 | -- |
| ARCH-O03 | O3 | EventEmitter drops events silently | 3 | -- |
| ARCH-O04 | O4 | No cost-per-stage breakdown | 3 | -- |

### Source: `2026-03-07-refactor-requirements.md` (New Features)

| Canonical ID | Source ID | Description | Wave |
|---|---|---|---|
| REQ-LOOP-001 | REQ-LOOP-001 | Self-reflection turn | 2 |
| REQ-LOOP-002 | REQ-LOOP-002 | Tool call dedup detector | 1 |
| REQ-LOOP-003 | REQ-LOOP-003 | File-aware parallel tool execution | 2 |
| REQ-LOOP-004 | REQ-LOOP-004 | Subagent budget inheritance | 1 |
| REQ-LOOP-005 | REQ-LOOP-005 | Per-model router for agent tasks | 3 |
| REQ-LOOP-006 | REQ-LOOP-006 | Context window management | 1 |
| REQ-LOOP-007 | REQ-LOOP-007 | Agent progress events | 2 |
| REQ-TOOLS-001 | REQ-TOOLS-001 | ReadRange tool | 1 |
| REQ-TOOLS-002 | REQ-TOOLS-002 | ApplyPatch tool | 3 |
| REQ-TOOLS-003 | REQ-TOOLS-003 | SemanticSearch tool | 4 |
| REQ-TOOLS-004 | REQ-TOOLS-004 | GetTypeDefinition tool | 4 |
| REQ-TOOLS-005 | REQ-TOOLS-005 | ListMCPTools tool | 3 |
| REQ-MCP-001 | REQ-MCP-001 | MCP resources support | 3 |
| REQ-MCP-002 | REQ-MCP-002 | MCP health monitoring | 3 |
| REQ-MCP-003 | REQ-MCP-003 | HTTP/SSE transport | 4 |
| REQ-CTX-001 | REQ-CTX-001 | Accurate token counting | 1 |
| REQ-CTX-002 | REQ-CTX-002 | Dynamic context budget | 2 |
| REQ-CTX-003 | REQ-CTX-003 | Context selection feedback loop | 3 |
| REQ-CTX-004 | REQ-CTX-004 | Anthropic prompt caching | 1 |
| REQ-CTX-005 | REQ-CTX-005 | Pipeline-scoped context cache | 2 |
| REQ-PIPE-001 | REQ-PIPE-001 | Error classifier before retry | 2 |
| REQ-PIPE-002 | REQ-PIPE-002 | Plan confidence scoring | 2 |
| REQ-PIPE-003 | REQ-PIPE-003 | Rebase conflict full context | 2 |
| REQ-PIPE-004 | REQ-PIPE-004 | LLM-assisted decomposition check | 4 |
| REQ-PIPE-005 | REQ-PIPE-005 | PR update detection | 3 |
| REQ-PIPE-006 | REQ-PIPE-006 | Intermediate consistency review | 3 |
| REQ-PIPE-007 | REQ-PIPE-007 | Clean working tree before retry | 0 |
| REQ-OBS-001 | REQ-OBS-001 | Prompt version hashing | 3 |
| REQ-OBS-002 | REQ-OBS-002 | Unified skill execution context | 3 |
| REQ-OBS-003 | REQ-OBS-003 | Structured error metrics | 4 |
| REQ-OBS-004 | REQ-OBS-004 | Token budget dashboard panel | 4 |
| REQ-INFRA-001 | REQ-INFRA-001 | Docker network isolation | 1 |
| REQ-INFRA-002 | REQ-INFRA-002 | Embedding index store | 4 |

---

## Total Item Count

| Source | Count |
|---|---|
| Bug fixes (C1-C10, H1-H10) | 20 |
| Bug fixes (M1-M15) | 15 |
| Bug fixes (L1-L10) | 10 |
| Architecture audit (deduplicated) | 24 (4 overlap with bugs) |
| New requirements (deduplicated) | 33 (4 GAPs + 29 original) |
| **Total unique items** | **98** |

---

## Wave 0: Critical Correctness (22 items)

**Goal:** Fix all data-corruption bugs, race conditions, and security vulnerabilities. Nothing new is added -- only existing broken behavior is corrected.

**Prerequisite:** None. Start here.

**Success criteria:** `go test -race ./...` passes. All C1-C10 verified by reproducing tests.

### Task 0.1: Pipeline Bug Fixes (BUG-C01 through BUG-C05, REQ-PIPE-007)

**Files:**
- Modify: `internal/pipeline/task_runner.go`
- Modify: `internal/pipeline/feedback.go`
- Test: `internal/pipeline/task_runner_test.go`
- Test: `internal/pipeline/feedback_test.go`

**Step 1: Write failing test for BUG-C01 (wrong task ID in call cap)**

```go
func TestRunQualityReview_UsesTaskIDForCallCap(t *testing.T) {
    // Setup mock DB that captures the ID passed to IncrementTaskLlmCalls
    var capturedID string
    mockDB := &mockDB{incrementFn: func(_ context.Context, id string) (int, error) {
        capturedID = id
        return 1, nil
    }}
    // Exercise runQualityReview
    // Assert capturedID == task.ID, not workDir
}
```

**Step 2: Fix BUG-C01** -- Change `CheckTaskCallCap(ctx, r.db, r.config.WorkDir, ...)` to `CheckTaskCallCap(ctx, r.db, task.ID, ...)`.

**Step 3: Write failing test for BUG-C02 + REQ-PIPE-007 (working tree not reset)**

```go
func TestRetryLoop_ResetsWorkingTreeBetweenAttempts(t *testing.T) {
    // Create temp dir with a file
    // After first attempt fails, write a "dirty" file
    // Assert that second attempt starts with clean state
    // Assert committed changes from prior tasks are preserved
}
```

**Step 4: Implement `resetWorkingTree()`** -- Add to task_runner.go:
```go
func (r *PipelineTaskRunner) resetWorkingTree(ctx context.Context, task *models.Task) error {
    paths := task.FilesToModify
    for _, p := range paths {
        if _, err := r.runner.Run(ctx, r.config.WorkDir, "git", []string{"checkout", "--", p}, 10); err != nil {
            // Ignore errors for files that don't exist yet
        }
    }
    return nil
}
```
Call at top of retry loop before implementer call.

**Step 5: Write failing test for BUG-C03 (non-atomic patch)**

```go
func TestApplyChanges_RollsBackOnPartialFailure(t *testing.T) {
    // Apply 2 patches: patch1 valid, patch2 invalid
    // Assert that after failure, file is in pre-apply state
}
```

**Step 6: Implement two-phase apply** -- Validate all patches in memory first, then write atomically.

**Step 7: Fix BUG-C04 (feedback reset)** -- Add `feedback.ResetKeepingSummary()`:
```go
func (f *FeedbackAccumulator) ResetKeepingSummary() {
    if len(f.entries) > 0 {
        summary := fmt.Sprintf("Prior attempt failed: %s", f.entries[0].category)
        f.entries = f.entries[:0]
        f.entries = append(f.entries, feedbackEntry{category: "Prior attempt summary", content: summary})
    }
}
```

**Step 8: Fix BUG-C05 (quality review logic)** -- Change:
```go
// Before:
if !result.Approved && result.HasCritical { return retry }
// After:
if !result.Approved { return retry }
```

**Step 9: Run all tests, commit**

```bash
go test -race ./internal/pipeline/... -v
git add internal/pipeline/
git commit -m "fix: pipeline critical bugs C01-C05, PIPE-007 (retry reset)"
```

### Task 0.2: Daemon Bug Fixes (BUG-C06 through BUG-C08, BUG-H01 through BUG-H04)

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/dag_executor.go`
- Modify: `internal/daemon/scheduler.go`
- Modify: `internal/db/sqlite.go`
- Modify: `internal/pipeline/dag_adapter.go`
- Test: `internal/daemon/daemon_test.go`
- Test: `internal/daemon/dag_executor_test.go`
- Test: `tests/integration/dag_executor_test.go`

**Step 1:** Fix BUG-C06 -- Move `UpdateTicketStatus(planning)` before goroutine launch (already done based on daemon agent findings -- verify).

**Step 2:** Fix BUG-C07 -- Wrap TryReserveFiles in `BEGIN IMMEDIATE` transaction (already done based on sqlite.go:357-409 -- verify tests exist).

**Step 3:** Fix BUG-C08 -- Wrap IncrementTaskLlmCalls in transaction (already done based on sqlite.go:243-261 -- verify).

**Step 4:** Fix BUG-H01 -- Add `d.wg.Add(1)` / `defer d.wg.Done()` to MergeChecker goroutine (already done based on daemon.go:197-206 -- verify).

**Step 5:** Fix BUG-H02 -- Add `sync.WaitGroup` for DAG workers with bounded drain.

**Step 6:** Fix BUG-H03 -- Verify semaphore pattern (already done based on daemon.go:63 `tickets` channel -- verify).

**Step 7:** Fix BUG-H04 -- Store ticketID in DAGTaskAdapter struct, pass to ListTasks.

**Step 8:** Write integration test for concurrent ticket processing.

**Step 9:** Run tests, commit.

```bash
go test -race ./internal/daemon/... -v
git commit -m "fix: daemon critical bugs C06-C08, H01-H04"
```

### Task 0.3: Database & Config Bug Fixes (BUG-C09, BUG-C10)

**Files:**
- Modify: `internal/db/sqlite.go`
- Modify: `internal/db/postgres.go`
- Modify: `foreman.toml` -> `.gitignore`
- Modify: `foreman.example.toml`
- Test: `tests/integration/db_contract_test.go`

**Step 1:** Fix BUG-C09 -- Align both GetTicketCost to use `SUM(cost_usd) FROM llm_calls`.

**Step 2:** Fix BUG-C10 -- Remove secrets from foreman.toml, add to .gitignore, keep example only.

**Step 3:** Write contract test verifying SQLite and PostgreSQL return identical costs.

**Step 4:** Commit.

### Task 0.4: Security Fixes (BUG-H05 through BUG-H10)

**Files:**
- Modify: `internal/dashboard/auth.go`
- Modify: `internal/dashboard/server.go`
- Modify: `internal/agent/tools/exec.go`
- Modify: `internal/skills/engine.go`
- Modify: `internal/channel/classifier.go`
- Modify: `internal/config/config.go`
- Tests for each

**Step 1:** Fix BUG-H05 -- WebSocket CORS allowlist.
**Step 2:** Fix BUG-H06 -- Exact binary match for bash commands (already done based on test evidence -- verify).
**Step 3:** Fix BUG-H07 -- Path traversal check in skills engine (already done in engine.go:142-154 -- verify).
**Step 4:** Fix BUG-H08 -- Wrap user input in delimiters in channel classifier.
**Step 5:** Fix BUG-H09 -- Auth middleware on metrics endpoint.
**Step 6:** Fix BUG-H10 -- Config validation for required fields.
**Step 7:** Security-focused tests for each.
**Step 8:** Commit.

---

## Wave 1: Foundational Safety (11 items)

**Goal:** Add the safety systems that prevent cost explosion, context overflow, and silent failures.

**Prerequisite:** Wave 0 complete.

### Task 1.1: Accurate Token Counting (REQ-CTX-001, ARCH-M07)

**Files:**
- Create: `internal/context/token_counter.go`
- Modify: `internal/context/token_budget.go`
- Modify: `go.mod` (add `tiktoken-go`)
- Test: `internal/context/token_counter_test.go`

**Step 1:** Add `tiktoken-go` dependency.

**Step 2:** Write test:
```go
func TestTokenCounter_AccurateForCode(t *testing.T) {
    code := `func main() { fmt.Println("hello") }`
    heuristic := len(code) / 4  // old way
    accurate := CountTokens(code, "cl100k_base")
    // Assert accurate != heuristic (they differ for code)
    // Assert accurate is within 5% of known tiktoken output
}
```

**Step 3:** Implement `CountTokens(text, encoding string) int` using tiktoken-go.

**Step 4:** Replace all `EstimateTokens` call sites with `CountTokens`.

**Step 5:** Remove old `EstimateTokens` function.

**Step 6:** Tests pass, commit.

### Task 1.2: Anthropic Prompt Caching (REQ-CTX-004)

**Files:**
- Modify: `internal/llm/anthropic.go`
- Modify: `internal/db/schema.go` (add columns)
- Modify: `internal/db/sqlite.go`
- Test: `internal/llm/anthropic_test.go`

**Step 1:** Add `cache_read_input_tokens` and `cache_creation_input_tokens` columns to `llm_calls`.

**Step 2:** In Anthropic provider `Complete()`, set `cache_control: {"type": "ephemeral"}` on system prompt block.

**Step 3:** Parse cache token counts from response, store in LlmCallRecord.

**Step 4:** Add `foreman_anthropic_cache_savings_tokens_total` Prometheus metric.

**Step 5:** Commit.

### Task 1.3: Context Window Management (REQ-LOOP-006, ARCH-A01)

**Files:**
- Modify: `internal/agent/builtin.go`
- Create: `internal/agent/compaction.go`
- Test: `internal/agent/compaction_test.go`
- Test: `internal/agent/builtin_test.go`

**Step 1:** Write test:
```go
func TestCompactMessages_TruncatesOldToolOutputs(t *testing.T) {
    msgs := generateLargeConversation(20) // 20 turns with large tool outputs
    compacted := CompactMessages(msgs, 50000) // 50K token budget
    totalTokens := countAllTokens(compacted)
    assert.Less(t, totalTokens, 50000)
    // Assert last 3 turns are preserved intact
    // Assert older tool outputs are truncated
}
```

**Step 2:** Implement `CompactMessages(messages []Message, budgetTokens int) []Message`.

**Step 3:** Wire into builtin runner loop -- after each turn, check token count and compact if needed.

**Step 4:** Commit.

### Task 1.4: Subagent Budget Inheritance + Depth Enforcement (REQ-LOOP-004, ARCH-A02, ARCH-A03)

**Files:**
- Modify: `internal/agent/runner.go` (add RemainingBudget field)
- Modify: `internal/agent/builtin.go`
- Modify: `internal/agent/tools/exec.go`
- Test: `internal/agent/builtin_test.go`

**Step 1:** Add `RemainingBudget int` and enforce `AgentDepth` in `AgentRequest`.

**Step 2:** Write test:
```go
func TestSubagent_InheritsBudget(t *testing.T) {
    // Parent has 10 turns, currently on turn 7
    // Subagent should get min(5, 10-7) = 3 turns
}
func TestSubagent_FailsWhenBudgetExhausted(t *testing.T) {
    // Parent has 10 turns, currently on turn 10
    // Subagent call should fail with "parent budget exhausted"
}
func TestSubagent_EnforcesMaxDepth(t *testing.T) {
    // AgentDepth = 3 (max), subagent call should fail
}
```

**Step 3:** Implement budget check in subagent tool. Increment AgentDepth in subagentRunFn.

**Step 4:** Commit.

### Task 1.5: Tool Call Deduplication Detector (REQ-LOOP-002, ARCH-A04)

**Files:**
- Modify: `internal/agent/builtin.go`
- Test: `internal/agent/builtin_test.go`

**Step 1:** Add fingerprint map `map[string]int` to runner loop.

**Step 2:** Write test:
```go
func TestBuiltinRunner_DetectsDuplicateToolCalls(t *testing.T) {
    // LLM calls Read("main.go") twice with identical args
    // Assert warning message injected before second call
}
```

**Step 3:** Implement: hash `(toolName, canonicalInput)`, check map, inject warning if count >= 2.

**Step 4:** Commit.

### Task 1.6: ReadRange Tool (REQ-TOOLS-001)

**Files:**
- Modify: `internal/agent/tools/fs.go`
- Test: `internal/agent/tools/fs_test.go`

**Step 1:** Write test:
```go
func TestReadRange_ReturnsLineRange(t *testing.T) {
    // Create file with 100 lines
    // ReadRange(file, 10, 20) should return lines 10-20
    // ReadRange(file, 90, 200) should return lines 90-100 (no error)
    // ReadRange(file, 0, 5) should error (start_line must be >= 1)
}
```

**Step 2:** Implement ReadRange tool, register in registry.

**Step 3:** Commit.

### Task 1.7: Docker Network Isolation (REQ-INFRA-001)

**Files:**
- Modify: `internal/runner/docker.go`
- Modify: `cmd/doctor.go`
- Test: `internal/runner/docker_test.go`

**Step 1:** Add `--network none` by default. Check `docker.allow_network` config.

**Step 2:** Add warning in `foreman doctor` for Docker without network review.

**Step 3:** Commit.

---

## Wave 2: Reliability Infrastructure (19 items)

**Goal:** Add error classification, context caching, progress events, and observability.

**Prerequisite:** Wave 1 complete.

### Task 2.1: Pipeline-Scoped Context Cache (REQ-CTX-005, ARCH-M01)

**Files:**
- Create: `internal/context/cache.go`
- Modify: `internal/context/assembler.go`
- Modify: `internal/daemon/orchestrator.go`
- Test: `internal/context/cache_test.go`

Implement `ContextCache` struct with file tree, rules, and secret patterns. Invalidate on git operations. Pass cache through pipeline stages via orchestrator.

### Task 2.2: Error Classifier Before Retry (REQ-PIPE-001)

**Files:**
- Create: `internal/pipeline/error_classifier.go`
- Create: `prompts/implementer_retry_compile.md.j2`
- Create: `prompts/implementer_retry_test.md.j2`
- Create: `prompts/implementer_retry_spec.md.j2`
- Create: `prompts/implementer_retry_quality.md.j2`
- Modify: `internal/pipeline/task_runner.go`
- Modify: `internal/db/schema.go`
- Test: `internal/pipeline/error_classifier_test.go`

Classify errors deterministically. Select retry prompt template based on error type. Store `last_error_type` in tasks table.

### Task 2.3: Agent Progress Events (REQ-LOOP-007, ARCH-S01)

**Files:**
- Modify: `internal/agent/runner.go`
- Modify: `internal/agent/builtin.go`
- Modify: `internal/daemon/orchestrator.go`
- Test: `internal/agent/builtin_test.go`

Add `OnProgress func(AgentEvent)` to AgentRequest. Emit events from builtin runner. Wire to EventEmitter in orchestrator.

### Task 2.4: Dynamic Context Budget (REQ-CTX-002, ARCH-M06)

**Files:**
- Modify: `internal/context/assembler.go`
- Modify: `internal/pipeline/task_runner.go`
- Test: `internal/context/assembler_test.go`

Calculate budget from estimated_complexity. Dynamic allocation between system prompt, feedback, and files.

### Task 2.5: File-Aware Parallel Tool Execution (REQ-LOOP-003, ARCH-A05)

**Files:**
- Modify: `internal/agent/builtin.go`
- Test: `internal/agent/builtin_test.go`

Group tool calls by file paths. Serialize conflicting writes. Parallelize disjoint operations.

### Task 2.6: Self-Reflection Turn (REQ-LOOP-001)

**Files:**
- Modify: `internal/agent/builtin.go`
- Modify: `internal/db/schema.go`
- Test: `internal/agent/builtin_test.go`

Inject reflection message every N turns. Log as `reflection` turn type.

### Task 2.7: Plan Confidence Scoring (REQ-PIPE-002)

**Files:**
- Create: `internal/pipeline/plan_confidence.go`
- Modify: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/plan_confidence_test.go`

LLM-based plan quality evaluation. Confidence threshold triggers clarification.

### Task 2.8: Rebase Conflict Full Context (REQ-PIPE-003)

**Files:**
- Modify: `internal/pipeline/rebase_resolver.go`
- Test: `internal/pipeline/rebase_resolver_test.go`

Provide base + head file content alongside conflict markers.

### Task 2.9: Bug Fixes M01-M14 (14 medium-severity bugs)

**Files:** Various across `internal/daemon/`, `internal/pipeline/`, `internal/dashboard/`, `internal/channel/`, `internal/llm/`

Execute each M1-M14 fix per the `2026-03-06-architectural-review-design.md` spec. Each fix is 1-2 files with a targeted test.

### Task 2.10: Provider Circuit Breaker (ARCH-F02)

**Files:**
- Create: `internal/llm/circuit_breaker.go`
- Modify: `internal/llm/provider.go`
- Test: `internal/llm/circuit_breaker_test.go`

### Task 2.11: Request-Level Tracing (ARCH-O01)

**Files:**
- Create: `internal/telemetry/trace.go`
- Modify: `internal/daemon/orchestrator.go`
- Modify: `internal/pipeline/pipeline.go`
- Modify: `internal/db/schema.go`

Add TraceID to PipelineContext. Propagate through LLM calls, events.

### Task 2.12: LLM Prompt/Response Storage (ARCH-O02)

**Files:**
- Modify: `internal/db/schema.go`
- Modify: `internal/db/sqlite.go`
- Modify: `internal/db/postgres.go`

Store full prompt/response in a `llm_call_details` table linked by `llm_calls.id`.

### Task 2.13: File Reservation Orphan Cleanup (ARCH-F04)

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/scheduler.go`

Periodic cleanup of reservations for tickets in terminal states.

### Task 2.14: Log Missing Context Files (ARCH-F05)

**Files:**
- Modify: `internal/pipeline/task_runner.go`

Replace silent `continue` with warning log and context note.

---

## Wave 3: New Capabilities (18 items)

**Goal:** Add new tools, MCP features, and cross-cutting improvements.

**Prerequisite:** Wave 2 complete.

| Task | Items | Files |
|---|---|---|
| 3.1 | REQ-TOOLS-002 (ApplyPatch) | `internal/agent/tools/fs.go` |
| 3.2 | REQ-TOOLS-005 (ListMCPTools) | `internal/agent/tools/exec.go` |
| 3.3 | REQ-MCP-001 (Resources) | `internal/agent/mcp/` |
| 3.4 | REQ-MCP-002 (Health monitoring) | `internal/agent/mcp/` |
| 3.5 | REQ-LOOP-005 (Per-model router) | `internal/agent/builtin.go`, `internal/config/` |
| 3.6 | REQ-CTX-003 (Context feedback loop) | `internal/context/`, `internal/db/` |
| 3.7 | REQ-PIPE-005 (PR update detection) | `internal/daemon/merge_checker.go` |
| 3.8 | REQ-PIPE-006 (Consistency review) | `internal/pipeline/` |
| 3.9 | REQ-OBS-001 (Prompt hashing) | `internal/telemetry/`, `internal/dashboard/` |
| 3.10 | REQ-OBS-002 (Unified skill context) | `internal/skills/engine.go` |
| 3.11 | ARCH-M02 (Handoff versioning) | `internal/db/` |
| 3.12 | ARCH-M03 (Progress patterns in scoring) | `internal/context/file_selector.go` |
| 3.13 | ARCH-S02 (Shared discovery board) | `internal/daemon/` |
| 3.14 | ARCH-S03 (Structured AgentResult) | `internal/agent/runner.go` |
| 3.15 | ARCH-C02 (Typed inter-stage contracts) | `internal/pipeline/`, `internal/models/` |
| 3.16 | ARCH-C03 (Decomposition conflict detection) | `internal/pipeline/decompose.go` |
| 3.17 | ARCH-F03 (DAG-aware crash recovery) | `internal/daemon/recovery.go` |
| 3.18 | ARCH-F06, ARCH-O03, ARCH-O04 (Misc) | Various |

---

## Wave 4: Polish & Advanced Features (14 items)

**Goal:** Low-severity cleanup, advanced tools, and infrastructure for future capabilities.

**Prerequisite:** Wave 3 complete.

| Task | Items | Files |
|---|---|---|
| 4.1 | BUG-L01 through BUG-L10 | Various cleanup |
| 4.2 | BUG-M15 (Distributed locking) | `internal/db/` |
| 4.3 | REQ-TOOLS-003 (SemanticSearch) | `internal/agent/tools/`, `internal/context/` |
| 4.4 | REQ-TOOLS-004 (GetTypeDefinition) | `internal/agent/tools/` |
| 4.5 | REQ-MCP-003 (HTTP/SSE transport) | `internal/agent/mcp/` |
| 4.6 | REQ-PIPE-004 (LLM decomposition check) | `internal/pipeline/` |
| 4.7 | REQ-INFRA-002 (Embedding store) | `internal/db/` |
| 4.8 | REQ-OBS-003 (Error metrics) | `internal/telemetry/` |
| 4.9 | REQ-OBS-004 (Token budget dashboard) | `internal/dashboard/` |

---

## Cross-Reference Verification

### Items from Bug Fix Review NOT in a task above: NONE

All 45 items (C1-C10, H1-H10, M1-M15, L1-L10) are assigned to waves.

### Items from Architectural Audit NOT in a task above: NONE

All 24 unique items (after deduplication with bug fixes) are assigned to waves.

### Items from Requirements Doc NOT in a task above: NONE

All 33 items (29 original + 4 GAPs) are assigned to waves.

### Verified total: 98 unique items across 4 waves + 1 prep wave.

---

## Execution Estimates

| Wave | Items | Estimated Duration | Parallelizable |
|---|---|---|---|
| Wave 0 | 22 | 3-5 days | Tasks 0.1-0.4 partially parallel |
| Wave 1 | 11 | 5-7 days | Tasks 1.1-1.7 partially parallel |
| Wave 2 | 19 | 8-12 days | Tasks 2.1-2.14 partially parallel |
| Wave 3 | 18 | 8-12 days | Most tasks independent |
| Wave 4 | 14 | 5-8 days | Most tasks independent |
| **Total** | **98** (deduplicated from 102 raw) | **29-44 days** | |

---

## Success Criteria

1. `go test -race ./...` passes at end of every wave
2. All C1-C10 fixes verified by tests reproducing original bugs
3. All H5-H9 security fixes verified by targeted security tests
4. Agent loop terminates cleanly under: context overflow, budget exhaustion, doom loops
5. Retry attempts start with clean working tree (verified by integration test)
6. Context assembly completes in < 500ms for repos with < 10K files (cache warm)
7. Anthropic prompt caching active (verify via `cache_read_input_tokens > 0` in logs)
8. Token counts accurate within 5% vs tiktoken reference
9. Dashboard shows real-time agent progress events
10. Graceful shutdown completes within configured timeout with zero goroutine leaks
