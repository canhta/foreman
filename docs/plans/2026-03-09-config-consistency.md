# Config Consistency Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate all inconsistencies between `foreman.example.toml`, `docs/configuration.md`, and the Go config structs — adding missing sub-config structs, wiring them into the build functions, and correcting all phantom/outdated doc fields.

**Architecture:** Three layers of fixes: (1) add missing Go structs to `internal/models/config.go` and defaults to `internal/config/config.go`, (2) wire new structs into `cmd/start.go` and `cmd/doctor.go`, (3) sync `foreman.example.toml` and `docs/configuration.md` to reflect the real system.

**Tech Stack:** Go structs with `mapstructure` tags, Viper defaults, TOML, Markdown.

---

## Inventory of All Issues

### A — Struct missing (docs/example describe config that doesn't exist in code)

| # | Issue | Affected files |
|---|---|---|
| A1 | `TrackerConfig` has no provider sub-structs — Jira/Linear wired with empty strings | `internal/models/config.go`, `cmd/start.go`, `cmd/doctor.go` |
| A2 | `GitConfig` has no provider sub-structs — GitHub token read from raw env var, GitLab absent | `internal/models/config.go`, `cmd/start.go` |
| A3 | No top-level `[agent_runner]` struct — docs describe it, struct only has `[skills.agent_runner]` | `internal/models/config.go`, `internal/config/config.go` |
| A4 | `BuiltinRunnerConfig` missing `max_turns`, `max_context_tokens`, `reflection_interval` | `internal/models/config.go`, `internal/config/config.go` |
| A5 | `LimitsConfig` missing `conflict_resolution_token_budget` | `internal/models/config.go`, `internal/config/config.go` |
| A6 | `ContextConfig` missing `context_generate_max_tokens` | `internal/models/config.go`, `internal/config/config.go` |

### B — Struct exists but undocumented (code has it, docs/example don't mention it)

| # | Issue | Affected files |
|---|---|---|
| B1 | `LLMConfig.EmbeddingProvider` / `EmbeddingModel` — in struct, not in docs or example | `docs/configuration.md`, `foreman.example.toml` |
| B2 | `DecomposeConfig.LLMAssist` / `LLMAssistModel` — in struct, not in docs or example | `docs/configuration.md`, `foreman.example.toml` |
| B3 | `ContextConfig.ContextFeedbackBoost` — in struct, not in docs or example | `docs/configuration.md`, `foreman.example.toml` |
| B4 | `WhatsAppChannelConfig.PairingMode` — in struct, not documented | `docs/configuration.md` |
| B5 | Daemon fields `max_parallel_tasks`, `task_timeout_minutes`, `merge_check_interval_secs` — in struct, missing from example | `foreman.example.toml` |

### C — Doc/example has wrong content (references non-existent struct fields)

| # | Issue | Affected files |
|---|---|---|
| C1 | `docs/configuration.md` `[agent_runner]` section — wrong key name (should be top-level or corrected) | `docs/configuration.md` |
| C2 | `docs/configuration.md` `[context]` shows `context_generate_max_tokens` instead of `context_feedback_boost` | `docs/configuration.md` |
| C3 | `docs/configuration.md` `[limits]` shows `conflict_resolution_token_budget` but field absent from struct | `docs/configuration.md` |
| C4 | `docs/configuration.md` `[[mcp.servers]]` shows `mcp_resource_max_bytes` per-server — actually lives at `[mcp]` level | `docs/configuration.md` |
| C5 | `docs/configuration.md` `[agent_runner.builtin]` shows `max_turns`, `max_context_tokens`, `reflection_interval` but struct lacks them | `docs/configuration.md`, `internal/models/config.go` |

### D — Example TOML missing entire sections

| # | Missing section |
|---|---|
| D1 | `[tracker.github]`, `[tracker.jira]`, `[tracker.linear]`, `[tracker.local_file]` |
| D2 | `[git.github]`, `[git.gitlab]` |
| D3 | `[channel]` / `[channel.whatsapp]` |
| D4 | `[[mcp.servers]]` example block |
| D5 | `[decompose]` section |
| D6 | `[limits]` advanced fields: `plan_confidence_threshold`, `intermediate_review_interval` |
| D7 | Top-level `[agent_runner]` section (after A3 is resolved) |

---

## Task 1: Add tracker provider sub-config structs

