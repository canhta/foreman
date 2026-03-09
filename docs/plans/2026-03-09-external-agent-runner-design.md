# External Agent Runner Integration — Design

**Date:** 2026-03-09
**Status:** Approved
**Scope:** Wire existing AgentRunner implementations (Claude Code, Copilot) into the core pipeline as first-class implementation and planning runners.

## Goal

Enable Claude Code and GitHub Copilot as full pipeline runners — not just skills-hook agents. The user selects a runner via `foreman.toml`. The rest of the pipeline (DAG execution, PR creation, hooks) is identical regardless of runner.

## Current State

- `AgentRunner` interface exists at `internal/agent/runner.go` with three implementations: `BuiltinRunner`, `ClaudeCodeRunner`, `CopilotRunner`.
- All three are only used by the skills engine (`internal/skills/engine.go`) at hook points (`post_lint`, `pre_pr`, `post_pr`).
- The core pipeline (`task_runner.go`) uses `LLMProvider.Complete()` directly for implementation — single-turn, deterministic, no codebase exploration.
- The planner (`planner.go`) also uses `LLMProvider.Complete()` with assembled context — no codebase exploration.
- Runner selection already exists in `foreman.toml` under `[agent_runner].provider` but only affects skills.

## Target State

```
Ticket → Planner (builtin LLM or AgentRunner) → DAG → TaskRunner (builtin or AgentRunner) → PR
```

Two integration points, both using the existing `AgentRunner` interface:

1. **Planner step**: AgentRunner explores the codebase, returns `[]PlannedTask` via `--json-schema` structured output.
2. **Implementation step**: AgentRunner receives a structured prompt, produces code changes autonomously.

## Architecture

### Integration Point 1: Planner

The orchestrator has a `TicketPlanner` interface:

```go
// internal/daemon/orchestrator.go
type TicketPlanner interface {
    Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlanResult, error)
}
```

Currently satisfied by `pipeline.Planner` (LLM-based). For external runners, create `AgentPlanner` — a new `TicketPlanner` implementation that delegates to `AgentRunner`.

**AgentPlanner flow:**
1. Build a planning prompt from the ticket (title, description, acceptance criteria).
2. Set `OutputSchema` to the JSON schema of `PlannerResult` (status, tasks[], codebase_patterns).
3. Call `AgentRunner.Run()` — the agent explores the codebase (read files, grep, glob) and returns a structured plan.
4. Parse `AgentResult.Structured` into `PlannerResult`.
5. Run existing `ValidatePlan()` and `TopologicalSort()` — same validation as builtin path.
6. Return `*PlanResult` to the orchestrator.

**Why this is better than the builtin planner:** The builtin planner gets a static context snapshot (`AssemblePlannerContext`). Claude Code/Copilot can actively explore — follow imports, check interfaces, understand architecture — producing better task decompositions and file assignments.

**Fallback:** If the agent returns invalid structured output, fall back to the builtin `Planner`. Log a warning.

**Location:** `internal/pipeline/agent_planner.go`

### Integration Point 2: Task Implementation

Surgical branch in `PipelineTaskRunner.RunTask()`:

```
RunTask(ctx, task)
  ├── agentRunner != nil?
  │     YES → promptBuilder.Build(task, contextFiles, config)
  │           → skillInjector.Inject(workDir)  [claudecode only]
  │           → agentRunner.Run(prompt, workDir)
  │           → collect diff via git
  │           → run SpecReview (if acceptance criteria exist, non-blocking)
  │           → run QualityReview (non-blocking)
  │           → commit
  │           → run post_lint hooks
  │           → done
  │
  └── NO  → existing builtin loop (unchanged)
```

The external path bypasses: implementer LLM call, output parsing, file application, TDD verification, test execution within Foreman's loop. The agent handles all of this internally.

**Fields added to `PipelineTaskRunner`:**
- `agentRunner agent.AgentRunner` — optional, nil = builtin path
- `promptBuilder *PromptBuilder` — optional, used only when agentRunner != nil
- `skillInjector *SkillInjector` — optional, used only when runner is claudecode

**Fields added to `TaskRunnerConfig`:**
- `AgentRunner agent.AgentRunner`
- `AgentRunnerName string` — "claudecode", "copilot", or "" (for skill injection decision)

**Fields added to `TaskRunnerFactoryInput`:**
- `AgentRunner agent.AgentRunner`
- `AgentRunnerName string`

**Wiring in `cmd/start.go`:** The `taskRunnerFactory.Create()` method reads the `AgentRunner` from its own field (set during daemon startup from config) and passes it through to `NewPipelineTaskRunner`.

**Wiring in orchestrator:** `Orchestrator` gets an optional `agentRunner` field. Set during construction if `[agent_runner].provider != "builtin"`. Passed to `TaskRunnerFactoryInput` when creating DAG runners. Also used to decide whether to use `AgentPlanner` or `Planner`.

## New Components

### 1. PromptBuilder (`internal/pipeline/prompt_builder.go`)

Translates a `PlannedTask` + project context into a self-contained prompt for an autonomous agent.

