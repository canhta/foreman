package llm

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// LlmProvider is implemented by each LLM backend (Anthropic, OpenAI, etc.)
// Every call is stateless — no conversation memory.
type LlmProvider interface {
	Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

// Error types for structured error handling.
type RateLimitError struct {
	RetryAfterSecs int
}

func (e *RateLimitError) Error() string {
	return "rate limit exceeded"
}

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return "authentication error: " + e.Message
}

type BudgetExceededError struct {
	Current float64
	Limit   float64
}

func (e *BudgetExceededError) Error() string {
	return "budget exceeded"
}

type ConnectionError struct {
	Attempt int
	Err     error
}

func (e *ConnectionError) Error() string {
	return e.Err.Error()
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}
