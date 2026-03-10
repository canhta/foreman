package bus

import "sync"

// Handler handles a typed event for a specific topic.
type Handler func(data any)

// GlobalHandler handles all events across all topics.
type GlobalHandler func(topic string, data any)

// Bus is a simple async pub/sub event bus for pipeline observability.
type Bus struct {
	mu     sync.RWMutex
	subs   map[string][]Handler
	global []GlobalHandler
	wg     sync.WaitGroup
}

// New creates a new Bus.
func New() *Bus {
	return &Bus{subs: make(map[string][]Handler)}
}

// Subscribe registers a handler for the given topic.
func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], h)
}

// SubscribeAll registers a handler that receives every published event.
func (b *Bus) SubscribeAll(h GlobalHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.global = append(b.global, h)
}

// Publish dispatches data to all subscribers of topic and all global handlers.
// Each handler runs in its own goroutine; call Drain to wait for completion.
func (b *Bus) Publish(topic string, data any) {
	b.mu.RLock()
	handlers := make([]Handler, len(b.subs[topic]))
	copy(handlers, b.subs[topic])
	globals := make([]GlobalHandler, len(b.global))
	copy(globals, b.global)
	b.mu.RUnlock()

	for _, h := range handlers {
		h := h
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			h(data)
		}()
	}
	for _, h := range globals {
		h := h
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			h(topic, data)
		}()
	}
}

// Drain waits for all in-flight handlers to complete.
func (b *Bus) Drain() {
	b.wg.Wait()
}
