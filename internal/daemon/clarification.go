package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// ClarificationDB is the subset of db.Database needed for timeout checks.
type ClarificationDB interface {
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// ClarificationTracker is the subset of tracker.IssueTracker needed for timeout checks.
type ClarificationTracker interface {
	AddComment(ctx context.Context, externalID, comment string) error
	RemoveLabel(ctx context.Context, externalID, label string) error
}

func checkClarificationTimeouts(ctx context.Context, db ClarificationDB, tracker ClarificationTracker, timeoutHours int, clarificationLabel string) {
	tickets, err := db.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusClarificationNeeded},
	})
	if err != nil {
		return
	}

	timeout := time.Duration(timeoutHours) * time.Hour
	for _, t := range tickets {
		if t.ClarificationRequestedAt == nil || time.Since(*t.ClarificationRequestedAt) <= timeout {
			continue
		}

		tracker.AddComment(ctx, t.ExternalID, fmt.Sprintf(
			"No response received after %d hours. Marking as blocked. "+
				"Re-apply the pickup label to retry after updating the ticket.",
			timeoutHours,
		))
		tracker.RemoveLabel(ctx, t.ExternalID, clarificationLabel)
		db.UpdateTicketStatus(ctx, t.ID, models.TicketStatusBlocked)
	}
}
