package models

// Config is the top-level Foreman configuration structure.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type Config struct {
	Models      ModelsConfig      `toml:"models" mapstructure:"models"`
	Decompose   DecomposeConfig   `toml:"decompose" mapstructure:"decompose"`
	Runner      RunnerConfig      `toml:"runner" mapstructure:"runner"`
	Pipeline    PipelineConfig    `toml:"pipeline" mapstructure:"pipeline"`
	Tracker     TrackerConfig     `toml:"tracker" mapstructure:"tracker"`
	MCP         MCPConfig         `toml:"mcp" mapstructure:"mcp"`
	Channel     ChannelConfig     `toml:"channel" mapstructure:"channel"`
	Agents      []AgentRoleConfig `toml:"agents" mapstructure:"agents"`
	LLM         LLMConfig         `toml:"llm" mapstructure:"llm"`
	Git         GitConfig         `toml:"git" mapstructure:"git"`
	Secrets     SecretsConfig     `toml:"secrets" mapstructure:"secrets"`
	Dashboard   DashboardConfig   `toml:"dashboard" mapstructure:"dashboard"`
	Database    DatabaseConfig    `toml:"database" mapstructure:"database"`
	Cost        CostConfig        `toml:"cost" mapstructure:"cost"`
	Daemon      DaemonConfig      `toml:"daemon" mapstructure:"daemon"`
	Skills      SkillsConfig      `toml:"skills" mapstructure:"skills"`
	AgentRunner AgentRunnerConfig `toml:"agent_runner" mapstructure:"agent_runner"`
	Limits      LimitsConfig      `toml:"limits" mapstructure:"limits"`
	RateLimit   RateLimitConfig   `toml:"rate_limit" mapstructure:"rate_limit"`
	Context     ContextConfig     `toml:"context" mapstructure:"context"`
	Permissions PermissionConfig  `toml:"permissions" mapstructure:"permissions"`
	PromptsDir  string            `toml:"prompts_dir" mapstructure:"prompts_dir"`
}

// PermissionRule defines a single permission rule for tool access control.
type PermissionRule struct {
	Permission string `toml:"permission" json:"permission" mapstructure:"permission"`
	Pattern    string `toml:"pattern" json:"pattern" mapstructure:"pattern"`
	Action     string `toml:"action" json:"action" mapstructure:"action"`
}

// PermissionConfig holds default and pattern-based permission rules.
type PermissionConfig struct {
	Rules []PermissionRule `toml:"rules" json:"rules" mapstructure:"rules"`
}

// AgentRoleConfig defines a custom named agent with specific tools, model, and prompt.
type AgentRoleConfig struct {
	Name         string   `toml:"name" mapstructure:"name"`
	Model        string   `toml:"model" mapstructure:"model"`
	SystemPrompt string   `toml:"system_prompt" mapstructure:"system_prompt"`
	Trigger      string   `toml:"trigger" mapstructure:"trigger"`
	AllowedTools []string `toml:"allowed_tools" mapstructure:"allowed_tools"`
	MaxTurns     int      `toml:"max_turns" mapstructure:"max_turns"`
	TimeoutSecs  int      `toml:"timeout_secs" mapstructure:"timeout_secs"`
}

// MCPConfig holds configuration for MCP (Model Context Protocol) servers.
type MCPConfig struct {
	Servers          []MCPServerEntry `toml:"servers" mapstructure:"servers"`
	ResourceMaxBytes int              `toml:"resource_max_bytes" mapstructure:"resource_max_bytes"` // default 512 KB
}

