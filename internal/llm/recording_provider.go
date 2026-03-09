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

// LlmCallRecorder is the subset of db.Database needed to record structured LLM call
// records with per-stage cost attribution (ARCH-O04). Implementations that also
// satisfy CallDetailsStore can be passed directly to NewRecordingProvider.
type LlmCallRecorder interface {
	RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error
}

// RecordingProvider wraps an LlmProvider and persists full prompt/response details
// to the database after every successful call (ARCH-O02 observability).
// If the db also implements LlmCallRecorder, a structured LlmCallRecord is written
// with per-stage cost data so GetTicketCostByStage returns meaningful results (ARCH-O04).
// DB errors are logged as warnings and never propagate to callers.
type RecordingProvider struct {
	inner       LlmProvider
	db          CallDetailsStore
	recorder    LlmCallRecorder // optional; set when db also implements LlmCallRecorder
	costCtrl    CostCalculator  // optional; used to compute cost_usd per call
	agentRunner string          // identifies which runner owns these calls (default: "builtin")
}

// CostCalculator computes a USD cost given a model name and token counts.
// Implemented by telemetry.CostController.
type CostCalculator interface {
	CalculateCost(model string, inputTokens, outputTokens int) float64
}

// NewRecordingProvider wraps provider with a RecordingProvider that stores call details.
// If db also implements LlmCallRecorder, structured LlmCallRecord rows are written
// for per-stage cost attribution (ARCH-O04).
// costCtrl may be nil; if provided, each LlmCallRecord is populated with a cost_usd value.
func NewRecordingProvider(provider LlmProvider, db CallDetailsStore, costCtrl CostCalculator) *RecordingProvider {
	rp := &RecordingProvider{inner: provider, db: db, costCtrl: costCtrl, agentRunner: "builtin"}
	if rec, ok := db.(LlmCallRecorder); ok {
		rp.recorder = rec
	}
	return rp
}

// WithAgentRunner sets the agent runner label stamped on every LlmCallRecord.
func (r *RecordingProvider) WithAgentRunner(name string) *RecordingProvider {
	r.agentRunner = name
	return r
}

// WithCostCalculator attaches a cost calculator so each recorded LlmCallRecord
// is populated with a cost_usd value (ARCH-O04).
func (r *RecordingProvider) WithCostCalculator(cc CostCalculator) *RecordingProvider {
	r.costCtrl = cc
	return r
}

// Inner returns the wrapped LlmProvider. Useful for bypassing recording for internal calls.
func (r *RecordingProvider) Inner() LlmProvider { return r.inner }

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
	callID := r.storeDetails(ctx, req, resp)

	// Record structured LlmCallRecord for per-stage cost attribution (ARCH-O04).
	// Only executed when the db also implements LlmCallRecorder (i.e. in production).
	if r.recorder != nil {
		r.recordCall(ctx, callID, req, resp)
	}

	return resp, nil
}

// storeDetails persists the full prompt/response pair and returns the generated call ID.
func (r *RecordingProvider) storeDetails(ctx context.Context, req models.LlmRequest, resp *models.LlmResponse) string {
	callID := fmt.Sprintf("llm-%d", time.Now().UnixNano())

	promptBytes, err := json.Marshal(req)
	if err != nil {
		log.Warn().Err(err).Str("call_id", callID).Msg("recording_provider: failed to marshal request")
		return callID
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
	return callID
}

// recordCall writes a structured LlmCallRecord so GetTicketCostByStage returns
// meaningful per-stage data (ARCH-O04). Best-effort: errors are logged, not propagated.
func (r *RecordingProvider) recordCall(ctx context.Context, callID string, req models.LlmRequest, resp *models.LlmResponse) {
	tc := telemetry.TraceFromContext(ctx)

	var costUSD float64
	if r.costCtrl != nil {
		costUSD = r.costCtrl.CalculateCost(resp.Model, resp.TokensInput, resp.TokensOutput)
	}

	call := &models.LlmCallRecord{
		ID:                  callID,
		TicketID:            tc.TicketID,
		Role:                req.Stage, // pipeline actor (e.g. "planning", "implementing"); not the provider name
		Provider:            r.inner.ProviderName(),
		Model:               resp.Model,
		Stage:               req.Stage,
		AgentRunner:         r.agentRunner,
		TokensInput:         resp.TokensInput,
		TokensOutput:        resp.TokensOutput,
		CacheReadTokens:     resp.CacheReadTokens,
		CacheCreationTokens: resp.CacheCreationTokens,
		DurationMs:          resp.DurationMs,
		CostUSD:             costUSD,
		PromptVersion:       req.PromptVersion,
		Status:              "success",
		CreatedAt:           time.Now(),
	}

	if err := r.recorder.RecordLlmCall(ctx, call); err != nil {
		log.Warn().Err(err).Str("call_id", callID).Msg("recording_provider: failed to record llm call")
	}
}
