package project

import (
	"github.com/canhta/foreman/internal/models"
)

// MergeConfigs produces a models.Config by applying project-level overrides
// on top of global defaults. The projectDir is used to set work directory
// and database path.
func MergeConfigs(global *models.Config, proj *ProjectConfig, projectDir string) *models.Config {
	merged := *global // shallow copy

	// Tracker — always from project
	merged.Tracker = proj.Tracker

	// Git — always from project
	merged.Git = proj.Git

	// Models — from project
	merged.Models = proj.Models

	// Agent runner — from project
	merged.AgentRunner = proj.AgentRunner

	// Skills agent runner — from project if set
	if proj.Skills.AgentRunner.Provider != "" {
		merged.Skills = proj.Skills
	}

	// Decompose — from project
	merged.Decompose = proj.Decompose

	// Context — from project
	merged.Context = proj.Context

	// Runner — from project if set, else global
	if proj.Runner.Mode != "" {
		merged.Runner = proj.Runner
	}

	// Cost — merge: per-ticket from project, daily/monthly stay global
	if proj.Cost.MaxCostPerTicketUSD > 0 {
		merged.Cost.MaxCostPerTicketUSD = proj.Cost.MaxCostPerTicketUSD
	}

	// Limits — from project, mapped to daemon config fields and limits config
	merged.Daemon.MaxParallelTickets = proj.Limits.MaxParallelTickets
	merged.Daemon.MaxParallelTasks = proj.Limits.MaxParallelTasks
	merged.Daemon.TaskTimeoutMinutes = proj.Limits.TaskTimeoutMinutes
	merged.Limits.MaxTasksPerTicket = proj.Limits.MaxTasksPerTicket
	merged.Limits.MaxImplementationRetries = proj.Limits.MaxImplementationRetries
	merged.Limits.MaxSpecReviewCycles = proj.Limits.MaxSpecReviewCycles
	merged.Limits.MaxQualityReviewCycles = proj.Limits.MaxQualityReviewCycles
	merged.Limits.MaxTaskDurationSecs = proj.Limits.MaxTaskDurationSecs
	merged.Limits.MaxTotalDurationSecs = proj.Limits.MaxTotalDurationSecs
	merged.Limits.ContextTokenBudget = proj.Limits.ContextTokenBudget
	merged.Limits.EnablePartialPR = proj.Limits.EnablePartialPR
	merged.Limits.EnableClarification = proj.Limits.EnableClarification
	merged.Limits.EnableTDDVerification = proj.Limits.EnableTDDVerification
	merged.Limits.SearchReplaceSimilarity = proj.Limits.SearchReplaceSimilarity
	merged.Limits.SearchReplaceMinContext = proj.Limits.SearchReplaceMinContextLines
	merged.Limits.PlanConfidenceThreshold = proj.Limits.PlanConfidenceThreshold
	merged.Limits.IntermediateReviewInterval = proj.Limits.IntermediateReviewInterval
	merged.Limits.ConflictResolutionTokenBudget = proj.Limits.ConflictResolutionTokenBudget
	// MaxLlmCallsPerTask lives in CostConfig, not LimitsConfig
	if proj.Limits.MaxLlmCallsPerTask > 0 {
		merged.Cost.MaxLlmCallsPerTask = proj.Limits.MaxLlmCallsPerTask
	}

	// LLM API keys — project overrides if set, else global
	if proj.LLM.Anthropic.APIKey != "" {
		merged.LLM.Anthropic.APIKey = proj.LLM.Anthropic.APIKey
	}
	if proj.LLM.OpenAI.APIKey != "" {
		merged.LLM.OpenAI.APIKey = proj.LLM.OpenAI.APIKey
	}
	if proj.LLM.OpenRouter.APIKey != "" {
		merged.LLM.OpenRouter.APIKey = proj.LLM.OpenRouter.APIKey
	}

	// Work directory — always project-specific
	merged.Daemon.WorkDir = ProjectWorkDir(projectDir)

	// Database — always project-specific
	merged.Database.SQLite.Path = ProjectDBPath(projectDir)

	// Env files — from project
	if len(proj.EnvFiles) > 0 {
		merged.Daemon.EnvFiles = proj.EnvFiles
	}

	return &merged
}
