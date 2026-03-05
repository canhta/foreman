package models

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
)

// LlmRequest holds the parameters for a single stateless LLM call.
type LlmRequest struct {
	Model         string   `json:"model"`
	SystemPrompt  string   `json:"system_prompt"`
	UserPrompt    string   `json:"user_prompt"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   float64  `json:"temperature"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// LlmResponse holds the result of a single LLM call.
type LlmResponse struct {
	Content      string     `json:"content"`
	TokensInput  int        `json:"tokens_input"`
	TokensOutput int        `json:"tokens_output"`
	Model        string     `json:"model"`
	DurationMs   int64      `json:"duration_ms"`
	StopReason   StopReason `json:"stop_reason"`
}
