# Configuration

Foreman is configured via a TOML file named `foreman.toml` in the working directory, or via the `--config` flag. Copy `foreman.example.toml` from the repo as a starting point.

Environment variables can be interpolated using `${VAR_NAME}` syntax anywhere in the config. All secrets should use this form — never hardcode API keys in the config file.

## Configuration Sections

| Section | Purpose |
|---|---|
| `[daemon]` | Poll intervals, parallelism limits, working directory, log level, env file injection |
| `[dashboard]` | Web UI port, host binding, authentication token |
| `[tracker]` | Issue tracker provider and credentials |
| `[git]` | Repository clone URL, branch settings, PR options |
| `[llm]` | LLM provider selection and API keys |
| `[models]` | Per-role model routing (planner, implementer, reviewers…) |
| `[cost]` | Per-ticket / per-day / per-month spend limits |
| `[limits]` | Task counts, retry caps, token budgets, feature flags |
| `[pipeline.hooks]` | YAML skill names to run at each hook point |
| `[decompose]` | Automatic large-ticket decomposition settings |
| `[secrets]` | Secrets scanner patterns and always-excluded files |
| `[rate_limit]` | LLM request rate limiting and backoff |
| `[runner]` | Local or Docker command runner |
| `[database]` | SQLite or PostgreSQL connection |
| `[agent_runner]` | Agent runner type (builtin / claudecode / copilot) |
| `[mcp]` | MCP server connections (stdio transport) |
| `[channel]` | Messaging channel (WhatsApp) for inbound tickets and notifications |
| `[context]` | Context assembly tuning (AGENTS.md generation, feedback boost) |

---

## Daemon

```toml
[daemon]
poll_interval_secs      = 60    # How often to check for new tickets (seconds)
idle_poll_interval_secs = 300   # Longer interval when no work is queued
max_parallel_tickets    = 3     # Concurrent pipeline limit
                                # Max 3 for SQLite; use PostgreSQL for more
max_parallel_tasks      = 3     # Concurrent tasks per ticket (DAG executor worker pool)
task_timeout_minutes       = 15    # Per-task timeout in minutes
merge_check_interval_secs  = 300   # How often to poll PR merge status (seconds)
work_dir   = "~/.foreman/work"  # Directory where repos are cloned
log_level  = "info"             # trace | debug | info | warn | error
log_format = "json"             # json | pretty

# env_files maps worktree-relative destination paths to source file paths on disk.
# Each file is copied into every task worktree and all vars are loaded into the
# process environment before the pipeline runs. Use ~/ for home-relative paths.
# [daemon.env_files]
# ".env" = "~/.foreman/envs/myproject.env"
```

---

## Dashboard

```toml
[dashboard]
enabled    = true
port       = 8080
host       = "127.0.0.1"                   # Loopback only by default; use 0.0.0.0 with caution
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"  # Required; generate with: foreman token generate
```

Generate a dashboard token:

```bash
./foreman token generate
```

---

## Issue Tracker

```toml
[tracker]
provider                    = "github"            # github | jira | linear | local_file
pickup_label                = "foreman-ready"     # Only issues with this label are picked up
clarification_label         = "foreman-needs-info" # Label added when agent needs clarification
clarification_timeout_hours = 72                  # Hours before a clarification request times out
```

> **`pickup_label` is how you control which issues the agent works on.** Apply that label (e.g. `foreman-ready`) to an issue in GitHub/Jira/Linear to assign it to Foreman. Issues without the label are ignored.

### GitHub Issues

```toml
[tracker.github]
owner    = "your-org"
repo     = "your-repo"
token    = "${GITHUB_TOKEN}"
base_url = ""   # optional: override for GitHub Enterprise
```

### Jira

```toml
[tracker.jira]
base_url    = "https://yourcompany.atlassian.net"
email       = "bot@yourcompany.com"
api_token   = "${JIRA_API_TOKEN}"
project_key = "PROJ"

# Status transitions in your Jira workflow
status_in_progress = "In Progress"
status_in_review   = "In Review"
status_done        = "Done"
status_blocked     = "Blocked"
```

Create a Jira API token at:
`https://id.atlassian.com/manage-profile/security/api-tokens`

