package models

import "time"

type Ticket struct {
	UpdatedAt                time.Time
	CreatedAt                time.Time
	ClarificationRequestedAt *time.Time
	CompletedAt              *time.Time
	StartedAt                *time.Time
	Assignee                 string
	RepoURL                  string
	ID                       string
	Reporter                 string
	ExternalID               string
	Status                   TicketStatus
	ExternalStatus           string
	Priority                 string
	BranchName               string
	PRURL                    string
	Title                    string
	Description              string
	AcceptanceCriteria       string
	ErrorMessage             string
	Labels                   []string
	Comments                 []TicketComment
	TotalLlmCalls            int
	TokensOutput             int
	TokensInput              int
	LastCompletedTaskSeq     int
	CostUSD                  float64
	PRNumber                 int
	IsPartial                bool
	ParentTicketID           string
	ChildTicketIDs           []string
	DecomposeDepth           int
}

type TicketComment struct {
	CreatedAt time.Time
	Author    string
	Body      string
}

type Task struct {
	CreatedAt              time.Time
	CompletedAt            *time.Time
	StartedAt              *time.Time
	ID                     string
	TicketID               string
	Title                  string
	Description            string
	CommitSHA              string
	Status                 TaskStatus
	EstimatedComplexity    string
	DependsOn              []string
	TestAssertions         []string
	FilesToModify          []string
	FilesToRead            []string
	AcceptanceCriteria     []string
	ImplementationAttempts int
	SpecReviewAttempts     int
	QualityReviewAttempts  int
	TotalLlmCalls          int
	CostUSD                float64
	Sequence               int
}

type LlmCallRecord struct {
	CreatedAt       time.Time
	ResponseSummary string
	PromptHash      string
	Role            string
	Provider        string
	Model           string
	TicketID        string
	TaskID          string
	ErrorMessage    string
	Status          string
	ID              string
	TokensOutput    int
	DurationMs      int64
	TokensInput     int
	CostUSD         float64
	Attempt         int
}

type HandoffRecord struct {
	CreatedAt time.Time
	ID        string
	TicketID  string
	FromRole  string
	ToRole    string
	Key       string
	Value     string
}

type ProgressPattern struct {
	CreatedAt        time.Time
	ID               string
	TicketID         string
	PatternKey       string
	PatternValue     string
	DiscoveredByTask string
	Directories      []string
}

type EventRecord struct {
	CreatedAt time.Time
	ID        string
	TicketID  string
	TaskID    string
	EventType string
	Severity  string
	Message   string
	Details   string
}

type TicketFilter struct {
	Status   string
	StatusIn []TicketStatus
}
