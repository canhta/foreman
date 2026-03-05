# Features

Foreman is an autonomous software development daemon. The following is a complete overview of its capabilities.

## Core Pipeline

### Ticket-to-PR Automation
Foreman monitors an issue tracker for labelled tickets, decomposes them into ordered tasks, implements each task with LLM-guided TDD, runs quality gates, and opens a pull request — all without human involvement between "ticket labelled" and "PR ready for review."

### Clarification Requests
Before planning, Foreman checks whether a ticket has sufficient detail (description length, acceptance criteria, specificity). If a ticket is too vague, Foreman comments on it with a specific question, applies a `foreman-needs-info` label, and waits. After a configurable timeout (default: 72 hours) with no response, the ticket is marked blocked. This prevents wasted LLM calls on ambiguous requirements.

### Planner
The planner decomposes a ticket into 2–20 granular tasks. Each task includes:
- A title and description
- Specific acceptance criteria
- Files to read and files to modify
- Test assertions the implementation must satisfy
- An estimated complexity (simple / medium / complex)
- Optional task dependencies (`depends_on` edges that drive the parallel DAG executor)

### Parallel DAG Task Execution
Tasks with no unmet dependencies execute immediately in parallel. A coordinator goroutine owns all mutable DAG state (zero mutexes); a bounded worker pool (configurable via `max_parallel_tasks`, default 3) pulls from a ready queue. When a task completes, the coordinator checks its dependents — if all dependencies are now satisfied, the dependent is pushed to the ready queue.

Failure semantics:
- A failing task triggers BFS pruning: all transitive dependents are marked `skipped`.
- Independent branches continue executing unaffected.
- If all tasks fail, the ticket is marked `failed` and no PR is created.
- If some tasks succeed, Foreman creates a partial PR (when `enable_partial_pr = true`).

Each task runs with an individual timeout (`task_timeout_minutes`, default 15 min) to prevent a hung task from blocking its dependents.

### Plan Validation
After planning, a deterministic validator checks the plan before any code is written:
- All referenced file paths exist (or are explicitly marked as new)
- No cycles in the task dependency graph
- No two tasks modify the same file without an explicit ordering
- Token-aware cost estimation (not a flat average) — warns if the plan will exceed 50% or 80% of the per-ticket budget
- Task count within the configured limit

### TDD-Driven Implementation
Each task is implemented using a strict TDD workflow:
1. The implementer writes failing tests first
2. Foreman mechanically verifies the RED phase — tests must fail for the right reason (assertion failure, not a compile error)
3. The implementer writes the minimal implementation to make the tests pass
4. Foreman verifies the GREEN phase

Failure-type discrimination distinguishes assertion failures (valid RED) from compile/import errors (invalid RED), providing precise feedback for retries.

### Tiered Feedback Loops
Failures at each gate trigger targeted retries with specific error context:
- **Lint failure** → retry implementer with lint errors (max 2 retries)
- **Test failure** → retry implementer with test output (max 2 retries)
- **Spec review rejection** → retry implementer with reviewer comments (max 2 cycles)
- **Quality review rejection** → retry implementer with quality feedback (max 1 cycle)

### Spec Review
After lint and tests pass, a spec reviewer LLM call checks whether the implementation satisfies the task's acceptance criteria. The reviewer has access to the original ticket, the task description, and the full diff.

### Quality Review
A quality reviewer LLM call checks code quality independently of spec compliance — looking for correctness issues, performance problems, security concerns, and maintainability gaps.

### Final Review
After all tasks are complete, a final reviewer inspects the full diff across the entire ticket. This catches cross-task issues that per-task reviews cannot see.

### Absolute LLM Call Cap
Every task tracks an absolute count of LLM calls (implementer + spec reviewer + quality reviewer combined). When it hits the configured cap (default: 8 calls/task), the task fails immediately. This prevents runaway retries from consuming unbounded budget.

### Partial PRs
When `enable_partial_pr = true`, if some tasks succeed and others fail or are skipped, Foreman creates a PR containing the completed work. The PR body includes a GitHub-flavoured checklist:

