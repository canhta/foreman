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

// AgentRequest defines the input for a single agent task.
type AgentRequest struct {
	Prompt        string
	SystemPrompt  string
	WorkDir       string
	FallbackModel string
	AllowedTools  []string
	OutputSchema  json.RawMessage
	MCPServers    []mcp.MCPServerConfig
	MaxTurns      int
	TimeoutSecs   int
	AgentDepth    int
}

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
