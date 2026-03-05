package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// mockMergeDB implements MergeCheckerDB for testing.
type mockMergeDB struct {
	tickets       map[string]*models.Ticket
	statusUpdates map[string]models.TicketStatus
}

func newMockMergeDB() *mockMergeDB {
	return &mockMergeDB{
		tickets:       make(map[string]*models.Ticket),
		statusUpdates: make(map[string]models.TicketStatus),
	}
}

func (m *mockMergeDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	var result []models.Ticket
	for _, t := range m.tickets {
		if filter.Status != "" && string(t.Status) == filter.Status {
			result = append(result, *t)
		}
		for _, s := range filter.StatusIn {
			if t.Status == s {
				result = append(result, *t)
				break
			}
		}
	}
	return result, nil
}

func (m *mockMergeDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.statusUpdates[id] = status
	if t, ok := m.tickets[id]; ok {
		t.Status = status
	}
	return nil
}

func (m *mockMergeDB) GetTicketByExternalID(_ context.Context, extID string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ExternalID == extID {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockMergeDB) GetChildTickets(_ context.Context, parentExtID string) ([]models.Ticket, error) {
	var children []models.Ticket
	for _, t := range m.tickets {
		if t.ParentTicketID == parentExtID {
			children = append(children, *t)
		}
	}
	return children, nil
}

// mockPRChecker returns configured statuses per PR number.
type mockPRChecker struct {
	statuses map[int]git.PRMergeStatus
}

func (m *mockPRChecker) GetPRStatus(_ context.Context, prNumber int) (git.PRMergeStatus, error) {
	if s, ok := m.statuses[prNumber]; ok {
		return s, nil
	}
	return git.PRMergeStatus{State: "open"}, nil
}

func TestMergeChecker_HandleMerged(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID: "T1", ExternalID: "EXT-1", Status: models.TicketStatusAwaitingMerge, PRNumber: 42,
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: "merged", MergedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	assert.Equal(t, models.TicketStatusMerged, db.statusUpdates["T1"])
}

func TestMergeChecker_HandleClosed(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID: "T1", ExternalID: "EXT-1", Status: models.TicketStatusAwaitingMerge, PRNumber: 10,
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		10: {State: "closed", ClosedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	assert.Equal(t, models.TicketStatusPRClosed, db.statusUpdates["T1"])
}

func TestMergeChecker_ParentCompletion(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["parent"] = &models.Ticket{
		ID: "parent", ExternalID: "EXT-P", Status: models.TicketStatusDecomposed,
	}
	db.tickets["child1"] = &models.Ticket{
		ID: "child1", ExternalID: "EXT-C1", Status: models.TicketStatusAwaitingMerge,
		PRNumber: 1, ParentTicketID: "EXT-P",
	}
	db.tickets["child2"] = &models.Ticket{
		ID: "child2", ExternalID: "EXT-C2", Status: models.TicketStatusMerged,
		ParentTicketID: "EXT-P",
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		1: {State: "merged", MergedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	// child1 merged
	assert.Equal(t, models.TicketStatusMerged, db.statusUpdates["child1"])
	// parent should be done since both children are now merged
	assert.Equal(t, models.TicketStatusDone, db.statusUpdates["parent"])
}
