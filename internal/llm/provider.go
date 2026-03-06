package llm

import (
	"context"
	"fmt"
	"strings"

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

// ServerOverloadError is returned when Anthropic responds with HTTP 529 (overloaded).
// Distinct from RateLimitError — overload should trigger a short backoff retry,
// whereas rate limit carries an explicit retry-after window.
type ServerOverloadError struct{}

func (e *ServerOverloadError) Error() string {
	return "anthropic server overloaded"
}

type ConnectionError struct {
	Err     error
	Attempt int
}

func (e *ConnectionError) Error() string {
	return e.Err.Error()
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// NewProviderFromConfig constructs an LlmProvider from the named provider and config.
func NewProviderFromConfig(providerName string, cfg models.LLMConfig) (LlmProvider, error) {
	switch providerName {
	case "anthropic":
		return NewAnthropicProvider(cfg.Anthropic.APIKey, cfg.Anthropic.BaseURL), nil
	case "openai":
		return NewOpenAIProvider(cfg.OpenAI.APIKey, cfg.OpenAI.BaseURL), nil
	case "openrouter":
		return NewOpenRouterProvider(cfg.OpenRouter.APIKey, cfg.OpenRouter.BaseURL), nil
	case "local":
		return NewLocalProvider(cfg.Local.BaseURL), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", providerName)
	}
}

// resolveModel returns the bare model name to send to an API.
// It strips an optional "provider:" prefix (e.g. "openai:gpt-4o" → "gpt-4o")
// and falls back to defaultModel when the result would be empty.
func resolveModel(model, defaultModel string) string {
	if idx := strings.Index(model, ":"); idx >= 0 {
		model = model[idx+1:]
	}
	if model == "" {
		return defaultModel
	}
	return model
}