**Input:**
- `models.Task` (title, description, acceptance criteria, test assertions, files to read/modify)
- Context files `map[string]string` (from existing `selectContextFiles`)
- Project config: test command, lint command, language, codebase patterns
- Retry feedback (if attempt > 1): previous failure reason + error type

**Output:** Structured prompt string with sections:
- **Task** — title + description
- **Expected Behaviors** — acceptance criteria as mechanically verifiable assertions
- **Constraints** — project-specific rules (language, test framework, style conventions)
- **Affected Area Hints** — relevant file paths and patterns (not pre-selected content — the agent explores)
- **Definition of Done** — test command that must pass, lint command that must pass
- **Retry Context** (attempts > 1 only) — previous failure reason + targeted guidance by error type

**Criteria reformulation:** Uses `LLMProvider.Complete()` with cheap model (haiku) to transform raw acceptance criteria into mechanically verifiable assertions. Falls back to raw criteria if the LLM call fails.

**No system prompt injection.** The prompt is the user prompt. System prompt comes from skill templates (for claudecode) or is left to the agent's default (for copilot).

### 2. SkillInjector (`internal/pipeline/skill_injector.go`)

Only used when runner is `claudecode`. Writes TDD skill templates into the working directory before spawning Claude Code.

**Embedded templates** shipped in binary under `assets/claude/`:
- `settings.json` — registers UserPromptSubmit hook (raises skill activation from ~20% to ~84%)
- `skills/tdd.md` — TDD orchestrator enforcing RED→GREEN→REFACTOR phase gates
- `agents/tdd-test-writer.md` — RED phase, isolated context (requirements only, no implementation knowledge)
- `agents/tdd-implementer.md` — GREEN phase, isolated context (failing test only, minimal implementation)
- `agents/tdd-refactorer.md` — REFACTOR phase, isolated context (clean up without changing behavior)

**Template rendering:** Templates contain `{{.TestCommand}}`, `{{.LintCommand}}`, `{{.Language}}` placeholders. Rendered with project config values from `foreman.toml`.

**Conflict handling:**
- `.claude/settings.json`: read existing → deep-merge Foreman's hooks into existing config → write back. Never overwrite user config.
- All other Foreman-managed files go under `.claude/foreman/` namespace (e.g., `.claude/foreman/skills/tdd.md`). No collision with user files.
- Cleanup after runner completes: remove `.claude/foreman/` directory. Restore original `settings.json` if it was modified.

**Context isolation principle:** Each TDD phase agent runs in a completely isolated context. The test-writer never sees implementation plans. The implementer never sees the test-writing process. This makes TDD genuine, not performative.

### 3. AgentPlanner (`internal/pipeline/agent_planner.go`)

Alternative `TicketPlanner` implementation that delegates planning to `AgentRunner`.

**Flow:**
1. Build planning prompt from ticket (title, description, acceptance criteria, labels).
2. Set `OutputSchema` to JSON schema of `PlannerResult`.
3. Set `WorkDir` so the agent can explore the codebase.
4. Call `AgentRunner.Run()`.
5. Extract `AgentResult.Structured` → `PlannerResult`.
6. Run `ValidatePlan()` + `TopologicalSort()` (same as builtin).
7. Run confidence scoring if enabled (same as builtin).
8. Return `*PlanResult`.

**The planning prompt instructs the agent to:**
- Explore the codebase to understand architecture, patterns, and conventions
- Decompose the ticket into ordered, independent tasks
- For each task: specify title, description, acceptance criteria, files to read/modify, dependencies
- Detect codebase patterns (language, framework, test runner, style notes)

## Non-Blocking Review on External Runner Diff

After the external runner completes and Foreman collects the diff:

1. Run `SpecReviewer.Review()` if `task.AcceptanceCriteria` is non-empty.
   - Input: `SpecReviewInput{TaskTitle, Diff, TestOutput: "", AcceptanceCriteria}`
   - `TestOutput` is empty because tests ran inside the agent, not Foreman.
2. Run `QualityReviewer.Review()` on the diff.
   - Input: `QualityReviewInput{Diff, CodebasePatterns}`
3. If either review rejects:
   - Store review issues in `TaskResult.ReviewWarnings` (new field).
   - Orchestrator appends warnings to PR description body.
   - **PR creation is NOT blocked.** The warnings are informational.
4. If both approve: clean PR description, no warnings.

## Repo-Level Reservation Sentinel

External runners modify files freely (unlike the builtin path which declares files upfront). To prevent conflicts when multiple tickets run in parallel:

- When dispatching to an external runner, call `TryReserveFiles(ticketID, []string{"__REPO_LOCK__"})`.
- Any other ticket attempting `TryReserveFiles` with `__REPO_LOCK__` or any specific file sees the conflict and waits.
- Requires a small change to `TryReserveFiles`: if `__REPO_LOCK__` is reserved by another ticket, treat it as conflicting with any file set.
- Released via existing `ReleaseFiles(ticketID)` after runner completes.
- Effect: external runner tasks on the same repo are serialized. Builtin tasks can still run in parallel (they use per-file reservations).

## Cost Tracking

External runners report cost as a lump sum in `AgentResult.Usage.CostUSD`.