**Files:**
- Modify: `internal/models/config.go`
- Modify: `internal/config/config.go`

**Context:** `TrackerConfig` currently holds only top-level fields. `buildTracker` passes empty strings for Jira and Linear — those providers are essentially broken. GitHub tracker reads its token from raw `os.Getenv("GITHUB_TOKEN")` with no config path.

**Step 1: Add sub-structs to `internal/models/config.go`**

Add after the existing `TrackerConfig` struct:

```go
type TrackerConfig struct {
	Provider                  string              `mapstructure:"provider"`
	PickupLabel               string              `mapstructure:"pickup_label"`
	ClarificationLabel        string              `mapstructure:"clarification_label"`
	ClarificationTimeoutHours int                 `mapstructure:"clarification_timeout_hours"`
	Jira                      JiraTrackerConfig   `mapstructure:"jira"`
	GitHub                    GitHubTrackerConfig `mapstructure:"github"`
	Linear                    LinearTrackerConfig `mapstructure:"linear"`
	LocalFile                 LocalFileTrackerConfig `mapstructure:"local_file"`
}

type JiraTrackerConfig struct {
	BaseURL            string `mapstructure:"base_url"`
	Email              string `mapstructure:"email"`
	APIToken           string `mapstructure:"api_token"`
	ProjectKey         string `mapstructure:"project_key"`
	StatusInProgress   string `mapstructure:"status_in_progress"`
	StatusInReview     string `mapstructure:"status_in_review"`
	StatusDone         string `mapstructure:"status_done"`
	StatusBlocked      string `mapstructure:"status_blocked"`
}

type GitHubTrackerConfig struct {
	Owner   string `mapstructure:"owner"`
	Repo    string `mapstructure:"repo"`
	Token   string `mapstructure:"token"`
	BaseURL string `mapstructure:"base_url"` // empty = https://api.github.com
}

type LinearTrackerConfig struct {
	APIKey  string `mapstructure:"api_key"`
	TeamID  string `mapstructure:"team_id"`
	BaseURL string `mapstructure:"base_url"` // empty = https://api.linear.app
}

type LocalFileTrackerConfig struct {
	Path string `mapstructure:"path"`
}
```

**Step 2: Add defaults in `internal/config/config.go`**

Add in `setDefaults`:
```go
v.SetDefault("tracker.jira.status_in_progress", "In Progress")
v.SetDefault("tracker.jira.status_in_review", "In Review")
v.SetDefault("tracker.jira.status_done", "Done")
v.SetDefault("tracker.jira.status_blocked", "Blocked")
v.SetDefault("tracker.local_file.path", "./tickets")
```

**Step 3: Add env var expansion in `expandEnvVars`**

```go
cfg.Tracker.Jira.APIToken = expandEnv(cfg.Tracker.Jira.APIToken)
cfg.Tracker.GitHub.Token = expandEnv(cfg.Tracker.GitHub.Token)
cfg.Tracker.Linear.APIKey = expandEnv(cfg.Tracker.Linear.APIKey)
```

**Step 4: Run tests**

