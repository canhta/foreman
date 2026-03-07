# Foreman -- Complete Refactor Requirements

**Date:** 2026-03-07
**Status:** Approved for implementation
**Predecessor:** `2026-03-07-deep-architectural-audit.md` (bug fixes), `2026-03-07-deep-architectural-audit.md` (architecture)

---

## SECTION 1: Agent Loop

### REQ-LOOP-001 -- Self-Reflection Turn After File Operations
**Current:** The builtin runner loops `(call -> tools -> call)` with no intermediate assessment step.
**Requirement:** After every N turns (configurable, default: 5), inject a structured reflection message before the next LLM call:
> *"Summarize what you have accomplished, what files you have changed, and what remains. Do you have sufficient context to complete the task, or do you still need to gather information?"*

The reflection response must be logged as a distinct turn type (`reflection`) in the `llm_calls` table. If the reflection indicates the task is complete but no stop signal was emitted, treat it as an implicit stop.

### REQ-LOOP-002 -- Tool Call Deduplication Detector
**Current:** No detection of redundant tool calls within a session.
**Requirement:** The builtin runner must maintain an in-memory call fingerprint map keyed by `(tool_name, canonical_args_hash)`. If the same fingerprint appears >= 2 times in the same agent session, inject a warning message before the next LLM turn:
> *"You have already called [tool] with these arguments. Either the previous result was insufficient -- explain why and what is different this time -- or proceed using the information you already have."*

This must not hard-block execution; it is a guidance injection only.

### REQ-LOOP-003 -- File-Aware Parallel Tool Execution
**Current:** All tool calls in a single turn execute in parallel using `errgroup` with no dependency awareness.
**Requirement:** Before parallel execution, group tool calls by the set of file paths appearing in their arguments. Tool calls operating on disjoint file sets may execute in parallel. Tool calls sharing any file path must execute sequentially in the order returned by the LLM. No behavior change for non-filesystem tools.

### REQ-LOOP-004 -- Subagent Budget Inheritance
**Current:** `Subagent` tool spawns a sub-agent with independent `MaxTurns`, with no relationship to the parent budget.
**Requirement:** `AgentRequest` must include a new field `RemainingBudget int`. When the builtin runner spawns a `Subagent`, the subagent's `MaxTurns` must be set to `min(subagent_max_turns_default, parent.RemainingTurns - currentTurn)`. If the computed value is <= 0, the `Subagent` call must fail immediately with error `"parent budget exhausted"`.

### REQ-LOOP-005 -- Per-Model Router for Agent Tasks
**Current:** The builtin runner uses the implementer model as default with no way to configure a separate model for agent (skill) tasks.
**Requirement:** Add `[agent_runner.builtin] model = ""` config key. When set, the builtin runner must use this model for all LLM calls within agent sessions instead of falling through to the implementer model.

### REQ-LOOP-006 -- Context Window Management (GAP-1)
**Current:** Messages accumulate without pruning, windowing, or summarization in the builtin runner loop.
**Requirement:** After each turn, estimate total message history tokens. When exceeding 70% of the model's context window, truncate old tool outputs to summaries (keep first 200 chars + "... [truncated N chars]"). When exceeding 85%, generate a structured summary of completed work and replace all messages older than the last 3 turns with the summary. The summary must follow the template: Goal, Accomplished, Remaining, Relevant Files.

### REQ-LOOP-007 -- Agent Execution Progress Events (GAP-3)
**Current:** `AgentRunner.Run()` is a black box -- the orchestrator cannot monitor progress, enforce cost mid-execution, or report activity to the dashboard.
**Requirement:** `AgentRequest` must accept an optional `OnProgress func(AgentEvent)` callback. The builtin runner must emit events: `turn_start`, `tool_start`, `tool_end`, `turn_end` (with token counts and cost). The orchestrator must wire this to the `EventEmitter` for dashboard visibility and to the cost controller for mid-execution budget enforcement.

---

## SECTION 2: Tool Registry

### REQ-TOOLS-001 -- `ReadRange` Tool
**Current:** `Read` tool reads an entire file with no line range support, consuming full token budget regardless of file size.
**Requirement:** Add a new tool `ReadRange(file string, start_line int, end_line int) string`. The tool must validate that `start_line >= 1` and `end_line >= start_line`. If `end_line` exceeds the file length, return up to EOF without error. The existing `Read` tool must remain unchanged. Both tools must enforce path guard rules identically.