- Insert one `llm_calls` row per external runner execution:
  - `source = "claudecode"` or `"copilot"`
  - `role = "agent_implementation"` or `"agent_planning"`
  - `cost_usd = result.Usage.CostUSD`
  - `input_tokens = result.Usage.InputTokens`
  - `output_tokens = result.Usage.OutputTokens`
  - `duration_ms = result.Usage.DurationMs`
- Granular per-call data is unavailable for external runners. Documented as a known limitation.

## Smart Retry for External Runners

External runner fails mid-task → no partial state to recover (unlike builtin path with per-attempt feedback).

- Retry = re-run from scratch with the same prompt + failure reason appended.
- Max 2 retries (configurable via `MaxImplementationRetries`, same as builtin).
- Failure reasons extracted from: agent timeout, empty diff, test failure (if Foreman runs post-verification), agent error output.
- No state recovery. Each retry is a clean run.

## Commit Granularity

External runners may produce 0, 1, or N commits during execution.

- After runner completes, Foreman runs `git diff` to check for uncommitted changes.
- If uncommitted changes exist: Foreman stages and commits them.
- If the runner already committed: Foreman uses the existing commits.
- PR diff is always `git diff main..HEAD` — commit structure is preserved in the branch.
- Squash behavior is the user's choice at merge time.

## Configuration

No new config keys. Existing `foreman.toml` structure:

```toml
[agent_runner]
provider = "claudecode"  # "builtin" | "claudecode" | "copilot"

[agent_runner.claudecode]
bin = "claude"
model = "sonnet"
max_turns_default = 20
timeout_secs_default = 600
max_budget_usd = 5.0

[agent_runner.copilot]
cli_path = "copilot"
model = "gpt-4o"
timeout_secs_default = 300
```

When `provider = "builtin"`: no `AgentRunner` is created, pipeline behaves exactly as today.
When `provider = "claudecode"` or `"copilot"`: `AgentRunner` is created at daemon startup and passed to both the orchestrator (for planning) and the task runner factory (for implementation).

## What Is NOT Changed

- `task_runner.go` builtin path — zero modifications to existing retry loops, TDD verification, review cycles, error classifier, feedback accumulator
- `implementer.go`, `spec_reviewer.go`, `quality_reviewer.go`, `tdd_verifier.go` — used by builtin path as-is
- `AgentRunner` interface, `ClaudeCodeRunner`, `CopilotRunner` — already exist, not modified
- `runner.CommandRunner` — low-level shell executor, unchanged
- `planner.go` — existing `Planner` struct unchanged, still used when provider = "builtin"
- Skills hooks — continue to fire after any runner completes
- Daemon, tracker, git, DB, dashboard — zero changes except:
  - Orchestrator gets optional `agentRunner` field
  - `TaskRunnerFactoryInput` gets optional `AgentRunner` + `AgentRunnerName` fields
  - `cmd/start.go` factory wires `AgentRunner` through

## Error Handling

| Scenario | Behavior |
|---|---|
| External runner times out | Task marked failed, retry with timeout info |
| External runner produces no diff | Retry with "no changes produced" feedback |
| Agent planner returns invalid schema | Fall back to builtin `Planner`, log warning |
| Agent planner returns non-OK status | Same as builtin: return to orchestrator for handling |
| Spec/quality review rejects | Warning in PR description, PR still created |
| SkillInjector can't merge settings.json | Log warning, skip injection, run without skills |
| PromptBuilder criteria reformulation fails | Fall back to raw acceptance criteria |

## Testing Strategy

- **Unit tests for `PromptBuilder`**: prompt structure, retry context inclusion, criteria reformulation fallback.
- **Unit tests for `SkillInjector`**: template rendering, settings.json merge (existing + empty + conflict), cleanup.
- **Unit tests for `AgentPlanner`**: structured output parsing, validation pass-through, fallback to builtin.
- **Unit tests for surgical branch**: mock `AgentRunner`, verify diff collection, review invocation, commit creation.
- **Unit test for repo-level reservation**: `__REPO_LOCK__` sentinel blocks other tickets, released after completion.
- **Integration test**: mock `AgentRunner` producing a known diff → verify spec/quality review runs → verify commit → verify PR description includes warnings if review rejected.
- **Existing builtin path tests**: must continue passing with zero modification.

## Scope Summary

| Item | Action | Complexity |
|---|---|---|
| `AgentPlanner` | Build | Medium |
| Surgical branch in `RunTask()` | Build | Low |
| `PromptBuilder` | Build | Medium |
| `SkillInjector` with settings merge | Build | Medium |
| TDD skill templates (assets) | Build | Medium |
| Non-blocking review on external diff | Build | Low |
| Repo-level reservation sentinel | Build | Low |
| Cost row for external runners | Build | Low |
| Orchestrator + factory wiring | Build | Low |
| `AgentRunner`, `ClaudeCodeRunner`, `CopilotRunner` | Already exist — don't touch | — |
| Builtin runner wrapper | Dropped | — |
| Mid-run clarification | Deferred to v2 | — |
| Pre-run ClarificationGenerator | Wire existing `CheckTicketClarity()` | — |