```bash
go test ./internal/config/... ./internal/models/...
```
Expected: PASS (no logic changes yet, just struct additions)

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go
git commit -m "feat: add tracker provider sub-config structs (jira, github, linear, local_file)"
```

---

## Task 2: Wire tracker sub-configs into buildTracker

**Files:**
- Modify: `cmd/start.go:372-392`
- Modify: `cmd/doctor.go:140-162`

**Context:** `buildTracker` currently reads GitHub token from `os.Getenv` and passes empty strings for Jira/Linear. After Task 1, the config struct has the real values.

**Step 1: Update `buildTracker` in `cmd/start.go`**

Replace the function body:

```go
func buildTracker(cfg *models.Config) (tracker.IssueTracker, error) {
	t := cfg.Tracker
	switch t.Provider {
	case "github":
		gh := t.GitHub
		token := gh.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN") // fallback for backwards compat
		}
		if token == "" {
			return nil, fmt.Errorf("tracker.github.token (or GITHUB_TOKEN env) required for github tracker")
		}
		owner, repo := gh.Owner, gh.Repo
		if owner == "" || repo == "" {
			owner, repo = parseOwnerRepo(cfg.Git.CloneURL) // fallback
		}
		return tracker.NewGitHubIssuesTracker(gh.BaseURL, token, owner, repo, t.PickupLabel), nil
	case "jira":
		j := t.Jira
		if j.BaseURL == "" {
			return nil, fmt.Errorf("tracker.jira.base_url is required")
		}
		if j.APIToken == "" {
			return nil, fmt.Errorf("tracker.jira.api_token is required")
		}
		if j.ProjectKey == "" {
			return nil, fmt.Errorf("tracker.jira.project_key is required")
		}
		return tracker.NewJiraTracker(j.BaseURL, j.Email, j.APIToken, j.ProjectKey, t.PickupLabel), nil
	case "linear":
		l := t.Linear
		if l.APIKey == "" {
			return nil, fmt.Errorf("tracker.linear.api_key is required")
		}
		return tracker.NewLinearTracker(l.APIKey, t.PickupLabel, l.BaseURL), nil
	case "local_file":
		path := t.LocalFile.Path
		if path == "" {
			path = "./tickets"
		}
		return tracker.NewLocalFileTracker(path, t.PickupLabel), nil
	default:
		return nil, fmt.Errorf("unknown tracker provider: %s", t.Provider)
	}
}
```

**Step 2: Update `buildTrackerForDoctor` in `cmd/doctor.go`**

Apply the same pattern — replace the `jira` and `linear` cases:

```go
case "jira":
    j := cfg.Tracker.Jira
    if j.BaseURL == "" || j.APIToken == "" || j.ProjectKey == "" {
        return nil, fmt.Errorf("tracker.jira.base_url, api_token, and project_key are required")
    }
    return tracker.NewJiraTracker(j.BaseURL, j.Email, j.APIToken, j.ProjectKey, cfg.Tracker.PickupLabel), nil
case "linear":
    l := cfg.Tracker.Linear
    if l.APIKey == "" {
        return nil, fmt.Errorf("tracker.linear.api_key is required")
    }
    return tracker.NewLinearTracker(l.APIKey, cfg.Tracker.PickupLabel, l.BaseURL), nil
```

**Step 3: Run tests**

```bash
go build ./...
go test ./cmd/... 2>/dev/null || true   # cmd tests may need integration setup
go test ./internal/tracker/...
```
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/start.go cmd/doctor.go
git commit -m "fix: wire tracker provider sub-configs — jira and linear now configurable"
```

---

## Task 3: Add git provider sub-config structs

**Files:**
- Modify: `internal/models/config.go`
- Modify: `internal/config/config.go`

**Context:** `GitConfig` has no provider sub-structs. `buildPRCreator` reads `GITHUB_TOKEN` from env directly. GitLab has no implementation path at all.

**Step 1: Add sub-structs to `internal/models/config.go`**

Extend `GitConfig`:

```go
type GitConfig struct {
	Provider       string            `mapstructure:"provider"`
	Backend        string            `mapstructure:"backend"`
	CloneURL       string            `mapstructure:"clone_url"`
	DefaultBranch  string            `mapstructure:"default_branch"`
	BranchPrefix   string            `mapstructure:"branch_prefix"`
	PRReviewers    []string          `mapstructure:"pr_reviewers"`
	AutoPush       bool              `mapstructure:"auto_push"`
	PRDraft        bool              `mapstructure:"pr_draft"`
	RebaseBeforePR bool              `mapstructure:"rebase_before_pr"`
	GitHub         GitHubConfig      `mapstructure:"github"`
	GitLab         GitLabConfig      `mapstructure:"gitlab"`
}

type GitHubConfig struct {
	Token   string `mapstructure:"token"`
	BaseURL string `mapstructure:"base_url"` // empty = https://api.github.com
}

type GitLabConfig struct {
	Token   string `mapstructure:"token"`
	BaseURL string `mapstructure:"base_url"` // default: https://gitlab.com
}
```

**Step 2: Add defaults in `internal/config/config.go`**

```go
v.SetDefault("git.gitlab.base_url", "https://gitlab.com")
```

**Step 3: Add env var expansion in `expandEnvVars`**

```go
cfg.Git.GitHub.Token = expandEnv(cfg.Git.GitHub.Token)
cfg.Git.GitLab.Token = expandEnv(cfg.Git.GitLab.Token)
```

**Step 4: Run tests**

```bash
go test ./internal/config/... ./internal/models/...
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go
git commit -m "feat: add git provider sub-config structs (github, gitlab)"
```

