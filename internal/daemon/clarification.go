package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
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

func checkClarificationTimeouts(ctx context.Context, log zerolog.Logger, db ClarificationDB, tracker ClarificationTracker, timeoutHours int, clarificationLabel string) {
	tickets, err := db.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusClarificationNeeded},
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to list clarification-needed tickets")
		return
	}

	timeout := time.Duration(timeoutHours) * time.Hour
	for _, t := range tickets {
		if t.ClarificationRequestedAt == nil || time.Since(*t.ClarificationRequestedAt) <= timeout {
			continue
		}

		comment := fmt.Sprintf(
			"No response received after %d hours. Marking as blocked. "+
				"Re-apply the pickup label to retry after updating the ticket.",
			timeoutHours,
		)
		if err := tracker.AddComment(ctx, t.ExternalID, comment); err != nil {
			log.Error().Err(err).Str("ticket_id", t.ID).Str("external_id", t.ExternalID).Msg("failed to add timeout comment")
			continue
		}

		if err := tracker.RemoveLabel(ctx, t.ExternalID, clarificationLabel); err != nil {
			log.Error().Err(err).Str("ticket_id", t.ID).Str("external_id", t.ExternalID).Msg("failed to remove clarification label")
			continue
		}

		if err := db.UpdateTicketStatus(ctx, t.ID, models.TicketStatusBlocked); err != nil {
			log.Error().Err(err).Str("ticket_id", t.ID).Msg("failed to update ticket status to blocked")
			continue
		}

		if err := db.RecordEvent(ctx, &models.EventRecord{
			ID:        fmt.Sprintf("evt-%s-%d", t.ID, time.Now().UnixNano()),
			TicketID:  t.ID,
			EventType: "clarification_timeout",
			CreatedAt: time.Now(),
		}); err != nil {
			log.Error().Err(err).Str("ticket_id", t.ID).Msg("failed to record clarification_timeout event")
		}
	}
}
