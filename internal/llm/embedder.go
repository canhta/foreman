package llm

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// Embedder generates embedding vectors for text chunks.
type Embedder interface {
	// Embed returns a normalized float32 vector for each input string.
	// All vectors have the same dimensionality.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// NewEmbedder returns an Embedder based on config, or nil if no embedding provider is configured.
func NewEmbedder(cfg models.LLMConfig) Embedder {
	switch cfg.EmbeddingProvider {
	case "openai":
		baseURL := cfg.OpenAI.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		return NewOpenAIEmbedder(cfg.OpenAI.APIKey, cfg.EmbeddingModel, baseURL)
	case "anthropic":
		return NewAnthropicEmbedder(cfg.Anthropic.APIKey, cfg.EmbeddingModel)
	default:
		return nil
	}
}
