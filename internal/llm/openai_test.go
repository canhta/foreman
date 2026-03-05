package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestOpenAIProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing or wrong Authorization header")
		}
		resp := map[string]interface{}{
			"id":    "chatcmpl_test",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": "Hello from GPT"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "gpt-4o",
		UserPrompt: "Say hello",
		MaxTokens:  100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from GPT" {
		t.Errorf("expected 'Hello from GPT', got %q", resp.Content)
	}
	if resp.StopReason != models.StopReasonEndTurn {
		t.Errorf("expected end_turn, got %s", resp.StopReason)
	}
}

func TestOpenAIProvider_ProviderName(t *testing.T) {
	p := NewOpenAIProvider("key", "url")
	if p.ProviderName() != "openai" {
		t.Errorf("expected 'openai', got %q", p.ProviderName())
	}
}

func TestOpenAIProvider_Complete_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify tools were sent
		tools, ok := reqBody["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Return a tool_calls response
		resp := map[string]interface{}{
			"id":    "chatcmpl_tools",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "Read",
									"arguments": `{"path":"main.go"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 50, "completion_tokens": 20},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model: "gpt-4o",
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
		t.Fatalf("expected tool_use, got %s", result.StopReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "Read" {
		t.Errorf("expected tool name 'Read', got %q", result.ToolCalls[0].Name)
	}
}

func TestOpenAIProvider_StructuredOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify response_format was set
		rf, ok := reqBody["response_format"].(map[string]interface{})
		if !ok {
			t.Error("expected response_format in request")
		}
		if rf["type"] != "json_schema" {
			t.Errorf("expected json_schema type, got %v", rf["type"])
		}

		resp := map[string]interface{}{
			"id":    "chatcmpl_struct",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": `{"severity":"high"}`},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 20, "completion_tokens": 10},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	schema := json.RawMessage(`{"type":"object","properties":{"severity":{"type":"string"}}}`)
	provider := NewOpenAIProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:        "gpt-4o",
		UserPrompt:   "Analyze this",
		MaxTokens:    100,
		OutputSchema: &schema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != `{"severity":"high"}` {
		t.Errorf("expected JSON content, got %q", resp.Content)
	}
}

func TestOpenAIProvider_ToolResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		messages := reqBody["messages"].([]interface{})
		// system + user + assistant(tool_calls) + tool result = 4
		if len(messages) < 4 {
			t.Errorf("expected at least 4 messages, got %d", len(messages))
		}

		// Find the tool message
		for _, m := range messages {
			msg := m.(map[string]interface{})
			if msg["role"] == "tool" {
				if msg["tool_call_id"] != "call_1" {
					t.Errorf("expected tool_call_id 'call_1', got %v", msg["tool_call_id"])
				}
			}
		}

		resp := map[string]interface{}{
			"id":    "chatcmpl_tr",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": "Done."},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 100, "completion_tokens": 10},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:        "gpt-4o",
		SystemPrompt: "You are helpful",
		Messages: []models.Message{
			{Role: "user", Content: "Read main.go"},
			{Role: "assistant", ToolCalls: []models.ToolCall{
				{ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)},
			}},
			{Role: "user", ToolResults: []models.ToolResult{
				{ToolCallID: "call_1", Content: "package main"},
			}},
		},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "Done." {
		t.Errorf("unexpected content: %s", result.Content)
	}
}

func TestOpenRouterProvider_ProviderName(t *testing.T) {
	p := NewOpenRouterProvider("key", "url")
	if p.ProviderName() != "openrouter" {
		t.Errorf("expected 'openrouter', got %q", p.ProviderName())
	}
}

func TestOpenRouterProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":    "or_test",
			"model": "openai/gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": "OpenRouter response"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenRouterProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "openai/gpt-4o",
		UserPrompt: "Hello",
		MaxTokens:  100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "OpenRouter response" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestLocalProvider_ProviderName(t *testing.T) {
	p := NewLocalProvider("url")
	if p.ProviderName() != "local" {
		t.Errorf("expected 'local', got %q", p.ProviderName())
	}
}

func TestLocalProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":    "local_test",
			"model": "llama3",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": "Local response"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewLocalProvider(server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "llama3",
		UserPrompt: "Hello",
		MaxTokens:  100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Local response" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestLocalProvider_GracefulDegradation_NoTools(t *testing.T) {
	// Local model responds with text even when tools are provided — graceful degradation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":    "local_degrade",
			"model": "llama3",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": "I'll just answer directly"},
					"finish_reason": "stop", // text response despite tools being sent
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 20, "completion_tokens": 8},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewLocalProvider(server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "llama3",
		UserPrompt: "Read main.go",
		MaxTokens:  100,
		Tools: []models.ToolDef{
			{Name: "Read", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Graceful degradation: text response with end_turn
	if result.StopReason != models.StopReasonEndTurn {
		t.Errorf("expected end_turn (graceful degradation), got %s", result.StopReason)
	}
}
