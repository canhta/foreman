package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// EventStore is a subset of db.Database for event recording.
type EventStore interface {
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// EventEmitter writes events to the database and fans them out to WebSocket subscribers.
type EventEmitter struct {
	store       EventStore
	subscribers map[chan *models.EventRecord]struct{}
	mu          sync.RWMutex
}

// NewEventEmitter creates a new EventEmitter backed by the given store.
func NewEventEmitter(store EventStore) *EventEmitter {
	return &EventEmitter{
		store:       store,
		subscribers: make(map[chan *models.EventRecord]struct{}),
	}
}

// newID generates a random UUID-like identifier using crypto/rand.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Emit records an event in the store and broadcasts it to all subscribers.
// severity should be one of: "info", "success", "warning", "error".
// metadata is marshalled to JSON and stored in the Details field.
func (e *EventEmitter) Emit(ctx context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string) {
	var details string
	if metadata != nil {
		b, _ := json.Marshal(metadata)
		details = string(b)
	}

	evt := &models.EventRecord{
		ID:        newID(),
		TicketID:  ticketID,
		TaskID:    taskID,
		EventType: eventType,
		Severity:  severity,
		Message:   message,
		Details:   details,
		CreatedAt: time.Now(),
	}

	if err := e.store.RecordEvent(ctx, evt); err != nil {
		log.Error().Err(err).Str("ticket_id", ticketID).Str("event_type", eventType).Msg("failed to record event")
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	for ch := range e.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow — backpressure is caller's responsibility.
		}
	}
}

// Subscribe registers a new subscriber channel and returns it.
// The channel is buffered (64) to tolerate brief slowness in consumers.
func (e *EventEmitter) Subscribe() chan *models.EventRecord {
	ch := make(chan *models.EventRecord, 64)
	e.mu.Lock()
	e.subscribers[ch] = struct{}{}
	e.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel from the subscriber set and closes it.
func (e *EventEmitter) Unsubscribe(ch chan *models.EventRecord) {
	e.mu.Lock()
	delete(e.subscribers, ch)
	e.mu.Unlock()
	close(ch)
}