### REQ-TOOLS-002 -- `ApplyPatch` Tool (Unified Diff Format)
**Current:** `Edit` uses SEARCH/REPLACE blocks. Large multi-hunk edits require multiple `Edit` or `MultiEdit` calls.
**Requirement:** Add a new tool `ApplyPatch(file string, patch string)` that applies a unified diff format patch. Use a pure-Go unified diff applier as primary implementation (e.g., `sergi/go-diff`), with system `patch` command as optional optimization. On failure, return the specific rejection reason. Subject to the same path guard rules as `Edit`.

### REQ-TOOLS-003 -- `SemanticSearch` Tool
**Current:** `Grep` uses regex pattern matching only. There is no semantic/conceptual search.
**Requirement:** Add a new tool `SemanticSearch(query string, top_k int) []SearchResult` using embedding-based similarity search. On first invocation per ticket, build a file-chunk embedding index using `text-embedding-3-small` (OpenAI) or `voyage-code-3` (Anthropic). Cache the index in the database keyed by `(repo_path, HEAD_sha)`. `SearchResult` must include `{file, start_line, end_line, score, snippet}`. Disabled if no embedding model is configured. **Depends on:** REQ-INFRA-002.

### REQ-TOOLS-004 -- `GetTypeDefinition` Tool
**Current:** `GetSymbol` cannot retrieve interface/struct/type alias definitions.
**Requirement:** Add `GetTypeDefinition(symbol string, file string) string` that uses `go/types` (for Go repos) or `tree-sitter` (for other languages) to resolve and return the full type definition. For non-Go repos, fall back to regex-based heuristic extraction. Must handle cross-file resolution.

### REQ-TOOLS-005 -- `ListMCPTools` Tool
**Current:** Agents have no visibility into which MCP tools are available at runtime.
**Requirement:** Add a read-only tool `ListMCPTools() []MCPToolSummary` returning all registered MCP tools from the `Manager`, including `{normalized_name, original_name, server_name, description}`. Queries in-memory registry only.

---

## SECTION 3: MCP Infrastructure

### REQ-MCP-001 -- Resources Support (`resources/list` and `resources/read`)
**Current:** MCP integration is scoped to `tools` only.
**Requirement:** Implement `resources/list` and `resources/read` in `StdioClient`. The `Manager` must expose `ReadResource(ctx, serverName, uri string) (string, error)`. Add agent tool `ReadMCPResource(server, uri string) string`. Resource content must be subject to secrets scanning. Max response size configurable (`mcp_resource_max_bytes`, default: 512KB).

### REQ-MCP-002 -- MCP Server Health Monitoring
**Current:** No active health monitoring for MCP servers.
**Requirement:** `StdioClient` must send a `ping` request every `health_check_interval_secs` (default: 30). If ping does not respond within 5 seconds, mark `unhealthy`. After 3 consecutive failed pings, apply configured `restart_policy`. Health status exposed on dashboard REST API and WebSocket.

### REQ-MCP-003 -- HTTP/SSE Transport
**Current:** Only stdio transport is implemented.
**Requirement:** Implement HTTP+SSE MCP client connecting via `POST /messages` (sending) and `GET /sse` (receiving). Support `Authorization: Bearer <token>` headers. Config:
```toml
[[mcp.servers]]
name = "remote-db"
transport = "http"
url = "https://mcp.example.com"
auth_token = "${MCP_TOKEN}"
```

---

## SECTION 4: Context Assembly

### REQ-CTX-001 -- Accurate Token Counting (Replace `len/4` Heuristic)
**Current:** Token counting uses `len(text)/4` approximation everywhere.
**Requirement:** Replace all usages with provider-accurate counting:
- **All models:** Integrate `tiktoken-go` (`github.com/pkoukk/tiktoken-go`) using the model's encoding (cl100k_base for GPT-4/Claude, o200k_base for o-series).
- **Anthropic validation:** Optionally call `POST /v1/messages/count_tokens` for calibration at pipeline start.
- **Local/OpenRouter:** Fall back to cl100k_base via tiktoken-go.

The `len/4` heuristic must be fully removed.

### REQ-CTX-002 -- Dynamic Context Budget by Task Complexity
**Current:** `context_token_budget` is a flat global cap.
**Requirement:** Tasks receive dynamic context budget from `estimated_complexity`:
- `low` -> `context_token_budget * 0.5`
- `medium` -> `context_token_budget * 1.0`
- `high` -> `context_token_budget * 1.5`, capped at model context minus max_output_tokens

Default to `medium`. Log as `context_assembly` event.

