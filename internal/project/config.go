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
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
}

// ProjectLimits holds per-project execution limits.
// Fields previously under [daemon] that are project-specific.
type ProjectLimits struct {
	MaxParallelTickets            int     `mapstructure:"max_parallel_tickets"`
	MaxParallelTasks              int     `mapstructure:"max_parallel_tasks"`
	TaskTimeoutMinutes            int     `mapstructure:"task_timeout_minutes"`
	MaxTasksPerTicket             int     `mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries      int     `mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles           int     `mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles        int     `mapstructure:"max_quality_review_cycles"`
	MaxLlmCallsPerTask            int     `mapstructure:"max_llm_calls_per_task"`
	MaxTaskDurationSecs           int     `mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs          int     `mapstructure:"max_total_duration_secs"`
	ContextTokenBudget            int     `mapstructure:"context_token_budget"`
	EnablePartialPR               bool    `mapstructure:"enable_partial_pr"`
	EnableClarification           bool    `mapstructure:"enable_clarification"`
	EnableTDDVerification         bool    `mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity       float64 `mapstructure:"search_replace_similarity"`
	SearchReplaceMinContextLines  int     `mapstructure:"search_replace_min_context_lines"`
	PlanConfidenceThreshold       float64 `mapstructure:"plan_confidence_threshold"`
	IntermediateReviewInterval    int     `mapstructure:"intermediate_review_interval"`
	ConflictResolutionTokenBudget int     `mapstructure:"conflict_resolution_token_budget"`
}

// ProjectConfig holds all per-project configuration.
type ProjectConfig struct {
	Project     ProjectMeta              `mapstructure:"project"`
	Tracker     models.TrackerConfig     `mapstructure:"tracker"`
	Git         models.GitConfig         `mapstructure:"git"`
	Models      models.ModelsConfig      `mapstructure:"models"`
	Cost        models.CostConfig        `mapstructure:"cost"`
	Limits      ProjectLimits            `mapstructure:"limits"`
	AgentRunner models.AgentRunnerConfig `mapstructure:"agent_runner"`
	Skills      models.SkillsConfig      `mapstructure:"skills"`
	Decompose   models.DecomposeConfig   `mapstructure:"decompose"`
	Context     models.ContextConfig     `mapstructure:"context"`
	Runner      models.RunnerConfig      `mapstructure:"runner"`
	EnvFiles    map[string]string        `mapstructure:"env_files"`
	LLM         models.LLMConfig         `mapstructure:"llm"` // optional per-project API key overrides
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
