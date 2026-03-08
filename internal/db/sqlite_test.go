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

func TestSQLiteDB_RecordLlmCall_CacheTokens(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-cache", ExternalID: "X-cache", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	call := &models.LlmCallRecord{
		ID: "llm-cache", TicketID: "t-cache", Role: "implementer",
		Provider: "anthropic", Model: "claude-sonnet-4-6", Attempt: 1,
		TokensInput: 500, TokensOutput: 100, CostUSD: 0.005, DurationMs: 300,
		Status:              "success",
		CacheReadTokens:     800,
		CacheCreationTokens: 200,
		CreatedAt:           time.Now(),
	}
	require.NoError(t, db.RecordLlmCall(ctx, call))

	got, err := db.ListLlmCalls(ctx, "t-cache")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 800, got[0].CacheReadTokens)
	assert.Equal(t, 200, got[0].CacheCreationTokens)
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

func TestSQLiteDB_UpdateTicketStatusIfEquals(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-cond", ExternalID: "X-cond", Title: "t", Description: "d",
		Status: models.TicketStatusDecomposed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	t.Run("updates when status matches", func(t *testing.T) {
		updated, err := db.UpdateTicketStatusIfEquals(ctx, "t-cond", models.TicketStatusDone, models.TicketStatusDecomposed)
		require.NoError(t, err)
		assert.True(t, updated)

		got, err := db.GetTicket(ctx, "t-cond")
		require.NoError(t, err)
		assert.Equal(t, models.TicketStatusDone, got.Status)
	})

	t.Run("returns false when status does not match", func(t *testing.T) {
		// Status is now Done, so requiring Decomposed should fail
		updated, err := db.UpdateTicketStatusIfEquals(ctx, "t-cond", models.TicketStatusDone, models.TicketStatusDecomposed)
		require.NoError(t, err)
		assert.False(t, updated)
	})

	t.Run("returns false when ticket does not exist", func(t *testing.T) {
		updated, err := db.UpdateTicketStatusIfEquals(ctx, "nonexistent", models.TicketStatusDone, models.TicketStatusDecomposed)
		require.NoError(t, err)
		assert.False(t, updated)
	})
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

// TestHandoffVersioning verifies SetHandoff creates Version=0, Supersedes="",
// UpdateHandoff increments Version, returns error for missing IDs,
// and GetHandoffs reflects the updated value.
func TestHandoffVersioning_SetCreatesVersionZero(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	h := &models.HandoffRecord{
		ID: "h-ver-1", TicketID: "t-1", FromRole: "planner", ToRole: "implementer",
		Key: "plan", Value: "v0", CreatedAt: time.Now(),
	}
	require.NoError(t, db.SetHandoff(ctx, h))

	got, err := db.GetHandoffs(ctx, "t-1", "implementer")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].Version)
	assert.Equal(t, "", got[0].Supersedes)
	assert.Equal(t, "v0", got[0].Value)
}

func TestHandoffVersioning_UpdateHandoffIncrementsVersion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	h := &models.HandoffRecord{
		ID: "h-upd-1", TicketID: "t-1", FromRole: "planner", ToRole: "implementer",
		Key: "plan", Value: "original", CreatedAt: time.Now(),
	}
	require.NoError(t, db.SetHandoff(ctx, h))

	require.NoError(t, db.UpdateHandoff(ctx, "h-upd-1", "updated", ""))

	got, err := db.GetHandoffs(ctx, "t-1", "implementer")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "updated", got[0].Value)
	assert.Equal(t, 1, got[0].Version)
}

func TestHandoffVersioning_UpdateHandoffNonExistentReturnsError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := db.UpdateHandoff(ctx, "does-not-exist", "value", "")
	require.Error(t, err)
}

func TestHandoffVersioning_SupersedesIsStoredOnUpdate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	h := &models.HandoffRecord{
		ID: "h-sup-1", TicketID: "t-1", FromRole: "planner", ToRole: "implementer",
		Key: "plan", Value: "v1", CreatedAt: time.Now(),
	}
	require.NoError(t, db.SetHandoff(ctx, h))

	require.NoError(t, db.UpdateHandoff(ctx, "h-sup-1", "v2", "h-sup-1"))

	got, err := db.GetHandoffs(ctx, "t-1", "implementer")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "v2", got[0].Value)
	assert.Equal(t, "h-sup-1", got[0].Supersedes)
}

