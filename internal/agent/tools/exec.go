package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

// hardBlockedCommands are never allowed regardless of AllowedCommands config.
var hardBlockedCommands = []string{"rm", "curl", "wget", "ssh", "scp", "git push", "git reset", "dd", "mkfs", "shutdown", "reboot"}

func registerExec(r *Registry, cmd runner.CommandRunner) {
	r.Register(&bashTool{cmd: cmd, registry: r})
	r.Register(&runTestTool{cmd: cmd, registry: r})
	r.Register(&subagentTool{registry: r})
}

// --- Bash ---

type bashTool struct {
	cmd      runner.CommandRunner
	registry *Registry
}

func (t *bashTool) Name() string        { return "Bash" }
func (t *bashTool) Description() string { return "Execute a shell command (whitelist-restricted)" }
func (t *bashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"timeout_secs":{"type":"integer","description":"Timeout in seconds (default 30)"}},"required":["command"]}`)
}
func (t *bashTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("bash: command runner not available")
	}
	var args struct {
		Command     string `json:"command"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}
	if err := validateBashCommand(args.Command, t.registry.AllowedCommands()); err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}
	timeout := args.TimeoutSecs
	if timeout == 0 {
		timeout = 30
	}
	parts := strings.Fields(args.Command)
	if len(parts) == 0 {
		return "", fmt.Errorf("bash: empty command")
	}
	out, err := t.cmd.Run(ctx, workDir, parts[0], parts[1:], timeout)
	if err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}
	result := out.Stdout
	if out.Stderr != "" {
		result += "\nSTDERR: " + out.Stderr
	}
	if out.TimedOut {
		return result, fmt.Errorf("bash: command timed out after %ds", timeout)
	}
	return result, nil
}

func validateBashCommand(command string, allowed []string) error {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, blocked := range hardBlockedCommands {
		if strings.HasPrefix(lower, blocked+" ") || lower == blocked ||
			strings.Contains(lower, " "+blocked+" ") || strings.Contains(lower, ";"+blocked) {
			return fmt.Errorf("command %q is not allowed (hard-blocked)", blocked)
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("no commands are allowed — set allowed_commands in config")
	}
	for _, a := range allowed {
		if strings.HasPrefix(strings.TrimSpace(command), a) {
			return nil
		}
	}
	return fmt.Errorf("command %q is not in the allowed commands list", strings.Fields(command)[0])
}

// --- RunTest ---

type runTestTool struct {
	cmd      runner.CommandRunner
	registry *Registry
}

func (t *runTestTool) Name() string { return "RunTest" }
func (t *runTestTool) Description() string {
	return "Run tests and return structured pass/fail results"
}
func (t *runTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"test":{"type":"string","description":"Test name filter"},"package":{"type":"string","description":"Package path (default ./...)"},"timeout_secs":{"type":"integer"}}}`)
}
func (t *runTestTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("RunTest: command runner not available")
	}
	var args struct {
		Test        string `json:"test"`
		Package     string `json:"package"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("RunTest: %w", err)
	}
	pkg := args.Package
	if pkg == "" {
		pkg = "./..."
	}
	timeout := args.TimeoutSecs
	if timeout == 0 {
		timeout = 120
	}
	cmdArgs := []string{"test", pkg, "-v"}
	if args.Test != "" {
		cmdArgs = append(cmdArgs, "-run", args.Test)
	}
	out, err := t.cmd.Run(ctx, workDir, "go", cmdArgs, timeout)
	if err != nil {
		return "", fmt.Errorf("RunTest: %w", err)
	}
	result := runner.ParseTestOutput(out.Stdout+out.Stderr, "go")
	return fmt.Sprintf("passed=%d failed=%d total=%d\n%s",
		result.PassedTests, result.FailedTests, result.TotalTests, out.Stdout), nil
}

// --- Subagent ---

// subagentTool delegates a bounded subtask to a fresh BuiltinRunner invocation.
// It uses a RunFn function reference to avoid circular imports with agent package.
type subagentTool struct {
	registry *Registry
}

func (t *subagentTool) Name() string { return "Subagent" }
func (t *subagentTool) Description() string {
	return "Delegate a bounded subtask to a fresh agent with a restricted tool set. Returns the agent's final output."
}
func (t *subagentTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"Prompt for the subagent"},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names the subagent may use (subset of current tools)"},"max_turns":{"type":"integer","description":"Max turns for subagent (default 5, max 10)"}},"required":["task"]}`)
}
func (t *subagentTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	runFn := t.registry.GetRunFn()
	if runFn == nil {
		return "", fmt.Errorf("subagent: runner not initialized (SetRunFn not called)")
	}
	var in struct {
		Task     string   `json:"task"`
		Tools    []string `json:"tools"`
		MaxTurns int      `json:"max_turns"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("subagent: %w", err)
	}
	maxTurns := in.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 5
	}
	if maxTurns > 10 {
		maxTurns = 10
	}
	result, err := runFn(ctx, in.Task, workDir, in.Tools, maxTurns)
	if err != nil {
		return "", fmt.Errorf("subagent: %w", err)
	}
	return result, nil
}