// MCPServerEntry defines a single MCP server to connect to.
type MCPServerEntry struct {
	Env                     map[string]string `toml:"env" mapstructure:"env"`
	Name                    string            `toml:"name" mapstructure:"name"`
	Command                 string            `toml:"command" mapstructure:"command"`
	RestartPolicy           string            `toml:"restart_policy" mapstructure:"restart_policy"`
	Args                    []string          `toml:"args" mapstructure:"args"`
	AllowedTools            []string          `toml:"allowed_tools" mapstructure:"allowed_tools"`
	MaxRestarts             int               `toml:"max_restarts" mapstructure:"max_restarts"`
	RestartDelaySecs        int               `toml:"restart_delay_secs" mapstructure:"restart_delay_secs"`
	HealthCheckIntervalSecs int               `toml:"health_check_interval_secs" mapstructure:"health_check_interval_secs"`
}

type DaemonConfig struct {
	// EnvFiles maps worktree-relative destination paths to absolute source paths
	// on disk (outside the repo). Each file is copied into every task worktree
	// and all vars are loaded into the process environment.
	// Use ~/ prefix for home-relative paths (not $HOME — shell vars are not expanded).
	// Example: {".env": "~/.foreman/envs/myproject.env"}
	EnvFiles               map[string]string `toml:"env_files" mapstructure:"env_files"`
	WorkDir                string            `toml:"work_dir" mapstructure:"work_dir"`
	LogLevel               string            `toml:"log_level" mapstructure:"log_level"`
	LogFormat              string            `toml:"log_format" mapstructure:"log_format"`
	PollIntervalSecs       int               `toml:"poll_interval_secs" mapstructure:"poll_interval_secs"`
	IdlePollIntervalSecs   int               `toml:"idle_poll_interval_secs" mapstructure:"idle_poll_interval_secs"`
	MaxParallelTickets     int               `toml:"max_parallel_tickets" mapstructure:"max_parallel_tickets"`
	MaxParallelTasks       int               `toml:"max_parallel_tasks" mapstructure:"max_parallel_tasks"`
	TaskTimeoutMinutes     int               `toml:"task_timeout_minutes" mapstructure:"task_timeout_minutes"`
	MergeCheckIntervalSecs int               `toml:"merge_check_interval_secs" mapstructure:"merge_check_interval_secs"`
	LockTTLSeconds         int               `toml:"lock_ttl_seconds" mapstructure:"lock_ttl_seconds"`
}

type DashboardConfig struct {
	Host      string `toml:"host" mapstructure:"host"`
	AuthToken string `toml:"auth_token" mapstructure:"auth_token"`
	Port      int    `toml:"port" mapstructure:"port"`
	Enabled   bool   `toml:"enabled" mapstructure:"enabled"`
}

//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type TrackerConfig struct {
	Provider                  string                 `toml:"provider" mapstructure:"provider"`
	PickupLabel               string                 `toml:"pickup_label" mapstructure:"pickup_label"`
	ClarificationLabel        string                 `toml:"clarification_label" mapstructure:"clarification_label"`
	ClarificationTimeoutHours int                    `toml:"clarification_timeout_hours" mapstructure:"clarification_timeout_hours"`
	Jira                      JiraTrackerConfig      `toml:"jira" mapstructure:"jira"`
	GitHub                    GitHubTrackerConfig    `toml:"github" mapstructure:"github"`
	Linear                    LinearTrackerConfig    `toml:"linear" mapstructure:"linear"`
	LocalFile                 LocalFileTrackerConfig `toml:"local_file" mapstructure:"local_file"`
}

type JiraTrackerConfig struct {
	BaseURL          string `toml:"base_url" mapstructure:"base_url"`
	Email            string `toml:"email" mapstructure:"email"`
	APIToken         string `toml:"api_token" mapstructure:"api_token"`
	ProjectKey       string `toml:"project_key" mapstructure:"project_key"`
	StatusInProgress string `toml:"status_in_progress" mapstructure:"status_in_progress"`
	StatusInReview   string `toml:"status_in_review" mapstructure:"status_in_review"`
	StatusDone       string `toml:"status_done" mapstructure:"status_done"`
	StatusBlocked    string `toml:"status_blocked" mapstructure:"status_blocked"`
}

