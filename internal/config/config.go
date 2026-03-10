package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/viper"
)

func LoadDefaults() (*models.Config, error) {
	v := viper.New()
	setDefaults(v)
	bindEnvOverrides(v)

	var cfg models.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func LoadFromFile(path string) (*models.Config, error) {
	v := viper.New()
	setDefaults(v)
	bindEnvOverrides(v)

	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg models.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	expandEnvVars(&cfg)
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("daemon.poll_interval_secs", 60)
	v.SetDefault("daemon.idle_poll_interval_secs", 300)
	v.SetDefault("daemon.lock_ttl_seconds", 3600)
	v.SetDefault("daemon.work_dir", "") // set per-project by merge.go; never create a global work dir
	v.SetDefault("daemon.log_level", "info")
	v.SetDefault("daemon.log_format", "json")
	v.SetDefault("daemon.max_parallel_tickets", 3)
	v.SetDefault("daemon.max_parallel_tasks", 3)
	v.SetDefault("daemon.task_timeout_minutes", 15)
	v.SetDefault("daemon.merge_check_interval_secs", 60)

	v.SetDefault("dashboard.enabled", true)
	v.SetDefault("dashboard.port", 8080)
	v.SetDefault("dashboard.host", "127.0.0.1")

	v.SetDefault("tracker.provider", "local_file")
	v.SetDefault("tracker.pickup_label", "foreman-ready")
	v.SetDefault("tracker.clarification_label", "foreman-needs-info")
	v.SetDefault("tracker.clarification_timeout_hours", 72)
	v.SetDefault("tracker.jira.status_in_progress", "In Progress")
	v.SetDefault("tracker.jira.status_in_review", "In Review")
	v.SetDefault("tracker.jira.status_done", "Done")
	v.SetDefault("tracker.jira.status_blocked", "Blocked")
	v.SetDefault("tracker.local_file.path", "./tickets")

	v.SetDefault("git.provider", "github")
	v.SetDefault("git.backend", "native")
	v.SetDefault("git.default_branch", "main")
	v.SetDefault("git.auto_push", true)
	v.SetDefault("git.pr_draft", true)
	v.SetDefault("git.branch_prefix", "foreman/")
	v.SetDefault("git.rebase_before_pr", true)
	v.SetDefault("git.gitlab.base_url", "https://gitlab.com")

	v.SetDefault("llm.default_provider", "anthropic")
	v.SetDefault("llm.outage.max_connection_retries", 3)
	v.SetDefault("llm.outage.connection_retry_delay_secs", 30)

	v.SetDefault("models.planner", "anthropic:claude-sonnet-4-6")
	v.SetDefault("models.implementer", "anthropic:claude-sonnet-4-6")
	v.SetDefault("models.spec_reviewer", "anthropic:claude-haiku-4-5")
	v.SetDefault("models.quality_reviewer", "anthropic:claude-haiku-4-5")
	v.SetDefault("models.final_reviewer", "anthropic:claude-sonnet-4-6")
	v.SetDefault("models.clarifier", "anthropic:claude-haiku-4-5")

	v.SetDefault("cost.max_cost_per_ticket_usd", 15.0)
	v.SetDefault("cost.max_cost_per_day_usd", 150.0)
	v.SetDefault("cost.max_cost_per_month_usd", 3000.0)
	v.SetDefault("cost.alert_threshold_percent", 80)
	v.SetDefault("cost.max_llm_calls_per_task", 8)

	v.SetDefault("limits.max_tasks_per_ticket", 20)
	v.SetDefault("limits.max_implementation_retries", 2)
	v.SetDefault("limits.max_spec_review_cycles", 2)
	v.SetDefault("limits.max_quality_review_cycles", 1)
	v.SetDefault("limits.max_task_duration_secs", 600)
	v.SetDefault("limits.max_total_duration_secs", 7200)
	v.SetDefault("limits.context_token_budget", 80000)
	v.SetDefault("limits.enable_partial_pr", true)
	v.SetDefault("limits.enable_clarification", true)
	v.SetDefault("limits.enable_tdd_verification", true)
	v.SetDefault("limits.search_replace_similarity", 0.92)
	v.SetDefault("limits.search_replace_min_context_lines", 3)
	v.SetDefault("limits.intermediate_review_interval", 3)
	v.SetDefault("limits.conflict_resolution_token_budget", 40000)

	v.SetDefault("secrets.enabled", true)
	v.SetDefault("secrets.always_exclude", []string{".env", ".env.*", "*.pem", "*.key", "*.p12"})

	v.SetDefault("rate_limit.requests_per_minute", 50)
	v.SetDefault("rate_limit.burst_size", 10)
	v.SetDefault("rate_limit.backoff_base_ms", 1000)
	v.SetDefault("rate_limit.backoff_max_ms", 60000)
	v.SetDefault("rate_limit.jitter_percent", 25)

	v.SetDefault("decompose.enabled", false)
	v.SetDefault("decompose.max_ticket_words", 150)
	v.SetDefault("decompose.max_scope_keywords", 2)
	v.SetDefault("decompose.approval_label", "foreman-ready")
	v.SetDefault("decompose.parent_label", "foreman-decomposed")
	v.SetDefault("decompose.llm_assist", false)
	v.SetDefault("decompose.llm_assist_model", "")

	v.SetDefault("runner.mode", "local")
	v.SetDefault("runner.local.allowed_commands", []string{"npm", "yarn", "pnpm", "cargo", "go", "pytest", "make", "bun"})

	// MCP defaults
	v.SetDefault("mcp.resource_max_bytes", 512*1024) // 512 KB

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

	// Skills agent runner defaults
	v.SetDefault("skills.agent_runner.provider", "builtin")
	v.SetDefault("skills.agent_runner.max_cost_per_ticket_usd", 2.0)
	v.SetDefault("skills.agent_runner.max_turns_default", 10)
	v.SetDefault("skills.agent_runner.timeout_secs_default", 120)
	v.SetDefault("skills.agent_runner.builtin.default_allowed_tools", []string{"Read", "Glob", "Grep"})
	v.SetDefault("skills.agent_runner.builtin.max_turns", 10)
	v.SetDefault("skills.agent_runner.builtin.max_context_tokens", 50000)
	v.SetDefault("skills.agent_runner.builtin.reflection_interval", 0)
	v.SetDefault("skills.agent_runner.claudecode.bin", "claude")
	v.SetDefault("skills.agent_runner.claudecode.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
	v.SetDefault("skills.agent_runner.claudecode.max_turns_default", 10)
	v.SetDefault("skills.agent_runner.claudecode.timeout_secs_default", 180)
	v.SetDefault("skills.agent_runner.copilot.cli_path", "copilot")
	v.SetDefault("skills.agent_runner.copilot.model", "gpt-4o")
	v.SetDefault("skills.agent_runner.copilot.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
	v.SetDefault("skills.agent_runner.copilot.timeout_secs_default", 180)

	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("prompts_dir", "prompts")
	v.SetDefault("database.sqlite.path", "~/.foreman/global.db") // global daemon DB (auth tokens etc.); project DBs live at projects/<id>/foreman.db
	v.SetDefault("database.sqlite.busy_timeout_ms", 5000)
	v.SetDefault("database.sqlite.wal_mode", true)
	v.SetDefault("database.sqlite.event_flush_interval_ms", 100)

	// Context assembly defaults (REQ-CTX-003)
	v.SetDefault("context.context_feedback_boost", 1.5)
	v.SetDefault("context.context_generate_max_tokens", 32000)
}

// bindEnvOverrides registers environment variable overrides.
// Env vars take precedence over TOML values: FOREMAN_DASHBOARD_PORT, FOREMAN_DASHBOARD_HOST.
func bindEnvOverrides(v *viper.Viper) {
	_ = v.BindEnv("dashboard.port", "FOREMAN_DASHBOARD_PORT")
	_ = v.BindEnv("dashboard.host", "FOREMAN_DASHBOARD_HOST")
}

func expandEnvVars(cfg *models.Config) {
	cfg.LLM.Anthropic.APIKey = expandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.OpenAI.APIKey = expandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.OpenRouter.APIKey = expandEnv(cfg.LLM.OpenRouter.APIKey)
	cfg.Dashboard.AuthToken = expandEnv(cfg.Dashboard.AuthToken)
	cfg.Skills.AgentRunner.Copilot.GitHubToken = expandEnv(cfg.Skills.AgentRunner.Copilot.GitHubToken)
	cfg.Tracker.Jira.APIToken = expandEnv(cfg.Tracker.Jira.APIToken)
	cfg.Tracker.GitHub.Token = expandEnv(cfg.Tracker.GitHub.Token)
	cfg.Tracker.Linear.APIKey = expandEnv(cfg.Tracker.Linear.APIKey)
	cfg.Git.GitHub.Token = expandEnv(cfg.Git.GitHub.Token)
	cfg.Git.GitLab.Token = expandEnv(cfg.Git.GitLab.Token)
	cfg.Daemon.WorkDir = expandTilde(cfg.Daemon.WorkDir)
	for dest, src := range cfg.Daemon.EnvFiles {
		cfg.Daemon.EnvFiles[dest] = expandTilde(src)
	}
	cfg.Database.SQLite.Path = expandTilde(cfg.Database.SQLite.Path)
}

func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}

func Validate(cfg *models.Config) []error {
	var errs []error

	// Validate LLM provider has API key
	switch cfg.LLM.DefaultProvider {
	case "anthropic":
		if cfg.LLM.Anthropic.APIKey == "" {
			errs = append(errs, fmt.Errorf("llm.anthropic.api_key is required when default_provider is anthropic"))
		}
	case "openai":
		if cfg.LLM.OpenAI.APIKey == "" {
			errs = append(errs, fmt.Errorf("llm.openai.api_key is required when default_provider is openai"))
		}
	case "openrouter":
		if cfg.LLM.OpenRouter.APIKey == "" {
			errs = append(errs, fmt.Errorf("llm.openrouter.api_key is required when default_provider is openrouter"))
		}
	}

	// Validate dashboard port
	if cfg.Dashboard.Enabled && (cfg.Dashboard.Port < 1 || cfg.Dashboard.Port > 65535) {
		errs = append(errs, fmt.Errorf("dashboard.port must be 1-65535 (got %d)", cfg.Dashboard.Port))
	}

	// Validate dashboard auth token
	if cfg.Dashboard.Enabled && cfg.Dashboard.AuthToken == "" {
		errs = append(errs, fmt.Errorf("dashboard.auth_token is required when dashboard is enabled"))
	}

	// Validate branch_prefix ends with a separator so branch names are well-formed.
	// e.g. "foreman" would produce "foremanSU-123" instead of "foreman/SU-123".
	if cfg.Git.BranchPrefix != "" && !strings.HasSuffix(cfg.Git.BranchPrefix, "/") && !strings.HasSuffix(cfg.Git.BranchPrefix, "-") {
		errs = append(errs, fmt.Errorf("git.branch_prefix %q must end with a separator (/ or -), e.g. \"foreman/\"", cfg.Git.BranchPrefix))
	}

	// Validate cost budgets are positive
	if cfg.Cost.MaxCostPerTicketUSD <= 0 {
		errs = append(errs, fmt.Errorf("cost.max_cost_per_ticket_usd must be positive"))
	}
	if cfg.Cost.MaxCostPerDayUSD <= 0 {
		errs = append(errs, fmt.Errorf("cost.max_cost_per_day_usd must be positive"))
	}
	if cfg.Cost.MaxCostPerMonthUSD <= 0 {
		errs = append(errs, fmt.Errorf("cost.max_cost_per_month_usd must be positive"))
	}

	return errs
}
