# OpenCode Patterns Adoption — Design Document

## Problem

Foreman is a production daemon that orchestrates autonomous development workflows. While its core pipeline (planning → implementation → review → PR) is solid, several subsystems are less mature compared to OpenCode's battle-tested patterns. This document identifies what we can adopt from OpenCode to make Foreman more robust, efficient, and extensible.

## Analysis: OpenCode vs Foreman — Feature Comparison

### 1. Snapshot / Undo System

**OpenCode:** Has a dedicated `Snapshot` system that maintains a **separate git repo** purely for tracking file changes. Features:
- `track()` — snapshots current working tree state (creates git tree hash)
- `patch(hash)` — diffs current state against a snapshot to find changed files
- `restore(hash)` — reverts working tree to a snapshot
- `revert(patches)` — selectively reverts specific file changes
- `diff(hash)` / `diffFull(from, to)` — full before/after diff with per-file stats
- Scheduled cleanup via `Scheduler` (hourly GC, 7-day pruning)
- Stored in `$DATA_DIR/snapshot/<project-id>/` — separate from user's git

**Foreman:** No snapshot system. Uses git worktrees for isolation but has no undo/rollback mechanism within a task. If implementation fails badly, the whole worktree may need to be reset.

**Adoption value:** HIGH — Foreman could snapshot before each pipeline stage (pre-implementation, pre-review) enabling surgical rollback on failure instead of full worktree reset.

### 2. Worktree Management

**OpenCode:** Full worktree lifecycle management:
- `create()` — creates git worktree with random name, auto-bootstrap
- `remove()` — force-remove with cleanup, branch deletion
- `reset()` — hard reset to default branch, clean, submodule update
- Event-driven: `Worktree.Ready`, `Worktree.Failed` bus events
- Start commands: runs project's start command + custom worktree command
- Candidate generation with collision avoidance (26 attempts)

**Foreman:** Basic worktree creation via `git.CreateWorktree()`. No reset, no lifecycle events, no start commands.

**Adoption value:** MEDIUM — Foreman already uses worktrees but could benefit from richer lifecycle management and start command support.

### 3. Session & Context Compaction

**OpenCode:** Sophisticated compaction system:
- **Pruning** — backwards scan of old tool results, marks as compacted (tokens freed without losing conversation structure)
- **Summarization** — when tokens exceed model context minus buffer, triggers LLM to summarize history
- **Threshold-based** — auto-triggers at `context_size - buffer` tokens
- **Plugin hooks** — `experimental.session.compacting` allows override
- **Replay** — after compaction, replays the last user message for continuity

**Foreman builtin runner:** Has two-phase compaction:
- Phase 1 (70% budget): truncate old tool outputs
- Phase 2 (85% budget): LLM summarization
- Reflection prompts every N turns

**Adoption value:** MEDIUM — Foreman's compaction is simpler but functional. Could adopt the pruning-first approach and replay pattern.

### 4. Command System

**OpenCode:** Unified command registry that merges:
- Built-in commands (init, review)
- Config-defined commands (with template + model + agent + subtask options)
- MCP prompts (as invokable commands)
- Skills (auto-registered as commands)
- Template variables: `$ARGUMENTS`, `$1`, `$2`, etc.
- Commands can specify agent, model, and subtask mode

**Foreman:** No command abstraction. Skills are YAML files triggered at hook points (post_lint, pre_pr, etc.). No user-invokable commands.

**Adoption value:** HIGH — Foreman could use commands as the user-facing interface (dashboard, API), while skills remain the pipeline hook mechanism.

### 5. Plugin System

**OpenCode:** Extensible plugin architecture:
- Plugins are npm packages or local files
- Provide typed hooks: `tool.execute.before/after`, `session.compacting`, `chat.system.transform`, etc.
- Built-in + installable plugins
- Plugin lifecycle: install → init → hook registration → event subscription
- Auth plugins (Anthropic, Codex, Copilot, GitLab)

**Foreman:** No plugin system. Extensions are via YAML skills only.