Use your Atlassian account email for `email` and store the token in `JIRA_API_TOKEN`.

### Linear

```toml
[tracker.linear]
api_key  = "${LINEAR_API_KEY}"
team_id  = "TEAM_ID"
base_url = ""   # optional override
```

### Local File Tracker

For development and testing without an issue tracker:

```toml
[tracker.local_file]
path = "./tickets"   # Directory containing .json ticket files
```

---

## Git

```toml
[git]
provider          = "github"    # github | gitlab | bitbucket
backend           = "native"    # native (git CLI) | gogit (pure Go fallback)
clone_url         = "git@github.com:your-org/your-repo.git"
default_branch    = "main"
auto_push         = true
pr_draft          = true
pr_reviewers      = ["team-lead"]
branch_prefix     = "foreman"   # Branch names: foreman/PROJ-123-add-auth
rebase_before_pr  = true
```

### GitHub

```toml
[git.github]
token = "${GITHUB_TOKEN}"
```

> **Token permissions required:** `repo` scope (classic PAT) or `Contents: Read and write` + `Pull requests: Read and write` (fine-grained PAT). For org repos, SAML SSO authorization and PAT approval may also be needed. See [integrations.md](integrations.md) for full setup steps.

### GitLab

```toml
[git.gitlab]
token    = "${GITLAB_TOKEN}"
base_url = "https://gitlab.com"   # Override for self-hosted GitLab
```

For private repositories, use an SSH clone URL and run `foreman setup-ssh` once to provision a dedicated key:

```bash
./foreman setup-ssh
# Prints the public key — add it to your GitHub account:
# https://github.com/settings/ssh/new
# Then verify:
./foreman doctor
```

Adding the key to your **GitHub account** (not repo deploy keys) grants access to all repos your account can reach, including private org repos. Nothing is written to `~/.ssh/config` — the key is used exclusively by Foreman via `GIT_SSH_COMMAND`.

SSH clone URL format:
```toml
[git]
clone_url = "git@github.com:your-org/your-repo.git"
```

---

## LLM Providers

```toml
[llm]
default_provider   = "anthropic"   # anthropic | openai | openrouter | local
embedding_provider = ""            # "openai" | "anthropic" — provider for embedding calls
embedding_model    = ""            # e.g. "text-embedding-3-small", "voyage-code-3"
```

### Anthropic

```toml
[llm.anthropic]
api_key  = "${ANTHROPIC_API_KEY}"
base_url = "https://api.anthropic.com"   # Optional override
```

### OpenAI

```toml
[llm.openai]
api_key  = "${OPENAI_API_KEY}"
base_url = "https://api.openai.com"
```

### OpenRouter

```toml
[llm.openrouter]
api_key  = "${OPENROUTER_API_KEY}"
base_url = "https://openrouter.ai/api"
```

### Local (Ollama or OpenAI-compatible)

```toml
[llm.local]
base_url = "http://localhost:11434"
```

### Provider Outage Behaviour

```toml
[llm.outage]
max_connection_retries      = 3    # Retries before pausing the pipeline
connection_retry_delay_secs = 30
fallback_provider           = ""   # Optional: "openai", "openrouter", etc.
# If all retries exhausted and no fallback: pause pipeline, retry on next poll.
# The ticket is NOT failed.
```

---

## Model Routing

Route each pipeline role to a specific provider and model. This lets you use cheaper models for lightweight review roles and your best model for implementation.

```toml
[models]
# Format: "provider:model_name"
planner          = "anthropic:claude-sonnet-4-6"
implementer      = "anthropic:claude-sonnet-4-6"
spec_reviewer    = "anthropic:claude-haiku-4-5"
quality_reviewer = "anthropic:claude-haiku-4-5"
final_reviewer   = "anthropic:claude-sonnet-4-6"
clarifier        = "anthropic:claude-haiku-4-5"
```

---

## Cost Control

```toml
[cost]
max_cost_per_ticket_usd  = 15.00    # Abort + escalate when exceeded
max_cost_per_day_usd     = 150.00   # Pause all pipelines when exceeded
max_cost_per_month_usd   = 3000.00  # Hard stop
alert_threshold_percent  = 80       # Alert at 80% of any budget level
max_llm_calls_per_task   = 8        # Absolute per-task cap (all roles combined)
```

