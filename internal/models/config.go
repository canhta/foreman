package models

// Config is the top-level Foreman configuration structure.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type Config struct {
	Models      ModelsConfig      `mapstructure:"models"`
	Decompose   DecomposeConfig   `mapstructure:"decompose"`
	Runner      RunnerConfig      `mapstructure:"runner"`
	Pipeline    PipelineConfig    `mapstructure:"pipeline"`
	Tracker     TrackerConfig     `mapstructure:"tracker"`
	MCP         MCPConfig         `mapstructure:"mcp"`
	Channel     ChannelConfig     `mapstructure:"channel"`
	Agents      []AgentRoleConfig `mapstructure:"agents"`
	LLM         LLMConfig         `mapstructure:"llm"`
	Git         GitConfig         `mapstructure:"git"`
	Secrets     SecretsConfig     `mapstructure:"secrets"`
	Dashboard   DashboardConfig   `mapstructure:"dashboard"`
	Database    DatabaseConfig    `mapstructure:"database"`
	Cost        CostConfig        `mapstructure:"cost"`
	Daemon      DaemonConfig      `mapstructure:"daemon"`
	Skills      SkillsConfig      `mapstructure:"skills"`
	AgentRunner AgentRunnerConfig `mapstructure:"agent_runner"`
	Limits      LimitsConfig      `mapstructure:"limits"`
	RateLimit   RateLimitConfig   `mapstructure:"rate_limit"`
	Context     ContextConfig     `mapstructure:"context"`
	PromptsDir  string            `mapstructure:"prompts_dir"`
}

// AgentRoleConfig defines a custom named agent with specific tools, model, and prompt.
type AgentRoleConfig struct {
	Name         string   `mapstructure:"name"`
	Model        string   `mapstructure:"model"`
	SystemPrompt string   `mapstructure:"system_prompt"`
	Trigger      string   `mapstructure:"trigger"`
	AllowedTools []string `mapstructure:"allowed_tools"`
	MaxTurns     int      `mapstructure:"max_turns"`
	TimeoutSecs  int      `mapstructure:"timeout_secs"`
}

// MCPConfig holds configuration for MCP (Model Context Protocol) servers.
type MCPConfig struct {
	Servers          []MCPServerEntry `mapstructure:"servers"`
	ResourceMaxBytes int              `mapstructure:"resource_max_bytes"` // default 512 KB
}

// MCPServerEntry defines a single MCP server to connect to.
type MCPServerEntry struct {
	Env                     map[string]string `mapstructure:"env"`
	Name                    string            `mapstructure:"name"`
	Command                 string            `mapstructure:"command"`
	RestartPolicy           string            `mapstructure:"restart_policy"`
	Args                    []string          `mapstructure:"args"`
	AllowedTools            []string          `mapstructure:"allowed_tools"`
	MaxRestarts             int               `mapstructure:"max_restarts"`
	RestartDelaySecs        int               `mapstructure:"restart_delay_secs"`
	HealthCheckIntervalSecs int               `mapstructure:"health_check_interval_secs"`
}

type DaemonConfig struct {
	// EnvFiles maps worktree-relative destination paths to absolute source paths
	// on disk (outside the repo). Each file is copied into every task worktree
	// and all vars are loaded into the process environment.
	// Use ~/ prefix for home-relative paths (not $HOME — shell vars are not expanded).
	// Example: {".env": "~/.foreman/envs/myproject.env"}
	EnvFiles               map[string]string `mapstructure:"env_files"`
	WorkDir                string            `mapstructure:"work_dir"`
	LogLevel               string            `mapstructure:"log_level"`
	LogFormat              string            `mapstructure:"log_format"`
	PollIntervalSecs       int               `mapstructure:"poll_interval_secs"`
	IdlePollIntervalSecs   int               `mapstructure:"idle_poll_interval_secs"`
	MaxParallelTickets     int               `mapstructure:"max_parallel_tickets"`
	MaxParallelTasks       int               `mapstructure:"max_parallel_tasks"`
	TaskTimeoutMinutes     int               `mapstructure:"task_timeout_minutes"`
	MergeCheckIntervalSecs int               `mapstructure:"merge_check_interval_secs"`
	LockTTLSeconds         int               `mapstructure:"lock_ttl_seconds"`
}

type DashboardConfig struct {
	Host      string `mapstructure:"host"`
	AuthToken string `mapstructure:"auth_token"`
	Port      int    `mapstructure:"port"`
	Enabled   bool   `mapstructure:"enabled"`
}

//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type TrackerConfig struct {
	Provider                  string                 `mapstructure:"provider"`
	PickupLabel               string                 `mapstructure:"pickup_label"`
	ClarificationLabel        string                 `mapstructure:"clarification_label"`
	ClarificationTimeoutHours int                    `mapstructure:"clarification_timeout_hours"`
	Jira                      JiraTrackerConfig      `mapstructure:"jira"`
	GitHub                    GitHubTrackerConfig    `mapstructure:"github"`
	Linear                    LinearTrackerConfig    `mapstructure:"linear"`
	LocalFile                 LocalFileTrackerConfig `mapstructure:"local_file"`
}

