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

type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *AnthropicProvider) ProviderName() string {
	return "anthropic"
}

// --- Anthropic API request types ---

type anthropicRequest struct {
	Model       string               `json:"model"`
	MaxTokens   int                  `json:"max_tokens"`
	System      string               `json:"system,omitempty"`
	Messages    []anthropicMessage   `json:"messages"`
	Temperature float64              `json:"temperature,omitempty"`
	Stop        []string             `json:"stop_sequences,omitempty"`
	Tools       []anthropicToolDef   `json:"tools,omitempty"`
}

type anthropicToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicMessage uses a polymorphic Content field.
// For simple messages: Content is a string.
// For tool-use/tool-result messages: Content is an array of content blocks.
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// --- Anthropic API response types ---

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`    // tool_use block
	Name  string          `json:"name,omitempty"`  // tool_use block
	Input json.RawMessage `json:"input,omitempty"` // tool_use block
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Tool result content block (sent as user message content) ---

type anthropicToolResultBlock struct {
	Type      string `json:"type"` // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicToolUseBlock struct {
	Type  string          `json:"type"` // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	body := anthropicRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		System:      req.SystemPrompt,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	// Convert tool definitions
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, anthropicToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	// Build messages
	if len(req.Messages) > 0 {
		body.Messages = buildAnthropicMessages(req.Messages)
	} else {
		body.Messages = []anthropicMessage{
			{Role: "user", Content: req.UserPrompt},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
		var apiErr anthropicError
		json.Unmarshal(respBody, &apiErr)
		return nil, &AuthError{Message: apiErr.Error.Message}
	}

	if httpResp.StatusCode != 200 {
		var apiErr anthropicError
		json.Unmarshal(respBody, &apiErr)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", httpResp.StatusCode, apiErr.Error.Message)
	}

	var resp anthropicResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return parseAnthropicResponse(resp, durationMs), nil
}

// buildAnthropicMessages converts models.Message slice to Anthropic API format.
func buildAnthropicMessages(messages []models.Message) []anthropicMessage {
	var result []anthropicMessage
	for _, msg := range messages {
		switch {
		case len(msg.ToolCalls) > 0:
			// Assistant message with tool_use content blocks
			var blocks []interface{}
			if msg.Content != "" {
				blocks = append(blocks, map[string]string{"type": "text", "text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, anthropicToolUseBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			result = append(result, anthropicMessage{Role: "assistant", Content: blocks})

		case len(msg.ToolResults) > 0:
			// User message with tool_result content blocks
			var blocks []anthropicToolResultBlock
			for _, tr := range msg.ToolResults {
				blocks = append(blocks, anthropicToolResultBlock{
					Type:      "tool_result",
					ToolUseID: tr.ToolCallID,
					Content:   tr.Content,
					IsError:   tr.IsError,
				})
			}
			result = append(result, anthropicMessage{Role: "user", Content: blocks})

		default:
			// Simple text message
			result = append(result, anthropicMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	return result
}

// parseAnthropicResponse extracts text content and tool calls from the API response.
func parseAnthropicResponse(resp anthropicResponse, durationMs int64) *models.LlmResponse {
	var content string
	var toolCalls []models.ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, models.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}

	return &models.LlmResponse{
		Content:      content,
		TokensInput:  resp.Usage.InputTokens,
		TokensOutput: resp.Usage.OutputTokens,
		Model:        resp.Model,
		DurationMs:   durationMs,
		StopReason:   models.StopReason(resp.StopReason),
		ToolCalls:    toolCalls,
	}
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, models.LlmRequest{
		Model:      "claude-haiku-4-5-20251001",
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