```markdown
## Tasks
- [x] Add user authentication middleware
- [x] Write auth unit tests
- [ ] ~~Add rate limiting~~ (skipped)
- [ ] ~~Write rate limit tests~~ (failed)
```

This is better than discarding all work when a single task hits a retry cap or a dependency fails.

### Crash Recovery
Task completions are checkpointed by `last_completed_task_seq` in the database. If the daemon crashes mid-pipeline, it resumes from the last committed task on the next start — no work is lost.

---

## Quality Gates

### Lint + Auto-Fix
Foreman runs the repo's linter after each implementation. Lint errors are passed to the implementer as structured feedback. Many fixable errors are auto-corrected before escalation.

### Full Test Suite Pre-PR
After all tasks are implemented and rebased, Foreman runs the full test suite before creating the PR. A full-suite failure blocks PR creation (or creates a partial PR if configured).

### Secrets Scanner
Before any file enters an LLM context, Foreman scans for known secret patterns (API keys, private keys, tokens). Matching files are redacted or excluded. Writes from the agent are also checked for secret content patterns. This runs on every context assembly, not just at startup.

### Forbidden File Patterns
The command runner enforces a list of forbidden paths (`.env`, `.ssh`, `.aws`, `*.key`, `*.pem`) that cannot be read or written by agent operations. This is enforced at the tool layer, not just as a guideline.

---

## Issue Tracker Integrations

### Jira (Cloud and Server)
Polls for tickets with a configurable label (`foreman-ready`). Posts status comments at each pipeline stage. Syncs ticket status through configurable status mappings. Supports clarification label flow.

### GitHub Issues
Polls for issues with a configurable label. Posts comments. Attaches PRs. Supports the full clarification flow.

### Linear
Polls for issues with a configured label. Team ID is configurable. Supports the same comment and status update flow.

### Local File Tracker
A file-based tracker for local development and testing. No external API required.

---

## LLM Provider Support

### Anthropic (Claude)
Primary provider. Supports Claude Haiku, Sonnet, and Opus. Native structured output via `tool_choice: {type: "tool"}` pattern. Extended thinking via `thinking` parameter. Prompt caching support. Used for all roles by default.

### OpenAI (GPT-4o, o1, o3)
Structured output via `response_format.json_schema`. Function calling / tool-use support for the builtin agent runner. Compatible with all pipeline roles.

### OpenRouter
OpenAI-compatible API that routes to any model. Inherits OpenAI tool-use support. Useful for accessing models not available directly.

### Local Models (Ollama and OpenAI-compatible servers)
Any server implementing the OpenAI Chat Completions API is supported. Tool-use degrades gracefully — if the model does not support tools, the builtin runner falls back to single-turn text responses.

### Cross-Provider Fallback
When a provider is fully down (not just rate-limited), Foreman can fall back to a configured secondary provider. If all retries are exhausted and no fallback is available, the pipeline is paused and retried on the next poll cycle — the ticket is not failed.

### Per-Role Model Routing
Each pipeline role (planner, implementer, spec reviewer, quality reviewer, final reviewer, clarifier) can be routed to a different model and provider. Use expensive models for critical judgment roles and cheaper models for simpler review tasks.

### Extended Thinking
Anthropic's extended thinking parameter is supported in LLM requests. This can be enabled per skill step for complex reasoning tasks. Extended thinking tokens are tracked separately in cost accounting.

### Native Structured Output
All providers support `OutputSchema` in LLM requests. Anthropic uses the `tool_choice` forced-tool mechanism. OpenAI uses `response_format.json_schema`. This allows skills and pipeline steps to request validated JSON responses.

### Prompt Caching
Anthropic prompt caching (`cache_control: {type: "ephemeral"}`) is supported to reduce costs on repeated context assembly. The system prompt and large static context blocks are cacheable.

---

## Git Integration

### Multi-Host Support
PR creation is supported for GitHub, GitLab, and Bitbucket.

### Native Git CLI (Default)
Uses the system `git` binary for all operations. Fastest and most compatible.

### go-git Fallback
A pure Go git implementation (`go-git/v5`) is used automatically when the `git` CLI is not available. Enabled via `backend = "gogit"` in config or automatically if `git` is not on `$PATH`.

