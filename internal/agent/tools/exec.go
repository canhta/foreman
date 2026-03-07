package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/agent/agentconst"
	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/runner"
)

// hardBlockedCommands are never allowed regardless of AllowedCommands config.
var hardBlockedCommands = []string{"rm", "curl", "wget", "ssh", "scp", "git push", "git reset", "dd", "mkfs", "shutdown", "reboot"}

func registerExec(r *Registry, cmd runner.CommandRunner, mcpMgr *mcp.Manager) {
	r.Register(&bashTool{cmd: cmd, registry: r})
	r.Register(&runTestTool{cmd: cmd, registry: r})
	r.Register(&subagentTool{registry: r})
	r.Register(&listMCPToolsTool{mcpMgr: mcpMgr})
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
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	binary := parts[0]
	for _, a := range allowed {
		if binary == a {
			return nil
		}
	}
	return fmt.Errorf("command %q is not in the allowed commands list", binary)
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

	// Enforce max agent depth before delegating.
	parentBudget, parentDepth := t.registry.GetParentBudgetAndDepth()
	if parentDepth >= agentconst.MaxAgentDepth {
		return "", fmt.Errorf("subagent: max agent depth %d reached; cannot nest further", agentconst.MaxAgentDepth)
	}

	// Enforce budget: if parent has a remaining budget and it's exhausted, fail.
	// parentBudget == 0 means unconstrained; parentBudget < 0 means exhausted.
	if parentBudget < 0 {
		return "", fmt.Errorf("subagent: parent budget exhausted")
	}

	maxTurns := in.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 5
	}
	if maxTurns > 10 {
		maxTurns = 10
	}

	// Inherit budget: cap subagent turns to parent's remaining budget if constrained.
	if parentBudget > 0 && maxTurns > parentBudget {
		maxTurns = parentBudget
	}

	result, err := runFn(ctx, in.Task, workDir, in.Tools, maxTurns, parentBudget, parentDepth+1)
	if err != nil {
		return "", fmt.Errorf("subagent: %w", err)
	}
	return result, nil
}

// --- ListMCPTools ---

// listMCPToolsTool returns the in-memory MCP tool summaries from the Manager.
// It makes no network calls — results come from the cache populated during init.
type listMCPToolsTool struct {
	mcpMgr *mcp.Manager
}

func (t *listMCPToolsTool) Name() string { return "ListMCPTools" }
func (t *listMCPToolsTool) Description() string {
	return "List all MCP tools available at runtime. Returns normalized_name, original_name, server_name, and description for each tool. Reads from in-memory cache only — no network calls."
}
func (t *listMCPToolsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *listMCPToolsTool) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	if t.mcpMgr == nil {
		return "[]", nil
	}
	summaries := t.mcpMgr.ListToolSummaries()
	b, err := json.Marshal(summaries)
	if err != nil {
		return "", fmt.Errorf("ListMCPTools: marshal: %w", err)
	}
	return string(b), nil
}
