package models

import "encoding/json"

type TicketStatus string

const (
	TicketStatusQueued              TicketStatus = "queued"
	TicketStatusClarificationNeeded TicketStatus = "clarification_needed"
	TicketStatusPlanning            TicketStatus = "planning"
	TicketStatusPlanValidating      TicketStatus = "plan_validating"
	TicketStatusImplementing        TicketStatus = "implementing"
	TicketStatusReviewing           TicketStatus = "reviewing"
	TicketStatusPRCreated           TicketStatus = "pr_created"
	TicketStatusDone                TicketStatus = "done"
	TicketStatusPartial             TicketStatus = "partial"
	TicketStatusFailed              TicketStatus = "failed"
	TicketStatusBlocked             TicketStatus = "blocked"
)

type TaskStatus string

const (
	TaskStatusPending       TaskStatus = "pending"
	TaskStatusImplementing  TaskStatus = "implementing"
	TaskStatusTDDVerifying  TaskStatus = "tdd_verifying"
	TaskStatusTesting       TaskStatus = "testing"
	TaskStatusSpecReview    TaskStatus = "spec_review"
	TaskStatusQualityReview TaskStatus = "quality_review"
	TaskStatusDone          TaskStatus = "done"
	TaskStatusFailed        TaskStatus = "failed"
	TaskStatusSkipped       TaskStatus = "skipped"
)

type StopReason string

const (
	StopReasonEndTurn      StopReason = "end_turn"
	StopReasonMaxTokens    StopReason = "max_tokens"
	StopReasonStopSequence StopReason = "stop_sequence"
	StopReasonToolUse      StopReason = "tool_use"
)

// ToolDef describes a tool the LLM can call.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult holds the output of executing a tool.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// Message represents a single message in a multi-turn conversation.
type Message struct {
	Role        string       `json:"role"`
	Content     string       `json:"content,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

// ThinkingConfig enables extended thinking for supported models.
type ThinkingConfig struct {
	Enabled      bool `json:"enabled"`
	BudgetTokens int  `json:"budget_tokens"` // e.g. 10000
}

// LlmRequest holds the parameters for a single stateless LLM call.
type LlmRequest struct {
	Model             string           `json:"model"`
	SystemPrompt      string           `json:"system_prompt"`
	UserPrompt        string           `json:"user_prompt"`
	MaxTokens         int              `json:"max_tokens"`
	Temperature       float64          `json:"temperature"`
	StopSequences     []string         `json:"stop_sequences,omitempty"`
	Messages          []Message        `json:"messages,omitempty"`            // Multi-turn (overrides UserPrompt when non-empty)
	Tools             []ToolDef        `json:"tools,omitempty"`               // Tool definitions for tool-use
	OutputSchema      *json.RawMessage `json:"output_schema,omitempty"`       // JSON Schema for structured output
	Thinking          *ThinkingConfig  `json:"thinking,omitempty"`            // Extended thinking (Anthropic only)
	CacheSystemPrompt bool             `json:"cache_system_prompt,omitempty"` // Prompt caching (Anthropic only)
}

// LlmResponse holds the result of a single LLM call.
type LlmResponse struct {
	Content             string     `json:"content"`
	TokensInput         int        `json:"tokens_input"`
	TokensOutput        int        `json:"tokens_output"`
	Model               string     `json:"model"`
	DurationMs          int64      `json:"duration_ms"`
	StopReason          StopReason `json:"stop_reason"`
	ToolCalls           []ToolCall `json:"tool_calls,omitempty"`            // Populated when StopReason == StopReasonToolUse
	CacheReadTokens     int        `json:"cache_read_tokens,omitempty"`     // Anthropic prompt caching
	CacheCreationTokens int        `json:"cache_creation_tokens,omitempty"` // Anthropic prompt caching
}
