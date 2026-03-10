package bus

import "sync"

// Handler handles a typed event for a specific topic.
type Handler func(data any)

// GlobalHandler handles all events across all topics.
type GlobalHandler func(topic string, data any)

type subEntry struct {
	handler Handler
	active  bool
}

type globalEntry struct {
	handler GlobalHandler
	active  bool
}

// Bus is a simple async pub/sub event bus for pipeline observability.
type Bus struct {
	mu     sync.RWMutex
	subs   map[string][]*subEntry
	global []*globalEntry
	wg     sync.WaitGroup
}

// New creates a new Bus.
func New() *Bus {
	return &Bus{subs: make(map[string][]*subEntry)}
}

// Subscribe registers a handler for topic and returns a cancel func that unregisters it.
// Calling cancel is safe to call multiple times and from any goroutine.
func (b *Bus) Subscribe(topic string, h Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := &subEntry{handler: h, active: true}
	b.subs[topic] = append(b.subs[topic], e)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		e.active = false
	}
}

// SubscribeAll registers a handler that receives every published event.
// Returns a cancel func that unregisters the handler.
func (b *Bus) SubscribeAll(h GlobalHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := &globalEntry{handler: h, active: true}
	b.global = append(b.global, e)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		e.active = false
	}
}

// Publish dispatches data to all active subscribers of topic and all active global handlers.
// Each handler runs in its own goroutine; call Drain to wait for completion.
func (b *Bus) Publish(topic string, data any) {
	b.mu.RLock()
	entries := make([]*subEntry, len(b.subs[topic]))
	copy(entries, b.subs[topic])
	globals := make([]*globalEntry, len(b.global))
	copy(globals, b.global)
	b.mu.RUnlock()

	for _, e := range entries {
		e := e
		b.mu.RLock()
		active := e.active
		b.mu.RUnlock()
		if !active {
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			e.handler(data)
		}()
	}
	for _, e := range globals {
		e := e
		b.mu.RLock()
		active := e.active
		b.mu.RUnlock()
		if !active {
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			e.handler(topic, data)
		}()
	}
}

// Drain waits for all in-flight handlers to complete.
func (b *Bus) Drain() {
	b.wg.Wait()
}
