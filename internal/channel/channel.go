package channel

import (
	"context"
	"time"
)

// Channel is a bidirectional messaging transport (e.g., WhatsApp).
// It receives inbound messages and sends outbound notifications.
// Completely separate from tracker.IssueTracker.
type Channel interface {
	// Start begins listening for inbound messages.
	// Blocks until ctx is cancelled or a fatal error occurs.
	Start(ctx context.Context, handler InboundHandler) error

	// Stop disconnects the channel transport.
	Stop() error

	// Send sends a text message to a recipient.
	Send(ctx context.Context, recipientID string, message string) error

	// Name returns the channel name (e.g., "whatsapp").
	Name() string
}

// InboundMessage represents a message received from a channel.
type InboundMessage struct {
	Timestamp time.Time
	SenderID  string
	Body      string
}

// InboundHandler processes messages received by a Channel.
type InboundHandler interface {
	HandleMessage(ctx context.Context, msg InboundMessage) error
}

// HealthChecker reports the health of a channel transport.
type HealthChecker interface {
	IsConnected() bool
}

// CommandHandler provides daemon state to the channel router.
// Implemented by the daemon — the channel layer never imports daemon internals.
type CommandHandler interface {
	Status(ctx context.Context) (string, error)
	Pause(ctx context.Context) (string, error)
	Resume(ctx context.Context) (string, error)
	Cost(ctx context.Context) (string, error)
}
