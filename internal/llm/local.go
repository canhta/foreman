package llm

import (
	"context"
	"net/http"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// LocalProvider connects to a locally-running OpenAI-compatible server (e.g. Ollama).
// Tools are sent if the request includes them; if the model doesn't support tools
// it will return a text response with StopReasonEndTurn (graceful degradation).
type LocalProvider struct {
	OpenAIProvider
}

func NewLocalProvider(baseURL string) *LocalProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &LocalProvider{
		OpenAIProvider: OpenAIProvider{
			apiKey:  "local", // Ollama doesn't require a real key
			baseURL: baseURL,
			client:  &http.Client{Timeout: 10 * time.Minute}, // local models can be slow
		},
	}
}

func (p *LocalProvider) ProviderName() string { return "local" }

func (p *LocalProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	body := openAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	// Send tool definitions if provided — model will ignore them if unsupported,
	// returning a text response which the caller treats as end-of-turn.
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

func (p *LocalProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, models.LlmRequest{
		Model:      "llama3",
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