---

## Task 4: Wire git sub-configs into buildPRCreator / buildPRChecker

**Files:**
- Modify: `cmd/start.go:401-417`

**Step 1: Update `buildPRCreator` and `buildPRChecker`**

```go
func buildPRCreator(cfg *models.Config) git.PRCreator {
	token := cfg.Git.GitHub.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN") // backwards compat fallback
	}
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRCreator(cfg.Git.GitHub.BaseURL, token, owner, repo)
}

func buildPRChecker(cfg *models.Config) git.PRChecker {
	token := cfg.Git.GitHub.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRChecker(cfg.Git.GitHub.BaseURL, token, owner, repo)
}
```

**Step 2: Run tests**

```bash
go build ./...
go test ./internal/git/...
```
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/start.go
git commit -m "fix: use git.github.token config for PR creator/checker with env fallback"
```

---

## Task 5: Add top-level agent_runner config + expand BuiltinRunnerConfig

**Files:**
- Modify: `internal/models/config.go`
- Modify: `internal/config/config.go`

**Context:** Docs describe `[agent_runner]` (for core pipeline) separately from `[skills.agent_runner]` (for YAML skills). Currently only `[skills.agent_runner]` exists in the struct. Also, `BuiltinRunnerConfig` is missing `MaxTurns`, `MaxContextTokens`, and `ReflectionInterval` that both sections document.

**Step 1: Expand `BuiltinRunnerConfig` in `internal/models/config.go`**

```go
type BuiltinRunnerConfig struct {
	Model               string   `mapstructure:"model"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	MaxTurns            int      `mapstructure:"max_turns"`
	MaxContextTokens    int      `mapstructure:"max_context_tokens"`
	ReflectionInterval  int      `mapstructure:"reflection_interval"`
}
```

**Step 2: Add top-level `AgentRunner` field to `Config`**

```go
type Config struct {
	// ... existing fields ...
	AgentRunner AgentRunnerConfig `mapstructure:"agent_runner"`
	// ... rest unchanged ...
}
```

> Note: `AgentRunnerConfig` is already defined (used by `SkillsConfig.AgentRunner`). The same type serves both.

**Step 3: Add defaults in `internal/config/config.go`**

Add a new block in `setDefaults`:
```go
// Core pipeline agent runner defaults
v.SetDefault("agent_runner.provider", "builtin")
v.SetDefault("agent_runner.builtin.max_turns", 15)
v.SetDefault("agent_runner.builtin.max_context_tokens", 100000)
v.SetDefault("agent_runner.builtin.reflection_interval", 5)
v.SetDefault("agent_runner.builtin.default_allowed_tools", []string{"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "Bash", "GetErrors"})
v.SetDefault("agent_runner.claudecode.bin", "claude")
v.SetDefault("agent_runner.claudecode.max_turns_default", 15)
v.SetDefault("agent_runner.claudecode.timeout_secs_default", 300)
v.SetDefault("agent_runner.copilot.cli_path", "copilot")
v.SetDefault("agent_runner.copilot.model", "gpt-4o")
v.SetDefault("agent_runner.copilot.timeout_secs_default", 300)

// Skills agent runner: also add missing builtin fields
v.SetDefault("skills.agent_runner.builtin.max_turns", 10)
v.SetDefault("skills.agent_runner.builtin.max_context_tokens", 50000)
v.SetDefault("skills.agent_runner.builtin.reflection_interval", 0)
```

**Step 4: Run tests**

```bash
go test ./internal/config/... ./internal/models/...
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go
git commit -m "feat: add top-level [agent_runner] config and expand BuiltinRunnerConfig fields"
```

---

## Task 6: Add missing Limits and Context config fields

**Files:**
- Modify: `internal/models/config.go`
- Modify: `internal/config/config.go`

**Context:** Docs describe `conflict_resolution_token_budget` in `[limits]` and `context_generate_max_tokens` in `[context]`, but neither exists in the struct.

**Step 1: Add to `LimitsConfig` in `internal/models/config.go`**

```go
type LimitsConfig struct {
	// ... existing fields ...
	ConflictResolutionTokenBudget int `mapstructure:"conflict_resolution_token_budget"`
}
```

**Step 2: Add to `ContextConfig`**

```go
type ContextConfig struct {
	ContextFeedbackBoost    float64 `mapstructure:"context_feedback_boost"`
	ContextGenerateMaxTokens int    `mapstructure:"context_generate_max_tokens"`
}
```

**Step 3: Add defaults in `internal/config/config.go`**

```go
v.SetDefault("limits.conflict_resolution_token_budget", 40000)
v.SetDefault("context.context_generate_max_tokens", 32000)
```

**Step 4: Run tests**

```bash
go test ./internal/config/... ./internal/models/...
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go
git commit -m "feat: add conflict_resolution_token_budget and context_generate_max_tokens config fields"
```

---

## Task 7: Fix docs/configuration.md — correct phantom fields and wrong section names

**Files:**
- Modify: `docs/configuration.md`

**Changes needed (each is a targeted edit):**

**7a — Fix `[agent_runner]` section header** (lines ~449-488)

The section is labeled `[agent_runner]` which now maps to the real top-level `[agent_runner]` struct added in Task 5. Verify the TOML keys shown match the struct:
- `[agent_runner.builtin]` → fields: `max_turns`, `max_context_tokens`, `reflection_interval`, `model` ✓ (now in struct)
- `[agent_runner.claudecode]` → fields: `bin`, `max_turns_default`, `timeout_secs_default` ✓
- `[agent_runner.copilot]` → fields: `cli_path`, `github_token`, `model`, `timeout_secs_default` ✓

No text changes needed once Task 5 is done — the section was correct, the struct was wrong.

**7b — Fix `[context]` section** (lines ~564-570)

Replace:
```toml
[context]
context_generate_max_tokens = 32000
```
With:
```toml
[context]
context_generate_max_tokens = 32000   # Token budget for LLM prompt when generating AGENTS.md
context_feedback_boost      = 1.5     # Score multiplier for files seen in prior similar tasks
```

**7c — Fix `mcp_resource_max_bytes` placement** (lines ~547-558)

Remove `mcp_resource_max_bytes` from inside the `[[mcp.servers]]` block. Add a separate `[mcp]` section above it:

```toml
[mcp]
resource_max_bytes = 524288   # Max bytes per MCP resource read (default: 512 KB)

[[mcp.servers]]
name    = "internal-db"
command = "npx"
args    = ["-y", "@company/db-mcp-server"]
...
# (no mcp_resource_max_bytes here)
```

**7d — Add embedding fields to `## LLM Providers` section**

After the `[llm]` block, add:

```toml
[llm]
default_provider   = "anthropic"
embedding_provider = ""            # "openai" | "anthropic" — provider for embedding calls
embedding_model    = ""            # e.g. "text-embedding-3-small", "voyage-code-3"
```

**7e — Add `llm_assist` fields to `## Decomposition` section**

```toml
[decompose]
enabled            = false
max_ticket_words   = 150
max_scope_keywords = 2
approval_label     = "foreman-ready"
parent_label       = "foreman-decomposed"
llm_assist         = false    # Use LLM to evaluate decomposition need (vs. keyword heuristic only)
llm_assist_model   = ""       # Override model for decomposition LLM; empty = [models].planner
```

**7f — Add `pairing_mode` to WhatsApp section**

In the `[channel.whatsapp]` block add:
```toml
pairing_mode    = ""          # "phone" | "qr" — login method (used by `foreman channel login`)
```

**7g — Document tracker sub-sections correctly**

The docs already show `[tracker.jira]`, `[tracker.github]`, `[tracker.linear]`, `[tracker.local_file]`. After Task 1 adds the structs these are valid. Verify all field names match the new structs (check `status_in_progress` etc. for Jira).

**Step 1: Apply all edits 7a–7g to `docs/configuration.md`**

Make each change as a targeted Edit. Read the current file line ranges before each edit to locate the exact text.

**Step 2: Commit**

```bash
git add docs/configuration.md
git commit -m "docs: fix configuration.md — correct phantom fields, add missing documented options"
```

---

## Task 8: Update `foreman.example.toml` — add all missing sections

**Files:**
- Modify: `foreman.example.toml`

**Step 1: Expand `[daemon]` section**

Current:
```toml
[daemon]
poll_interval_secs = 60
idle_poll_interval_secs = 300
max_parallel_tickets = 3
work_dir = "~/.foreman/work"
log_level = "info"
log_format = "json"
lock_ttl_seconds = 3600
```

Replace with:
```toml
[daemon]
poll_interval_secs          = 60
idle_poll_interval_secs     = 300
max_parallel_tickets        = 3      # Max 3 for SQLite; use PostgreSQL for more
max_parallel_tasks          = 3      # Concurrent tasks per ticket (DAG executor)
task_timeout_minutes        = 15     # Per-task timeout in minutes
merge_check_interval_secs   = 300    # How often to poll PR merge status
work_dir                    = "~/.foreman/work"
log_level                   = "info"    # trace | debug | info | warn | error
log_format                  = "json"    # json | pretty
lock_ttl_seconds            = 3600
```

**Step 2: Add tracker sub-sections after `[tracker]`**

```toml
[tracker]
provider = "local_file"  # local_file, jira, github, linear
pickup_label = "foreman-ready"
clarification_label = "foreman-needs-info"
clarification_timeout_hours = 72

# ── GitHub Issues ──────────────────────────────────────
# [tracker.github]
# token  = "${GITHUB_TOKEN}"
# owner  = "your-org"
# repo   = "your-repo"
# base_url = ""    # optional: override for GitHub Enterprise

# ── Jira ───────────────────────────────────────────────
# [tracker.jira]
# base_url    = "https://yourcompany.atlassian.net"
# email       = "bot@yourcompany.com"
# api_token   = "${JIRA_API_TOKEN}"
# project_key = "PROJ"
# status_in_progress = "In Progress"
# status_in_review   = "In Review"
# status_done        = "Done"
# status_blocked     = "Blocked"

# ── Linear ─────────────────────────────────────────────
# [tracker.linear]
# api_key = "${LINEAR_API_KEY}"
# team_id = "TEAM_ID"
# base_url = ""   # optional override

# ── Local File (default, no auth needed) ───────────────
# [tracker.local_file]
# path = "./tickets"
```

**Step 3: Add git sub-sections after `[git]`**

```toml
[git]
provider = "github"
backend = "native"
clone_url = "https://github.com/your-org/your-repo.git"
default_branch = "main"
auto_push = true
pr_draft = true
pr_reviewers = []
branch_prefix = "foreman"
rebase_before_pr = true

[git.github]
token = "${GITHUB_TOKEN}"
# base_url = ""    # optional: GitHub Enterprise URL

# [git.gitlab]
# token    = "${GITLAB_TOKEN}"
# base_url = "https://gitlab.com"   # override for self-hosted
```

**Step 4: Add `[decompose]` section**

```toml
[decompose]
enabled            = false
max_ticket_words   = 150
max_scope_keywords = 2
approval_label     = "foreman-ready"
parent_label       = "foreman-decomposed"
llm_assist         = false   # use LLM heuristic (slower but smarter)
# llm_assist_model = ""      # override model; empty = [models].planner
```

**Step 5: Expand `[limits]` with advanced fields**

After existing limits fields, add:
```toml
# Plan quality gate
plan_confidence_threshold    = 0.60   # Clarify if plan confidence is below this (0 = disabled)

# Cross-task consistency review
intermediate_review_interval = 3      # Run review every N completed tasks (0 = disabled)

# Rebase conflict resolution
conflict_resolution_token_budget = 40000
```

**Step 6: Add `[channel]` / `[channel.whatsapp]` section**

```toml
# ─── Messaging Channel ────────────────────────────────
# Bidirectional WhatsApp channel for commands and ticket creation.
# Omit [channel] entirely to disable.

# [channel]
# provider = "whatsapp"
#
# [channel.whatsapp]
# session_db      = "~/.foreman/whatsapp.db"
# pairing_mode    = "phone"          # phone | qr
# dm_policy       = "allowlist"      # allowlist | pairing
# allowed_numbers = ["+84123456789"] # E.164 format
```

**Step 7: Add `[[mcp.servers]]` example**

```toml
# ─── MCP Servers ──────────────────────────────────────
# Connect Foreman's builtin agent to external MCP servers via stdin/stdout.

# [mcp]
# resource_max_bytes = 524288   # Max bytes per MCP resource read (default: 512 KB)

# [[mcp.servers]]
# name    = "internal-db"
# command = "npx"
# args    = ["-y", "@company/db-mcp-server"]
# allowed_tools          = ["query", "schema"]
# restart_policy         = "on-failure"
# max_restarts           = 3
# restart_delay_secs     = 2
# health_check_interval_secs = 30
# [mcp.servers.env]
# DB_URL = "${DATABASE_URL}"
```

**Step 8: Add top-level `[agent_runner]` section**

```toml
# ─── Core Pipeline Agent Runner ───────────────────────
# Configures the agent runner used by the main pipeline (not skills).
# See [skills.agent_runner] for the skills-specific runner.

[agent_runner]
provider = "builtin"   # builtin | claudecode | copilot

[agent_runner.builtin]
max_turns           = 15
max_context_tokens  = 100000
reflection_interval = 5      # Self-reflection turn every N tool rounds (0 = disabled)
# model = ""                 # override; empty = [models].implementer

# [agent_runner.claudecode]
# bin                  = "claude"
# max_turns_default    = 15
# timeout_secs_default = 300
# # model = "sonnet"

# [agent_runner.copilot]
# cli_path             = "copilot"
# github_token         = "${GITHUB_TOKEN}"
# model                = "gpt-4o"
# timeout_secs_default = 300
```

**Step 9: Add embedding fields to `[llm]` section**

```toml
[llm]
default_provider = "anthropic"
# embedding_provider = ""    # "openai" | "anthropic"
# embedding_model    = ""    # e.g. "text-embedding-3-small"
```

**Step 10: Add `[context]` section**

```toml
[context]
context_generate_max_tokens = 32000   # Token budget for `foreman context generate`
context_feedback_boost      = 1.5     # Score multiplier for files from prior similar tasks
```

**Step 11: Run build to verify TOML is valid**

```bash
go run ./main.go --config foreman.example.toml --help 2>&1 | head -5
```
Expected: Help text printed (no "failed to read config" error)

**Step 12: Commit**

```bash
git add foreman.example.toml
git commit -m "docs: update foreman.example.toml with all missing sections and fields"
```

---

## Task 9: Update env var reference table in docs/configuration.md

**Files:**
- Modify: `docs/configuration.md:574-585`

**Step 1: Add new env vars to the table**

Add rows:
```markdown
| `JIRA_API_TOKEN` | `[tracker.jira] api_token` |
| `LINEAR_API_KEY` | `[tracker.linear] api_key` |
| `GITLAB_TOKEN`   | `[git.gitlab] token` |
```

These already exist in the table — verify they're present and correct. If not, add them.

**Step 2: Commit**

```bash
git add docs/configuration.md
git commit -m "docs: ensure env var reference table is complete"
```

---

## Task 10: Validate end-to-end

**Step 1: Full build**

```bash
go build -o /tmp/foreman-test ./main.go
```
Expected: No errors

**Step 2: Load example config without error**

```bash
/tmp/foreman-test doctor --config foreman.example.toml 2>&1 | head -20
```
Expected: Doctor output (may show "not configured" for various integrations, but no "failed to read config" panic)

**Step 3: Run all tests**

```bash
go test ./...
```
Expected: All tests pass (or same failures as before this work)

**Step 4: Commit**

```bash
git add .
git commit -m "chore: config consistency — final validation pass"
```

---

## Summary of Changes

| File | Changes |
|---|---|
| `internal/models/config.go` | +`JiraTrackerConfig`, `GitHubTrackerConfig`, `LinearTrackerConfig`, `LocalFileTrackerConfig` fields on `TrackerConfig`; +`GitHubConfig`, `GitLabConfig` on `GitConfig`; expand `BuiltinRunnerConfig`; +`AgentRunner` on `Config`; +`ConflictResolutionTokenBudget` on `LimitsConfig`; +`ContextGenerateMaxTokens` on `ContextConfig` |
| `internal/config/config.go` | +defaults for all new fields; +env var expansion for tracker/git tokens |
| `cmd/start.go` | Fix `buildTracker`, `buildPRCreator`, `buildPRChecker` to use config sub-structs |
| `cmd/doctor.go` | Fix `buildTrackerForDoctor` for Jira and Linear |
| `docs/configuration.md` | Fix `[context]` section, `mcp_resource_max_bytes` placement, add embedding/decompose/WhatsApp fields |
| `foreman.example.toml` | Add `[daemon]` fields, tracker/git sub-sections, `[decompose]`, `[channel]`, `[[mcp.servers]]`, `[agent_runner]`, `[context]`, advanced limits |
