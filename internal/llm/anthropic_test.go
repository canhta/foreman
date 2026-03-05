package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestAnthropicProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or wrong API key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}

		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		resp := map[string]interface{}{
			"id":   "msg_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello from Claude"},
			},
			"model":       "claude-sonnet-4-5-20250929",
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 20,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are helpful.",
		UserPrompt:   "Say hello",
		MaxTokens:    1024,
		Temperature:  0.3,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Claude" {
		t.Errorf("expected 'Hello from Claude', got %q", resp.Content)
	}
	if resp.TokensInput != 100 {
		t.Errorf("expected 100 input tokens, got %d", resp.TokensInput)
	}
	if resp.TokensOutput != 20 {
		t.Errorf("expected 20 output tokens, got %d", resp.TokensOutput)
	}
}

func TestAnthropicProvider_ProviderName(t *testing.T) {
	p := NewAnthropicProvider("key", "url")
	if p.ProviderName() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", p.ProviderName())
	}
}

func TestAnthropicProvider_Complete_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify tools were sent
		tools, ok := reqBody["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Return a tool_use response
		resp := map[string]interface{}{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "call_1",
					"name":  "Read",
					"input": map[string]string{"path": "main.go"},
				},
			},
			"model":       "claude-sonnet-4-5-20250929",
			"stop_reason": "tool_use",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 50,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []models.Message{
			{Role: "user", Content: "Read main.go"},
		},
		Tools: []models.ToolDef{
			{
				Name:        "Read",
				Description: "Read a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != models.StopReasonToolUse {
		t.Fatalf("expected tool_use stop reason, got %s", result.StopReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "Read" {
		t.Fatalf("expected tool name 'Read', got %q", result.ToolCalls[0].Name)
	}
}

func TestAnthropicProvider_Complete_WithToolResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		messages, ok := reqBody["messages"].([]interface{})
		if !ok || len(messages) < 2 {
			t.Errorf("expected at least 2 messages, got %d", len(messages))
		}

		// Return final text response
		resp := map[string]interface{}{
			"id":   "msg_456",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "The file contains a Go program."},
			},
			"model":       "claude-sonnet-4-5-20250929",
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  200,
				"output_tokens": 30,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []models.Message{
			{Role: "user", Content: "Read main.go"},
			{Role: "assistant", ToolCalls: []models.ToolCall{
				{ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)},
			}},
			{Role: "user", ToolResults: []models.ToolResult{
				{ToolCallID: "call_1", Content: "package main\n\nfunc main() {}"},
			}},
		},
		Tools: []models.ToolDef{
			{Name: "Read", Description: "Read a file", InputSchema: json.RawMessage(`{}`)},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != models.StopReasonEndTurn {
		t.Fatalf("expected end_turn, got %s", result.StopReason)
	}
	if result.Content != "The file contains a Go program." {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestBuildAnthropicMessages(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", ToolCalls: []models.ToolCall{
			{ID: "tc1", Name: "Read", Input: json.RawMessage(`{"path":"x.go"}`)},
		}},
		{Role: "user", ToolResults: []models.ToolResult{
			{ToolCallID: "tc1", Content: "file content"},
		}},
		{Role: "assistant", Content: "Done."},
	}

	result := buildAnthropicMessages(messages)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// First message: simple string content
	if result[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", result[0].Role)
	}

	// Second message: assistant with tool_use blocks
	if result[1].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result[1].Role)
	}
	blocks, ok := result[1].Content.([]interface{})
	if !ok {
		t.Fatal("expected assistant content to be array of blocks")
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", len(blocks))
	}

	// Third message: user with tool_result blocks
	if result[2].Role != "user" {
		t.Errorf("expected role 'user', got %q", result[2].Role)
	}

	// Fourth message: simple assistant text
	if result[3].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result[3].Role)
	}
}
