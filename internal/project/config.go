package project

import (
	"fmt"
	"os"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/viper"
)

// ProjectMeta holds project identity fields.
type ProjectMeta struct {
	Name        string `toml:"name" mapstructure:"name"`
	Description string `toml:"description" mapstructure:"description"`
}

// ProjectLimits holds per-project execution limits.
// Fields previously under [daemon] that are project-specific.
type ProjectLimits struct {
	MaxParallelTickets            int     `toml:"max_parallel_tickets" mapstructure:"max_parallel_tickets"`
	MaxParallelTasks              int     `toml:"max_parallel_tasks" mapstructure:"max_parallel_tasks"`
	TaskTimeoutMinutes            int     `toml:"task_timeout_minutes" mapstructure:"task_timeout_minutes"`
	MaxTasksPerTicket             int     `toml:"max_tasks_per_ticket" mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries      int     `toml:"max_implementation_retries" mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles           int     `toml:"max_spec_review_cycles" mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles        int     `toml:"max_quality_review_cycles" mapstructure:"max_quality_review_cycles"`
	MaxLlmCallsPerTask            int     `toml:"max_llm_calls_per_task" mapstructure:"max_llm_calls_per_task"`
	MaxTaskDurationSecs           int     `toml:"max_task_duration_secs" mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs          int     `toml:"max_total_duration_secs" mapstructure:"max_total_duration_secs"`
	ContextTokenBudget            int     `toml:"context_token_budget" mapstructure:"context_token_budget"`
	EnablePartialPR               bool    `toml:"enable_partial_pr" mapstructure:"enable_partial_pr"`
	EnableClarification           bool    `toml:"enable_clarification" mapstructure:"enable_clarification"`
	EnableTDDVerification         bool    `toml:"enable_tdd_verification" mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity       float64 `toml:"search_replace_similarity" mapstructure:"search_replace_similarity"`
	SearchReplaceMinContextLines  int     `toml:"search_replace_min_context_lines" mapstructure:"search_replace_min_context_lines"`
	PlanConfidenceThreshold       float64 `toml:"plan_confidence_threshold" mapstructure:"plan_confidence_threshold"`
	IntermediateReviewInterval    int     `toml:"intermediate_review_interval" mapstructure:"intermediate_review_interval"`
	ConflictResolutionTokenBudget int     `toml:"conflict_resolution_token_budget" mapstructure:"conflict_resolution_token_budget"`
}

// ProjectConfig holds all per-project configuration.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type ProjectConfig struct {
	Project     ProjectMeta              `toml:"project" mapstructure:"project"`
	Tracker     models.TrackerConfig     `toml:"tracker" mapstructure:"tracker"`
	Git         models.GitConfig         `toml:"git" mapstructure:"git"`
	Models      models.ModelsConfig      `toml:"models" mapstructure:"models"`
	Cost        models.CostConfig        `toml:"cost" mapstructure:"cost"`
	Limits      ProjectLimits            `toml:"limits" mapstructure:"limits"`
	AgentRunner models.AgentRunnerConfig `toml:"agent_runner" mapstructure:"agent_runner"`
	Skills      models.SkillsConfig      `toml:"skills" mapstructure:"skills"`
	Decompose   models.DecomposeConfig   `toml:"decompose" mapstructure:"decompose"`
	Context     models.ContextConfig     `toml:"context" mapstructure:"context"`
	Runner      models.RunnerConfig      `toml:"runner" mapstructure:"runner"`
	EnvFiles    map[string]string        `toml:"env_files" mapstructure:"env_files"`
	LLM         models.LLMConfig         `toml:"llm" mapstructure:"llm"` // optional per-project API key overrides
}

// LoadProjectConfig loads a project config from a TOML file.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")

	// Set defaults matching the current global defaults
	v.SetDefault("limits.max_parallel_tickets", 3)
	v.SetDefault("limits.max_parallel_tasks", 3)
	v.SetDefault("limits.task_timeout_minutes", 15)
	v.SetDefault("limits.max_tasks_per_ticket", 20)
	v.SetDefault("limits.max_implementation_retries", 2)
	v.SetDefault("limits.max_spec_review_cycles", 2)
	v.SetDefault("limits.max_quality_review_cycles", 1)
	v.SetDefault("limits.max_llm_calls_per_task", 8)
	v.SetDefault("limits.max_task_duration_secs", 600)
	v.SetDefault("limits.max_total_duration_secs", 7200)
	v.SetDefault("limits.context_token_budget", 80000)
	v.SetDefault("limits.enable_partial_pr", true)
	v.SetDefault("limits.enable_clarification", true)
	v.SetDefault("limits.enable_tdd_verification", true)
	v.SetDefault("limits.search_replace_similarity", 0.92)
	v.SetDefault("limits.search_replace_min_context_lines", 3)
	v.SetDefault("limits.plan_confidence_threshold", 0.60)
	v.SetDefault("limits.intermediate_review_interval", 3)
	v.SetDefault("limits.conflict_resolution_token_budget", 40000)
	v.SetDefault("agent_runner.provider", "builtin")
	v.SetDefault("runner.mode", "local")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read project config %s: %w", path, err)
	}

	var cfg ProjectConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal project config: %w", err)
	}

	expandProjectEnvVars(&cfg)
	return &cfg, nil
}

func expandProjectEnvVars(cfg *ProjectConfig) {
	cfg.Tracker.Jira.APIToken = expandEnv(cfg.Tracker.Jira.APIToken)
	cfg.Tracker.GitHub.Token = expandEnv(cfg.Tracker.GitHub.Token)
	cfg.Tracker.Linear.APIKey = expandEnv(cfg.Tracker.Linear.APIKey)
	cfg.Git.GitHub.Token = expandEnv(cfg.Git.GitHub.Token)
	cfg.Git.GitLab.Token = expandEnv(cfg.Git.GitLab.Token)
	cfg.LLM.Anthropic.APIKey = expandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.OpenAI.APIKey = expandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.OpenRouter.APIKey = expandEnv(cfg.LLM.OpenRouter.APIKey)
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}
