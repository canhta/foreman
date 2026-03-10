package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMProvider is a simple stub for testing CircuitBreaker.
type mockLLMProvider struct {
	name    string
	calls   int
	respErr error
}

func (m *mockLLMProvider) ProviderName() string                { return m.name }
func (m *mockLLMProvider) HealthCheck(_ context.Context) error { return nil }
func (m *mockLLMProvider) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.respErr != nil {
		return nil, m.respErr
	}
	return &models.LlmResponse{Content: "ok"}, nil
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 60, cfg.WindowSecs)
	assert.Equal(t, 120, cfg.CooldownSecs)
}

func TestCircuitBreakerOpenError_Error(t *testing.T) {
	retryAt := time.Now().Add(time.Minute)
	err := &CircuitBreakerOpenError{RetryAfter: retryAt}
	assert.Contains(t, err.Error(), "circuit breaker open")
	assert.Contains(t, err.Error(), "retry after")
}

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	inner := &mockLLMProvider{name: "test"}
	cb := NewCircuitBreakerProvider(inner, DefaultCircuitBreakerConfig())
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_ProviderName(t *testing.T) {
	inner := &mockLLMProvider{name: "anthropic"}
	cb := NewCircuitBreakerProvider(inner, DefaultCircuitBreakerConfig())
	assert.Equal(t, "anthropic", cb.ProviderName())
}

func TestCircuitBreaker_HealthCheck(t *testing.T) {
	inner := &mockLLMProvider{name: "test"}
	cb := NewCircuitBreakerProvider(inner, DefaultCircuitBreakerConfig())
	assert.NoError(t, cb.HealthCheck(context.Background()))
}

func TestCircuitBreaker_SuccessfulCallsStayClosed(t *testing.T) {
	inner := &mockLLMProvider{name: "test"}
	cb := NewCircuitBreakerProvider(inner, DefaultCircuitBreakerConfig())

	for i := 0; i < 10; i++ {
		resp, err := cb.Complete(context.Background(), models.LlmRequest{})
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Content)
	}
	assert.Equal(t, CircuitClosed, cb.State())
	assert.Equal(t, 10, inner.calls)
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ConnectionError{Err: errors.New("conn refused")}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		WindowSecs:       60,
		CooldownSecs:     120,
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// First 3 failures should trip the circuit.
	for i := 0; i < 3; i++ {
		_, err := cb.Complete(context.Background(), models.LlmRequest{})
		require.Error(t, err)
	}

	assert.Equal(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_OpenStateRejectsWithoutCallingInner(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ConnectionError{Err: errors.New("timeout")}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		WindowSecs:       60,
		CooldownSecs:     3600,
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// Trip the circuit.
	for i := 0; i < 2; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	require.Equal(t, CircuitOpen, cb.State())

	callsBefore := inner.calls

	// Now the open circuit should reject and NOT call inner.
	_, err := cb.Complete(context.Background(), models.LlmRequest{})
	var openErr *CircuitBreakerOpenError
	require.ErrorAs(t, err, &openErr)
	assert.True(t, openErr.RetryAfter.After(time.Now()))
	assert.Equal(t, callsBefore, inner.calls, "inner provider should not be called when circuit is open")
}

func TestCircuitBreaker_RecoversThroughHalfOpen(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ConnectionError{Err: errors.New("timeout")}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		WindowSecs:       60,
		CooldownSecs:     0, // zero cooldown — circuit transitions to half-open immediately
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// Trip circuit.
	for i := 0; i < 2; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	require.Equal(t, CircuitOpen, cb.State())

	// With 0 cooldown the next checkState call moves to half-open.
	// But inner still returns error, so it stays open again.
	inner.respErr = &ConnectionError{Err: errors.New("still down")}
	_, err := cb.Complete(context.Background(), models.LlmRequest{})
	// Should succeed past the half-open gate and then fail from inner.
	require.Error(t, err)
	// Not a CircuitBreakerOpenError — the inner error should propagate.
	var openErr *CircuitBreakerOpenError
	assert.False(t, errors.As(err, &openErr))
}

func TestCircuitBreaker_ClosesOnSuccessFromHalfOpen(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ConnectionError{Err: errors.New("timeout")}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		WindowSecs:       60,
		CooldownSecs:     0,
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// Trip circuit.
	for i := 0; i < 2; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	require.Equal(t, CircuitOpen, cb.State())

	// Remove error so next call succeeds (half-open → closed).
	inner.respErr = nil
	resp, err := cb.Complete(context.Background(), models.LlmRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_NonTransientErrorsDoNotOpenCircuit(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &RateLimitError{RetryAfterSecs: 30}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		WindowSecs:       60,
		CooldownSecs:     120,
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// Rate limit errors are non-transient and should not trip the circuit.
	for i := 0; i < 10; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	assert.Equal(t, CircuitClosed, cb.State(), "rate limit errors should not open the circuit")
}

func TestCircuitBreaker_AuthErrorDoesNotOpenCircuit(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &AuthError{Message: "invalid key"}}
	cfg := CircuitBreakerConfig{FailureThreshold: 2, WindowSecs: 60, CooldownSecs: 120}
	cb := NewCircuitBreakerProvider(inner, cfg)

	for i := 0; i < 5; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_BudgetExceededDoesNotOpenCircuit(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &BudgetExceededError{Current: 100, Limit: 50}}
	cfg := CircuitBreakerConfig{FailureThreshold: 2, WindowSecs: 60, CooldownSecs: 120}
	cb := NewCircuitBreakerProvider(inner, cfg)

	for i := 0; i < 5; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_ServerOverloadTripsCircuit(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ServerOverloadError{}}
	cfg := CircuitBreakerConfig{FailureThreshold: 2, WindowSecs: 60, CooldownSecs: 120}
	cb := NewCircuitBreakerProvider(inner, cfg)

	for i := 0; i < 2; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	assert.Equal(t, CircuitOpen, cb.State())
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		err       error
		transient bool
	}{
		{&ConnectionError{Err: errors.New("timeout")}, true},
		{&ServerOverloadError{}, true},
		{errors.New("unknown error"), true},
		{&RateLimitError{RetryAfterSecs: 60}, false},
		{&AuthError{Message: "bad key"}, false},
		{&BudgetExceededError{Limit: 10, Current: 20}, false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.transient, isTransientError(tc.err), "error: %v", tc.err)
	}
}

func TestCircuitBreaker_FailuresExpireAfterWindow(t *testing.T) {
	inner := &mockLLMProvider{name: "test", respErr: &ConnectionError{Err: errors.New("timeout")}}
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		WindowSecs:       1, // 1-second window
		CooldownSecs:     120,
	}
	cb := NewCircuitBreakerProvider(inner, cfg)

	// Record 2 failures.
	for i := 0; i < 2; i++ {
		cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	}
	assert.Equal(t, CircuitClosed, cb.State())

	// Wait for the window to expire.
	time.Sleep(1100 * time.Millisecond)

	// Now failures should have been pruned; circuit stays closed.
	cb.Complete(context.Background(), models.LlmRequest{}) //nolint:errcheck
	assert.Equal(t, CircuitClosed, cb.State())
}
