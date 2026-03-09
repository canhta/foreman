package models

import "time"

type Ticket struct {
	UpdatedAt                time.Time
	CreatedAt                time.Time
	ClarificationRequestedAt *time.Time
	CompletedAt              *time.Time
	StartedAt                *time.Time
	PRURL                    string
	PRHeadSHA                string
	ErrorMessage             string
	ID                       string
	Reporter                 string
	ExternalID               string
	Status                   TicketStatus
	ExternalStatus           string
	Priority                 string
	BranchName               string
	Assignee                 string
	Title                    string
	Description              string
	AcceptanceCriteria       string
	RepoURL                  string
	ParentTicketID           string
	ChannelSenderID          string
	Comments                 []TicketComment
	Labels                   []string
	ChildTicketIDs           []string
	TotalLlmCalls            int
	TokensOutput             int
	TokensInput              int
	LastCompletedTaskSeq     int
	CostUSD                  float64
	PRNumber                 int
	DecomposeDepth           int
	IsPartial                bool
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
	AgentRunner            string // runner that executed this task: "builtin", "claudecode", "copilot"
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
	CreatedAt           time.Time
	ResponseSummary     string
	PromptHash          string
	PromptVersion       string
	Role                string
	Provider            string
	Model               string
	Stage               string
	TicketID            string
	TaskID              string
	ErrorMessage        string
	Status              string
	ID                  string
	AgentRunner         string // runner that made this call: "builtin", "claudecode", "copilot"
	TokensOutput        int
	DurationMs          int64
	TokensInput         int
	CostUSD             float64
	Attempt             int
	CacheReadTokens     int
	CacheCreationTokens int
}

type HandoffRecord struct {
	CreatedAt  time.Time
	ID         string
	TicketID   string
	FromRole   string
	ToRole     string
	Key        string
	Value      string
	Supersedes string
	Version    int
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
	Seq       int64 `json:"seq,omitempty"`
}

type TicketFilter struct {
	Status   string
	StatusIn []TicketStatus
}

// TeamStat represents aggregated ticket stats per submitter.
type TeamStat struct {
	ChannelSenderID string  `json:"channel_sender_id"`
	TicketCount     int     `json:"ticket_count"`
	CostUSD         float64 `json:"cost_usd"`
	FailedCount     int     `json:"failed_count"`
}

// TicketSummary is a Ticket with aggregated task counts for list views.
type TicketSummary struct {
	Ticket
	TasksTotal int `json:"tasks_total"`
	TasksDone  int `json:"tasks_done"`
}