func TestSQLiteDB_SaveAndGetProgressPatterns(t *testing.T) {
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

	// No llm_calls yet — cost should be 0.
	cost, err := db.GetTicketCost(ctx, "t-1")
	require.NoError(t, err)
	assert.Equal(t, 0.0, cost)

	// Record two llm_calls with known costs.
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "lc-1", TicketID: "t-1", Role: "planner", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, CostUSD: 0.05, Status: "success", CreatedAt: time.Now(),
	}))
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "lc-2", TicketID: "t-1", Role: "implementer", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, CostUSD: 0.10, Status: "success", CreatedAt: time.Now(),
	}))

	cost, err = db.GetTicketCost(ctx, "t-1")
	require.NoError(t, err)
	assert.InDelta(t, 0.15, cost, 0.001, "cost should be sum of llm_calls")
}

func TestSQLiteDB_GetTicketCostByStage(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-stage", ExternalID: "X-stage", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	t.Run("empty returns empty map", func(t *testing.T) {
		result, err := db.GetTicketCostByStage(ctx, "t-stage")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	// Insert calls in two stages.
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "cs-1", TicketID: "t-stage", Role: "planner", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, CostUSD: 0.10, Stage: "planning", Status: "success", CreatedAt: time.Now(),
	}))
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "cs-2", TicketID: "t-stage", Role: "implementer", Provider: "anthropic",
		Model: "claude-3", Attempt: 1, CostUSD: 0.20, Stage: "implement", Status: "success", CreatedAt: time.Now(),
	}))
	require.NoError(t, db.RecordLlmCall(ctx, &models.LlmCallRecord{
		ID: "cs-3", TicketID: "t-stage", Role: "implementer", Provider: "anthropic",
		Model: "claude-3", Attempt: 2, CostUSD: 0.30, Stage: "implement", Status: "success", CreatedAt: time.Now(),
	}))

	t.Run("groups costs by stage", func(t *testing.T) {
		result, err := db.GetTicketCostByStage(ctx, "t-stage")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.InDelta(t, 0.10, result["planning"], 0.001)
		assert.InDelta(t, 0.50, result["implement"], 0.001)
	})

	t.Run("unknown ticket returns empty map", func(t *testing.T) {
		result, err := db.GetTicketCostByStage(ctx, "nonexistent")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})
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

// ---------------------------------------------------------------------------
// BUG-C07: TryReserveFiles atomicity tests
// ---------------------------------------------------------------------------

// TestSQLiteDB_TryReserveFiles_NoConflict verifies that files with no existing
// reservation are reserved and no conflicts are returned.
func TestSQLiteDB_TryReserveFiles_NoConflict(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	conflicts, err := db.TryReserveFiles(ctx, "t-1", []string{"a/b.go", "c/d.go"})
	require.NoError(t, err)
	assert.Empty(t, conflicts, "no conflicts expected for fresh reservation")

	// Files should now be reserved for t-1.
	reserved, err := db.GetReservedFiles(ctx)
	require.NoError(t, err)
	assert.Equal(t, "t-1", reserved["a/b.go"])
	assert.Equal(t, "t-1", reserved["c/d.go"])
}

// TestSQLiteDB_TryReserveFiles_Conflict verifies that attempting to reserve a
// file already held by another ticket returns a non-empty conflict list and
// does NOT overwrite the existing reservation.
func TestSQLiteDB_TryReserveFiles_Conflict(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"t-1", "t-2"} {
		require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
			ID: id, ExternalID: id, Title: "t", Description: "d",
			Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
	}

	// t-1 reserves "shared.go".
	conflicts, err := db.TryReserveFiles(ctx, "t-1", []string{"shared.go"})
	require.NoError(t, err)
	require.Empty(t, conflicts)

	// t-2 tries to reserve "shared.go" — should get a conflict.
	conflicts, err = db.TryReserveFiles(ctx, "t-2", []string{"shared.go", "other.go"})
	require.NoError(t, err)
	require.Len(t, conflicts, 1, "expected exactly one conflict")
	assert.Contains(t, conflicts[0], "shared.go")
	assert.Contains(t, conflicts[0], "t-1")

	// "other.go" must NOT have been reserved (the whole operation aborts on conflict).
	reserved, err := db.GetReservedFiles(ctx)
	require.NoError(t, err)
	assert.Equal(t, "t-1", reserved["shared.go"], "shared.go still held by t-1")
	_, otherReserved := reserved["other.go"]
	assert.False(t, otherReserved, "other.go must not be reserved after a conflicting attempt")
}