type GitHubTrackerConfig struct {
	Owner   string `toml:"owner" mapstructure:"owner"`
	Repo    string `toml:"repo" mapstructure:"repo"`
	Token   string `toml:"token" mapstructure:"token"`
	BaseURL string `toml:"base_url" mapstructure:"base_url"` // empty = https://api.github.com
}

type LinearTrackerConfig struct {
	APIKey  string `toml:"api_key" mapstructure:"api_key"`
	TeamID  string `toml:"team_id" mapstructure:"team_id"`
	BaseURL string `toml:"base_url" mapstructure:"base_url"` // empty = https://api.linear.app
}

type LocalFileTrackerConfig struct {
	Path string `toml:"path" mapstructure:"path"`
}

//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type GitConfig struct {
	Provider       string         `toml:"provider" mapstructure:"provider"`
	Backend        string         `toml:"backend" mapstructure:"backend"`
	CloneURL       string         `toml:"clone_url" mapstructure:"clone_url"`
	DefaultBranch  string         `toml:"default_branch" mapstructure:"default_branch"`
	BranchPrefix   string         `toml:"branch_prefix" mapstructure:"branch_prefix"`
	PRReviewers    []string       `toml:"pr_reviewers" mapstructure:"pr_reviewers"`
	AutoPush       bool           `toml:"auto_push" mapstructure:"auto_push"`
	PRDraft        bool           `toml:"pr_draft" mapstructure:"pr_draft"`
	RebaseBeforePR bool           `toml:"rebase_before_pr" mapstructure:"rebase_before_pr"`
	AutoMerge      bool           `toml:"auto_merge" mapstructure:"auto_merge"`
	GitHub         GitHubConfig   `toml:"github" mapstructure:"github"`
	GitLab         GitLabConfig   `toml:"gitlab" mapstructure:"gitlab"`
	Worktree       WorktreeConfig `toml:"worktree" mapstructure:"worktree"`
}

// WorktreeConfig holds settings applied when a new task worktree is created.
type WorktreeConfig struct {
	// StartCommand is an optional shell command to run inside the worktree
	// directory after it is created (e.g. "npm install", "go mod download").
	// Failures are logged as warnings and do not abort worktree creation.
	StartCommand string `toml:"start_command" mapstructure:"start_command"`
}

type GitHubConfig struct {
	Token   string `toml:"token" mapstructure:"token"`
	BaseURL string `toml:"base_url" mapstructure:"base_url"` // empty = https://api.github.com
}

type GitLabConfig struct {
	Token   string `toml:"token" mapstructure:"token"`
	BaseURL string `toml:"base_url" mapstructure:"base_url"` // default: https://gitlab.com
}

type LLMConfig struct {
	DefaultProvider   string            `toml:"default_provider" mapstructure:"default_provider"`
	EmbeddingProvider string            `toml:"embedding_provider" mapstructure:"embedding_provider"` // "openai" or "anthropic"
	EmbeddingModel    string            `toml:"embedding_model" mapstructure:"embedding_model"`       // e.g. "text-embedding-3-small" or "voyage-code-3"
	Anthropic         LLMProviderConfig `toml:"anthropic" mapstructure:"anthropic"`
	OpenAI            LLMProviderConfig `toml:"openai" mapstructure:"openai"`
	OpenRouter        LLMProviderConfig `toml:"openrouter" mapstructure:"openrouter"`
	Local             LLMProviderConfig `toml:"local" mapstructure:"local"`
	Outage            LLMOutageConfig   `toml:"outage" mapstructure:"outage"`
}

type LLMProviderConfig struct {
	APIKey  string `toml:"api_key" mapstructure:"api_key"`
	BaseURL string `toml:"base_url" mapstructure:"base_url"`
}

type LLMOutageConfig struct {
	FallbackProvider         string `toml:"fallback_provider" mapstructure:"fallback_provider"`
	MaxConnectionRetries     int    `toml:"max_connection_retries" mapstructure:"max_connection_retries"`
	ConnectionRetryDelaySecs int    `toml:"connection_retry_delay_secs" mapstructure:"connection_retry_delay_secs"`
}

