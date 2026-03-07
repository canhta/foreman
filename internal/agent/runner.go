package agent

import (
	"context"
	"encoding/json"

	"github.com/canhta/foreman/internal/agent/mcp"
)

// AgentRunner abstracts any external agent SDK or CLI that can execute
// a bounded, scoped task and return a result. Used exclusively by the
// Skills engine at hook points — never inside the core pipeline.
type AgentRunner interface {
	// Run executes a single agent task and returns structured output.
	Run(ctx context.Context, req AgentRequest) (AgentResult, error)
	// HealthCheck verifies the runner is installed and configured.
	HealthCheck(ctx context.Context) error
	// RunnerName returns the identifier for logging/config.
	RunnerName() string
	// Close cleans up resources (e.g. stops Copilot CLI subprocess).
	Close() error
}

// AgentEventType identifies the kind of progress event emitted during agent execution.
type AgentEventType string

const (
	AgentEventTurnStart AgentEventType = "turn_start"
	AgentEventToolStart AgentEventType = "tool_start"
	AgentEventToolEnd   AgentEventType = "tool_end"
	AgentEventTurnEnd   AgentEventType = "turn_end"
)

// AgentEvent carries real-time progress information from a running agent session.
// It is delivered via the OnProgress callback in AgentRequest.
type AgentEvent struct {
	Type      AgentEventType
	ToolName  string
	Turn      int
	TokensIn  int
	TokensOut int
	CostUSD   float64
}

// AgentRequest defines the input for a single agent task.
type AgentRequest struct {
	OnProgress      func(AgentEvent)
	Prompt          string
	SystemPrompt    string
	WorkDir         string
	FallbackModel   string
	AllowedTools    []string
	OutputSchema    json.RawMessage
	MCPServers      []mcp.MCPServerConfig
	MaxTurns        int
	TimeoutSecs     int
	AgentDepth      int
	RemainingBudget int
}

// MaxAgentDepth is the maximum allowed nesting depth for subagent calls.
// A top-level agent is depth 0; its subagents are depth 1; etc.
const MaxAgentDepth = 3

// AgentResult holds the output of an agent task.
type AgentResult struct {
	Structured interface{}
	Output     string
	Usage      AgentUsage
}

// AgentUsage tracks resource consumption for an agent task.
type AgentUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64 // Estimated; 0 if runner doesn't expose it
	NumTurns     int     // Number of agentic turns used
	DurationMs   int     // Total execution time in milliseconds
}
