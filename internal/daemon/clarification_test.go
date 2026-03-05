package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

type mockClarificationDB struct {
	tickets       []models.Ticket
	updatedStatus map[string]models.TicketStatus
	events        []*models.EventRecord
}

func (m *mockClarificationDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	var result []models.Ticket
	for _, t := range m.tickets {
		for _, s := range filter.StatusIn {
			if t.Status == s {
				result = append(result, t)
			}
		}
	}
	return result, nil
}

func (m *mockClarificationDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.updatedStatus[id] = status
	return nil
}

func (m *mockClarificationDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	m.events = append(m.events, e)
	return nil
}

type mockClarificationTracker struct {
	comments      map[string][]string
	removedLabels map[string][]string
}

func (m *mockClarificationTracker) AddComment(_ context.Context, externalID, comment string) error {
	m.comments[externalID] = append(m.comments[externalID], comment)
	return nil
}

func (m *mockClarificationTracker) RemoveLabel(_ context.Context, externalID, label string) error {
	m.removedLabels[externalID] = append(m.removedLabels[externalID], label)
	return nil
}

func TestCheckClarificationTimeouts(t *testing.T) {
	past := time.Now().Add(-25 * time.Hour)
	db := &mockClarificationDB{
		tickets: []models.Ticket{
			{
				ID:                       "t1",
				ExternalID:               "PROJ-1",
				Status:                   models.TicketStatusClarificationNeeded,
				ClarificationRequestedAt: &past,
			},
		},
		updatedStatus: make(map[string]models.TicketStatus),
	}
	tracker := &mockClarificationTracker{
		comments:      make(map[string][]string),
		removedLabels: make(map[string][]string),
	}

	checkClarificationTimeouts(context.Background(), zerolog.Nop(), db, tracker, 24, "foreman:clarification")

	if db.updatedStatus["t1"] != models.TicketStatusBlocked {
		t.Errorf("expected blocked, got %s", db.updatedStatus["t1"])
	}
	if len(tracker.comments["PROJ-1"]) == 0 {
		t.Error("expected comment on timed-out ticket")
	}
	if len(tracker.removedLabels["PROJ-1"]) == 0 {
		t.Error("expected clarification label removed")
	}
	if len(db.events) == 0 {
		t.Error("expected RecordEvent to be called after successful status update")
	}
}

func TestCheckClarificationTimeouts_NotExpired(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	db := &mockClarificationDB{
		tickets: []models.Ticket{
			{
				ID:                       "t2",
				ExternalID:               "PROJ-2",
				Status:                   models.TicketStatusClarificationNeeded,
				ClarificationRequestedAt: &recent,
			},
		},
		updatedStatus: make(map[string]models.TicketStatus),
	}
	tracker := &mockClarificationTracker{
		comments:      make(map[string][]string),
		removedLabels: make(map[string][]string),
	}

	checkClarificationTimeouts(context.Background(), zerolog.Nop(), db, tracker, 24, "foreman:clarification")

	if _, ok := db.updatedStatus["t2"]; ok {
		t.Error("should not update non-expired ticket")
	}
	if len(tracker.comments) != 0 {
		t.Error("should not call AddComment on non-expired ticket")
	}
	if len(tracker.removedLabels) != 0 {
		t.Error("should not call RemoveLabel on non-expired ticket")
	}
}