type ModelsConfig struct {
	Planner         string `toml:"planner" mapstructure:"planner"`
	Implementer     string `toml:"implementer" mapstructure:"implementer"`
	SpecReviewer    string `toml:"spec_reviewer" mapstructure:"spec_reviewer"`
	QualityReviewer string `toml:"quality_reviewer" mapstructure:"quality_reviewer"`
	FinalReviewer   string `toml:"final_reviewer" mapstructure:"final_reviewer"`
	Clarifier       string `toml:"clarifier" mapstructure:"clarifier"`
}

type CostConfig struct {
	// FallbackPricing is used for unknown models when no entry exists in Pricing.
	// Defaults to $3.00/M input and $15.00/M output if not set.
	FallbackPricing     *PricingConfig           `toml:"fallback_pricing" mapstructure:"fallback_pricing"`
	Pricing             map[string]PricingConfig `toml:"pricing" mapstructure:"pricing"`
	MaxCostPerTicketUSD float64                  `toml:"max_cost_per_ticket_usd" mapstructure:"max_cost_per_ticket_usd"`
	MaxCostPerDayUSD    float64                  `toml:"max_cost_per_day_usd" mapstructure:"max_cost_per_day_usd"`
	MaxCostPerMonthUSD  float64                  `toml:"max_cost_per_month_usd" mapstructure:"max_cost_per_month_usd"`
	AlertThresholdPct   int                      `toml:"alert_threshold_percent" mapstructure:"alert_threshold_percent"`
	MaxLlmCallsPerTask  int                      `toml:"max_llm_calls_per_task" mapstructure:"max_llm_calls_per_task"`
}

type PricingConfig struct {
	Input  float64 `toml:"input" mapstructure:"input"`
	Output float64 `toml:"output" mapstructure:"output"`
}

type LimitsConfig struct {
	MaxTasksPerTicket        int     `toml:"max_tasks_per_ticket" mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries int     `toml:"max_implementation_retries" mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles      int     `toml:"max_spec_review_cycles" mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles   int     `toml:"max_quality_review_cycles" mapstructure:"max_quality_review_cycles"`
	MaxTaskDurationSecs      int     `toml:"max_task_duration_secs" mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs     int     `toml:"max_total_duration_secs" mapstructure:"max_total_duration_secs"`
	ContextTokenBudget       int     `toml:"context_token_budget" mapstructure:"context_token_budget"`
	EnablePartialPR          bool    `toml:"enable_partial_pr" mapstructure:"enable_partial_pr"`
	EnableClarification      bool    `toml:"enable_clarification" mapstructure:"enable_clarification"`
	EnableTDDVerification    bool    `toml:"enable_tdd_verification" mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity  float64 `toml:"search_replace_similarity" mapstructure:"search_replace_similarity"`
	SearchReplaceMinContext  int     `toml:"search_replace_min_context_lines" mapstructure:"search_replace_min_context_lines"`
	// IntermediateReviewInterval controls cross-task consistency checks (REQ-PIPE-006).
	// After every N completed tasks a lightweight LLM check fires. 0 disables it.
	IntermediateReviewInterval int `toml:"intermediate_review_interval" mapstructure:"intermediate_review_interval"`
	// PlanConfidenceThreshold is the minimum confidence score (0.0–1.0) required for a
	// plan to proceed without triggering a clarification request (REQ-PIPE-002).
	// A value of 0 disables confidence scoring entirely.
	PlanConfidenceThreshold float64 `toml:"plan_confidence_threshold" mapstructure:"plan_confidence_threshold"`
	// ConflictResolutionTokenBudget is the token budget allocated for LLM-assisted
	// merge conflict resolution. A value of 0 disables LLM assistance.
	ConflictResolutionTokenBudget int `toml:"conflict_resolution_token_budget" mapstructure:"conflict_resolution_token_budget"`
}

// ContextConfig holds configuration for the context assembly subsystem.
type ContextConfig struct {
	// ContextFeedbackBoost is the multiplier applied to file scores when a file
	// appears in files_touched of a prior similar task but was NOT in files_selected.
	// Default: 1.5
	ContextFeedbackBoost float64 `toml:"context_feedback_boost" mapstructure:"context_feedback_boost"`
	// ContextGenerateMaxTokens is the token budget for LLM prompt when generating AGENTS.md.
	// Default: 32000
	ContextGenerateMaxTokens int `toml:"context_generate_max_tokens" mapstructure:"context_generate_max_tokens"`
}

type SecretsConfig struct {
	ExtraPatterns []string `toml:"extra_patterns" mapstructure:"extra_patterns"`
	AlwaysExclude []string `toml:"always_exclude" mapstructure:"always_exclude"`
	Enabled       bool     `toml:"enabled" mapstructure:"enabled"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `toml:"requests_per_minute" mapstructure:"requests_per_minute"`
	BurstSize         int `toml:"burst_size" mapstructure:"burst_size"`
	BackoffBaseMs     int `toml:"backoff_base_ms" mapstructure:"backoff_base_ms"`
	BackoffMaxMs      int `toml:"backoff_max_ms" mapstructure:"backoff_max_ms"`
	JitterPercent     int `toml:"jitter_percent" mapstructure:"jitter_percent"`
}

