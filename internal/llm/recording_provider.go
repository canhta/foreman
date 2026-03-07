// internal/llm/recording_provider.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
)

// CallDetailsStore is the subset of db.Database needed to record full prompt/response pairs.
type CallDetailsStore interface {
	StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error
}

// RecordingProvider wraps an LlmProvider and persists full prompt/response details
// to the database after every successful call (ARCH-O02 observability).
// DB errors are logged as warnings and never propagate to callers.
type RecordingProvider struct {
	inner LlmProvider
	db    CallDetailsStore
}

// NewRecordingProvider wraps provider with a RecordingProvider that stores call details.
func NewRecordingProvider(provider LlmProvider, db CallDetailsStore) *RecordingProvider {
	return &RecordingProvider{inner: provider, db: db}
}

// ProviderName delegates to the inner provider.
func (r *RecordingProvider) ProviderName() string { return r.inner.ProviderName() }

// HealthCheck delegates to the inner provider.
func (r *RecordingProvider) HealthCheck(ctx context.Context) error {
	return r.inner.HealthCheck(ctx)
}

// Complete calls the inner provider and, on success, stores the full prompt and
// response in the database. DB storage failures are logged at warn level and do
// not affect the returned response.
func (r *RecordingProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	resp, err := r.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Store call details — best-effort; DB errors are not propagated to the caller.
	r.storeDetails(ctx, req, resp)

	return resp, nil
}

func (r *RecordingProvider) storeDetails(ctx context.Context, req models.LlmRequest, resp *models.LlmResponse) {
	callID := fmt.Sprintf("llm-%d", time.Now().UnixNano())

	promptBytes, err := json.Marshal(req)
	if err != nil {
		log.Warn().Err(err).Str("call_id", callID).Msg("recording_provider: failed to marshal request")
		return
	}
	fullPrompt := string(promptBytes)

	// Build stored response — prepend TraceID annotation if a trace is active.
	fullResponse := resp.Content
	if tc := telemetry.TraceFromContext(ctx); tc.TraceID != "" {
		fullResponse = "[trace_id: " + tc.TraceID + "]\n" + fullResponse
	}

	if storeErr := r.db.StoreCallDetails(ctx, callID, fullPrompt, fullResponse); storeErr != nil {
		log.Warn().Err(storeErr).Str("call_id", callID).Msg("recording_provider: failed to store call details")
	}
}
