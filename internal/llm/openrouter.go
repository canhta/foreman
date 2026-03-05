package llm

import (
	"context"
	"net/http"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// OpenRouterProvider wraps the OpenAI-compatible OpenRouter API.
// Tool-use, structured output, and all other features work identically to OpenAIProvider.
type OpenRouterProvider struct {
	OpenAIProvider
}

func NewOpenRouterProvider(apiKey, baseURL string) *OpenRouterProvider {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api"
	}
	return &OpenRouterProvider{
		OpenAIProvider: OpenAIProvider{
			apiKey:  apiKey,
			baseURL: baseURL,
			client:  &http.Client{Timeout: 5 * time.Minute},
		},
	}
}

func (p *OpenRouterProvider) ProviderName() string { return "openrouter" }

func (p *OpenRouterProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	body := openAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	for _, t := range req.Tools {
		body.Tools = append(body.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	if req.OutputSchema != nil {
		body.ResponseFormat = &openAIRespFmt{
			Type: "json_schema",
			JSONSchema: &openAIJSONSchema{
				Name:   "structured_output",
				Schema: *req.OutputSchema,
				Strict: true,
			},
		}
	}

	body.Messages = buildOpenAIMessages(req.SystemPrompt, req.Messages, req.UserPrompt)

	return p.doRequest(ctx, body)
}

func (p *OpenRouterProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, models.LlmRequest{
		Model:      "openai/gpt-4o-mini",
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
