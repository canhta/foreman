// internal/llm/recording_provider_test.go
package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
)

// mockLlmProvider is a test double for LlmProvider.
type mockLlmProvider struct {
	name        string
	completeErr error
	response    *models.LlmResponse
}

func (m *mockLlmProvider) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	if m.completeErr != nil {
		return nil, m.completeErr
	}
	return m.response, nil
}

func (m *mockLlmProvider) ProviderName() string                { return m.name }
func (m *mockLlmProvider) HealthCheck(_ context.Context) error { return nil }

// mockDatabase is a test double for the db.Database subset used by RecordingProvider.
type mockDatabase struct {
	storedCallID       string
	storedFullPrompt   string
	storedFullResponse string
	storeErr           error
	storeCalled        int
}

func (m *mockDatabase) StoreCallDetails(_ context.Context, callID, fullPrompt, fullResponse string) error {
	m.storeCalled++
	m.storedCallID = callID
	m.storedFullPrompt = fullPrompt
	m.storedFullResponse = fullResponse
	return m.storeErr
}

func TestRecordingProvider_StoresDetailsOnSuccess(t *testing.T) {
	inner := &mockLlmProvider{
		name: "test-provider",
		response: &models.LlmResponse{
			Content: "hello world",
		},
	}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	req := models.LlmRequest{Model: "gpt-4", UserPrompt: "say hello"}
	resp, err := rp.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("response unchanged: got %q, want %q", resp.Content, "hello world")
	}

	if db.storeCalled != 1 {
		t.Fatalf("StoreCallDetails called %d times, want 1", db.storeCalled)
	}
	if !strings.HasPrefix(db.storedCallID, "llm-") {
		t.Errorf("callID should start with 'llm-', got %q", db.storedCallID)
	}
	if !strings.Contains(db.storedFullPrompt, "gpt-4") {
		t.Errorf("fullPrompt should contain model name, got %q", db.storedFullPrompt)
	}
	if db.storedFullResponse != "hello world" {
		t.Errorf("fullResponse should be resp.Content, got %q", db.storedFullResponse)
	}
}

func TestRecordingProvider_DoesNotStoreOnError(t *testing.T) {
	inner := &mockLlmProvider{
		name:        "test-provider",
		completeErr: errors.New("LLM unavailable"),
	}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	req := models.LlmRequest{Model: "gpt-4", UserPrompt: "say hello"}
	_, err := rp.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if db.storeCalled != 0 {
		t.Errorf("StoreCallDetails should not be called on error, called %d times", db.storeCalled)
	}
}

func TestRecordingProvider_DBErrorDoesNotPropagateToCallers(t *testing.T) {
	// Even when StoreCallDetails returns an error, the LLM response is still returned.
	inner := &mockLlmProvider{
		name: "test-provider",
		response: &models.LlmResponse{
			Content: "response content",
		},
	}
	db := &mockDatabase{storeErr: errors.New("db write failed")}
	rp := NewRecordingProvider(inner, db, nil)

	req := models.LlmRequest{Model: "gpt-4", UserPrompt: "say hello"}
	resp, err := rp.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("db error should not propagate to caller, got: %v", err)
	}
	if resp.Content != "response content" {
		t.Errorf("response unchanged: got %q, want %q", resp.Content, "response content")
	}
}

func TestRecordingProvider_TraceIDIncludedInStoredResponse(t *testing.T) {
	inner := &mockLlmProvider{
		name: "test-provider",
		response: &models.LlmResponse{
			Content: "the model reply",
		},
	}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	ctx := telemetry.WithTrace(context.Background(), telemetry.TraceContext{
		TraceID:  "abc123trace",
		TicketID: "ticket-1",
	})

	req := models.LlmRequest{Model: "gpt-4", UserPrompt: "say hello"}
	resp, err := rp.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// LlmResponse must be unchanged (no mutation of the returned value).
	if resp.Content != "the model reply" {
		t.Errorf("response content mutated: got %q, want %q", resp.Content, "the model reply")
	}

	if !strings.Contains(db.storedFullResponse, "[trace_id: abc123trace]") {
		t.Errorf("storedFullResponse should contain trace_id annotation, got: %q", db.storedFullResponse)
	}
	if !strings.Contains(db.storedFullResponse, "the model reply") {
		t.Errorf("storedFullResponse should also contain original response, got: %q", db.storedFullResponse)
	}
}

func TestRecordingProvider_NoTraceIDSkipsAnnotation(t *testing.T) {
	inner := &mockLlmProvider{
		name: "test-provider",
		response: &models.LlmResponse{
			Content: "plain response",
		},
	}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	// No trace in context.
	req := models.LlmRequest{Model: "gpt-4", UserPrompt: "say hello"}
	_, err := rp.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(db.storedFullResponse, "[trace_id:") {
		t.Errorf("should not inject trace_id annotation when trace is empty, got: %q", db.storedFullResponse)
	}
	if db.storedFullResponse != "plain response" {
		t.Errorf("storedFullResponse should be plain response, got %q", db.storedFullResponse)
	}
}

func TestRecordingProvider_ProviderNameDelegatesToInner(t *testing.T) {
	inner := &mockLlmProvider{name: "anthropic"}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	if rp.ProviderName() != "anthropic" {
		t.Errorf("ProviderName() = %q, want %q", rp.ProviderName(), "anthropic")
	}
}

func TestRecordingProvider_HealthCheckDelegatesToInner(t *testing.T) {
	inner := &mockLlmProvider{name: "anthropic"}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	if err := rp.HealthCheck(context.Background()); err != nil {
		t.Errorf("unexpected HealthCheck error: %v", err)
	}
}

// TestRecordingProvider_PromptVersionSerializedIntoStoredPrompt verifies that
// when req.PromptVersion is set, storeDetails() marshals it into the full prompt
// JSON persisted via StoreCallDetails. This is the production observability path
// for REQ-OBS-001: prompt_version is queryable in llm_call_details.full_prompt.
func TestRecordingProvider_PromptVersionSerializedIntoStoredPrompt(t *testing.T) {
	inner := &mockLlmProvider{
		name: "test-provider",
		response: &models.LlmResponse{
			Content: "response",
		},
	}
	db := &mockDatabase{}
	rp := NewRecordingProvider(inner, db, nil)

	req := models.LlmRequest{
		Model:         "gpt-4",
		UserPrompt:    "say hello",
		PromptVersion: "abc123",
	}
	_, err := rp.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.storeCalled != 1 {
		t.Fatalf("StoreCallDetails called %d times, want 1", db.storeCalled)
	}
	if !strings.Contains(db.storedFullPrompt, `"prompt_version":"abc123"`) {
		t.Errorf("storedFullPrompt should contain prompt_version field, got: %q", db.storedFullPrompt)
	}
}
