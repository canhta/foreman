package daemon

import (
	"context"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockPickupDB struct {
	tickets map[string]*models.Ticket
}

func (m *mockPickupDB) GetTicketByExternalID(_ context.Context, externalID string) (*models.Ticket, error) {
	t, ok := m.tickets[externalID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return t, nil
}

type mockPickupTracker struct {
	labels map[string][]string
}

func (m *mockPickupTracker) HasLabel(_ context.Context, externalID, label string) (bool, error) {
	for _, l := range m.labels[externalID] {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}

func TestShouldPickUp_NewTicket(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{}}
	tracker := &mockPickupTracker{labels: map[string][]string{}}

	if !shouldPickUp(context.Background(), db, tracker, "NEW-1", "foreman:clarification") {
		t.Error("expected true for new ticket")
	}
}

func TestShouldPickUp_ClarificationWithLabel(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-1": {ID: "t1", ExternalID: "PROJ-1", Status: models.TicketStatusClarificationNeeded},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{
		"PROJ-1": {"foreman:clarification"},
	}}

	if shouldPickUp(context.Background(), db, tracker, "PROJ-1", "foreman:clarification") {
		t.Error("expected false — still has clarification label")
	}
}

func TestShouldPickUp_ClarificationLabelRemoved(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-2": {ID: "t2", ExternalID: "PROJ-2", Status: models.TicketStatusClarificationNeeded},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{
		"PROJ-2": {},
	}}

	if !shouldPickUp(context.Background(), db, tracker, "PROJ-2", "foreman:clarification") {
		t.Error("expected true — clarification label was removed (author responded)")
	}
}

func TestShouldPickUp_ActiveTicket(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-3": {ID: "t3", ExternalID: "PROJ-3", Status: models.TicketStatusImplementing},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{}}

	if shouldPickUp(context.Background(), db, tracker, "PROJ-3", "foreman:clarification") {
		t.Error("expected false — ticket already active")
	}
}
