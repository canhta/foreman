package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// WhatsAppChannel implements channel.Channel using whatsmeow.
type WhatsAppChannel struct {
	client    *whatsmeow.Client
	container *sqlstore.Container
	handler   channel.InboundHandler
	logger    zerolog.Logger
	limiter   *rateLimiter
	sessionDB string
	mu        sync.Mutex
	connected bool
}

// New creates a new WhatsAppChannel. Does not connect — call Start() to begin.
func New(sessionDB string, logger zerolog.Logger) *WhatsAppChannel {
	return &WhatsAppChannel{
		sessionDB: sessionDB,
		logger:    logger.With().Str("component", "whatsapp").Logger(),
		limiter:   newRateLimiter(10, time.Minute),
	}
}

func (w *WhatsAppChannel) Name() string { return "whatsapp" }

// IsConnected reports whether the WhatsApp client is currently connected.
func (w *WhatsAppChannel) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}

// Start connects to WhatsApp and begins listening for messages.
// Blocks until ctx is cancelled or a fatal error occurs.
func (w *WhatsAppChannel) Start(ctx context.Context, handler channel.InboundHandler) error {
	w.handler = handler

	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", w.sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp session db: %w", err)
	}
	w.container = container

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp device store: %w", err)
	}

	w.client = whatsmeow.NewClient(deviceStore, waLog.Noop)
	w.client.AddEventHandler(func(evt interface{}) {
		w.handleEvent(ctx, evt)
	})

	if err := w.client.Connect(); err != nil {
		return fmt.Errorf("whatsapp connect: %w", err)
	}

	w.mu.Lock()
	w.connected = true
	w.mu.Unlock()

	w.logger.Info().Msg("WhatsApp connected")

	// Start rate limiter cleanup
	go w.limiter.cleanupLoop(ctx)

	// Block until context cancelled
	<-ctx.Done()
	w.logger.Info().Msg("WhatsApp shutting down")
	return nil
}

// Stop disconnects the WhatsApp client.
func (w *WhatsAppChannel) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.connected {
		w.connected = false
		w.client.Disconnect()
	}
	return nil
}

// Send sends a text message to a WhatsApp JID.
func (w *WhatsAppChannel) Send(ctx context.Context, recipientID string, message string) error {
	jid, err := types.ParseJID(recipientID)
	if err != nil {
		return fmt.Errorf("invalid JID %q: %w", recipientID, err)
	}
	_, err = w.client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

func (w *WhatsAppChannel) handleEvent(ctx context.Context, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		w.handleMessage(ctx, v)
	case *events.LoggedOut:
		w.logger.Warn().Msg("WhatsApp session logged out — manual re-login required")
		go func() { _ = w.Stop() }()
	case *events.Disconnected:
		w.logger.Warn().Msg("WhatsApp disconnected")
		go w.handleDisconnect(ctx)
	case *events.Connected:
		w.logger.Info().Msg("WhatsApp connected")
	}
}

func (w *WhatsAppChannel) handleMessage(ctx context.Context, evt *events.Message) {
	if evt.Info.IsGroup {
		return
	}
	if evt.Info.IsFromMe {
		return
	}
	if isMediaMessage(evt.Message) {
		_ = w.Send(ctx, evt.Info.Sender.String(), "Attachments not supported — please describe the task in text")
		return
	}
	if !w.limiter.Allow(evt.Info.Sender.String()) {
		return
	}
	body := extractBody(evt.Message)
	if len(body) > 2000 {
		body = body[:2000]
	}
	if body == "" {
		return
	}

	_ = w.handler.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  evt.Info.Sender.String(),
		Body:      body,
		Timestamp: evt.Info.Timestamp,
	})
}

func (w *WhatsAppChannel) handleDisconnect(ctx context.Context) {
	backoff := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	for attempt := 0; ; attempt++ {
		delay := backoff[min(attempt, len(backoff)-1)]
		w.logger.Warn().Dur("delay", delay).Int("attempt", attempt+1).Msg("reconnecting")
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("reconnect cancelled — shutting down")
			return
		case <-time.After(delay):
		}
		if err := w.client.Connect(); err == nil {
			w.mu.Lock()
			w.connected = true
			w.mu.Unlock()
			w.logger.Info().Msg("reconnected")
			return
		}
	}
}

func isMediaMessage(msg *waE2E.Message) bool {
	return msg.GetImageMessage() != nil ||
		msg.GetVideoMessage() != nil ||
		msg.GetAudioMessage() != nil ||
		msg.GetDocumentMessage() != nil ||
		msg.GetStickerMessage() != nil
}

func extractBody(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}
	if msg.GetExtendedTextMessage() != nil {
		return msg.GetExtendedTextMessage().GetText()
	}
	return ""
}

// rateLimiter implements a simple per-JID windowed counter.
//
// NOTE (BUG-M11): This is an in-memory implementation. All per-JID counters are
// reset on process restart, which means a JID could exceed its effective rate limit
// across a restart window. For this internal anti-spam use-case (Foreman is a
// single-tenant autonomous dev tool) this is acceptable; the window is short
// (1 minute by default) and the impact of counter reset is minor. If billing or
// safety enforcement is required, replace with a database-backed counter.
type rateLimiter struct {
	buckets map[string]*rateBucket
	mu      sync.Mutex
	window  time.Duration
	max     int
}

type rateBucket struct {
	windowEnd time.Time
	count     int
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*rateBucket),
		max:     max,
		window:  window,
	}
}

func (r *rateLimiter) Allow(jid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	b, ok := r.buckets[jid]
	if !ok || now.After(b.windowEnd) {
		r.buckets[jid] = &rateBucket{count: 1, windowEnd: now.Add(r.window)}
		return true
	}
	if b.count >= r.max {
		return false
	}
	b.count++
	return true
}

func (r *rateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for jid, b := range r.buckets {
				if now.After(b.windowEnd) {
					delete(r.buckets, jid)
				}
			}
			r.mu.Unlock()
		}
	}
}
