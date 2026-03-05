package llm

import (
	"context"
	"fmt"

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