type JiraTrackerConfig struct {
	BaseURL          string `mapstructure:"base_url"`
	Email            string `mapstructure:"email"`
	APIToken         string `mapstructure:"api_token"`
	ProjectKey       string `mapstructure:"project_key"`
	StatusInProgress string `mapstructure:"status_in_progress"`
	StatusInReview   string `mapstructure:"status_in_review"`
	StatusDone       string `mapstructure:"status_done"`
	StatusBlocked    string `mapstructure:"status_blocked"`
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

//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type GitConfig struct {
	Provider       string       `mapstructure:"provider"`
	Backend        string       `mapstructure:"backend"`
	CloneURL       string       `mapstructure:"clone_url"`
	DefaultBranch  string       `mapstructure:"default_branch"`
	BranchPrefix   string       `mapstructure:"branch_prefix"`
	PRReviewers    []string     `mapstructure:"pr_reviewers"`
	AutoPush       bool         `mapstructure:"auto_push"`
	PRDraft        bool         `mapstructure:"pr_draft"`
	RebaseBeforePR bool         `mapstructure:"rebase_before_pr"`
	AutoMerge      bool         `mapstructure:"auto_merge"`
	GitHub         GitHubConfig `mapstructure:"github"`
	GitLab         GitLabConfig `mapstructure:"gitlab"`
}

type GitHubConfig struct {
	Token   string `mapstructure:"token"`
	BaseURL string `mapstructure:"base_url"` // empty = https://api.github.com
}

type GitLabConfig struct {
	Token   string `mapstructure:"token"`
	BaseURL string `mapstructure:"base_url"` // default: https://gitlab.com
}

type LLMConfig struct {
	DefaultProvider   string            `mapstructure:"default_provider"`
	EmbeddingProvider string            `mapstructure:"embedding_provider"` // "openai" or "anthropic"
	EmbeddingModel    string            `mapstructure:"embedding_model"`    // e.g. "text-embedding-3-small" or "voyage-code-3"
	Anthropic         LLMProviderConfig `mapstructure:"anthropic"`
	OpenAI            LLMProviderConfig `mapstructure:"openai"`
	OpenRouter        LLMProviderConfig `mapstructure:"openrouter"`
	Local             LLMProviderConfig `mapstructure:"local"`
	Outage            LLMOutageConfig   `mapstructure:"outage"`
}

type LLMProviderConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

type LLMOutageConfig struct {
	FallbackProvider         string `mapstructure:"fallback_provider"`
	MaxConnectionRetries     int    `mapstructure:"max_connection_retries"`
	ConnectionRetryDelaySecs int    `mapstructure:"connection_retry_delay_secs"`
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
	// FallbackPricing is used for unknown models when no entry exists in Pricing.
	// Defaults to $3.00/M input and $15.00/M output if not set.
	FallbackPricing     *PricingConfig           `mapstructure:"fallback_pricing"`
	Pricing             map[string]PricingConfig `mapstructure:"pricing"`
	MaxCostPerTicketUSD float64                  `mapstructure:"max_cost_per_ticket_usd"`
	MaxCostPerDayUSD    float64                  `mapstructure:"max_cost_per_day_usd"`
	MaxCostPerMonthUSD  float64                  `mapstructure:"max_cost_per_month_usd"`
	AlertThresholdPct   int                      `mapstructure:"alert_threshold_percent"`
	MaxLlmCallsPerTask  int                      `mapstructure:"max_llm_calls_per_task"`
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
	// IntermediateReviewInterval controls cross-task consistency checks (REQ-PIPE-006).
	// After every N completed tasks a lightweight LLM check fires. 0 disables it.
	IntermediateReviewInterval int `mapstructure:"intermediate_review_interval"`
	// PlanConfidenceThreshold is the minimum confidence score (0.0–1.0) required for a
	// plan to proceed without triggering a clarification request (REQ-PIPE-002).
	// A value of 0 disables confidence scoring entirely.
	PlanConfidenceThreshold float64 `mapstructure:"plan_confidence_threshold"`
	// ConflictResolutionTokenBudget is the token budget allocated for LLM-assisted
	// merge conflict resolution. A value of 0 disables LLM assistance.
	ConflictResolutionTokenBudget int `mapstructure:"conflict_resolution_token_budget"`
}

// ContextConfig holds configuration for the context assembly subsystem.
type ContextConfig struct {
	// ContextFeedbackBoost is the multiplier applied to file scores when a file
	// appears in files_touched of a prior similar task but was NOT in files_selected.
	// Default: 1.5
	ContextFeedbackBoost float64 `mapstructure:"context_feedback_boost"`
	// ContextGenerateMaxTokens is the token budget for LLM prompt when generating AGENTS.md.
	// Default: 32000
	ContextGenerateMaxTokens int `mapstructure:"context_generate_max_tokens"`
}