**Adoption value:** MEDIUM — A Go plugin system is harder to implement. However, Foreman could adopt the **hook pattern** (pre/post hooks on pipeline stages) as a lighter alternative.

### 6. Event Bus

**OpenCode:** Typed event bus:
- `BusEvent.define()` — creates typed events with Zod schemas
- `Bus.publish()` / `Bus.subscribe()` — pub/sub
- `GlobalBus` for cross-instance events
- Events for: session changes, messages, parts, errors, worktree status

**Foreman:** Has `telemetry.EventEmitter` but it's limited to pipeline events. No general-purpose event bus.

**Adoption value:** MEDIUM — Foreman could generalize its event system for better observability and dashboard integration.

### 7. Scheduler

**OpenCode:** Simple periodic task scheduler:
- `Scheduler.register({ id, interval, run, scope })` — registers recurring tasks
- Scopes: `instance` (per-project) or `global`
- Used for: snapshot cleanup, session maintenance
- Auto-cleanup on instance shutdown

**Foreman:** Has `daemon.Scheduler` for ticket polling. Could adopt the pattern for maintenance tasks (worktree cleanup, stale lock removal, metric collection).

**Adoption value:** LOW — Foreman already has scheduling, just different scope.

### 8. Provider Transform / Model Abstraction

**OpenCode:** Rich model abstraction:
- `ProviderTransform.message()` — transforms messages per-provider (Anthropic vs OpenAI vs Gemini format differences)
- Provider-specific system prompts (different prompts for Claude vs GPT vs Gemini)
- Model variants support
- `wrapLanguageModel()` with middleware for transparent transforms

**Foreman:** Has `llm.LlmProvider` interface with Anthropic/OpenAI/OpenRouter/Local implementations. Each handles its own message format.

**Adoption value:** LOW — Foreman's approach is adequate for server-side use.

### 9. Doom Loop Detection

**OpenCode:** Tracks last 3 tool calls. If same tool + same input repeats, triggers permission check to break the loop.

**Foreman builtin runner:** Has deduplication detection but less sophisticated.

**Adoption value:** MEDIUM — Simple to adopt, prevents wasted LLM calls.

### 10. Structured Output Mode

**OpenCode:** Can force LLM to produce JSON matching a schema:
- Injects `StructuredOutput` tool
- Adds system prompt requiring its use
- Validates output against Zod schema
- Falls back if model doesn't call the tool

**Foreman:** Has `output_schema` support in agentsdk steps but not in the builtin runner.

**Adoption value:** HIGH — Would improve plan parsing, review parsing, and structured feedback.

### 11. Instruction/Context Loading

**OpenCode:** Hierarchical instruction loading:
- Searches up directory tree for `AGENTS.md`, `CLAUDE.md`, `CONTEXT.md`
- Supports config directories and global config
- HTTP URL support for remote instructions
- Deduplication via claiming mechanism
- Loaded once per session, cached

**Foreman:** Loads `AGENTS.md` and `.foreman/context.md` from workdir only. No hierarchy, no remote support.

**Adoption value:** MEDIUM — Directory hierarchy walk would improve multi-repo support.

## Prioritized Adoption Plan

### Phase 1: High-Value, Low-Risk (foundation improvements)

1. **Snapshot System** — Add file-state tracking for undo/rollback within pipeline stages
2. **Structured Output** — Add schema-validated output to builtin runner for plan/review parsing
3. **Command Registry** — Add user-invokable commands for dashboard/API interaction

### Phase 2: Medium-Value (robustness improvements)

4. **Doom Loop Detection** — Improve agent loop protection
5. **Enhanced Compaction** — Adopt pruning-first + replay pattern
6. **Worktree Lifecycle** — Add reset, start commands, event-driven status

### Phase 3: Architecture (extensibility)

7. **Event Bus** — Generalize telemetry into typed event bus
8. **Hierarchical Instructions** — Walk directory tree for context files
9. **Plugin Hooks** — Add pre/post hooks on all pipeline stages (Go interface, not npm)
