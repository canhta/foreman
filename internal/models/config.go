package models

type Config struct {
	Daemon    DaemonConfig    `mapstructure:"daemon"`
	Dashboard DashboardConfig `mapstructure:"dashboard"`
	Tracker   TrackerConfig   `mapstructure:"tracker"`
	Git       GitConfig       `mapstructure:"git"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Models    ModelsConfig    `mapstructure:"models"`
	Cost      CostConfig      `mapstructure:"cost"`
	Limits    LimitsConfig    `mapstructure:"limits"`
	Secrets   SecretsConfig   `mapstructure:"secrets"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
	Runner    RunnerConfig    `mapstructure:"runner"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Pipeline  PipelineConfig  `mapstructure:"pipeline"`
}

type DaemonConfig struct {
	PollIntervalSecs     int    `mapstructure:"poll_interval_secs"`
	IdlePollIntervalSecs int    `mapstructure:"idle_poll_interval_secs"`
	MaxParallelTickets   int    `mapstructure:"max_parallel_tickets"`
	WorkDir              string `mapstructure:"work_dir"`
	LogLevel             string `mapstructure:"log_level"`
	LogFormat            string `mapstructure:"log_format"`
}

type DashboardConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Port      int    `mapstructure:"port"`
	Host      string `mapstructure:"host"`
	AuthToken string `mapstructure:"auth_token"`
}

type TrackerConfig struct {
	Provider                  string `mapstructure:"provider"`
	PickupLabel               string `mapstructure:"pickup_label"`
	ClarificationLabel        string `mapstructure:"clarification_label"`
	ClarificationTimeoutHours int    `mapstructure:"clarification_timeout_hours"`
}

type GitConfig struct {
	Provider       string   `mapstructure:"provider"`
	Backend        string   `mapstructure:"backend"`
	CloneURL       string   `mapstructure:"clone_url"`
	DefaultBranch  string   `mapstructure:"default_branch"`
	AutoPush       bool     `mapstructure:"auto_push"`
	PRDraft        bool     `mapstructure:"pr_draft"`
	PRReviewers    []string `mapstructure:"pr_reviewers"`
	BranchPrefix   string   `mapstructure:"branch_prefix"`
	RebaseBeforePR bool     `mapstructure:"rebase_before_pr"`
}

type LLMConfig struct {
	DefaultProvider string          `mapstructure:"default_provider"`
	Anthropic       LLMProviderConfig `mapstructure:"anthropic"`
	OpenAI          LLMProviderConfig `mapstructure:"openai"`
	OpenRouter      LLMProviderConfig `mapstructure:"openrouter"`
	Local           LLMProviderConfig `mapstructure:"local"`
	Outage          LLMOutageConfig   `mapstructure:"outage"`
}

type LLMProviderConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

type LLMOutageConfig struct {
	MaxConnectionRetries     int    `mapstructure:"max_connection_retries"`
	ConnectionRetryDelaySecs int    `mapstructure:"connection_retry_delay_secs"`
	FallbackProvider         string `mapstructure:"fallback_provider"`
}

type ModelsConfig struct {
	Planner         string `mapstructure:"planner"`
	Implementer     string `mapstructure:"implementer"`
	SpecReviewer    string `mapstructure:"spec_reviewer"`
	QualityReviewer string `mapstructure:"quality_reviewer"`
	FinalReviewer   string `mapstructure:"final_reviewer"`
	Clarifier       string `mapstructure:"clarifier"`
}

type CostConfig struct {
	MaxCostPerTicketUSD float64                  `mapstructure:"max_cost_per_ticket_usd"`
	MaxCostPerDayUSD    float64                  `mapstructure:"max_cost_per_day_usd"`
	MaxCostPerMonthUSD  float64                  `mapstructure:"max_cost_per_month_usd"`
	AlertThresholdPct   int                      `mapstructure:"alert_threshold_percent"`
	MaxLlmCallsPerTask  int                      `mapstructure:"max_llm_calls_per_task"`
	Pricing             map[string]PricingConfig `mapstructure:"pricing"`
}

type PricingConfig struct {
	Input  float64 `mapstructure:"input"`
	Output float64 `mapstructure:"output"`
}

type LimitsConfig struct {
	MaxTasksPerTicket        int     `mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries int     `mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles      int     `mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles   int     `mapstructure:"max_quality_review_cycles"`
	MaxTaskDurationSecs      int     `mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs     int     `mapstructure:"max_total_duration_secs"`
	ContextTokenBudget       int     `mapstructure:"context_token_budget"`
	EnablePartialPR          bool    `mapstructure:"enable_partial_pr"`
	EnableClarification      bool    `mapstructure:"enable_clarification"`
	EnableTDDVerification    bool    `mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity  float64 `mapstructure:"search_replace_similarity"`
	SearchReplaceMinContext  int     `mapstructure:"search_replace_min_context_lines"`
}

type SecretsConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	ExtraPatterns []string `mapstructure:"extra_patterns"`
	AlwaysExclude []string `mapstructure:"always_exclude"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute"`
	BurstSize         int `mapstructure:"burst_size"`
	BackoffBaseMs     int `mapstructure:"backoff_base_ms"`
	BackoffMaxMs      int `mapstructure:"backoff_max_ms"`
	JitterPercent     int `mapstructure:"jitter_percent"`
}

type RunnerConfig struct {
	Mode   string            `mapstructure:"mode"`
	Docker DockerRunnerConfig `mapstructure:"docker"`
	Local  LocalRunnerConfig  `mapstructure:"local"`
}

type DockerRunnerConfig struct {
	Image             string `mapstructure:"image"`
	PersistPerTicket  bool   `mapstructure:"persist_per_ticket"`
	Network           string `mapstructure:"network"`
	CPULimit          string `mapstructure:"cpu_limit"`
	MemoryLimit       string `mapstructure:"memory_limit"`
	AutoReinstallDeps bool   `mapstructure:"auto_reinstall_deps"`
}

type LocalRunnerConfig struct {
	AllowedCommands []string `mapstructure:"allowed_commands"`
	ForbiddenPaths  []string `mapstructure:"forbidden_paths"`
}

type DatabaseConfig struct {
	Driver   string         `mapstructure:"driver"`
	SQLite   SQLiteConfig   `mapstructure:"sqlite"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

type SQLiteConfig struct {
	Path               string `mapstructure:"path"`
	BusyTimeoutMs      int    `mapstructure:"busy_timeout_ms"`
	WALMode            bool   `mapstructure:"wal_mode"`
	EventFlushInterval int    `mapstructure:"event_flush_interval_ms"`
}

type PostgresConfig struct {
	URL            string `mapstructure:"url"`
	MaxConnections int    `mapstructure:"max_connections"`
}

type PipelineConfig struct {
	Hooks HooksConfig `mapstructure:"hooks"`
}

type HooksConfig struct {
	PostLint []string `mapstructure:"post_lint"`
	PrePR    []string `mapstructure:"pre_pr"`
	PostPR   []string `mapstructure:"post_pr"`
}
