package bus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	b := New()
	var called atomic.Int32

	b.Subscribe("test.topic", func(data any) {
		if data.(string) != "hello" {
			t.Errorf("unexpected data: %v", data)
		}
		called.Add(1)
	})

	b.Publish("test.topic", "hello")
	b.Drain()

	if called.Load() != 1 {
		t.Errorf("expected handler called 1 time, got %d", called.Load())
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	var count atomic.Int32

	for range 3 {
		b.Subscribe("topic", func(data any) { count.Add(1) })
	}

	b.Publish("topic", nil)
	b.Drain()

	if count.Load() != 3 {
		t.Errorf("expected 3 handler calls, got %d", count.Load())
	}
}

func TestSubscribeAll(t *testing.T) {
	b := New()
	var mu sync.Mutex
	seen := map[string]int{}

	b.SubscribeAll(func(topic string, data any) {
		mu.Lock()
		seen[topic]++
		mu.Unlock()
	})

	b.Publish("a", 1)
	b.Publish("b", 2)
	b.Publish("a", 3)
	b.Drain()

	mu.Lock()
	defer mu.Unlock()
	if seen["a"] != 2 || seen["b"] != 1 {
		t.Errorf("unexpected counts: %v", seen)
	}
}

func TestNoHandlerForTopic(t *testing.T) {
	b := New()
	// Should not panic when no subscriber exists.
	b.Publish("unknown", "data")
	b.Drain()
}

func TestDrainWaitsForAllHandlers(t *testing.T) {
	b := New()
	var done atomic.Int32

	b.Subscribe("slow", func(data any) {
		time.Sleep(20 * time.Millisecond)
		done.Add(1)
	})

	b.Publish("slow", nil)
	b.Drain()

	if done.Load() != 1 {
		t.Error("Drain returned before handler finished")
	}
}

func TestPublishIsAsync(t *testing.T) {
	b := New()
	started := make(chan struct{})

	b.Subscribe("async", func(data any) {
		close(started)
		time.Sleep(50 * time.Millisecond)
	})

	b.Publish("async", nil)

	select {
	case <-started:
		// Handler started before Drain — confirming async dispatch.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler did not start within timeout")
	}

	b.Drain()
}

func TestGlobalAndTopicHandlersBothFire(t *testing.T) {
	b := New()
	var count atomic.Int32

	b.Subscribe("evt", func(data any) { count.Add(1) })
	b.SubscribeAll(func(topic string, data any) { count.Add(10) })

	b.Publish("evt", nil)
	b.Drain()

	if count.Load() != 11 {
		t.Errorf("expected count 11 (1+10), got %d", count.Load())
	}
}

func TestSubscribeCancel(t *testing.T) {
	b := New()
	var called atomic.Int32

	cancel := b.Subscribe("topic", func(data any) {
		called.Add(1)
	})

	// Cancel before publishing — handler must NOT be called.
	cancel()

	b.Publish("topic", "data")
	b.Drain()

	if called.Load() != 0 {
		t.Errorf("expected handler not to be called after cancel, got %d calls", called.Load())
	}
}

func TestSubscribeAllCancel(t *testing.T) {
	b := New()
	var called atomic.Int32

	cancel := b.SubscribeAll(func(topic string, data any) {
		called.Add(1)
	})

	// Cancel before publishing — handler must NOT be called.
	cancel()

	b.Publish("topic", "data")
	b.Drain()

	if called.Load() != 0 {
		t.Errorf("expected global handler not to be called after cancel, got %d calls", called.Load())
	}
}