### Per-Model Pricing Override

Default pricing is bundled for common models. Override when provider pricing changes:

```toml
[cost.pricing]
"anthropic:claude-sonnet-4-6" = { input = 3.00,  output = 15.00 }
"anthropic:claude-haiku-4-5"  = { input = 0.80,  output = 4.00  }
"openai:gpt-4o"                         = { input = 2.50,  output = 10.00 }
"openai:o3-mini"                        = { input = 1.10,  output = 4.40  }
```

Units: USD per 1 million tokens.

---

## Pipeline Limits

```toml
[limits]
max_tasks_per_ticket        = 20     # Planner is instructed not to exceed this
max_implementation_retries  = 2      # Per lint/test feedback tier
max_spec_review_cycles      = 2
max_quality_review_cycles   = 1
max_task_duration_secs      = 600    # 10 minutes per task before timeout
max_total_duration_secs     = 7200   # 2 hours per ticket before timeout
context_token_budget        = 80000  # Base token budget per LLM call (scaled by task complexity)
enable_partial_pr           = true   # Create PR with completed tasks on partial failure
enable_clarification        = true   # Ask for clarification on ambiguous tickets
enable_tdd_verification     = true   # Mechanical RED/GREEN TDD verification
search_replace_similarity   = 0.92   # Fuzzy match threshold for SEARCH blocks (0.0–1.0)
search_replace_min_context_lines = 3 # Minimum surrounding lines in each SEARCH block

# Plan quality gate (Wave 1+)
plan_confidence_threshold    = 0.60  # Confidence score below which clarification is triggered (0.0–1.0)

# Cross-task consistency review (Wave 2+)
intermediate_review_interval = 3     # Run consistency review every N completed tasks (0 = disabled)

# Rebase conflict resolution (Wave 2+)
conflict_resolution_token_budget = 40000  # Token budget for LLM-assisted conflict resolution
```

---

## Pipeline Hooks

```toml
[pipeline.hooks]
# List skill names to run at each hook point.
# Hook failures are logged but do not block the pipeline.
post_lint  = []                         # After lint passes (e.g., ["security-scan"])
pre_pr     = []                         # Before PR creation (e.g., ["write-changelog"])
post_pr    = []                         # After PR created (e.g., ["notify-slack"])
post_merge = []                         # After PR merged (e.g., ["deploy-staging"])
```

See [Skills](skills.md) for how to write skill files.

---

## Decomposition

Automatic ticket decomposition breaks oversized tickets into child tracker issues before planning.

```toml
[decompose]
enabled            = false              # Enable automatic decomposition
max_ticket_words   = 150                # Word count threshold for decomposition
max_scope_keywords = 2                  # Scope keyword count threshold ("and", "also", "plus", "additionally")
approval_label     = "foreman-ready"    # Label for approved child tickets
parent_label       = "foreman-decomposed"  # Label applied to decomposed parent tickets
llm_assist         = false              # Use LLM to evaluate decomposition need (vs. keyword heuristic only)
llm_assist_model   = ""                # Override model for decomposition LLM; empty = [models].planner
```

When enabled, tickets exceeding the word count or scope keyword thresholds are decomposed by an LLM into 3–6 focused child tickets. Each child is created in the tracker with a `{approval_label}-pending` label. The parent is labelled with `parent_label` and its status changes to `decomposed`.

Child tickets are not further decomposed (max depth = 1). When all children's PRs merge, the parent is automatically marked `done`.

---

## Secrets Scanning

```toml
[secrets]
enabled = true
# Additional regex patterns beyond built-in defaults
extra_patterns = []
# Files always excluded from LLM context, regardless of content
always_exclude = [".env", ".env.*", "*.pem", "*.key", "*.p12"]
```

Built-in patterns detect: AWS keys, GitHub tokens, private key blocks (`BEGIN RSA PRIVATE KEY`, etc.), and common secret formats. These cannot be disabled individually — use `enabled = false` to turn off the scanner entirely (not recommended).

---

## Rate Limiting

Shared across all pipeline workers for a given provider.

