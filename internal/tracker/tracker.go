package tracker

import (
	"context"
	"time"
)

// Ticket represents an issue from any tracker.
type Ticket struct {
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ExternalID         string
	Title              string
	Description        string
	AcceptanceCriteria string
	Priority           string
	Assignee           string
	Reporter           string
	Labels             []string
	Comments           []TicketComment
}

// TicketComment is a single comment on a ticket.
type TicketComment struct {
	CreatedAt time.Time
	Author    string
	Body      string
}

// CreateTicketRequest describes a new ticket to create in the tracker.
type CreateTicketRequest struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Labels             []string
	ParentID           string
	Metadata           map[string]string
}

// IssueTracker abstracts Jira, GitHub Issues, Linear, etc.
type IssueTracker interface {
	CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error)
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
