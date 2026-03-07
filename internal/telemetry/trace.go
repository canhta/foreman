// internal/telemetry/trace.go
package telemetry

import (
	goctx "context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type traceKeyType struct{}

var traceKey = traceKeyType{}

// TraceContext carries a TraceID and TicketID for end-to-end request tracing (ARCH-O01).
type TraceContext struct {
	TraceID  string
	TicketID string
}

// PipelineContext carries pipeline execution state into skill step executors (REQ-OBS-002).
// It is optional; when nil, skill steps behave as before (backward-compatible).
type PipelineContext struct {
	TraceID  string
	TicketID string
	TaskID   string
	Stage    string
	Attempt  int
}

// NewTraceID generates a random 16-character hex trace ID.
func NewTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a fixed-width zeroed ID only when entropy is unavailable.
		return fmt.Sprintf("%016x", 0)
	}
	return hex.EncodeToString(b)
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