type SecretsConfig struct {
	ExtraPatterns []string `mapstructure:"extra_patterns"`
	AlwaysExclude []string `mapstructure:"always_exclude"`
	Enabled       bool     `mapstructure:"enabled"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute"`
	BurstSize         int `mapstructure:"burst_size"`
	BackoffBaseMs     int `mapstructure:"backoff_base_ms"`
	BackoffMaxMs      int `mapstructure:"backoff_max_ms"`
	JitterPercent     int `mapstructure:"jitter_percent"`
}

type DecomposeConfig struct {
	ApprovalLabel    string `mapstructure:"approval_label"`
	ParentLabel      string `mapstructure:"parent_label"`
	LLMAssistModel   string `mapstructure:"llm_assist_model"`
	MaxTicketWords   int    `mapstructure:"max_ticket_words"`
	MaxScopeKeywords int    `mapstructure:"max_scope_keywords"`
	Enabled          bool   `mapstructure:"enabled"`
	LLMAssist        bool   `mapstructure:"llm_assist"`
}

type RunnerConfig struct {
	Mode   string             `mapstructure:"mode"`
	Docker DockerRunnerConfig `mapstructure:"docker"`
	Local  LocalRunnerConfig  `mapstructure:"local"`
}

type DockerRunnerConfig struct {
	Image             string `mapstructure:"image"`
	Network           string `mapstructure:"network"`
	CPULimit          string `mapstructure:"cpu_limit"`
	MemoryLimit       string `mapstructure:"memory_limit"`
	PersistPerTicket  bool   `mapstructure:"persist_per_ticket"`
	AutoReinstallDeps bool   `mapstructure:"auto_reinstall_deps"`
	AllowNetwork      bool   `mapstructure:"allow_network"`
}

type LocalRunnerConfig struct {
	AllowedCommands []string `mapstructure:"allowed_commands"`
	ForbiddenPaths  []string `mapstructure:"forbidden_paths"`
}

type DatabaseConfig struct {
	Driver   string         `mapstructure:"driver"`
	Postgres PostgresConfig `mapstructure:"postgres"`
	SQLite   SQLiteConfig   `mapstructure:"sqlite"`
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
	PostLint  []string `mapstructure:"post_lint"`
	PrePR     []string `mapstructure:"pre_pr"`
	PostPR    []string `mapstructure:"post_pr"`
	PostMerge []string `mapstructure:"post_merge"`
}

type SkillsConfig struct {
	AgentRunner AgentRunnerConfig `mapstructure:"agent_runner"`
}

type AgentRunnerConfig struct {
	Provider            string                 `mapstructure:"provider"`
	Builtin             BuiltinRunnerConfig    `mapstructure:"builtin"`
	Copilot             CopilotRunnerConfig    `mapstructure:"copilot"`
	ClaudeCode          ClaudeCodeRunnerConfig `mapstructure:"claudecode"`
	MaxCostPerTicketUSD float64                `mapstructure:"max_cost_per_ticket_usd"`
	MaxTurnsDefault     int                    `mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int                    `mapstructure:"timeout_secs_default"`
	TokenBudget         int                    `mapstructure:"token_budget"`
}

type BuiltinRunnerConfig struct {
	// Model overrides the LLM model used by the builtin runner for all agent sessions.
	// When empty, the runner falls back to the model passed at construction time
	// (i.e., the pipeline's implementer model).
	Model               string   `mapstructure:"model"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	MaxTurns            int      `mapstructure:"max_turns"`
	MaxContextTokens    int      `mapstructure:"max_context_tokens"`
	ReflectionInterval  int      `mapstructure:"reflection_interval"`
}

type ClaudeCodeRunnerConfig struct {
	Bin                 string   `mapstructure:"bin"`
	Model               string   `mapstructure:"model"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	MaxTurnsDefault     int      `mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int      `mapstructure:"timeout_secs_default"`
	MaxBudgetUSD        float64  `mapstructure:"max_budget_usd"`
}

type CopilotRunnerConfig struct {
	CLIPath             string   `mapstructure:"cli_path"`
	GitHubToken         string   `mapstructure:"github_token"`
	Model               string   `mapstructure:"model"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	TimeoutSecsDefault  int      `mapstructure:"timeout_secs_default"`
}

type ChannelConfig struct {
	Provider string                `mapstructure:"provider"` // "" (disabled) | "whatsapp"
	WhatsApp WhatsAppChannelConfig `mapstructure:"whatsapp"`
}

type WhatsAppChannelConfig struct {
	SessionDB      string   `mapstructure:"session_db"`
	PairingMode    string   `mapstructure:"pairing_mode"`
	DMPolicy       string   `mapstructure:"dm_policy"`
	AllowedNumbers []string `mapstructure:"allowed_numbers"`
}