### Branch Management
Foreman creates branches with a configurable prefix (`foreman/PROJ-123-add-auth`). Branches are rebased onto the default branch before PR creation.

### LLM-Assisted Rebase Conflict Resolution
When a rebase produces merge conflicts, Foreman attempts to resolve them automatically using an LLM call with the conflict diff as context. If LLM resolution fails, Foreman creates the PR anyway with a conflict warning note.

### Draft PRs
PRs are created as drafts by default. Configurable reviewers are automatically assigned.

---

## Execution Environments

### Local Runner
Runs commands directly on the host machine. An allowlist of permitted commands (`npm`, `go`, `cargo`, `pytest`, etc.) and a list of forbidden paths are enforced.

### Docker Runner
Runs each ticket's commands in a dedicated Docker container (one container per ticket, reused across tasks). Features:
- Network isolation (`network = "none"` by default)
- CPU and memory limits
- Automatic dep reinstall when package files change between tasks (detects `package.json`, `go.mod`, `Cargo.toml`, etc.)
- Configurable base image per repo via `.foreman-context.md`

---

## Concurrency and Daemon

### Parallel Ticket Processing
The daemon runs multiple pipelines concurrently (configurable, default up to 3 for SQLite, higher with PostgreSQL). A shared rate limiter (token bucket) prevents provider API overload across parallel workers.

### Parallel Task Execution Within a Ticket
Within each ticket, tasks execute in parallel respecting `depends_on` edges. The DAG executor uses a coordinator goroutine and a bounded worker pool (`max_parallel_tasks`). Tasks with no pending dependencies start immediately; completions unlock their dependents in real time.

### File Reservation Layer
Before beginning a ticket, Foreman checks whether any planned files are currently being modified by another active pipeline. Conflicting tickets are re-queued to avoid parallel edit conflicts. Reservations are released atomically when a pipeline completes or fails.

### Scheduler and Prioritization
The scheduler checks for new tickets on each poll cycle and manages pipeline slot allocation. Idle intervals (when no work is available) use a longer poll interval to reduce API calls.

---

## Cost Control

### Multi-Level Budget Enforcement
- **Per-ticket budget**: abort and escalate when a ticket exceeds its cost limit
- **Per-day budget**: pause all pipelines when the daily limit is reached
- **Per-month budget**: hard stop when the monthly limit is reached
- **Alert threshold**: configurable alert at a percentage of any budget (default: 80%)

### Absolute LLM Call Cap Per Task
Prevents a single task from consuming unbounded tokens through repeated retries. Default cap: 8 calls per task.

### Real-Time Cost Tracking
Every LLM call records input tokens, output tokens, cost, duration, and model. Costs are aggregated per ticket, per day, and per month. The `foreman cost` CLI and dashboard show current spend at all granularities.

### Token-Aware Cost Estimation at Plan Validation
Before executing a plan, Foreman estimates the total cost using actual token budgets and model pricing (not a flat per-call estimate). Plans that are projected to use 80%+ of the budget are rejected before any implementation begins.

### Configurable Per-Model Pricing
Model pricing (input/output per 1M tokens) is configurable in `foreman.toml` so cost estimates stay accurate as provider pricing changes.

---

## Observability

### Structured Logging
All log output uses `zerolog` for structured JSON logging with contextual fields (ticket ID, task ID, role, model, step). Supports `json` and `pretty` formats. Log level is configurable (`trace`, `debug`, `info`, `warn`, `error`).

### Prometheus Metrics
A Prometheus-compatible metrics endpoint is available on the dashboard server. Metrics include:
- Ticket and task counters by status
- LLM call counters, token usage, cost, and duration histograms — all labelled by role and model
- DAG execution metrics: `foreman_dag_tasks_completed_total`, `foreman_dag_tasks_failed_total`, `foreman_dag_tasks_skipped_total`, `foreman_dag_execution_duration_seconds`
- Test run results
- Retry counts by role
- Rate limit hits by provider
- TDD verification results
- Partial PR counts
- Clarification request counts
- Secrets detection counts
- Hook and skill execution counts

