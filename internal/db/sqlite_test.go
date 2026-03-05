package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (Database, func()) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "foreman-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	db, err := NewSQLiteDB(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	return db, func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}
}

func TestSQLiteDB_CreateAndGetTicket(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ticket := &models.Ticket{
		ID:          "t-1",
		ExternalID:  "PROJ-123",
		Title:       "Test ticket",
		Description: "Test description",
		Status:      models.TicketStatusQueued,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err := db.CreateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := db.GetTicketByExternalID(ctx, "PROJ-123")
	if err != nil {
		t.Fatalf("GetTicketByExternalID: %v", err)
	}
	if got.Title != "Test ticket" {
		t.Errorf("expected title 'Test ticket', got %q", got.Title)
	}
}

func TestSQLiteDB_GetEvents_EmptyTicketID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create two tickets with one event each.
	for _, id := range []string{"t-10", "t-11"} {
		db.CreateTicket(ctx, &models.Ticket{
			ID: id, ExternalID: id, Title: "t", Description: "d",
			Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
		db.RecordEvent(ctx, &models.EventRecord{
			ID: "e-" + id, TicketID: id, EventType: "ping", Severity: "info",
			Message: "msg", CreatedAt: time.Now(),
		})
	}

	// Empty ticketID must return all events.
	events, err := db.GetEvents(ctx, "", 100)
	if err != nil {
		t.Fatalf("GetEvents empty ticketID: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events for empty ticketID, got %d", len(events))
	}

	// Non-empty ticketID must still filter correctly.
	events, err = db.GetEvents(ctx, "t-10", 100)
	if err != nil {
		t.Fatalf("GetEvents specific ticketID: %v", err)
	}
	if len(events) != 1 || events[0].TicketID != "t-10" {
		t.Errorf("expected 1 event for t-10, got %d", len(events))
	}
}

func TestSQLiteDB_GetMonthlyCost(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.RecordDailyCost(ctx, "2026-03-01", 10.0))
	require.NoError(t, db.RecordDailyCost(ctx, "2026-03-02", 5.0))

	cost, err := db.GetMonthlyCost(ctx, "2026-03")
	require.NoError(t, err)
	assert.InDelta(t, 15.0, cost, 0.01)
}

func TestSQLiteDB_ListTasks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create ticket first (FK constraint)
	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	tasks := []models.Task{
		{ID: "task-1", TicketID: "t-1", Sequence: 1, Title: "Task One", Description: "Do one"},
		{ID: "task-2", TicketID: "t-1", Sequence: 2, Title: "Task Two", Description: "Do two"},
	}
	require.NoError(t, db.CreateTasks(ctx, "t-1", tasks))

	got, err := db.ListTasks(ctx, "t-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "task-1", got[0].ID)
	assert.Equal(t, "Task One", got[0].Title)
	assert.Equal(t, 1, got[0].Sequence)
	assert.Equal(t, "task-2", got[1].ID)
}

func TestSQLiteDB_ListLlmCalls(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create ticket first (FK constraint)
	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	call := &models.LlmCallRecord{
		ID: "llm-1", TicketID: "t-1", TaskID: "", Role: "planner",
		Provider: "anthropic", Model: "claude-3", Attempt: 1,
		TokensInput: 100, TokensOutput: 200, CostUSD: 0.001, DurationMs: 500,
		Status: "success", CreatedAt: time.Now(),
	}
	require.NoError(t, db.RecordLlmCall(ctx, call))

	got, err := db.ListLlmCalls(ctx, "t-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "llm-1", got[0].ID)
	assert.Equal(t, "planner", got[0].Role)
	assert.Equal(t, 100, got[0].TokensInput)
}

func TestSQLiteDB_ParentChildTickets(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := &models.Ticket{
		ID: "parent-1", ExternalID: "EXT-1", Title: "Parent",
		Description: "Parent ticket", Status: models.TicketStatusDecomposed,
		DecomposeDepth: 0, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, db.CreateTicket(ctx, parent))

	child := &models.Ticket{
		ID: "child-1", ExternalID: "EXT-2", Title: "Child",
		Description: "Child ticket", Status: models.TicketStatusQueued,
		ParentTicketID: "EXT-1", DecomposeDepth: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, db.CreateTicket(ctx, child))

	// Verify parent fields persisted
	got, err := db.GetTicket(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, 0, got.DecomposeDepth)

	// Verify child fields persisted
	got, err = db.GetTicket(ctx, "child-1")
	require.NoError(t, err)
	assert.Equal(t, "EXT-1", got.ParentTicketID)
	assert.Equal(t, 1, got.DecomposeDepth)

	// Test GetChildTickets
	children, err := db.GetChildTickets(ctx, "EXT-1")
	require.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "child-1", children[0].ID)
}

func TestSQLiteDB_RecordEvent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create ticket first (FK constraint)
	db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	err := db.RecordEvent(ctx, &models.EventRecord{
		ID:        "e-1",
		TicketID:  "t-1",
		EventType: "test_event",
		Severity:  "info",
		Message:   "test message",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	events, err := db.GetEvents(ctx, "t-1", 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}
