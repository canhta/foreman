package llm

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// LocalProvider wraps OpenAIProvider for local OpenAI-compatible servers (Ollama, LM Studio).
type LocalProvider struct {
	inner *OpenAIProvider
}

func NewLocalProvider(baseURL string) *LocalProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	inner := NewOpenAIProvider("", baseURL)
	inner.defaultModel = "llama3"
	return &LocalProvider{
		inner: inner,
	}
}

func (p *LocalProvider) ProviderName() string { return "local" }

func (p *LocalProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return p.inner.Complete(ctx, req)
}

func (p *LocalProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}
