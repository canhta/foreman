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
	tickets       []models.Ticket
	created       []*models.Ticket
	appendedID    string
	appendedText  string
	updatedID     string
	updatedStatus models.TicketStatus
}

func (m *mockRouterDB) CreateTicket(_ context.Context, t *models.Ticket) error {
	m.created = append(m.created, t)
	return nil
}

func (m *mockRouterDB) FindActiveClarification(_ context.Context, senderID string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ChannelSenderID == senderID && t.Status == models.TicketStatusClarificationNeeded {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockRouterDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.updatedID = id
	m.updatedStatus = status
	return nil
}

func (m *mockRouterDB) AppendTicketDescription(_ context.Context, id, text string) error {
	m.appendedID = id
	m.appendedText = text
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

func TestRouter_ClarificationReply(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	db := &mockRouterDB{
		mockPairingDB: *newMockPairingDB(),
		tickets: []models.Ticket{
			{
				ID:              "ticket-123",
				ChannelSenderID: "84111111111@s.whatsapp.net",
				Status:          models.TicketStatusClarificationNeeded,
			},
		},
	}
	router := NewRouter(ch, db, NewClassifier(nil), allowlist, nil, nil, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "The error is on line 42 of main.go",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	// Verify AppendTicketDescription was called with the message body
	if db.appendedID != "ticket-123" {
		t.Errorf("appendedID = %q, want %q", db.appendedID, "ticket-123")
	}
	if db.appendedText != "The error is on line 42 of main.go" {
		t.Errorf("appendedText = %q, want %q", db.appendedText, "The error is on line 42 of main.go")
	}

	// Verify ticket status was updated to queued
	if db.updatedID != "ticket-123" {
		t.Errorf("updatedID = %q, want %q", db.updatedID, "ticket-123")
	}
	if db.updatedStatus != models.TicketStatusQueued {
		t.Errorf("updatedStatus = %q, want %q", db.updatedStatus, models.TicketStatusQueued)
	}

	// Verify confirmation message was sent
	msg := ch.lastSent()
	if msg == nil {
		t.Fatal("expected a confirmation message to be sent")
	}
	if msg.message != "Updated ticket #ticket-123, resuming..." {
		t.Errorf("sent = %q, want %q", msg.message, "Updated ticket #ticket-123, resuming...")
	}
}

func TestRouter_NewTicket(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	db := &mockRouterDB{mockPairingDB: *newMockPairingDB()}
	router := NewRouter(ch, db, NewClassifier(nil), allowlist, nil, nil, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "Add dark mode to the settings page",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	if len(db.created) != 1 {
		t.Fatalf("expected 1 ticket created, got %d", len(db.created))
	}
	ticket := db.created[0]
	if ticket.Title != "Add dark mode to the settings page" {
		t.Errorf("title = %q", ticket.Title)
	}
	if ticket.ChannelSenderID != "84111111111@s.whatsapp.net" {
		t.Errorf("ChannelSenderID = %q", ticket.ChannelSenderID)
	}
	if ticket.Status != models.TicketStatusQueued {
		t.Errorf("status = %q", ticket.Status)
	}

	msg := ch.lastSent()
	if msg == nil {
		t.Fatal("expected a confirmation message")
	}
	if msg.recipientID != "84111111111@s.whatsapp.net" {
		t.Errorf("reply to = %q", msg.recipientID)
	}
}