type DecomposeConfig struct {
	ApprovalLabel    string `toml:"approval_label" mapstructure:"approval_label"`
	ParentLabel      string `toml:"parent_label" mapstructure:"parent_label"`
	LLMAssistModel   string `toml:"llm_assist_model" mapstructure:"llm_assist_model"`
	MaxTicketWords   int    `toml:"max_ticket_words" mapstructure:"max_ticket_words"`
	MaxScopeKeywords int    `toml:"max_scope_keywords" mapstructure:"max_scope_keywords"`
	Enabled          bool   `toml:"enabled" mapstructure:"enabled"`
	LLMAssist        bool   `toml:"llm_assist" mapstructure:"llm_assist"`
}

type RunnerConfig struct {
	Mode   string             `toml:"mode" mapstructure:"mode"`
	Docker DockerRunnerConfig `toml:"docker" mapstructure:"docker"`
	Local  LocalRunnerConfig  `toml:"local" mapstructure:"local"`
}

type DockerRunnerConfig struct {
	Image             string `toml:"image" mapstructure:"image"`
	Network           string `toml:"network" mapstructure:"network"`
	CPULimit          string `toml:"cpu_limit" mapstructure:"cpu_limit"`
	MemoryLimit       string `toml:"memory_limit" mapstructure:"memory_limit"`
	PersistPerTicket  bool   `toml:"persist_per_ticket" mapstructure:"persist_per_ticket"`
	AutoReinstallDeps bool   `toml:"auto_reinstall_deps" mapstructure:"auto_reinstall_deps"`
	AllowNetwork      bool   `toml:"allow_network" mapstructure:"allow_network"`
}

type LocalRunnerConfig struct {
	AllowedCommands []string `toml:"allowed_commands" mapstructure:"allowed_commands"`
	ForbiddenPaths  []string `toml:"forbidden_paths" mapstructure:"forbidden_paths"`
}

type DatabaseConfig struct {
	Driver string       `toml:"driver" mapstructure:"driver"`
	SQLite SQLiteConfig `toml:"sqlite" mapstructure:"sqlite"`
}

type SQLiteConfig struct {
	Path               string `toml:"path" mapstructure:"path"`
	BusyTimeoutMs      int    `toml:"busy_timeout_ms" mapstructure:"busy_timeout_ms"`
	WALMode            bool   `toml:"wal_mode" mapstructure:"wal_mode"`
	EventFlushInterval int    `toml:"event_flush_interval_ms" mapstructure:"event_flush_interval_ms"`
}

