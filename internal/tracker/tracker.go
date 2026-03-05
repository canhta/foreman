package tracker

import (
	"context"
	"time"
)

// Ticket represents an issue from any tracker.
type Ticket struct {
	ExternalID         string
	Title              string
	Description        string
	AcceptanceCriteria string
	Labels             []string
	Priority           string
	Assignee           string
	Reporter           string
	Comments           []TicketComment
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TicketComment is a single comment on a ticket.
type TicketComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// IssueTracker abstracts Jira, GitHub Issues, Linear, etc.
type IssueTracker interface {
	FetchReadyTickets(ctx context.Context) ([]Ticket, error)
	GetTicket(ctx context.Context, externalID string) (*Ticket, error)
	UpdateStatus(ctx context.Context, externalID string, status string) error
	AddComment(ctx context.Context, externalID string, comment string) error
	AttachPR(ctx context.Context, externalID string, prURL string) error
	AddLabel(ctx context.Context, externalID string, label string) error
	RemoveLabel(ctx context.Context, externalID string, label string) error
	HasLabel(ctx context.Context, externalID string, label string) (bool, error)
	ProviderName() string
}
