// internal/telemetry/trace.go
package telemetry

import (
	goctx "context"
	"fmt"
	"math/rand"
)

type traceKeyType struct{}

var traceKey = traceKeyType{}

// TraceContext carries a TraceID and TicketID for end-to-end request tracing (ARCH-O01).
type TraceContext struct {
	TraceID  string
	TicketID string
}

// NewTraceID generates a random 16-character hex trace ID.
func NewTraceID() string {
	return fmt.Sprintf("%016x", rand.Int63()) //nolint:gosec // trace ID doesn't need crypto randomness
}

// WithTrace injects a TraceContext into ctx.
func WithTrace(ctx goctx.Context, tc TraceContext) goctx.Context {
	return goctx.WithValue(ctx, traceKey, tc)
}

// TraceFromContext extracts the TraceContext from ctx.
// Returns zero value if not set.
func TraceFromContext(ctx goctx.Context) TraceContext {
	if tc, ok := ctx.Value(traceKey).(TraceContext); ok {
		return tc
	}
	return TraceContext{}
}

// StartTrace creates a new trace for the given ticketID and injects it into ctx.
func StartTrace(ctx goctx.Context, ticketID string) (goctx.Context, TraceContext) {
	tc := TraceContext{
		TraceID:  NewTraceID(),
		TicketID: ticketID,
	}
	return WithTrace(ctx, tc), tc
}
