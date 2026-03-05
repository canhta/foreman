package daemon

import (
	"context"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

// PickupDB is the subset of db.Database needed for pickup guard.
type PickupDB interface {
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
}

// PickupTracker is the subset of tracker.IssueTracker needed for pickup guard.
type PickupTracker interface {
	HasLabel(ctx context.Context, externalID, label string) (bool, error)
}

func shouldPickUp(ctx context.Context, log zerolog.Logger, db PickupDB, tracker PickupTracker, externalID, clarificationLabel string) bool {
	existing, err := db.GetTicketByExternalID(ctx, externalID)
	if err != nil {
		return true // New ticket, safe to pick up
	}
	if existing.Status == models.TicketStatusClarificationNeeded {
		hasLabel, err := tracker.HasLabel(ctx, externalID, clarificationLabel)
		if err != nil {
			log.Warn().Err(err).Str("external_id", externalID).Msg("failed to check label, treating as pickup-eligible")
			return true
		}
		return !hasLabel
	}
	return existing.Status == models.TicketStatusQueued
}
