package agent

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/telemetry"
)

// NewAgentRunner creates the configured agent runner.
// For "copilot", this starts the Copilot CLI process.
// database and llmCfg are optional (may be nil/zero); when provided and
// llmCfg.EmbeddingProvider is set, SemanticSearch is registered on the
// builtin runner's tool registry.
// mcpMgr is optional; when non-nil it is wired into the builtin runner's
// tool registry so that ListMCPTools returns the populated cache.
// metrics is optional; when non-nil, built-in tool calls are instrumented.
func NewAgentRunner(
	cfg models.AgentRunnerConfig,
	cmdRunner runner.CommandRunner,
	llmProvider llm.LlmProvider,
	agentModel string,
	database db.Database,
	llmCfg models.LLMConfig,
	mcpMgr *mcp.Manager,
	metrics *telemetry.Metrics,
) (AgentRunner, error) {
	switch cfg.Provider {
	case "builtin", "":
		hooks := buildToolHooks(metrics)
		reg := tools.NewRegistryWithMCP(nil, cmdRunner, hooks, mcpMgr)
		embedder := llm.NewEmbedder(llmCfg)
		reg.WithSemanticSearch(embedder, database)
		builtinRunner := NewBuiltinRunner(llmProvider, agentModel, BuiltinConfig{
			MaxTurnsDefault:     cfg.MaxTurnsDefault,
			DefaultAllowedTools: cfg.Builtin.DefaultAllowedTools,
			Model:               cfg.Builtin.Model,
		}, reg, nil)
		reg.SetRunFn(builtinRunner.subagentRunFn)
		return builtinRunner, nil

	case "claudecode":
		c := cfg.ClaudeCode
		return NewClaudeCodeRunner(cmdRunner, ClaudeCodeConfig{
			Bin:                 c.Bin,
			DefaultAllowedTools: c.DefaultAllowedTools,
			MaxTurnsDefault:     c.MaxTurnsDefault,
			TimeoutSecsDefault:  c.TimeoutSecsDefault,
			MaxBudgetUSD:        c.MaxBudgetUSD,
			Model:               c.Model,
		}), nil

	case "copilot":
		c := cfg.Copilot
		return NewCopilotRunner(CopilotConfig{
			CLIPath:             c.CLIPath,
			GitHubToken:         c.GitHubToken,
			Model:               c.Model,
			DefaultAllowedTools: c.DefaultAllowedTools,
			TimeoutSecsDefault:  c.TimeoutSecsDefault,
		})

	default:
		return nil, fmt.Errorf(
			"unknown agent runner provider %q — valid: builtin, claudecode, copilot",
			cfg.Provider,
		)
	}
}

// buildToolHooks constructs ToolHooks that record built-in tool call metrics.
// When metrics is nil, empty hooks are returned.
func buildToolHooks(metrics *telemetry.Metrics) tools.ToolHooks {
	if metrics == nil {
		return tools.ToolHooks{}
	}
	return tools.ToolHooks{
		PostToolUse: func(_ context.Context, name, _ string, err error) {
			status := "success"
			if err != nil {
				status = "error"
			}
			metrics.BuiltinToolCallsTotal.WithLabelValues(name, status).Inc()
		},
	}
}
