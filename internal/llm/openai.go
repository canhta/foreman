package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *OpenAIProvider) ProviderName() string { return "openai" }

// --- OpenAI API request/response types ---

type openAIRequest struct {
	Model          string           `json:"model"`
	Messages       []openAIMessage  `json:"messages"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    float64          `json:"temperature,omitempty"`
	Stop           []string         `json:"stop,omitempty"`
	Tools          []openAITool     `json:"tools,omitempty"`
	ResponseFormat *openAIRespFmt   `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content,omitempty"` // string or nil
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAITool struct {
	Type     string          `json:"type"` // "function"
	Function openAIFunction  `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // "function"
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type openAIRespFmt struct {
	Type       string           `json:"type"`                  // "json_schema"
	JSONSchema *openAIJSONSchema `json:"json_schema,omitempty"`
}

type openAIJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	body := openAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	// Convert tool definitions
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

	// Structured output via response_format
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

	// Build messages
	body.Messages = buildOpenAIMessages(req.SystemPrompt, req.Messages, req.UserPrompt)

	return p.doRequest(ctx, body)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body openAIRequest) (*models.LlmResponse, error) {
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
		return nil, &RateLimitError{RetryAfterSecs: 60}
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		var apiErr openAIError
		json.Unmarshal(respBody, &apiErr)
		return nil, &AuthError{Message: apiErr.Error.Message}
	}

	if httpResp.StatusCode != 200 {
		var apiErr openAIError
		json.Unmarshal(respBody, &apiErr)
		return nil, fmt.Errorf("openai API error (status %d): %s", httpResp.StatusCode, apiErr.Error.Message)
	}

	var resp openAIResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return parseOpenAIResponse(resp, durationMs), nil
}

// buildOpenAIMessages converts systemPrompt + messages + userPrompt to OpenAI format.
func buildOpenAIMessages(systemPrompt string, messages []models.Message, userPrompt string) []openAIMessage {
	var result []openAIMessage

	if systemPrompt != "" {
		result = append(result, openAIMessage{Role: "system", Content: systemPrompt})
	}

	if len(messages) > 0 {
		for _, msg := range messages {
			switch {
			case len(msg.ToolCalls) > 0:
				// Assistant message with tool_calls
				var calls []openAIToolCall
				for _, tc := range msg.ToolCalls {
					calls = append(calls, openAIToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: openAIFunctionCall{
							Name:      tc.Name,
							Arguments: string(tc.Input),
						},
					})
				}
				result = append(result, openAIMessage{
					Role:      "assistant",
					Content:   msg.Content,
					ToolCalls: calls,
				})

			case len(msg.ToolResults) > 0:
				// One message per tool result (OpenAI requires role: "tool")
				for _, tr := range msg.ToolResults {
					result = append(result, openAIMessage{
						Role:       "tool",
						Content:    tr.Content,
						ToolCallID: tr.ToolCallID,
					})
				}

			default:
				result = append(result, openAIMessage{Role: msg.Role, Content: msg.Content})
			}
		}
	} else if userPrompt != "" {
		result = append(result, openAIMessage{Role: "user", Content: userPrompt})
	}

	return result
}

// parseOpenAIResponse converts the API response to models.LlmResponse.
func parseOpenAIResponse(resp openAIResponse, durationMs int64) *models.LlmResponse {
	if len(resp.Choices) == 0 {
		return &models.LlmResponse{
			Model:      resp.Model,
			DurationMs: durationMs,
		}
	}

	choice := resp.Choices[0]
	content := ""
	if s, ok := choice.Message.Content.(string); ok {
		content = s
	}

	var toolCalls []models.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, models.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	stopReason := models.StopReasonEndTurn
	if choice.FinishReason == "tool_calls" {
		stopReason = models.StopReasonToolUse
	} else if choice.FinishReason == "length" {
		stopReason = models.StopReasonMaxTokens
	} else if choice.FinishReason == "stop" {
		stopReason = models.StopReasonEndTurn
	}

	return &models.LlmResponse{
		Content:      content,
		TokensInput:  resp.Usage.PromptTokens,
		TokensOutput: resp.Usage.CompletionTokens,
		Model:        resp.Model,
		DurationMs:   durationMs,
		StopReason:   stopReason,
		ToolCalls:    toolCalls,
	}
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, models.LlmRequest{
		Model:      "gpt-4o-mini",
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
