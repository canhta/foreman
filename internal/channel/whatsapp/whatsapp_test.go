package whatsapp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type captureInboundHandler struct {
	mu       sync.Mutex
	messages []channel.InboundMessage
}

func (h *captureInboundHandler) HandleMessage(_ context.Context, msg channel.InboundMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
	return nil
}

func (h *captureInboundHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.messages)
}

func (h *captureInboundHandler) first() channel.InboundMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.messages[0]
}

func TestExtractBody(t *testing.T) {
	t.Parallel()

	t.Run("conversation takes precedence", func(t *testing.T) {
		t.Parallel()
		msg := &waE2E.Message{
			Conversation: proto.String("primary text"),
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("fallback text"),
			},
		}
		assert.Equal(t, "primary text", extractBody(msg))
	})

	t.Run("falls back to extended text", func(t *testing.T) {
		t.Parallel()
		msg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("extended text"),
			},
		}
		assert.Equal(t, "extended text", extractBody(msg))
	})

	t.Run("returns empty when no text", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", extractBody(&waE2E.Message{}))
	})
}

func TestIsMediaMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  *waE2E.Message
		want bool
	}{
		{
			name: "plain text is not media",
			msg:  &waE2E.Message{Conversation: proto.String("hello")},
			want: false,
		},
		{
			name: "image is media",
			msg:  &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}},
			want: true,
		},
		{
			name: "document is media",
			msg:  &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{}},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isMediaMessage(tt.msg))
		})
	}
}

func TestRateLimiterAllow(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(2, 20*time.Millisecond)

	assert.True(t, rl.Allow("alice@s.whatsapp.net"))
	assert.True(t, rl.Allow("alice@s.whatsapp.net"))
	assert.False(t, rl.Allow("alice@s.whatsapp.net"), "third message in same window should be denied")

	time.Sleep(30 * time.Millisecond)
	assert.True(t, rl.Allow("alice@s.whatsapp.net"), "window should reset after expiry")
}

func TestRateLimiterIsPerSender(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(1, time.Minute)

	assert.True(t, rl.Allow("alice@s.whatsapp.net"))
	assert.False(t, rl.Allow("alice@s.whatsapp.net"))

	// Another sender should still be allowed in the same time window.
	assert.True(t, rl.Allow("bob@s.whatsapp.net"))
}

func TestHandleMessageGuardsAndForwarding(t *testing.T) {
	t.Parallel()

	t.Run("ignores group and from-self", func(t *testing.T) {
		t.Parallel()
		handler := &captureInboundHandler{}
		w := New("", zerolog.Nop())
		w.handler = handler
		w.limiter = newRateLimiter(10, time.Minute)

		sender := types.NewJID("1001", types.DefaultUserServer)
		now := time.Now()

		w.handleMessage(context.Background(), &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Sender: sender, IsGroup: true}, Timestamp: now},
			Message: &waE2E.Message{Conversation: proto.String("group message")},
		})
		w.handleMessage(context.Background(), &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Sender: sender, IsFromMe: true}, Timestamp: now},
			Message: &waE2E.Message{Conversation: proto.String("self message")},
		})

		assert.Equal(t, 0, handler.count())
	})

	t.Run("forwards and truncates long messages", func(t *testing.T) {
		t.Parallel()
		handler := &captureInboundHandler{}
		w := New("", zerolog.Nop())
		w.handler = handler
		w.limiter = newRateLimiter(10, time.Minute)

		sender := types.NewJID("2002", types.DefaultUserServer)
		now := time.Now()
		longText := strings.Repeat("x", 2100)

		w.handleMessage(context.Background(), &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Sender: sender}, Timestamp: now},
			Message: &waE2E.Message{Conversation: proto.String(longText)},
		})

		require.Equal(t, 1, handler.count())
		got := handler.first()
		assert.Equal(t, sender.String(), got.SenderID)
		assert.Equal(t, now, got.Timestamp)
		assert.Len(t, got.Body, 2000)
	})

	t.Run("enforces rate limit per sender", func(t *testing.T) {
		t.Parallel()
		handler := &captureInboundHandler{}
		w := New("", zerolog.Nop())
		w.handler = handler
		w.limiter = newRateLimiter(1, time.Minute)

		sender := types.NewJID("3003", types.DefaultUserServer)
		now := time.Now()

		first := &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Sender: sender}, Timestamp: now},
			Message: &waE2E.Message{Conversation: proto.String("first")},
		}
		second := &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Sender: sender}, Timestamp: now.Add(time.Second)},
			Message: &waE2E.Message{Conversation: proto.String("second")},
		}

		w.handleMessage(context.Background(), first)
		w.handleMessage(context.Background(), second)

		assert.Equal(t, 1, handler.count(), "second message should be dropped by limiter")
	})
}

func TestHandleEventConnectionState(t *testing.T) {
	t.Parallel()

	t.Run("connected event marks channel connected", func(t *testing.T) {
		t.Parallel()
		w := New("", zerolog.Nop())

		w.handleEvent(context.Background(), &events.Connected{})
		assert.True(t, w.IsConnected())
	})

	t.Run("disconnected event marks channel disconnected", func(t *testing.T) {
		t.Parallel()
		w := New("", zerolog.Nop())
		w.connected = true

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		w.handleEvent(ctx, &events.Disconnected{})
		assert.False(t, w.IsConnected())
	})
}
