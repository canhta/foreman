package agent

import (
	"context"
	"encoding/json"

	"github.com/canhta/foreman/internal/agent/agentconst"
	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/models"
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
	OnProgress func(AgentEvent)
	// Thinking configures adaptive or extended thinking for the builtin runner.
	// ClaudeCode and Copilot runners ignore this field (thinking is model-managed).
	Thinking        *models.ThinkingConfig
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
// Defined in agentconst to share with agent/tools without an import cycle.
const MaxAgentDepth = agentconst.MaxAgentDepth

// FileChange represents a single file modification extracted from agent output.
// It may carry either a full file replacement (IsDiff=false) or a unified diff (IsDiff=true).
type FileChange struct {
	// OldContent is the original content targeted by a SEARCH block, if any.
	OldContent string
	// NewContent is the replacement content or unified diff.
	NewContent string
	// Path is the file path relative to the agent's WorkDir.
	Path string
	// IsDiff is true when NewContent is a unified diff rather than full file content.
	IsDiff bool
}

// ReviewResult represents a parsed review decision returned by a reviewer agent.
type ReviewResult struct {
	// Summary is a brief summary of the review decision.
	// NOTE: Not populated by the current parser implementation; reserved for future extraction.
	Summary string
	// Severity is one of "none", "minor", "major", or "critical".
	Severity string
	// Issues contains review issues extracted from agent output.
	// NOTE: Not populated by the current parser implementation; reserved for future extraction.
	Issues []string
	// Approved is true when the review verdict was STATUS: APPROVED.
	Approved bool
}

// AgentResult holds the output of an agent task.
type AgentResult struct {
	// Structured holds arbitrary structured output when the request specified an OutputSchema.
	Structured interface{}
	// ReviewResult holds a parsed review decision when the agent output contained a STATUS: line.
	// Nil when no STATUS: pattern was found.
	ReviewResult *ReviewResult
	// Metadata holds arbitrary key-value pairs for runner-specific annotations.
	Metadata map[string]string
	// Output is the raw text output from the agent's final response.
	Output string
	// FileChanges holds parsed file modifications extracted from the agent output.
	// Populated when the output contains NEW FILE / MODIFY FILE blocks.
	FileChanges []FileChange
	// Usage tracks token and cost consumption for the task.
	Usage AgentUsage
}

// AgentUsage tracks resource consumption for an agent task.
type AgentUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64 // Estimated; 0 if runner doesn't expose it
	NumTurns     int     // Number of agentic turns used
	DurationMs   int     // Total execution time in milliseconds
}