### Event Log
Every significant pipeline event is recorded to the database events table with type, severity, ticket/task context, and a details blob. Events are viewable via the dashboard and the `foreman logs` CLI.

### Pipeline Event Types
Over 40 event types covering the full lifecycle: `ticket_picked_up`, `task_tdd_verify_pass`, `task_spec_review_fail`, `rebase_conflict_resolved`, `pr_created`, `cost_alert`, `secrets_detected`, `rate_limit_hit`, `pipeline_resumed_after_crash`, and more.

---

## Dashboard

### Web UI
A built-in HTTP server (default port 3333) serves a single-page dashboard showing:
- Active, completed, and failed tickets
- Per-ticket task breakdown with status indicators
- Real-time cost tracking
- Live event log

### REST API
JSON endpoints for all dashboard data: ticket list, ticket detail, task list, cost summary, and events. See [Dashboard](dashboard.md) for the full API reference.

### WebSocket Live Updates
The dashboard subscribes to a WebSocket endpoint for real-time pipeline status updates — no polling required.

### Bearer Token Authentication
All dashboard endpoints require a bearer token. Tokens are generated with `foreman token generate`, stored as SHA-256 hashes, and can be revoked via the API.

---

## YAML Skill Engine

### Extensible Pipeline Hooks
Three hook points allow custom steps to be injected into the pipeline without modifying core code:
- `post_lint` — after lint passes, before spec review (e.g., security scanning)
- `pre_pr` — before PR creation (e.g., changelog generation)
- `post_pr` — after PR is created (e.g., Slack notification)

### Skill Step Types
Skills are composed of typed steps:
- `llm_call` — call any configured LLM provider with a custom prompt
- `run_command` — execute a shell command in the repo
- `file_write` — write a file (e.g., CHANGELOG.md)
- `git_diff` — expose the current diff as a variable
- `agentsdk` — delegate to the configured AgentRunner
- `subskill` — compose another skill as a step

### Structured Output in Skills
`agentsdk` steps support `output_format` (json/diff/checklist), `output_schema` (JSON Schema for validated structure), and `fallback_model`.

### Built-in Skills
Foreman ships three built-in skill files:
- `feature-dev.yml` — default feature development workflow
- `bug-fix.yml` — bug fixing workflow with regression test emphasis
- `refactor.yml` — refactoring workflow with behaviour-preservation focus

### Community Skills
A `skills/community/` directory accepts community-contributed skill files. Community skills are submitted via PR.

---

## Agent Runner

### Pluggable AgentRunner Interface
Skills can delegate tasks to any agent runner implementation:
- **builtin** — a multi-turn tool-use loop over `LlmProvider` with 14 built-in tools, parallel execution, and reactive context injection
- **claudecode** — delegates to the `claude` CLI binary (Claude Code)
- **copilot** — delegates to the GitHub Copilot CLI via JSON-RPC

### Builtin Runner — 14 Built-in Tools
The builtin runner provides a typed tool registry covering four categories:
- **Filesystem**: `Read`, `Write`, `Edit`, `MultiEdit`, `ListDir`, `Glob`
- **Code intelligence**: `Grep`, `GetSymbol`, `GetErrors`, `TreeSummary`
- **Git**: `GetDiff`, `GetCommitLog`
- **Execution**: `Bash`, `RunTest`
- **Agent composition**: `Subagent`

### Parallel Tool Execution
All tool calls within a single agent turn execute in parallel using `errgroup`. This matches the performance of the Anthropic SDK's `BetaToolRunner` and is typically 3× faster than sequential execution on multi-tool turns.

### Reactive Context Injection
After each file-touching tool call (Read, Edit, Write, GetDiff), the builtin runner queries the database for progress patterns and scoped rules relevant to the accessed paths, then injects them as a context message before the next LLM turn. This eliminates stale context issues without reinserting the full context on every turn.

### Two-Layer Context System
1. **Pre-assembly** (all three runners): the skills engine always reads `AGENTS.md` or `.foreman/context.md`, adds path-scoped rules and ticket metadata, and prepends it to `AgentRequest.SystemPrompt`.
2. **Reactive injection** (builtin only): progress patterns and directory-specific rules are injected mid-turn based on which files the model has touched.

