package llm

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// OpenRouterProvider wraps OpenAIProvider with OpenRouter defaults.
// OpenRouter's API is OpenAI-compatible (same /v1/chat/completions endpoint).
type OpenRouterProvider struct {
	inner *OpenAIProvider
}

func NewOpenRouterProvider(apiKey, baseURL string) *OpenRouterProvider {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api"
	}
	return &OpenRouterProvider{
		inner: NewOpenAIProvider(apiKey, baseURL),
	}
}

func (p *OpenRouterProvider) ProviderName() string { return "openrouter" }

func (p *OpenRouterProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return p.inner.Complete(ctx, req)
}

func (p *OpenRouterProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}
