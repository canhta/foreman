package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type OpenAIProvider struct {
	apiKey       string
	baseURL      string
	client       *http.Client
	defaultModel string
}

func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		client:       &http.Client{Timeout: 5 * time.Minute},
		defaultModel: "gpt-4o-mini",
	}
}

func (p *OpenAIProvider) ProviderName() string { return "openai" }

type openaiRequest struct {
	Temperature         *float64        `json:"temperature,omitempty"`
	Model               string          `json:"model"`
	Messages            []openaiMessage `json:"messages"`
	Stop                []string        `json:"stop,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
}

// isReasoningModel returns true for OpenAI models that require max_completion_tokens
// instead of max_tokens and do not support the temperature parameter.
// This includes o-series models (o1, o3, o4-mini, …) and the gpt-5 family
// (gpt-5, gpt-5.4, gpt-5-mini, gpt-5-nano, gpt-5.3-codex, …).
func isReasoningModel(model string) bool {
	base := strings.ToLower(model)
	// o1*, o3*, o4* etc. — but not gpt-4o / gpt-4o-mini (those start with 'g')
	if len(base) >= 2 && base[0] == 'o' && base[1] >= '1' && base[1] <= '9' {
		return true
	}
	// gpt-5* family (gpt-5, gpt-5.4, gpt-5-mini, gpt-5-nano, gpt-5.3-codex, …)
	if strings.HasPrefix(base, "gpt-5") {
		return true
	}
	return false
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	messages := []openaiMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, openaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openaiMessage{Role: "user", Content: req.UserPrompt})

	model := resolveModel(req.Model, p.defaultModel)
	body := openaiRequest{
		Model:    model,
		Messages: messages,
		Stop:     req.StopSequences,
	}
	if isReasoningModel(model) {
		body.MaxCompletionTokens = req.MaxTokens
		// Reasoning models do not support a custom temperature
	} else {
		body.MaxTokens = req.MaxTokens
		t := req.Temperature
		body.Temperature = &t
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, &ConnectionError{Attempt: 1, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode == 429 {
		retryAfter := 60
		if ra := httpResp.Header.Get("Retry-After"); ra != "" {
			if v, err := strconv.Atoi(ra); err == nil {
				retryAfter = v
			}
		}
		return nil, &RateLimitError{RetryAfterSecs: retryAfter}
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		return nil, &AuthError{Message: "invalid API key"}
	}

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("openai API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var resp openaiResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var content string
	var stopReason string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		stopReason = resp.Choices[0].FinishReason
	}

	return &models.LlmResponse{
		Content:      content,
		TokensInput:  resp.Usage.PromptTokens,
		TokensOutput: resp.Usage.CompletionTokens,
		Model:        resp.Model,
		DurationMs:   durationMs,
		StopReason:   models.StopReason(stopReason),
	}, nil
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, models.LlmRequest{
		Model:      p.defaultModel,
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