### Path Guards and Security
All tool operations that access files enforce:
- Path traversal prevention (no `../../` escapes)
- Relative-path-only enforcement
- Secrets pattern blocking on writes (`.env`, `*.key`, private key content patterns)

### MCP Support (stdio transport)
Foreman's builtin agent runner supports MCP servers via stdio subprocess transport. Configured servers are spawned at agent startup; tools are discovered via `tools/list` and registered in the tool registry with normalized names (`mcp_{server}_{tool}`). The `Manager` routes `tools/call` requests to the correct server by matching the name prefix.

Restart policy (`always` / `never` / `on-failure`) with configurable `max_restarts` and `restart_delay_secs` provides graceful degradation — if a server exhausts its restart budget, its tools are marked unavailable and the agent continues with built-in tools only.

For Anthropic API-side MCP, set `URL` and `AuthToken` in `MCPServerConfig` — Anthropic's infrastructure handles the connection without a local subprocess.

---

## Miscellaneous Features

### `foreman context generate`
Generates an `AGENTS.md` file by scanning the repository and making a single LLM call. The LLM receives a tiered file selection (go.mod/package.json, CI configs, entry points, key package files) assembled within a configurable token budget (`context_generate_max_tokens`, default 32 000 tokens).

The generated file is optimised for autonomous agents — precise naming conventions, exact test commands, explicit anti-patterns, and file organisation rules. Use `--offline` for a static, LLM-free scan.

```bash
foreman context generate              # LLM-powered (default)
foreman context generate --offline    # Static analysis only
foreman context generate --dry-run    # Print to stdout without writing
foreman context generate --force      # Overwrite existing AGENTS.md
```

### `foreman context update`
Post-merge learning loop. Reads structured observations appended to `.foreman/observations.jsonl` by the pipeline after each successful PR merge (naming corrections, test patterns, discovered conventions) and issues a single LLM call to update `AGENTS.md` with the new knowledge. A cursor embedded in the file footer (`<!--foreman:last-update:...-->`) enables resumable reads.

### `foreman init`
Interactively generates a `foreman.toml` for a new project. With `--analyze`, it inspects the target repository to suggest appropriate configuration (detected language, test commands, lint commands).

### `foreman doctor`
Pre-flight check that validates config syntax, tests all API credentials, verifies database write access, and checks git connectivity.

### Pongo2 Prompt Templates
All LLM system prompts are Jinja2-compatible templates (`*.md.j2`) rendered with `pongo2`. Variables include ticket details, task context, code diffs, accumulated feedback, and previous attempt results.

### Fuzzy Search/Replace
When the LLM returns SEARCH/REPLACE blocks for code edits, Foreman uses fuzzy matching to handle minor whitespace or formatting differences between the LLM's memory of the file and the actual content. The similarity threshold is configurable (default: 0.92).

### Dependency Change Detection
After each task commit, Foreman diffs package/dependency manifest files (`go.mod`, `package.json`, `Cargo.toml`, `requirements.txt`, etc.). If they changed, the appropriate install command is run before the next task begins to avoid broken builds.

---

## Known Limitations and Gaps

> These are areas identified in design documents where behaviour may differ from the descriptions above or where features are partially implemented.

- **GitLab and Bitbucket PR creation** is defined in the interface but may not be fully tested. GitHub is the primary tested backend.
- **go-git fallback** implements the `GitProvider` interface but may have gaps for complex rebase scenarios.
- **Extended thinking** (Anthropic) and **prompt caching** are defined in the LLM request model but may not be surfaced in the standard pipeline steps — they are primarily available via skill YAML.
- **Cross-provider fallback** (`fallback_provider`) is defined in config but the circuit-breaker logic between primary and fallback is not fully detailed in the implementation plans.
- **MCP HTTP/SSE transport** is not implemented. Only stdio subprocess transport is supported.
- **MCP resources and prompts** are not supported. Tools only.
- **Community skills** (`skills/community/`) are defined but the submission and review process for community contributions is not yet documented.
