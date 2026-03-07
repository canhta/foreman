// internal/llm/circuit_breaker.go
package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/models"
)

// CircuitState represents the current state of the circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

// CircuitBreakerConfig configures the circuit breaker behaviour.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures within the window before the circuit opens.
	FailureThreshold int
	// WindowSecs is the time window (in seconds) for counting failures.
	WindowSecs int
	// CooldownSecs is the duration the circuit stays open before switching to half-open.
	CooldownSecs int
}

// DefaultCircuitBreakerConfig returns sensible production defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		WindowSecs:       60,
		CooldownSecs:     120,
	}
}

// CircuitBreakerOpenError is returned when the circuit is open.
type CircuitBreakerOpenError struct {
	RetryAfter time.Time
}

func (e *CircuitBreakerOpenError) Error() string {
	return fmt.Sprintf("circuit breaker open; retry after %s", e.RetryAfter.Format(time.RFC3339))
}

// CircuitBreakerProvider wraps an LlmProvider with a circuit breaker (ARCH-F02).
// When the circuit opens (FailureThreshold failures within WindowSecs),
// all LLM calls are rejected until the cooldown elapses.
type CircuitBreakerProvider struct {
	openSince time.Time
	inner     LlmProvider
	state     CircuitState
	failures  []time.Time
	config    CircuitBreakerConfig
	mu        sync.Mutex
}

// NewCircuitBreakerProvider wraps provider with a circuit breaker.
func NewCircuitBreakerProvider(provider LlmProvider, config CircuitBreakerConfig) *CircuitBreakerProvider {
	return &CircuitBreakerProvider{
		inner:  provider,
		config: config,
		state:  CircuitClosed,
	}
}

func (cb *CircuitBreakerProvider) ProviderName() string { return cb.inner.ProviderName() }

func (cb *CircuitBreakerProvider) HealthCheck(ctx context.Context) error {
	return cb.inner.HealthCheck(ctx)
}

// Complete calls the inner provider, enforcing circuit breaker state.
func (cb *CircuitBreakerProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	if err := cb.checkState(); err != nil {
		return nil, err
	}

	resp, err := cb.inner.Complete(ctx, req)
	if err != nil {
		cb.recordFailure(err)
		return nil, err
	}

	cb.recordSuccess()
	return resp, nil
}

// State returns the current circuit state (for observability/tests).
func (cb *CircuitBreakerProvider) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreakerProvider) checkState() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	window := time.Duration(cb.config.WindowSecs) * time.Second
	cooldown := time.Duration(cb.config.CooldownSecs) * time.Second

	switch cb.state {
	case CircuitOpen:
		if now.Sub(cb.openSince) >= cooldown {
			cb.state = CircuitHalfOpen
			log.Info().Str("provider", cb.inner.ProviderName()).Msg("circuit breaker: half-open (testing)")
			return nil
		}
		retryAfter := cb.openSince.Add(cooldown)
		return &CircuitBreakerOpenError{RetryAfter: retryAfter}
	case CircuitHalfOpen:
		return nil
	default: // CircuitClosed
		// Prune failures outside the window.
		pruned := cb.failures[:0]
		for _, t := range cb.failures {
			if now.Sub(t) < window {
				pruned = append(pruned, t)
			}
		}
		cb.failures = pruned
		return nil
	}
}

func (cb *CircuitBreakerProvider) recordFailure(err error) {
	// Only trip on server-side errors, not client errors (auth, rate limit).
	if isTransientError(err) {
		cb.mu.Lock()
		defer cb.mu.Unlock()
		cb.failures = append(cb.failures, time.Now())
		if len(cb.failures) >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
			cb.openSince = time.Now()
			log.Warn().
				Str("provider", cb.inner.ProviderName()).
				Int("failures", len(cb.failures)).
				Msg("circuit breaker: opened")
		}
	}
}

func (cb *CircuitBreakerProvider) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failures = cb.failures[:0]
		log.Info().Str("provider", cb.inner.ProviderName()).Msg("circuit breaker: closed (recovered)")
	}
}

// isTransientError returns true for errors that indicate provider instability
// (connection failures, server overload) vs. client errors (auth, budget).
func isTransientError(err error) bool {
	switch err.(type) {
	case *ConnectionError, *ServerOverloadError:
		return true
	case *RateLimitError, *AuthError, *BudgetExceededError:
		return false
	default:
		return true
	}
}
