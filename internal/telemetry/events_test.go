package telemetry

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockDB struct {
	events []*models.EventRecord
}

func (m *mockDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	m.events = append(m.events, e)
	return nil
}

func TestEventEmitter_Emit(t *testing.T) {
	db := &mockDB{}
	emitter := NewEventEmitter(db)

	emitter.Emit(context.Background(), "ticket-1", "task-1", "task_started", map[string]string{
		"task_title": "Add login",
	})

	if len(db.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(db.events))
	}
	if db.events[0].TicketID != "ticket-1" {
		t.Errorf("expected ticket-1, got %s", db.events[0].TicketID)
	}
	if db.events[0].EventType != "task_started" {
		t.Errorf("expected task_started, got %s", db.events[0].EventType)
	}
}

func TestEventEmitter_Subscribe(t *testing.T) {
	db := &mockDB{}
	emitter := NewEventEmitter(db)

	ch := emitter.Subscribe()
	defer emitter.Unsubscribe(ch)

	go emitter.Emit(context.Background(), "t1", "", "ticket_picked_up", nil)

	evt := <-ch
	if evt.EventType != "ticket_picked_up" {
		t.Errorf("expected ticket_picked_up, got %s", evt.EventType)
	}
}
