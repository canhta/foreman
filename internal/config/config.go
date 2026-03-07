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

	var cfg models.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func LoadFromFile(path string) (*models.Config, error) {
	v := viper.New()
	setDefaults(v)

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
	v.SetDefault("daemon.max_parallel_tickets", 3)
	v.SetDefault("daemon.max_parallel_tasks", 3)
	v.SetDefault("daemon.task_timeout_minutes", 15)
	v.SetDefault("daemon.merge_check_interval_secs", 300)
	v.SetDefault("daemon.lock_ttl_seconds", 3600)
	v.SetDefault("daemon.work_dir", "~/.foreman/work")
	v.SetDefault("daemon.log_level", "info")
	v.SetDefault("daemon.log_format", "json")

	v.SetDefault("dashboard.enabled", true)
	v.SetDefault("dashboard.port", 3333)
	v.SetDefault("dashboard.host", "127.0.0.1")

	v.SetDefault("tracker.provider", "local_file")
	v.SetDefault("tracker.pickup_label", "foreman-ready")
	v.SetDefault("tracker.clarification_label", "foreman-needs-info")
	v.SetDefault("tracker.clarification_timeout_hours", 72)

	v.SetDefault("git.provider", "github")
	v.SetDefault("git.backend", "native")
	v.SetDefault("git.default_branch", "main")
	v.SetDefault("git.auto_push", true)
	v.SetDefault("git.pr_draft", true)
	v.SetDefault("git.branch_prefix", "foreman")
	v.SetDefault("git.rebase_before_pr", true)

	v.SetDefault("llm.default_provider", "anthropic")
	v.SetDefault("llm.outage.max_connection_retries", 3)
	v.SetDefault("llm.outage.connection_retry_delay_secs", 30)

	v.SetDefault("models.planner", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.implementer", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.spec_reviewer", "anthropic:claude-haiku-4-5-20251001")
	v.SetDefault("models.quality_reviewer", "anthropic:claude-haiku-4-5-20251001")
	v.SetDefault("models.final_reviewer", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.clarifier", "anthropic:claude-haiku-4-5-20251001")

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

	// Skills agent runner defaults
	v.SetDefault("skills.agent_runner.provider", "builtin")
	v.SetDefault("skills.agent_runner.max_cost_per_ticket_usd", 2.0)
	v.SetDefault("skills.agent_runner.max_turns_default", 10)
	v.SetDefault("skills.agent_runner.timeout_secs_default", 120)
	v.SetDefault("skills.agent_runner.builtin.default_allowed_tools", []string{"Read", "Glob", "Grep"})
	v.SetDefault("skills.agent_runner.claudecode.bin", "claude")
	v.SetDefault("skills.agent_runner.claudecode.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
	v.SetDefault("skills.agent_runner.claudecode.max_turns_default", 10)
	v.SetDefault("skills.agent_runner.claudecode.timeout_secs_default", 180)
	v.SetDefault("skills.agent_runner.copilot.cli_path", "copilot")
	v.SetDefault("skills.agent_runner.copilot.model", "gpt-4o")
	v.SetDefault("skills.agent_runner.copilot.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
	v.SetDefault("skills.agent_runner.copilot.timeout_secs_default", 180)

	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.sqlite.path", "~/.foreman/foreman.db")
	v.SetDefault("database.sqlite.busy_timeout_ms", 5000)
	v.SetDefault("database.sqlite.wal_mode", true)
	v.SetDefault("database.sqlite.event_flush_interval_ms", 100)

	// Context assembly defaults (REQ-CTX-003)
	v.SetDefault("context.context_feedback_boost", 1.5)
}

func expandEnvVars(cfg *models.Config) {
	cfg.LLM.Anthropic.APIKey = expandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.OpenAI.APIKey = expandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.OpenRouter.APIKey = expandEnv(cfg.LLM.OpenRouter.APIKey)
	cfg.Dashboard.AuthToken = expandEnv(cfg.Dashboard.AuthToken)
	cfg.Database.Postgres.URL = expandEnv(cfg.Database.Postgres.URL)
	cfg.Skills.AgentRunner.Copilot.GitHubToken = expandEnv(cfg.Skills.AgentRunner.Copilot.GitHubToken)
	cfg.Daemon.WorkDir = expandTilde(cfg.Daemon.WorkDir)
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

	if cfg.Daemon.MaxParallelTasks < 1 {
		errs = append(errs, fmt.Errorf("max_parallel_tasks must be at least 1 (got %d)", cfg.Daemon.MaxParallelTasks))
	}
	if cfg.Daemon.TaskTimeoutMinutes < 1 {
		errs = append(errs, fmt.Errorf("task_timeout_minutes must be at least 1 (got %d)", cfg.Daemon.TaskTimeoutMinutes))
	}

	if cfg.Database.Driver == "sqlite" && cfg.Daemon.MaxParallelTickets > 3 {
		errs = append(errs, fmt.Errorf("max_parallel_tickets cannot exceed 3 with SQLite (got %d), use PostgreSQL for higher concurrency", cfg.Daemon.MaxParallelTickets))
	}

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
