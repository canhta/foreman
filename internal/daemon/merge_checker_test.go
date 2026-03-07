package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// mockMergeDB implements MergeCheckerDB for testing.
type mockMergeDB struct {
	mu               sync.Mutex
	tickets          map[string]*models.Ticket
	statusUpdates    map[string]models.TicketStatus
	prHeadSHAUpdates map[string]string
	events           []*models.EventRecord
}

func newMockMergeDB() *mockMergeDB {
	return &mockMergeDB{
		tickets:          make(map[string]*models.Ticket),
		statusUpdates:    make(map[string]models.TicketStatus),
		prHeadSHAUpdates: make(map[string]string),
		events:           nil,
	}
}

func (m *mockMergeDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates[id] = status
	if t, ok := m.tickets[id]; ok {
		t.Status = status
	}
	return nil
}

func (m *mockMergeDB) UpdateTicketStatusIfEquals(_ context.Context, id string, newStatus models.TicketStatus, requiredCurrentStatus models.TicketStatus) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.Status != requiredCurrentStatus {
		return false, nil
	}
	t.Status = newStatus
	m.statusUpdates[id] = newStatus
	return true, nil
}

func (m *mockMergeDB) SetTicketPRHeadSHA(_ context.Context, ticketID, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prHeadSHAUpdates[ticketID] = sha
	if t, ok := m.tickets[ticketID]; ok {
		t.PRHeadSHA = sha
	}
	return nil
}

func (m *mockMergeDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *mockMergeDB) GetTicketByExternalID(_ context.Context, extID string) (*models.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tickets {
		if t.ExternalID == extID {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockMergeDB) GetChildTickets(_ context.Context, parentExtID string) ([]models.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func TestMergeChecker_DetectsPRUpdate(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID:         "T1",
		ExternalID: "EXT-1",
		Status:     models.TicketStatusAwaitingMerge,
		PRNumber:   42,
		PRHeadSHA:  "abc123",
	}

	// PR is still open but HEAD SHA changed (someone pushed to branch externally)
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: git.PRStateOpen, HeadSHA: "def456"},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	// Status should be updated to pr_updated
	assert.Equal(t, models.TicketStatusPRUpdated, db.statusUpdates["T1"])
	// New SHA should be stored
	assert.Equal(t, "def456", db.prHeadSHAUpdates["T1"])
	// An event should have been recorded
	assert.Len(t, db.events, 1)
	assert.Equal(t, "pr_updated", db.events[0].EventType)
	assert.Equal(t, "T1", db.events[0].TicketID)
}

func TestMergeChecker_NoUpdateWhenSHAUnchanged(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID:         "T1",
		ExternalID: "EXT-1",
		Status:     models.TicketStatusAwaitingMerge,
		PRNumber:   42,
		PRHeadSHA:  "abc123",
	}

	// Same SHA — no external push
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: git.PRStateOpen, HeadSHA: "abc123"},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	// No status update should have been recorded
	assert.Empty(t, db.statusUpdates)
	assert.Empty(t, db.prHeadSHAUpdates)
}

func TestMergeChecker_NoUpdateWhenStoredSHAEmpty(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID:         "T1",
		ExternalID: "EXT-1",
		Status:     models.TicketStatusAwaitingMerge,
		PRNumber:   42,
		PRHeadSHA:  "", // not yet stored (legacy ticket)
	}

	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: git.PRStateOpen, HeadSHA: "abc123"},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	// Should store the SHA but NOT mark as pr_updated (first time initialization)
	assert.NotContains(t, db.statusUpdates, "T1")
	assert.Equal(t, "abc123", db.prHeadSHAUpdates["T1"])
}

