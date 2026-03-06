package channel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/rs/zerolog"
)

type mockIntegrationChannel struct {
	mu   sync.Mutex
	sent []struct{ To, Body string }
}

func (m *mockIntegrationChannel) Start(ctx context.Context, _ channel.InboundHandler) error {
	<-ctx.Done()
	return nil
}
func (m *mockIntegrationChannel) Stop() error  { return nil }
func (m *mockIntegrationChannel) Name() string { return "mock" }
func (m *mockIntegrationChannel) Send(_ context.Context, to, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, struct{ To, Body string }{to, body})
	return nil
}

type staticCommands struct{ status string }

func (s *staticCommands) Status(_ context.Context) (string, error) { return s.status, nil }
func (s *staticCommands) Pause(_ context.Context) (string, error)  { return "paused", nil }
func (s *staticCommands) Resume(_ context.Context) (string, error) { return "resumed", nil }
func (s *staticCommands) Cost(_ context.Context) (string, error)   { return "$0", nil }

func TestRouter_FullFlow(t *testing.T) {
	ch := &mockIntegrationChannel{}
	allowlist := channel.NewAllowlist([]string{"+84111111111"})
	classifier := channel.NewClassifier(nil)

	cmds := &staticCommands{status: "2 tickets active"}
	router := channel.NewRouter(ch, nil, classifier, allowlist, nil, cmds, zerolog.Nop())

	ctx := context.Background()

	// Test 1: Command routing
	err := router.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "/status",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage /status: %v", err)
	}

	ch.mu.Lock()
	if len(ch.sent) != 1 || ch.sent[0].Body != "2 tickets active" {
		t.Errorf("expected status reply, got %v", ch.sent)
	}
	ch.sent = nil
	ch.mu.Unlock()

	// Test 2: All command types
	for _, cmd := range []string{"/pause", "/resume", "/cost"} {
		err = router.HandleMessage(ctx, channel.InboundMessage{
			SenderID:  "84111111111@s.whatsapp.net",
			Body:      cmd,
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("HandleMessage %s: %v", cmd, err)
		}
	}

	ch.mu.Lock()
	if len(ch.sent) != 3 {
		t.Errorf("expected 3 command replies, got %d", len(ch.sent))
	}
	ch.sent = nil
	ch.mu.Unlock()

	// Test 3: Unknown sender rejected silently (no pairing manager)
	err = router.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  "84999999999@s.whatsapp.net",
		Body:      "hello",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage unknown: %v", err)
	}

	ch.mu.Lock()
	if len(ch.sent) != 0 {
		t.Errorf("expected no reply for unknown sender, got %v", ch.sent)
	}
	ch.mu.Unlock()

	// Test 4: New ticket from allowed sender (no DB, just logs)
	err = router.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "Build a login page with OAuth support",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage new ticket: %v", err)
	}
}
