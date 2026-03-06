package channel

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

type mockChannel struct {
	mu   sync.Mutex
	sent []sentMessage
}

type sentMessage struct {
	recipientID string
	message     string
}

func (m *mockChannel) Start(ctx context.Context, _ InboundHandler) error {
	<-ctx.Done()
	return nil
}
func (m *mockChannel) Stop() error  { return nil }
func (m *mockChannel) Name() string { return "mock" }
func (m *mockChannel) Send(_ context.Context, recipientID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMessage{recipientID, message})
	return nil
}
func (m *mockChannel) lastSent() *sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	return &m.sent[len(m.sent)-1]
}

type mockRouterDB struct {
	mockPairingDB
	tickets []models.Ticket
}

func (m *mockRouterDB) FindActiveClarification(_ context.Context, senderID string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ChannelSenderID == senderID && t.Status == models.TicketStatusClarificationNeeded {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockRouterDB) UpdateTicketStatus(_ context.Context, _ string, _ models.TicketStatus) error {
	return nil
}

type mockCommands struct {
	statusReply string
}

func (m *mockCommands) Status(_ context.Context) (string, error) { return m.statusReply, nil }
func (m *mockCommands) Pause(_ context.Context) (string, error)  { return "paused", nil }
func (m *mockCommands) Resume(_ context.Context) (string, error) { return "resumed", nil }
func (m *mockCommands) Cost(_ context.Context) (string, error)   { return "$42 today", nil }

func TestRouter_AllowlistReject(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	router := NewRouter(ch, nil, NewClassifier(nil), allowlist, nil, nil, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID: "84999999999@s.whatsapp.net",
		Body:     "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	// Message from non-allowlisted sender without pairing manager should be silently dropped
}

func TestRouter_CommandRouting(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	cmds := &mockCommands{statusReply: "3 tickets active"}
	db := &mockRouterDB{mockPairingDB: *newMockPairingDB()}
	router := NewRouter(ch, db, NewClassifier(nil), allowlist, nil, cmds, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "/status",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	msg := ch.lastSent()
	if msg == nil {
		t.Fatal("expected a sent message")
	}
	if msg.message != "3 tickets active" {
		t.Errorf("sent = %q, want %q", msg.message, "3 tickets active")
	}
}
