package agent

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// BuiltinConfig holds configuration for the builtin runner.
type BuiltinConfig struct {
	MaxTurnsDefault     int
	DefaultAllowedTools []string // e.g. ["Read", "Glob", "Grep"]
}

// BuiltinRunner runs a multi-turn tool-use loop against the LlmProvider.
// Unlike Claude Code or Copilot, this uses Foreman's own provider interface,
// making it work across all LLM providers (Anthropic, OpenAI, OpenRouter, local).
type BuiltinRunner struct {
	provider llm.LlmProvider
	model    string
	config   BuiltinConfig
}

// NewBuiltinRunner creates a builtin runner with multi-turn tool-use capability.
func NewBuiltinRunner(provider llm.LlmProvider, model string, config BuiltinConfig) *BuiltinRunner {
	return &BuiltinRunner{provider: provider, model: model, config: config}
}

func (r *BuiltinRunner) RunnerName() string { return "builtin" }

func (r *BuiltinRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	systemPrompt := "You are a focused task executor. Complete the task and return only the result."
	if req.SystemPrompt != "" {
		systemPrompt = systemPrompt + "\n\n" + req.SystemPrompt
	}

	// Determine which tools to offer
	toolNames := req.AllowedTools
	if len(toolNames) == 0 {
		toolNames = r.config.DefaultAllowedTools
	}
	toolDefs := BuiltinToolDefs(toolNames)

	maxTurns := req.MaxTurns
	if maxTurns == 0 {
		maxTurns = r.config.MaxTurnsDefault
	}
	if maxTurns == 0 {
		maxTurns = 10
	}

	messages := []models.Message{
		{Role: "user", Content: req.Prompt},
	}

	var usage AgentUsage

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := r.provider.Complete(ctx, models.LlmRequest{
			Model:        r.model,
			SystemPrompt: systemPrompt,
			MaxTokens:    4096,
			Temperature:  0.2,
			Messages:     messages,
			Tools:        toolDefs,
		})
		if err != nil {
			return AgentResult{}, fmt.Errorf("builtin: turn %d: %w", turn+1, err)
		}

		usage.InputTokens += resp.TokensInput
		usage.OutputTokens += resp.TokensOutput
		usage.DurationMs += int(resp.DurationMs)
		usage.NumTurns++

		// Done — model returned a final text response
		if resp.StopReason == models.StopReasonEndTurn || resp.StopReason == models.StopReasonMaxTokens {
			return AgentResult{Output: resp.Content, Usage: usage}, nil
		}

		// Tool use — execute each tool call, append results
		if resp.StopReason == models.StopReasonToolUse && len(resp.ToolCalls) > 0 {
			messages = append(messages, models.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			var toolResults []models.ToolResult
			for _, tc := range resp.ToolCalls {
				executor, ok := builtinTools[tc.Name]
				if !ok {
					toolResults = append(toolResults, models.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("unknown tool: %s", tc.Name),
						IsError:    true,
					})
					continue
				}
				output, err := executor(req.WorkDir, tc.Input)
				if err != nil {
					toolResults = append(toolResults, models.ToolResult{
						ToolCallID: tc.ID,
						Content:    err.Error(),
						IsError:    true,
					})
					continue
				}
				toolResults = append(toolResults, models.ToolResult{
					ToolCallID: tc.ID,
					Content:    output,
				})
			}

			messages = append(messages, models.Message{
				Role:        "user",
				ToolResults: toolResults,
			})
			continue
		}

		// Unexpected stop reason — return what we have
		return AgentResult{Output: resp.Content, Usage: usage}, nil
	}

	return AgentResult{}, fmt.Errorf("builtin: exceeded max turns %d without completion", maxTurns)
}

func (r *BuiltinRunner) HealthCheck(ctx context.Context) error {
	return r.provider.HealthCheck(ctx)
}

func (r *BuiltinRunner) Close() error { return nil }
