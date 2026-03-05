package daemon

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// PickupDB is the subset of db.Database needed for pickup guard.
type PickupDB interface {
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
}

// PickupTracker is the subset of tracker.IssueTracker needed for pickup guard.
type PickupTracker interface {
	HasLabel(ctx context.Context, externalID, label string) (bool, error)
}

func shouldPickUp(ctx context.Context, db PickupDB, tracker PickupTracker, externalID, clarificationLabel string) bool {
	existing, err := db.GetTicketByExternalID(ctx, externalID)
	if err != nil {
		return true // New ticket, safe to pick up
	}
	if existing.Status == models.TicketStatusClarificationNeeded {
		hasLabel, _ := tracker.HasLabel(ctx, externalID, clarificationLabel)
		return !hasLabel
	}
	return existing.Status == models.TicketStatusQueued
}
