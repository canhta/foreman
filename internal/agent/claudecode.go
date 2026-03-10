package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/canhta/foreman/internal/prompts"
	"github.com/canhta/foreman/internal/runner"
)

// ClaudeCodeConfig holds configuration for the Claude Code CLI runner.
type ClaudeCodeConfig struct {
	Bin                 string
	Model               string
	DefaultAllowedTools []string
	MaxTurnsDefault     int
	TimeoutSecsDefault  int
	MaxBudgetUSD        float64
}

// ClaudeCodeRunner invokes the Claude Agent SDK via CLI subprocess.
// Uses `claude -p --output-format json` and parses the SDKResultMessage.
type ClaudeCodeRunner struct {
	runner   runner.CommandRunner
	registry *prompts.Registry
	bin      string
	config   ClaudeCodeConfig
}

// NewClaudeCodeRunner creates a runner that shells out to the claude CLI.
func NewClaudeCodeRunner(cmdRunner runner.CommandRunner, cfg ClaudeCodeConfig) *ClaudeCodeRunner {
	bin := cfg.Bin
	if bin == "" {
		bin = "claude"
	}
	return &ClaudeCodeRunner{bin: bin, runner: cmdRunner, config: cfg}
}

func (r *ClaudeCodeRunner) RunnerName() string { return "claudecode" }

// WithRegistry attaches a prompt registry so the runner calls registry.ForClaude()
// before execution to write .claude/ directory structure into the working directory.
func (r *ClaudeCodeRunner) WithRegistry(reg *prompts.Registry) *ClaudeCodeRunner {
	r.registry = reg
	return r
}

func (r *ClaudeCodeRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	if r.registry != nil {
		vars := map[string]any{
			"test_command": "go test ./...", // could come from req context
		}
		if err := r.registry.ForClaude(req.WorkDir, vars); err != nil {
			return AgentResult{}, fmt.Errorf("claudecode: write .claude/: %w", err)
		}
	}

	args := []string{
		"-p", req.Prompt,
		"--output-format", "json",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
	}

	if mt := resolveInt(req.MaxTurns, r.config.MaxTurnsDefault); mt > 0 {
		args = append(args, "--max-turns", strconv.Itoa(mt))
	}
	if r.config.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", r.config.MaxBudgetUSD))
	}
	if req.OutputSchema != nil {
		args = append(args, "--json-schema", string(req.OutputSchema))
	}
	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = r.config.DefaultAllowedTools
	}
	for _, tool := range tools {
		args = append(args, "--allowedTools", tool)
	}
	if r.config.Model != "" {
		args = append(args, "--model", r.config.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}

	timeout := resolveInt(req.TimeoutSecs, r.config.TimeoutSecsDefault)
	if timeout == 0 {
		timeout = 120
	}

	out, err := r.runner.Run(ctx, req.WorkDir, r.bin, args, timeout)
	if err != nil {
		return AgentResult{}, fmt.Errorf("claudecode: command error: %w", err)
	}
	if out.TimedOut {
		return AgentResult{}, fmt.Errorf("claudecode: timed out after %ds", timeout)
	}
	if out.ExitCode != 0 {
		return AgentResult{}, fmt.Errorf("claudecode: exit %d: %s", out.ExitCode, truncate(out.Stderr, 500))
	}

	result, err := parseSDKResultMessage(out.Stdout)
	if err != nil {
		return AgentResult{}, err
	}
	// Fall back to configured model when SDK doesn't report one.
	if result.Usage.Model == "" && r.config.Model != "" {
		result.Usage.Model = r.config.Model
	}
	return result, nil
}

// sdkResultMessage mirrors the Claude Agent SDK's SDKResultMessage JSON output.
type sdkResultMessage struct {
	StructuredOutput json.RawMessage `json:"structured_output"`
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	Result           string          `json:"result"`
	Model            string          `json:"model"`
	Errors           []string        `json:"errors"`
	Usage            struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	DurationMs   int     `json:"duration_ms"`
	IsError      bool    `json:"is_error"`
}

func parseSDKResultMessage(stdout string) (AgentResult, error) {
	var msg sdkResultMessage

	// Try full output as single JSON object first
	if err := json.Unmarshal([]byte(stdout), &msg); err != nil {
		// Fall back to last non-empty line (stream-json may have multiple lines)
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if len(lines) == 0 {
			return AgentResult{}, fmt.Errorf("claudecode: empty output")
		}
		lastLine := lines[len(lines)-1]
		if err := json.Unmarshal([]byte(lastLine), &msg); err != nil {
			return AgentResult{}, fmt.Errorf("claudecode: parse error: %w", err)
		}
	}

	if msg.Type != "result" {
		return AgentResult{}, fmt.Errorf("claudecode: unexpected message type %q", msg.Type)
	}

	if msg.IsError {
		errMsg := msg.Subtype
		if len(msg.Errors) > 0 {
			errMsg = strings.Join(msg.Errors, "; ")
		}
		return AgentResult{}, fmt.Errorf("claudecode: agent error (%s): %s", msg.Subtype, errMsg)
	}

	return enrichResult(AgentResult{
		Output:     msg.Result,
		Structured: msg.StructuredOutput,
		Usage: AgentUsage{
			InputTokens:  msg.Usage.InputTokens,
			OutputTokens: msg.Usage.OutputTokens,
			CostUSD:      msg.TotalCostUSD,
			NumTurns:     msg.NumTurns,
			DurationMs:   msg.DurationMs,
			Model:        msg.Model,
		},
	}), nil
}

func (r *ClaudeCodeRunner) HealthCheck(ctx context.Context) error {
	out, err := r.runner.Run(ctx, ".", r.bin, []string{"--version"}, 10)
	if err != nil {
		return fmt.Errorf("claude binary error: %w", err)
	}
	if out.ExitCode != 0 {
		return fmt.Errorf("claude binary not found or not working at %q", r.bin)
	}
	return nil
}

func (r *ClaudeCodeRunner) Close() error { return nil }

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func resolveInt(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}
