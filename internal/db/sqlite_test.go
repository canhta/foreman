package db

import (
	"context"
	"fmt"
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

func TestSQLiteDB_UpdateTicketStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	require.NoError(t, db.UpdateTicketStatus(ctx, "t-1", models.TicketStatusImplementing))

	got, err := db.GetTicket(ctx, "t-1")
	require.NoError(t, err)
	assert.Equal(t, models.TicketStatusImplementing, got.Status)
}

func TestSQLiteDB_ListTickets_StatusFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for i, status := range []models.TicketStatus{
		models.TicketStatusQueued, models.TicketStatusImplementing, models.TicketStatusQueued,
	} {
		require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
			ID: fmt.Sprintf("t-%d", i), ExternalID: fmt.Sprintf("X-%d", i),
			Title: "t", Description: "d", Status: status,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
	}

	queued, err := db.ListTickets(ctx, models.TicketFilter{Status: string(models.TicketStatusQueued)})
	require.NoError(t, err)
	assert.Len(t, queued, 2)

	implementing, err := db.ListTickets(ctx, models.TicketFilter{StatusIn: []models.TicketStatus{models.TicketStatusImplementing}})
	require.NoError(t, err)
	assert.Len(t, implementing, 1)
}

func TestSQLiteDB_SetLastCompletedTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	require.NoError(t, db.SetLastCompletedTask(ctx, "t-1", 3))
	// No error = success; method has no return value to inspect beyond error
}

func TestSQLiteDB_UpdateTaskStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, db.CreateTasks(ctx, "t-1", []models.Task{
		{ID: "task-1", TicketID: "t-1", Sequence: 1, Title: "Do it", Description: "desc"},
	}))

	require.NoError(t, db.UpdateTaskStatus(ctx, "task-1", models.TaskStatusDone))

	tasks, err := db.ListTasks(ctx, "t-1")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, models.TaskStatusDone, tasks[0].Status)
}

func TestSQLiteDB_IncrementTaskLlmCalls(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, db.CreateTasks(ctx, "t-1", []models.Task{
		{ID: "task-1", TicketID: "t-1", Sequence: 1, Title: "Do it", Description: "desc"},
	}))

	count, err := db.IncrementTaskLlmCalls(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	count, err = db.IncrementTaskLlmCalls(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestSQLiteDB_SetAndGetHandoffs(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	h := &models.HandoffRecord{
		ID: "h-1", TicketID: "t-1", FromRole: "planner", ToRole: "implementer",
		Key: "plan", Value: `{"tasks":[]}`, CreatedAt: time.Now(),
	}
	require.NoError(t, db.SetHandoff(ctx, h))

	got, err := db.GetHandoffs(ctx, "t-1", "implementer")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "plan", got[0].Key)
	assert.Equal(t, `{"tasks":[]}`, got[0].Value)
}

func TestSQLiteDB_SaveAndGetProgressPatterns(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	p := &models.ProgressPattern{
		ID: "pp-1", TicketID: "t-1", PatternKey: "test_command",
		PatternValue: "go test ./...", DiscoveredByTask: "task-1", CreatedAt: time.Now(),
	}
	require.NoError(t, db.SaveProgressPattern(ctx, p))

	patterns, err := db.GetProgressPatterns(ctx, "t-1", nil)
	require.NoError(t, err)
	require.Len(t, patterns, 1)
	assert.Equal(t, "test_command", patterns[0].PatternKey)
	assert.Equal(t, "go test ./...", patterns[0].PatternValue)
}

func TestSQLiteDB_ReserveAndReleaseFiles(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	paths := []string{"internal/foo/bar.go", "internal/baz/qux.go"}
	require.NoError(t, db.ReserveFiles(ctx, "t-1", paths))

	reserved, err := db.GetReservedFiles(ctx)
	require.NoError(t, err)
	assert.Equal(t, "t-1", reserved["internal/foo/bar.go"])
	assert.Equal(t, "t-1", reserved["internal/baz/qux.go"])

	require.NoError(t, db.ReleaseFiles(ctx, "t-1"))

	reserved, err = db.GetReservedFiles(ctx)
	require.NoError(t, err)
	assert.Empty(t, reserved)
}

func TestSQLiteDB_GetTicketCost(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "llm-1", TicketID: "t-1", Role: "planner", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, TokensInput: 100, TokensOutput: 200,
		CostUSD: 0.005, DurationMs: 300, Status: "success", CreatedAt: time.Now(),
	}))
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "llm-2", TicketID: "t-1", Role: "reviewer", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, TokensInput: 50, TokensOutput: 100,
		CostUSD: 0.002, DurationMs: 150, Status: "success", CreatedAt: time.Now(),
	}))

	cost, err := db.GetTicketCost(ctx, "t-1")
	require.NoError(t, err)
	assert.InDelta(t, 0.007, cost, 0.0001)
}

func TestSQLiteDB_GetDailyCost(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.RecordDailyCost(ctx, "2026-03-06", 7.50))

	cost, err := db.GetDailyCost(ctx, "2026-03-06")
	require.NoError(t, err)
	assert.InDelta(t, 7.50, cost, 0.01)

	// Non-existent date returns 0, not error
	cost, err = db.GetDailyCost(ctx, "2026-01-01")
	require.NoError(t, err)
	assert.Equal(t, 0.0, cost)
}

func TestSQLiteDB_CreateAndValidateAuthToken(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateAuthToken(ctx, "hashed-token-abc", "CI token"))

	valid, err := db.ValidateAuthToken(ctx, "hashed-token-abc")
	require.NoError(t, err)
	assert.True(t, valid)

	valid, err = db.ValidateAuthToken(ctx, "wrong-hash")
	require.NoError(t, err)
	assert.False(t, valid)
}
