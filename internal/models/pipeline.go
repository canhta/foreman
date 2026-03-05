package models

type TicketStatus string

const (
	TicketStatusQueued               TicketStatus = "queued"
	TicketStatusClarificationNeeded  TicketStatus = "clarification_needed"
	TicketStatusPlanning             TicketStatus = "planning"
	TicketStatusPlanValidating       TicketStatus = "plan_validating"
	TicketStatusImplementing         TicketStatus = "implementing"
	TicketStatusReviewing            TicketStatus = "reviewing"
	TicketStatusPRCreated            TicketStatus = "pr_created"
	TicketStatusDone                 TicketStatus = "done"
	TicketStatusPartial              TicketStatus = "partial"
	TicketStatusFailed               TicketStatus = "failed"
	TicketStatusBlocked              TicketStatus = "blocked"
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
