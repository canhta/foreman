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
	TicketStatusDecomposing         TicketStatus = "decomposing"
	TicketStatusDecomposed          TicketStatus = "decomposed"
	TicketStatusAwaitingMerge       TicketStatus = "awaiting_merge"
	TicketStatusMerged              TicketStatus = "merged"
	TicketStatusPRClosed            TicketStatus = "pr_closed"
	TicketStatusPRUpdated           TicketStatus = "pr_updated"
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
	TaskStatusEscalated     TaskStatus = "escalated"
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
// For Opus 4.6 / Sonnet 4.6 set Adaptive=true (no BudgetTokens needed).
// For older models set Enabled=true with BudgetTokens (minimum 1024, must be < MaxTokens).
type ThinkingConfig struct {
	Enabled      bool `json:"enabled"`
	Adaptive     bool `json:"adaptive,omitempty"`
	BudgetTokens int  `json:"budget_tokens,omitempty"` // only for Enabled (legacy) mode
}

// LlmRequest holds the parameters for a single stateless LLM call.
type LlmRequest struct {
	OutputSchema  *json.RawMessage `json:"output_schema,omitempty"`
	Thinking      *ThinkingConfig  `json:"thinking,omitempty"`
	Model         string           `json:"model"`
	SystemPrompt  string           `json:"system_prompt"`
	UserPrompt    string           `json:"user_prompt"`
	PromptVersion string           `json:"prompt_version,omitempty"` // SHA256 of the prompt template used
	// Stage identifies the pipeline stage making this LLM call (e.g. "planning",
	// "implementing", "spec_review"). Populated by callers so RecordLlmCall can
	// store per-stage cost breakdowns (ARCH-O04). Empty string is valid and means
	// the stage is unknown.
	Stage             string    `json:"stage,omitempty"`
	StopSequences     []string  `json:"stop_sequences,omitempty"`
	Messages          []Message `json:"messages,omitempty"`
	Tools             []ToolDef `json:"tools,omitempty"`
	MaxTokens         int       `json:"max_tokens"`
	Temperature       float64   `json:"temperature"`
	CacheSystemPrompt bool      `json:"cache_system_prompt,omitempty"`
}

// LlmResponse holds the result of a single LLM call.
type LlmResponse struct {
	Content             string     `json:"content"`
	Model               string     `json:"model"`
	StopReason          StopReason `json:"stop_reason"`
	ToolCalls           []ToolCall `json:"tool_calls,omitempty"`
	TokensInput         int        `json:"tokens_input"`
	TokensOutput        int        `json:"tokens_output"`
	DurationMs          int64      `json:"duration_ms"`
	CacheReadTokens     int        `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int        `json:"cache_creation_tokens,omitempty"`
}
