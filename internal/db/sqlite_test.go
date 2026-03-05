package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
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
