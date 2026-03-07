package telemetry

import (
	"context"
	"testing"
	"time"

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

	emitter.Emit(context.Background(), "ticket-1", "task-1", "task_started", "info", "Task started", map[string]string{
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

	go emitter.Emit(context.Background(), "t1", "", "ticket_picked_up", "info", "Ticket picked up", nil)

	evt := <-ch
	if evt.EventType != "ticket_picked_up" {
		t.Errorf("expected ticket_picked_up, got %s", evt.EventType)
	}
}

func TestEventEmitter_DropDetection(t *testing.T) {
	db := &mockDB{}
	emitter := NewEventEmitter(db)

	// Create a subscriber with zero-buffer channel to force drops.
	slowCh := make(chan *models.EventRecord)
	emitter.mu.Lock()
	emitter.subscribers[slowCh] = struct{}{}
	emitter.mu.Unlock()

	// Also subscribe a normal fast subscriber to receive the meta-event.
	fastCh := emitter.Subscribe()
	defer emitter.Unsubscribe(fastCh)

	// Emit one event — slow subscriber cannot receive it, so it gets dropped.
	emitter.Emit(context.Background(), "t1", "", "task_done", "info", "done", nil)

	// droppedCount should be incremented.
	if emitter.DroppedCount() != 1 {
		t.Errorf("expected DroppedCount=1, got %d", emitter.DroppedCount())
	}

	// The fast subscriber should receive both the original event and the meta-event.
	var gotOriginal, gotMeta bool
	deadline := time.After(500 * time.Millisecond)
	for !gotOriginal || !gotMeta {
		select {
		case evt, ok := <-fastCh:
			if !ok {
				t.Fatal("fast channel closed unexpectedly")
			}
			if evt.EventType == "task_done" {
				gotOriginal = true
			}
			if evt.EventType == "events_dropped" {
				gotMeta = true
			}
		case <-deadline:
			t.Fatalf("timed out: gotOriginal=%v gotMeta=%v", gotOriginal, gotMeta)
		}
	}

	// Remove slow subscriber without closing (it was added manually).
	emitter.mu.Lock()
	delete(emitter.subscribers, slowCh)
	emitter.mu.Unlock()
}