```toml
[rate_limit]
requests_per_minute = 50      # Token bucket refill rate
burst_size          = 10      # Max burst above the steady-state rate
backoff_base_ms     = 1000    # Base delay on HTTP 429
backoff_max_ms      = 60000   # Maximum delay (1 minute)
jitter_percent      = 25      # Random jitter applied to all retry delays
```

---

## Execution Runner

```toml
[runner]
mode = "local"   # local | docker
```

### Local Runner

```toml
[runner.local]
allowed_commands = ["npm", "yarn", "pnpm", "cargo", "go", "pytest", "make", "bun"]
forbidden_paths  = [".env", ".ssh", ".aws", ".gnupg", "*.key", "*.pem"]
```

### Docker Runner

```toml
[runner.docker]
image              = "node:22-slim"  # Default image; override per repo in AGENTS.md
persist_per_ticket = true            # Reuse container across tasks for the same ticket
network            = "none"          # Network isolation (recommended)
cpu_limit          = "2.0"
memory_limit       = "4g"
auto_reinstall_deps = true           # Reinstall deps when package manifests change
```

---

## Database

```toml
[database]
driver = "sqlite"
```

### SQLite (Default)

```toml
[database.sqlite]
path                    = "~/.foreman/foreman.db"
busy_timeout_ms         = 5000    # SQLite PRAGMA busy_timeout
wal_mode                = true    # PRAGMA journal_mode=WAL (required for concurrency)
event_flush_interval_ms = 100     # Batch flush interval for non-critical writes
```

> `max_parallel_tickets` is capped at 3.

---

## Messaging Channel (WhatsApp)

Foreman supports a bidirectional WhatsApp channel for receiving tickets and sending notifications via direct message.

```toml
[channel]
provider = "whatsapp"   # Currently only "whatsapp" is supported; omit to disable

[channel.whatsapp]
session_db      = "~/.foreman/whatsapp.db"   # SQLite session storage for whatsmeow
pairing_mode    = "code"                      # "code" | "qr" — login method (used by `foreman channel login`)
dm_policy       = "allowlist"                 # allowlist | pairing
allowed_numbers = ["+84123456789"]            # E.164 phone numbers allowed to send commands/tickets
```

### DM Policy

- **`allowlist`** (default): Only numbers in `allowed_numbers` can interact. Unknown senders are silently ignored.
- **`pairing`**: Unknown senders receive a pairing code. An operator approves via `foreman pairing approve <CODE>`, which adds the number to `allowed_numbers` in the config file.

### Setup

```bash
# Link your WhatsApp account (pairing code mode):
./foreman channel login --phone +84123456789

# Or scan a QR code:
./foreman channel login --mode qr

# Check connection status:
./foreman channel status
```

### Supported Commands (via WhatsApp DM)

| Command | Action |
|---------|--------|
| `/status` | Show active tickets |
| `/pause` | Pause the daemon |
| `/resume` | Resume the daemon |
| `/cost` | Show daily/monthly spend |

Any non-command message from an allowed sender is classified (via LLM) and can create a new ticket.

### Pairing Management

```bash
./foreman pairing list              # Show pending pairing requests
./foreman pairing approve <CODE>    # Approve and add to allowlist
./foreman pairing revoke <PHONE>    # Remove from allowlist
```

---

## Agent Runner

Controls which runner drives the core pipeline and `agentsdk` skill steps.

- `"builtin"` (default): pipeline planning and implementation use direct `LlmProvider.Complete` calls; the builtin runner is invoked only for `agentsdk` skill steps.
- `"claudecode"` / `"copilot"`: the external runner takes over **both** planning (`AgentPlanner`) and per-task implementation (`runTaskWithAgent`). The agent handles codebase exploration, TDD, and file editing natively.

```toml
[agent_runner]
provider = "builtin"   # builtin | claudecode | copilot
```

### Builtin Runner

```toml
[agent_runner.builtin]
max_turns             = 15
max_context_tokens    = 100000
reflection_interval   = 5      # Run a self-reflection turn every N tool rounds (0 = disabled)
model                 = ""     # Override model; empty = use [models].implementer
                               # e.g. "anthropic:claude-haiku-4-5" for cheaper agent tasks
```

### Claude Code (pipeline agent)