// TestSQLiteDB_TryReserveFiles_Idempotent verifies that a ticket re-reserving
// its own files (e.g. after a crash) is treated as a no-op without conflict.
func TestSQLiteDB_TryReserveFiles_Idempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	// First reservation.
	conflicts, err := db.TryReserveFiles(ctx, "t-1", []string{"foo.go"})
	require.NoError(t, err)
	require.Empty(t, conflicts)

	// Same ticket re-reserves the same file — must be conflict-free.
	conflicts, err = db.TryReserveFiles(ctx, "t-1", []string{"foo.go"})
	require.NoError(t, err)
	assert.Empty(t, conflicts, "same ticket re-reserving its own file should have no conflict")
}

// TestSQLiteDB_TryReserveFiles_ConcurrentAtomicity verifies that when two
// goroutines race to reserve the same file, exactly one wins and the other
// sees a conflict — no double-booking occurs (BUG-C07).
func TestSQLiteDB_TryReserveFiles_ConcurrentAtomicity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"t-A", "t-B"} {
		require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
			ID: id, ExternalID: id, Title: "t", Description: "d",
			Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
	}

	const sharedFile = "internal/core/main.go"

	type result struct {
		conflicts []string
		err       error
	}
	chA := make(chan result, 1)
	chB := make(chan result, 1)

	// Launch both goroutines simultaneously.
	start := make(chan struct{})
	go func() {
		<-start
		c, e := db.TryReserveFiles(ctx, "t-A", []string{sharedFile})
		chA <- result{c, e}
	}()
	go func() {
		<-start
		c, e := db.TryReserveFiles(ctx, "t-B", []string{sharedFile})
		chB <- result{c, e}
	}()
	close(start)

	resA := <-chA
	resB := <-chB

	require.NoError(t, resA.err)
	require.NoError(t, resB.err)

	// Exactly one should win (no conflict) and one should lose (conflict).
	aWon := len(resA.conflicts) == 0
	bWon := len(resB.conflicts) == 0
	assert.True(t, aWon != bWon, "exactly one goroutine must win the reservation race, got aWon=%v bWon=%v", aWon, bWon)

	// Verify only one ticket owns the file.
	reserved, err := db.GetReservedFiles(ctx)
	require.NoError(t, err)
	owner, ok := reserved[sharedFile]
	require.True(t, ok, "file must be reserved by the winner")
	assert.True(t, owner == "t-A" || owner == "t-B")
}

// TestSQLiteDB_IncrementTaskLlmCalls_Concurrent verifies BUG-C08: concurrent
// increments are serialized by the transaction and the final count is correct.
func TestSQLiteDB_IncrementTaskLlmCalls_Concurrent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, db.CreateTasks(ctx, "t-1", []models.Task{
		{ID: "task-c", TicketID: "t-1", Sequence: 1, Title: "Concurrent task", Description: "desc"},
	}))

	const workers = 5
	counts := make(chan int, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	for range workers {
		go func() {
			<-start
			count, err := db.IncrementTaskLlmCalls(ctx, "task-c")
			counts <- count
			errs <- err
		}()
	}
	close(start)
	for range workers {
		require.NoError(t, <-errs)
	}
	close(counts)

	// Collect returned counts — they should be a permutation of {1, 2, 3, 4, 5},
	// meaning no two goroutines got the same count (no lost updates).
	seen := make(map[int]bool)
	for c := range counts {
		assert.False(t, seen[c], "duplicate count %d returned — concurrent increment race detected", c)
		seen[c] = true
	}
	assert.Len(t, seen, workers, "expected %d distinct count values", workers)
}

// ---------------------------------------------------------------------------
// BUG-M15: Distributed locking tests
// ---------------------------------------------------------------------------

