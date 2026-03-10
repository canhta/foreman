package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// EventStore is a subset of db.Database for event recording.
type EventStore interface {
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// EventEmitter writes events to the database and fans them out to WebSocket subscribers.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type EventEmitter struct {
	store          EventStore
	subscribers    map[chan *models.EventRecord]struct{}
	mu             sync.RWMutex
	droppedCount   int64              // accessed atomically (ARCH-O03)
	droppedCounter prometheus.Counter // optional Prometheus counter (ARCH-O03)
	seq            int64              // monotonic sequence number for WebSocket gap detection
}

// NewEventEmitter creates a new EventEmitter backed by the given store.
func NewEventEmitter(store EventStore) *EventEmitter {
	return &EventEmitter{
		store:       store,
		subscribers: make(map[chan *models.EventRecord]struct{}),
	}
}

// SetDroppedCounter wires a Prometheus counter that is incremented every time
// an event delivery is dropped due to a slow subscriber (ARCH-O03).
// This must be called before the emitter starts receiving events.
func (e *EventEmitter) SetDroppedCounter(c prometheus.Counter) {
	e.droppedCounter = c
}

// newID generates a random UUID-like identifier using crypto/rand.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// DroppedCount returns the total number of individual event deliveries dropped
// due to slow subscribers since this emitter was created (ARCH-O03).
func (e *EventEmitter) DroppedCount() int64 {
	return atomic.LoadInt64(&e.droppedCount)
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

	evt.Seq = atomic.AddInt64(&e.seq, 1)

	e.mu.RLock()
	defer e.mu.RUnlock()

	var localDrops int64
	for ch := range e.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow — backpressure is caller's responsibility.
			atomic.AddInt64(&e.droppedCount, 1)
			if e.droppedCounter != nil {
				e.droppedCounter.Inc()
			}
			localDrops++
		}
	}

	if localDrops > 0 {
		total := atomic.LoadInt64(&e.droppedCount)
		// Emit a meta-event to all subscribers (best-effort, non-blocking) so the
		// dashboard can detect event loss (ARCH-O03).
		metaEvt := &models.EventRecord{
			ID:        newID(),
			EventType: "events_dropped",
			Severity:  "warning",
			Message:   fmt.Sprintf("event dropped: slow subscriber (total drops: %d)", total),
			Details:   fmt.Sprintf(`{"count":%d}`, total),
			CreatedAt: time.Now(),
		}
		for ch := range e.subscribers {
			select {
			case ch <- metaEvt:
			default:
				// Best-effort — do not recurse.
			}
		}
		// Persist the meta-event to the store so it survives dashboard reconnects (ARCH-O03).
		go func() {
			_ = e.store.RecordEvent(context.Background(), metaEvt)
		}()
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

// GlobalEventEmitter fans in events from multiple per-project emitters
// into a single broadcast channel for global WebSocket subscribers.
type GlobalEventEmitter struct {
	subscribers map[chan *models.EventRecord]struct{}
	mu          sync.RWMutex
}

// NewGlobalEventEmitter creates a new GlobalEventEmitter.
func NewGlobalEventEmitter() *GlobalEventEmitter {
	return &GlobalEventEmitter{
		subscribers: make(map[chan *models.EventRecord]struct{}),
	}
}

// Forward broadcasts an event to all global subscribers.
// Events that cannot be delivered immediately are dropped (backpressure is caller's responsibility).
func (g *GlobalEventEmitter) Forward(event *models.EventRecord) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for ch := range g.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is slow.
		}
	}
}

// Subscribe registers a new global subscriber channel and returns it.
func (g *GlobalEventEmitter) Subscribe() chan *models.EventRecord {
	ch := make(chan *models.EventRecord, 64)
	g.mu.Lock()
	g.subscribers[ch] = struct{}{}
	g.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from the global subscriber set and closes it.
func (g *GlobalEventEmitter) Unsubscribe(ch chan *models.EventRecord) {
	g.mu.Lock()
	delete(g.subscribers, ch)
	g.mu.Unlock()
	close(ch)
}