```toml
[agent_runner.claudecode]
bin                  = "claude"
max_turns_default    = 15
timeout_secs_default = 300
# model = "sonnet"   # optional override
```

### Copilot (pipeline agent)

```toml
[agent_runner.copilot]
cli_path             = "copilot"
github_token         = "${GITHUB_TOKEN}"
model                = "gpt-4o"
timeout_secs_default = 300
```

---

## Skills Agent Runner

A separate agent runner configuration for YAML skill `agentsdk` steps. Does **not** draw from the core pipeline LLM budget.

```toml
[skills.agent_runner]
provider = "builtin"   # builtin | claudecode | copilot

# Cost cap for skill agent calls — does NOT draw from the core pipeline budget.
max_cost_per_ticket_usd = 2.00
max_turns_default       = 10
timeout_secs_default    = 120
```

### Builtin Runner (skills)

```toml
[skills.agent_runner.builtin]
default_allowed_tools = ["Read", "Glob", "Grep"]
reflection_interval   = 5
model                 = ""   # empty = use [models].implementer
```

### Claude Code

```toml
[skills.agent_runner.claudecode]
bin                  = "claude"   # resolved via $PATH if relative
default_allowed_tools = ["Read", "Edit", "Glob", "Grep", "Bash"]
max_turns_default    = 10
timeout_secs_default = 180
max_budget_usd       = 2.00
# model = "sonnet"             # optional: override default model
```

### Copilot

```toml
[skills.agent_runner.copilot]
cli_path             = "copilot"
github_token         = "${GITHUB_TOKEN}"
model                = "gpt-4o"
default_allowed_tools = ["Read", "Edit", "Glob", "Grep", "Bash"]
timeout_secs_default = 180
```

---

## MCP Servers (stdio transport)

Connect Foreman's builtin agent runner to external MCP servers via stdin/stdout subprocess.

```toml
[mcp]
resource_max_bytes = 524288   # Max bytes per MCP resource read (default: 512 KB)

[[mcp.servers]]
name    = "internal-db"
command = "npx"
args    = ["-y", "@company/db-mcp-server"]
allowed_tools          = ["query", "schema"]   # optional whitelist
restart_policy         = "on-failure"           # always | never | on-failure (default: on-failure)
max_restarts           = 3                      # default: 3
restart_delay_secs     = 2                      # default: 2
health_check_interval_secs = 30                 # Ping interval; 0 = disabled (default: 30)
[mcp.servers.env]
DB_URL = "${DATABASE_URL}"                      # explicit env passthrough only
```

Multiple servers can be configured by repeating `[[mcp.servers]]` blocks. Tools from all registered servers are automatically added to the builtin agent's tool registry with normalized names (`mcp_{server}_{tool}`, max 64 chars).

If a server exceeds its restart budget, its tools are marked unavailable and the agent continues with built-in tools only.

---

## Context Generation

```toml
[context]
context_generate_max_tokens = 32000   # Token budget for LLM prompt when generating AGENTS.md
context_feedback_boost      = 1.5     # Score multiplier for files seen in prior similar tasks
```

See [Context Generate](getting-started.md#generating-agentsmd) for usage.

---

| Variable | Used By |
|---|---|
| `ANTHROPIC_API_KEY` | `[llm.anthropic] api_key` |
| `OPENAI_API_KEY` | `[llm.openai] api_key` |
| `OPENROUTER_API_KEY` | `[llm.openrouter] api_key` |
| `GITHUB_TOKEN` | `[tracker.github] token` and `[git.github] token` |
| `JIRA_API_TOKEN` | `[tracker.jira] api_token` |
| `LINEAR_API_KEY` | `[tracker.linear] api_key` |
| `GITLAB_TOKEN` | `[git.gitlab] token` |
| `FOREMAN_DASHBOARD_TOKEN` | `[dashboard] auth_token` |

---

## See Also

- [Integrations](integrations.md) — setup guides for each issue tracker and LLM provider
- [Getting Started](getting-started.md) — minimal working configuration examples
- [Skills](skills.md) — `[pipeline.hooks]` and skill YAML reference
- [Agent Runner](agent-runner.md) — `[agent_runner]` options for builtin, Claude Code, and Copilot