type PipelineConfig struct {
	Hooks HooksConfig `toml:"hooks" mapstructure:"hooks"`
}

type HooksConfig struct {
	PostLint  []string `toml:"post_lint" mapstructure:"post_lint"`
	PrePR     []string `toml:"pre_pr" mapstructure:"pre_pr"`
	PostPR    []string `toml:"post_pr" mapstructure:"post_pr"`
	PostMerge []string `toml:"post_merge" mapstructure:"post_merge"`
}

type SkillsConfig struct {
	AgentRunner AgentRunnerConfig `toml:"agent_runner" mapstructure:"agent_runner"`
}

type AgentRunnerConfig struct {
	Provider            string                 `toml:"provider" mapstructure:"provider"`
	Builtin             BuiltinRunnerConfig    `toml:"builtin" mapstructure:"builtin"`
	Copilot             CopilotRunnerConfig    `toml:"copilot" mapstructure:"copilot"`
	ClaudeCode          ClaudeCodeRunnerConfig `toml:"claudecode" mapstructure:"claudecode"`
	MaxCostPerTicketUSD float64                `toml:"max_cost_per_ticket_usd" mapstructure:"max_cost_per_ticket_usd"`
	MaxTurnsDefault     int                    `toml:"max_turns_default" mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int                    `toml:"timeout_secs_default" mapstructure:"timeout_secs_default"`
	TokenBudget         int                    `toml:"token_budget" mapstructure:"token_budget"`
}

type BuiltinRunnerConfig struct {
	// Model overrides the LLM model used by the builtin runner for all agent sessions.
	// When empty, the runner falls back to the model passed at construction time
	// (i.e., the pipeline's implementer model).
	Model               string   `toml:"model" mapstructure:"model"`
	DefaultAllowedTools []string `toml:"default_allowed_tools" mapstructure:"default_allowed_tools"`
	MaxTurns            int      `toml:"max_turns" mapstructure:"max_turns"`
	MaxContextTokens    int      `toml:"max_context_tokens" mapstructure:"max_context_tokens"`
	ReflectionInterval  int      `toml:"reflection_interval" mapstructure:"reflection_interval"`
}

type ClaudeCodeRunnerConfig struct {
	Bin                 string   `toml:"bin" mapstructure:"bin"`
	Model               string   `toml:"model" mapstructure:"model"`
	DefaultAllowedTools []string `toml:"default_allowed_tools" mapstructure:"default_allowed_tools"`
	MaxTurnsDefault     int      `toml:"max_turns_default" mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int      `toml:"timeout_secs_default" mapstructure:"timeout_secs_default"`
	MaxBudgetUSD        float64  `toml:"max_budget_usd" mapstructure:"max_budget_usd"`
}

type CopilotRunnerConfig struct {
	CLIPath             string   `toml:"cli_path" mapstructure:"cli_path"`
	GitHubToken         string   `toml:"github_token" mapstructure:"github_token"`
	Model               string   `toml:"model" mapstructure:"model"`
	DefaultAllowedTools []string `toml:"default_allowed_tools" mapstructure:"default_allowed_tools"`
	TimeoutSecsDefault  int      `toml:"timeout_secs_default" mapstructure:"timeout_secs_default"`
}

type ChannelConfig struct {
	Provider string                `toml:"provider" mapstructure:"provider"` // "" (disabled) | "whatsapp"
	WhatsApp WhatsAppChannelConfig `toml:"whatsapp" mapstructure:"whatsapp"`
}

type WhatsAppChannelConfig struct {
	SessionDB      string   `toml:"session_db" mapstructure:"session_db"`
	PairingMode    string   `toml:"pairing_mode" mapstructure:"pairing_mode"`
	DMPolicy       string   `toml:"dm_policy" mapstructure:"dm_policy"`
	AllowedNumbers []string `toml:"allowed_numbers" mapstructure:"allowed_numbers"`
}
