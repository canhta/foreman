package agent

import (
	"fmt"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// NewAgentRunner creates the configured agent runner.
// For "copilot", this starts the Copilot CLI process.
// database and llmCfg are optional (may be nil/zero); when provided and
// llmCfg.EmbeddingProvider is set, SemanticSearch is registered on the
// builtin runner's tool registry.
func NewAgentRunner(
	cfg models.AgentRunnerConfig,
	cmdRunner runner.CommandRunner,
	llmProvider llm.LlmProvider,
	agentModel string,
	database db.Database,
	llmCfg models.LLMConfig,
) (AgentRunner, error) {
	switch cfg.Provider {
	case "builtin", "":
		reg := tools.NewRegistry(nil, cmdRunner, tools.ToolHooks{})
		embedder := llm.NewEmbedder(llmCfg)
		reg.WithSemanticSearch(embedder, database)
		builtinRunner := NewBuiltinRunner(llmProvider, agentModel, BuiltinConfig{
			MaxTurnsDefault:     cfg.MaxTurnsDefault,
			DefaultAllowedTools: cfg.Builtin.DefaultAllowedTools,
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