### REQ-CTX-003 -- Context Selection Feedback Loop
**Current:** File scoring uses static heuristics with no learning.
**Requirement:** Add `context_feedback` table recording files_selected vs files_touched per task. After commit/failure, write a row. Context assembler queries this table to boost score of files appearing in `files_touched` but not `files_selected` for similar tasks (Jaccard similarity >= 0.3). Boost factor configurable (`context_feedback_boost`, default: 1.5).

### REQ-CTX-004 -- Anthropic Prompt Caching
**Current:** Anthropic API calls do not use `cache_control` headers.
**Requirement:** Apply `cache_control: {"type": "ephemeral"}` to system prompt blocks and context blocks exceeding 1024 tokens. Track `cache_read_input_tokens` and `cache_creation_input_tokens` in `llm_calls` table. Add Prometheus metric `foreman_anthropic_cache_savings_tokens_total`.

### REQ-CTX-005 -- Pipeline-Scoped Context Cache (GAP-4)
**Current:** Every pipeline stage rebuilds file tree, rules, secret scan from scratch.
**Requirement:** Introduce a `ContextCache` per pipeline that caches file tree, rules, and secret scan results at pipeline start. Invalidate only after git operations (commit, checkout, rebase). Share scored file relevance data between stages. Target: 80%+ reduction in redundant I/O per ticket.

---

## SECTION 5: Pipeline Hardening

### REQ-PIPE-001 -- Error Classifier Before Retry
**Current:** All retry feedback is raw unclassified output.
**Requirement:** Before every implementer retry, classify error type using deterministic parsing:
- `compile_error`, `type_error`, `lint_style`, `test_assertion`, `test_runtime`, `spec_violation`, `quality_concern`

Each error type gets a different retry prompt template. Classifier result stored in `tasks.last_error_type`. Metrics per error type via Prometheus.

### REQ-PIPE-002 -- Plan Confidence Scoring
**Current:** Plan validation is purely deterministic.
**Requirement:** After deterministic validation, make a second LLM call to evaluate plan quality. Return `confidence_score` (0.0-1.0) and `concerns`. If score < `plan_confidence_threshold` (default: 0.6), trigger clarification. Score stored in handoffs under key `plan_confidence`.

### REQ-PIPE-003 -- Rebase Conflict Resolution with Full File Context
**Current:** LLM conflict resolution receives only conflict markers.
**Requirement:** Provide full file content from base and head, plus task descriptions. Truncate in reverse priority if exceeding `conflict_resolution_token_budget` (default: 40,000).

### REQ-PIPE-004 -- LLM-Assisted Decomposition Check
**Current:** Decomposition is triggered by word count and scope keyword heuristics only.
**Requirement:** When heuristics do not trigger, run secondary LLM check if `decompose.llm_assist = true`. Heuristics take precedence for safety. Both signals recorded in events table.

### REQ-PIPE-005 -- PR Update Detection in `awaiting_merge`
**Current:** `MergeChecker` only polls for merge or close status.
**Requirement:** Store PR HEAD SHA at creation. Compare during polls. If changed, set status to `pr_updated`, emit event, send notification. Ticket requires manual re-labeling to re-enter pipeline.

### REQ-PIPE-006 -- Intermediate Cross-Task Consistency Review
**Current:** Final review runs only after all tasks complete.
**Requirement:** After every `intermediate_review_interval` completed tasks (default: 3), run lightweight LLM consistency check on cumulative diff. Check naming, error handling, import patterns only. Inject violations as `progress_patterns`. Does not block execution.

### REQ-PIPE-007 -- Clean Working Tree Before Retry (GAP-2)
**Current:** File changes from previous implementation attempt remain on disk during retries. Feedback accumulator loses cross-attempt learning.
**Requirement:** Before each implementation retry, run `git checkout -- .` and `git clean -fd` for task-relevant paths. Preserve committed changes from prior tasks. The feedback accumulator must retain a 1-line summary of prior attempt failures across resets (not full carry-forward, not full wipe).

---

## SECTION 6: Observability & Developer Experience

### REQ-OBS-001 -- Prompt Version Hashing
**Current:** Prompt template changes are not tracked.
**Requirement:** At startup, compute `SHA256(content)` for every `.md.j2` template. Store as `prompt_snapshot` row. Add `prompt_version` column to `llm_calls`. Add `GET /api/prompts/versions` endpoint.

### REQ-OBS-002 -- Unified Execution Context for Skills and Pipeline
**Current:** `agentsdk` skill steps run in isolation from pipeline state.
**Requirement:** Pass `PipelineContext` struct into skill step executors enabling handoff read/write, progress pattern writes, and structured event emission.

