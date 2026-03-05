package agent

import (
	"fmt"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// NewAgentRunner creates the configured agent runner.
// For "copilot", this starts the Copilot CLI process.
func NewAgentRunner(
	cfg models.AgentRunnerConfig,
	cmdRunner runner.CommandRunner,
	llmProvider llm.LlmProvider,
	agentModel string,
) (AgentRunner, error) {
	switch cfg.Provider {
	case "builtin", "":
		reg := tools.NewRegistry(nil, cmdRunner, tools.ToolHooks{})
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