// TestAcquireLock_PreventsConcurrentPickup verifies that when two goroutines
// race to acquire the same lock, exactly one succeeds and the other fails.
func TestAcquireLock_PreventsConcurrentPickup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const lockName = "ticket:concurrent-test"

	type result struct {
		acquired bool
		err      error
	}
	chA := make(chan result, 1)
	chB := make(chan result, 1)

	start := make(chan struct{})
	go func() {
		<-start
		acquired, err := db.AcquireLock(ctx, lockName, 300)
		chA <- result{acquired, err}
	}()
	go func() {
		<-start
		acquired, err := db.AcquireLock(ctx, lockName, 300)
		chB <- result{acquired, err}
	}()
	close(start)

	resA := <-chA
	resB := <-chB

	require.NoError(t, resA.err)
	require.NoError(t, resB.err)

	// Exactly one must have acquired the lock, the other must not.
	assert.True(t, resA.acquired != resB.acquired,
		"exactly one goroutine must win the lock race, got A=%v B=%v", resA.acquired, resB.acquired)
}

// TestReleaseLock_AllowsReacquisition verifies that after releasing a lock,
// another caller can acquire it.
func TestReleaseLock_AllowsReacquisition(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const lockName = "ticket:reacquire-test"

	acquired, err := db.AcquireLock(ctx, lockName, 300)
	require.NoError(t, err)
	require.True(t, acquired, "first acquire must succeed")

	// Release the lock.
	require.NoError(t, db.ReleaseLock(ctx, lockName))

	// Now a second acquire should succeed.
	acquired, err = db.AcquireLock(ctx, lockName, 300)
	require.NoError(t, err)
	assert.True(t, acquired, "re-acquire after release must succeed")
}

// TestExpiredLock_CanBeOverridden verifies that an expired lock (TTL=0) can
// be acquired by a new caller.
func TestExpiredLock_CanBeOverridden(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const lockName = "ticket:expired-test"

	// Cast to *SQLiteDB to directly manipulate the underlying DB for setup.
	sdb := db.(*SQLiteDB)

	// Insert a lock that is already expired (expires_at in the past).
	_, err := sdb.db.ExecContext(ctx,
		`INSERT INTO distributed_locks (lock_name, acquired_at, expires_at, holder_id)
		 VALUES (?, datetime('now', '-10 seconds'), datetime('now', '-5 seconds'), 'old-holder:99')`,
		lockName)
	require.NoError(t, err, "setup: insert expired lock")

	// AcquireLock should clean up the expired lock and succeed.
	acquired, err := db.AcquireLock(ctx, lockName, 300)
	require.NoError(t, err)
	assert.True(t, acquired, "acquiring an expired lock must succeed")
}