### REQ-OBS-003 -- Structured Error Classification Metrics
**Current:** No metrics on failure modes.
**Requirement:** Add Prometheus counters: `foreman_task_failures_total{error_type, runner}`, `foreman_retry_triggered_total{stage, error_type}`, `foreman_plan_confidence_score_histogram`, `foreman_context_cache_hit_ratio`, `foreman_mcp_tool_calls_total{server, tool, status}`.

### REQ-OBS-004 -- Token Budget Utilization Dashboard Panel
**Current:** Dashboard shows cost but not token budget utilization.
**Requirement:** Add `GET /api/tasks/{id}/context` endpoint returning budget, used, utilization_pct, files_selected, files_touched, cache_hits.

---

## SECTION 7: Infrastructure

### REQ-INFRA-001 -- Docker Runner: Enforce Network Isolation by Default
**Current:** Docker runner does not set `--network none` by default.
**Requirement:** Pass `--network none` unless `docker.allow_network = true`. `foreman doctor` must warn if Docker mode configured without reviewing network setting.

### REQ-INFRA-002 -- Embedding Index Store
**Current:** No vector/embedding storage infrastructure.
**Requirement:** Add `embeddings` table to database schema with brute-force cosine similarity for SQLite, optional pgvector for PostgreSQL. Required by REQ-TOOLS-003.

---

## Priority Matrix

| ID | Title | Effort | Impact | Priority |
|---|---|---|---|---|
| REQ-PIPE-007 | Clean working tree before retry | XS | Retry correctness | P0 |
| REQ-LOOP-006 | Context window management | M | Prevents hard failures | P0 |
| REQ-TOOLS-001 | `ReadRange` tool | XS | Token -40% | P0 |
| REQ-CTX-001 | Accurate token counting | M | Correctness | P0 |
| REQ-CTX-004 | Anthropic prompt caching | S | Cost -50% | P0 |
| REQ-LOOP-002 | Tool call dedup detector | S | Loop quality | P0 |
| REQ-LOOP-004 | Subagent budget inheritance | XS | Cost control | P0 |
| REQ-INFRA-001 | Docker network isolation default | XS | Security | P0 |
| REQ-LOOP-007 | Agent progress events | S | Monitoring | P1 |
| REQ-CTX-005 | Pipeline-scoped context cache | M | I/O -80% | P1 |
| REQ-PIPE-001 | Error classifier before retry | M | Retry quality | P1 |
| REQ-PIPE-003 | Rebase full-file conflict context | S | Merge reliability | P1 |
| REQ-MCP-001 | MCP resources support | M | Large file handling | P1 |
| REQ-LOOP-001 | Self-reflection turn | S | Agent quality | P1 |
| REQ-LOOP-003 | File-aware parallel tool execution | S | Correctness | P1 |
| REQ-PIPE-002 | Plan confidence scoring | M | Plan quality | P1 |
| REQ-CTX-002 | Dynamic context budget | S | Quality + cost | P1 |
| REQ-PIPE-005 | PR update detection | S | Observability | P2 |
| REQ-PIPE-006 | Intermediate consistency review | M | Output quality | P2 |
| REQ-CTX-003 | Context selection feedback loop | L | Long-term quality | P2 |
| REQ-TOOLS-002 | `ApplyPatch` tool | M | Large refactor support | P2 |
| REQ-TOOLS-003 | `SemanticSearch` tool | L | Discovery quality | P2 |
| REQ-MCP-002 | MCP health monitoring | M | Reliability | P2 |
| REQ-MCP-003 | HTTP/SSE transport | L | Remote MCP support | P2 |
| REQ-OBS-001 | Prompt version hashing | S | Debuggability | P2 |
| REQ-OBS-002 | Unified skill execution context | M | DX | P2 |
| REQ-LOOP-005 | Per-model router for agent tasks | XS | Cost control | P2 |
| REQ-TOOLS-005 | `ListMCPTools` tool | XS | DX | P2 |
| REQ-PIPE-004 | LLM-assisted decomposition check | M | Plan quality | P3 |
| REQ-TOOLS-004 | `GetTypeDefinition` tool | M | Code intelligence | P3 |
| REQ-OBS-003 | Structured error classification metrics | S | Observability | P3 |
| REQ-OBS-004 | Token budget dashboard panel | M | Observability | P3 |
| REQ-INFRA-002 | Embedding index store | L | Enables semantic search | P3 |