func TestMergeChecker_SendsNotificationOnPRUpdate(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID:              "T1",
		ExternalID:      "EXT-1",
		Status:          models.TicketStatusAwaitingMerge,
		PRNumber:        42,
		PRHeadSHA:       "abc123",
		ChannelSenderID: "user-456",
	}

	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: git.PRStateOpen, HeadSHA: "def456"},
	}}

	var notifiedTicketID string
	var notifiedMsg string

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.SetNotify(func(_ context.Context, ticket *models.Ticket, msg string) {
		notifiedTicketID = ticket.ID
		notifiedMsg = msg
	})
	mc.checkAll(context.Background())

	// Status should be updated to pr_updated
	assert.Equal(t, models.TicketStatusPRUpdated, db.statusUpdates["T1"])
	// Notification should have been sent
	assert.Equal(t, "T1", notifiedTicketID)
	assert.Contains(t, notifiedMsg, "EXT-1")
	assert.Contains(t, notifiedMsg, "def456")
	assert.Contains(t, notifiedMsg, "Manual re-labeling")
}

func TestMergeChecker_NoNotificationWhenNoSHAChange(t *testing.T) {
	db := newMockMergeDB()
	db.tickets["T1"] = &models.Ticket{
		ID:              "T1",
		ExternalID:      "EXT-1",
		Status:          models.TicketStatusAwaitingMerge,
		PRNumber:        42,
		PRHeadSHA:       "abc123",
		ChannelSenderID: "user-456",
	}

	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: git.PRStateOpen, HeadSHA: "abc123"},
	}}

	notifyCalled := false
	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.SetNotify(func(_ context.Context, _ *models.Ticket, _ string) {
		notifyCalled = true
	})
	mc.checkAll(context.Background())

	assert.False(t, notifyCalled, "notify should not be called when SHA is unchanged")
}

func TestMergeChecker_ParentCompletion_ConcurrentIdempotency(t *testing.T) {
	// Simulate two goroutines calling checkParentCompletion concurrently for the same
	// parent. Only one should proceed with tracker side effects (ARCH-F06).
	mdb := newMockMergeDB()
	mdb.tickets["parent"] = &models.Ticket{
		ID: "parent", ExternalID: "EXT-P", Status: models.TicketStatusDecomposed,
	}
	mdb.tickets["child1"] = &models.Ticket{
		ID: "child1", ExternalID: "EXT-C1", Status: models.TicketStatusMerged,
		ParentTicketID: "EXT-P",
	}
	mdb.tickets["child2"] = &models.Ticket{
		ID: "child2", ExternalID: "EXT-C2", Status: models.TicketStatusMerged,
		ParentTicketID: "EXT-P",
	}

	// Track how many times the tracker UpdateStatus is called.
	var trackerCallCount int64
	mockTracker := &countingTracker{callCount: &trackerCallCount}

	mc := NewMergeChecker(mdb, nil, nil, mockTracker, zerolog.Nop())

	// Simulate two concurrent calls to checkParentCompletion.
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			mc.checkParentCompletion(context.Background(), "EXT-P")
		}()
	}
	wg.Wait()

	// Parent should be Done.
	assert.Equal(t, models.TicketStatusDone, mdb.tickets["parent"].Status)
	// Tracker UpdateStatus must be called exactly once, not twice.
	assert.Equal(t, int64(1), atomic.LoadInt64(&trackerCallCount),
		"tracker should be called exactly once even with concurrent completions")
}

// countingTracker is a minimal tracker.IssueTracker stub that counts UpdateStatus calls.
type countingTracker struct {
	callCount *int64
}

func (c *countingTracker) UpdateStatus(_ context.Context, _ string, _ string) error {
	atomic.AddInt64(c.callCount, 1)
	return nil
}

func (c *countingTracker) CreateTicket(_ context.Context, _ tracker.CreateTicketRequest) (*tracker.Ticket, error) {
	return nil, nil
}
func (c *countingTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) {
	return nil, nil
}
func (c *countingTracker) GetTicket(_ context.Context, _ string) (*tracker.Ticket, error) {
	return nil, nil
}
func (c *countingTracker) AddComment(_ context.Context, _, _ string) error  { return nil }
func (c *countingTracker) AttachPR(_ context.Context, _, _ string) error    { return nil }
func (c *countingTracker) AddLabel(_ context.Context, _, _ string) error    { return nil }
func (c *countingTracker) RemoveLabel(_ context.Context, _, _ string) error { return nil }
func (c *countingTracker) HasLabel(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (c *countingTracker) ProviderName() string { return "counting" }

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