// TestReleaseLock_OnlyReleaseOwnLock verifies that a holder cannot release
// a lock it does not own.
func TestReleaseLock_OnlyReleaseOwnLock(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const lockName = "ticket:ownership-test"

	// Cast to *SQLiteDB to insert a lock owned by someone else.
	sdb := db.(*SQLiteDB)
	_, err := sdb.db.ExecContext(ctx,
		`INSERT INTO distributed_locks (lock_name, acquired_at, expires_at, holder_id)
		 VALUES (?, datetime('now'), datetime('now', '+300 seconds'), 'other-host:9999')`,
		lockName)
	require.NoError(t, err, "setup: insert lock by another holder")

	// ReleaseLock should be a no-op (no error, but lock remains).
	require.NoError(t, db.ReleaseLock(ctx, lockName))

	// Lock must still exist.
	var count int
	require.NoError(t, sdb.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM distributed_locks WHERE lock_name = ?`, lockName).Scan(&count))
	assert.Equal(t, 1, count, "lock held by another must not be released")
}

// ---------------------------------------------------------------------------
// REQ-INFRA-002: Embedding Index Store tests
// ---------------------------------------------------------------------------

func TestEmbeddingStore_RoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	rec := EmbeddingRecord{
		RepoPath:  "/repo/myproject",
		HeadSHA:   "abc123def456",
		FilePath:  "internal/foo/bar.go",
		StartLine: 10,
		EndLine:   20,
		ChunkText: "func Foo() {}",
		Vector:    []float32{0.1, 0.2, 0.3, 0.4},
	}

	require.NoError(t, db.UpsertEmbedding(ctx, rec))

	results, err := db.GetEmbeddingsByRepoSHA(ctx, rec.RepoPath, rec.HeadSHA)
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	assert.Equal(t, rec.RepoPath, got.RepoPath)
	assert.Equal(t, rec.HeadSHA, got.HeadSHA)
	assert.Equal(t, rec.FilePath, got.FilePath)
	assert.Equal(t, rec.StartLine, got.StartLine)
	assert.Equal(t, rec.EndLine, got.EndLine)
	assert.Equal(t, rec.ChunkText, got.ChunkText)
	require.Len(t, got.Vector, len(rec.Vector))
	for i := range rec.Vector {
		assert.InDelta(t, rec.Vector[i], got.Vector[i], 1e-6, "vector[%d] mismatch", i)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	assert.InDelta(t, float32(0), CosineSimilarity(a, b), 1e-6)
}

func TestCosineSimilarity_Identical(t *testing.T) {
	v := []float32{1, 2, 3}
	assert.InDelta(t, float32(1.0), CosineSimilarity(v, v), 1e-6)
}

func TestEmbeddingStore_UpsertIdempotency(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	base := EmbeddingRecord{
		RepoPath:  "/repo/myproject",
		HeadSHA:   "abc123",
		FilePath:  "main.go",
		StartLine: 1,
		EndLine:   10,
		ChunkText: "func main() {}",
		Vector:    []float32{1, 2, 3},
	}
	require.NoError(t, db.UpsertEmbedding(ctx, base))

	// Upsert with the same natural key but a different vector.
	updated := base
	updated.Vector = []float32{4, 5, 6}
	require.NoError(t, db.UpsertEmbedding(ctx, updated))

	results, err := db.GetEmbeddingsByRepoSHA(ctx, base.RepoPath, base.HeadSHA)
	require.NoError(t, err)
	require.Len(t, results, 1, "upsert must not create a duplicate row")
	require.Len(t, results[0].Vector, 3)
	assert.InDelta(t, float32(4), results[0].Vector[0], 1e-6, "vector must reflect the updated values")
	assert.InDelta(t, float32(5), results[0].Vector[1], 1e-6)
	assert.InDelta(t, float32(6), results[0].Vector[2], 1e-6)
}

func TestEmbeddingStore_DeleteByRepoSHA(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	repoPath := "/repo/myproject"
	headSHA := "deadbeef1234"

	for i, fp := range []string{"a.go", "b.go"} {
		require.NoError(t, db.UpsertEmbedding(ctx, EmbeddingRecord{
			RepoPath:  repoPath,
			HeadSHA:   headSHA,
			FilePath:  fp,
			StartLine: i * 10,
			EndLine:   i*10 + 5,
			ChunkText: "chunk " + fp,
			Vector:    []float32{float32(i), float32(i + 1)},
		}))
	}

	// Verify 2 rows inserted.
	rows, err := db.GetEmbeddingsByRepoSHA(ctx, repoPath, headSHA)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Delete and verify count=0.
	require.NoError(t, db.DeleteEmbeddingsByRepoSHA(ctx, repoPath, headSHA))

	rows, err = db.GetEmbeddingsByRepoSHA(ctx, repoPath, headSHA)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// TestDB_ContextFeedback_WriteAndQuery verifies writing and querying context_feedback rows
// with Jaccard similarity filtering.
func TestDB_ContextFeedback_WriteAndQuery(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert a row: files_selected=["a.go","b.go","c.go"], files_touched=["a.go","b.go","d.go"]
	err := db.WriteContextFeedback(ctx, ContextFeedbackRow{
		TicketID:      "ticket-1",
		TaskID:        "task-1",
		FilesSelected: []string{"a.go", "b.go", "c.go"},
		FilesTouched:  []string{"a.go", "b.go", "d.go"},
	})
	require.NoError(t, err, "WriteContextFeedback must succeed")

	// Insert another row with very different files (low Jaccard with query set)
	err = db.WriteContextFeedback(ctx, ContextFeedbackRow{
		TicketID:      "ticket-2",
		TaskID:        "task-2",
		FilesSelected: []string{"x.go", "y.go", "z.go"},
		FilesTouched:  []string{"x.go", "y.go"},
	})
	require.NoError(t, err)

	// Query with candidates=["a.go","b.go","e.go"] — Jaccard vs ["a.go","b.go","c.go"] = |{a,b}|/|{a,b,c,e}| = 2/4 = 0.5 >= 0.3
	rows, err := db.QueryContextFeedback(ctx, []string{"a.go", "b.go", "e.go"}, 0.3)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the first row should match Jaccard >= 0.3")
	assert.Equal(t, "task-1", rows[0].TaskID)
	assert.ElementsMatch(t, []string{"a.go", "b.go", "d.go"}, rows[0].FilesTouched)

	// Query with candidates=["a.go"] — Jaccard vs ["a.go","b.go","c.go"] = 1/3 = 0.33 >= 0.3
	rows2, err := db.QueryContextFeedback(ctx, []string{"a.go"}, 0.3)
	require.NoError(t, err)
	require.Len(t, rows2, 1)
	assert.Equal(t, "task-1", rows2[0].TaskID)

	// Query with candidates=["z.go"] — Jaccard vs ["a.go","b.go","c.go"] = 0/4=0, vs ["x.go","y.go","z.go"]=1/3=0.33 >= 0.3
	rows3, err := db.QueryContextFeedback(ctx, []string{"z.go"}, 0.3)
	require.NoError(t, err)
	require.Len(t, rows3, 1)
	assert.Equal(t, "task-2", rows3[0].TaskID)
}

// --- DAGState tests (ARCH-F03) ---

func TestDAGState_SaveAndLoad(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	state := DAGState{
		TicketID:       "ticket-dag-1",
		CompletedTasks: []string{"task-A", "task-B"},
	}
	require.NoError(t, db.SaveDAGState(ctx, state.TicketID, state))

	got, err := db.GetDAGState(ctx, state.TicketID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, state.TicketID, got.TicketID)
	assert.Equal(t, state.CompletedTasks, got.CompletedTasks)
}

func TestDAGState_UpdateExistingState(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	initial := DAGState{
		TicketID:       "ticket-dag-2",
		CompletedTasks: []string{"task-A"},
	}
	require.NoError(t, db.SaveDAGState(ctx, initial.TicketID, initial))

	updated := DAGState{
		TicketID:       "ticket-dag-2",
		CompletedTasks: []string{"task-A", "task-B", "task-C"},
	}
	require.NoError(t, db.SaveDAGState(ctx, updated.TicketID, updated))

	got, err := db.GetDAGState(ctx, "ticket-dag-2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"task-A", "task-B", "task-C"}, got.CompletedTasks)
}

func TestDAGState_GetMissing_ReturnsNil(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	got, err := db.GetDAGState(ctx, "nonexistent-ticket")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDAGState_DeleteRemovesRow(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	state := DAGState{
		TicketID:       "ticket-dag-del",
		CompletedTasks: []string{"task-A"},
	}
	require.NoError(t, db.SaveDAGState(ctx, state.TicketID, state))

	// Verify it exists.
	got, err := db.GetDAGState(ctx, state.TicketID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Delete it.
	require.NoError(t, db.DeleteDAGState(ctx, state.TicketID))

	// Must return nil after deletion.
	got, err = db.GetDAGState(ctx, state.TicketID)
	require.NoError(t, err)
	assert.Nil(t, got, "DAG state must be nil after deletion")
}

func TestDAGState_DeleteNonexistent_IsNoop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Deleting a row that does not exist must not return an error.
	require.NoError(t, db.DeleteDAGState(ctx, "nonexistent-ticket"))
}

func TestSQLiteDB_AppendTicketDescription(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "t-append", ExternalID: "X-append", Title: "t", Description: "original description",
		Status: models.TicketStatusClarificationNeeded, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	require.NoError(t, db.AppendTicketDescription(ctx, "t-append", "clarification reply here"))

	got, err := db.GetTicket(ctx, "t-append")
	require.NoError(t, err)
	assert.Contains(t, got.Description, "original description")
	assert.Contains(t, got.Description, "clarification reply here")
}

// TestSQLiteDB_StoreCallDetails_NoFKRequired verifies that StoreCallDetails succeeds
// with a call ID that has no corresponding row in llm_calls. The table must be a
// standalone log table without a FK constraint.
func TestSQLiteDB_StoreCallDetails_NoFKRequired(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	callID := "llm-1234567890"
	fullPrompt := `{"model":"gpt-4","user_prompt":"hello"}`
	fullResponse := "world"

	// Must succeed even though "llm-1234567890" does not exist in llm_calls.
	err := db.StoreCallDetails(ctx, callID, fullPrompt, fullResponse)
	require.NoError(t, err, "StoreCallDetails must not require a matching llm_calls row")

	// Verify the data was actually persisted.
	gotPrompt, gotResponse, err := db.GetCallDetails(ctx, callID)
	require.NoError(t, err)
	assert.Equal(t, fullPrompt, gotPrompt)
	assert.Equal(t, fullResponse, gotResponse)
}
