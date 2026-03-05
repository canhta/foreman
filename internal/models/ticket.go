package models

import "time"

type Ticket struct {
	ID                       string
	ExternalID               string
	Title                    string
	Description              string
	AcceptanceCriteria       string
	Labels                   []string
	Priority                 string
	Assignee                 string
	Reporter                 string
	Comments                 []TicketComment
	Status                   TicketStatus
	ExternalStatus           string
	RepoURL                  string
	BranchName               string
	PRURL                    string
	PRNumber                 int
	IsPartial                bool
	CostUSD                  float64
	TokensInput              int
	TokensOutput             int
	TotalLlmCalls            int
	ClarificationRequestedAt *time.Time
	ErrorMessage             string
	LastCompletedTaskSeq     int
	CreatedAt                time.Time
	StartedAt                *time.Time
	CompletedAt              *time.Time
	UpdatedAt                time.Time
}

type TicketComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

type Task struct {
	ID                     string
	TicketID               string
	Sequence               int
	Title                  string
	Description            string
	AcceptanceCriteria     []string
	FilesToRead            []string
	FilesToModify          []string
	TestAssertions         []string
	EstimatedComplexity    string
	DependsOn              []string
	Status                 TaskStatus
	ImplementationAttempts int
	SpecReviewAttempts     int
	QualityReviewAttempts  int
	TotalLlmCalls          int
	CommitSHA              string
	CostUSD                float64
	CreatedAt              time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
}

type LlmCallRecord struct {
	ID              string
	TicketID        string
	TaskID          string
	Role            string
	Provider        string
	Model           string
	Attempt         int
	TokensInput     int
	TokensOutput    int
	CostUSD         float64
	DurationMs      int64
	PromptHash      string
	ResponseSummary string
	Status          string
	ErrorMessage    string
	CreatedAt       time.Time
}

type HandoffRecord struct {
	ID        string
	TicketID  string
	FromRole  string
	ToRole    string
	Key       string
	Value     string
	CreatedAt time.Time
}

type ProgressPattern struct {
	ID               string
	TicketID         string
	PatternKey       string
	PatternValue     string
	Directories      []string
	DiscoveredByTask string
	CreatedAt        time.Time
}

type EventRecord struct {
	ID        string
	TicketID  string
	TaskID    string
	EventType string
	Severity  string
	Message   string
	Details   string
	CreatedAt time.Time
}

type TicketFilter struct {
	Status   string
	StatusIn []string
}
