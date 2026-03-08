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
	var reqBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or wrong API key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		resp := map[string]interface{}{
			"id":   "msg_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello from Claude"},
			},
			"model":       "claude-sonnet-4-6",
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
		Model:        "claude-sonnet-4-6",
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

	// System prompt must be sent as a top-level "system" string field, not inside messages[].
	if system, ok := reqBody["system"].(string); !ok || system != "You are helpful." {
		t.Errorf("expected system to be top-level string %q, got %v", "You are helpful.", reqBody["system"])
	}
	// User prompt must appear as the sole message.
	msgs, _ := reqBody["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg, _ := msgs[0].(map[string]interface{})
	if msg["role"] != "user" || msg["content"] != "Say hello" {
		t.Errorf("unexpected message: %v", msg)
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
			"model":       "claude-sonnet-4-6",
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
		Model: "claude-sonnet-4-6",
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
			"model":       "claude-sonnet-4-6",
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
		Model: "claude-sonnet-4-6",
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

func TestAnthropicProvider_StructuredOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify tool_choice was set to force structured_output tool
		toolChoice, ok := reqBody["tool_choice"].(map[string]interface{})
		if !ok {
			t.Error("expected tool_choice in request")
		}
		if toolChoice["type"] != "tool" || toolChoice["name"] != "structured_output" {
			t.Errorf("unexpected tool_choice: %v", toolChoice)
		}

		// Return structured output as a tool_use block
		resp := map[string]interface{}{
			"id":   "msg_struct",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "call_struct",
					"name":  "structured_output",
					"input": map[string]string{"severity": "high", "summary": "Issue found"},
				},
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "tool_use",
			"usage":       map[string]interface{}{"input_tokens": 50, "output_tokens": 20},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	schema := json.RawMessage(`{"type":"object","properties":{"severity":{"type":"string"},"summary":{"type":"string"}}}`)
	provider := NewAnthropicProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:        "claude-sonnet-4-6",
		UserPrompt:   "Analyze this",
		MaxTokens:    1024,
		OutputSchema: &schema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Content should be the JSON of the tool input, stop reason should be end_turn
	if resp.StopReason != models.StopReasonEndTurn {
		t.Errorf("expected end_turn, got %s", resp.StopReason)
	}
	if resp.Content == "" {
		t.Error("expected structured output in content")
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		t.Errorf("content should be valid JSON, got: %s", resp.Content)
	}
}

func TestAnthropicProvider_Thinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify thinking param was sent
		thinking, ok := reqBody["thinking"].(map[string]interface{})
		if !ok {
			t.Error("expected thinking in request")
		}
		if thinking["type"] != "adaptive" {
			t.Errorf("unexpected thinking type: %v", thinking["type"])
		}
		if _, present := thinking["budget_tokens"]; present {
			t.Error("budget_tokens must not be sent for adaptive thinking")
		}

		// Return response with thinking block + text
		resp := map[string]interface{}{
			"id":   "msg_think",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "thinking", "thinking": "Let me reason through this..."},
				{"type": "text", "text": "The answer is 42."},
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": 100, "output_tokens": 50},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "claude-sonnet-4-6",
		UserPrompt: "What is the answer?",
		MaxTokens:  1024,
		Thinking:   &models.ThinkingConfig{Adaptive: true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Thinking block should NOT appear in content
	if resp.Content != "The answer is 42." {
		t.Errorf("expected only text content, got: %q", resp.Content)
	}
}

func TestAnthropicProvider_PromptCaching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// System should be an array of blocks with cache_control
		system, ok := reqBody["system"].([]interface{})
		if !ok {
			t.Errorf("expected system to be array of blocks when caching, got: %T", reqBody["system"])
		} else if len(system) != 1 {
			t.Errorf("expected 1 system block, got %d", len(system))
		} else {
			block := system[0].(map[string]interface{})
			if block["type"] != "text" {
				t.Errorf("expected text block, got %v", block["type"])
			}
			cc, ok := block["cache_control"].(map[string]interface{})
			if !ok || cc["type"] != "ephemeral" {
				t.Errorf("expected ephemeral cache_control, got %v", block["cache_control"])
			}
		}

		resp := map[string]interface{}{
			"id":   "msg_cache",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "cached response"},
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":                50,
				"output_tokens":               10,
				"cache_read_input_tokens":     200,
				"cache_creation_input_tokens": 0,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:             "claude-sonnet-4-6",
		SystemPrompt:      "Large system prompt to cache",
		UserPrompt:        "Hello",
		MaxTokens:         1024,
		CacheSystemPrompt: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CacheReadTokens != 200 {
		t.Errorf("expected 200 cache read tokens, got %d", resp.CacheReadTokens)
	}
}

func TestAnthropicProvider_Thinking_TemperatureOmitted(t *testing.T) {
	var reqBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&reqBody)
		resp := map[string]interface{}{
			"id": "msg_think", "type": "message", "role": "assistant",
			"content":     []map[string]interface{}{{"type": "text", "text": "ok"}},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:       "claude-sonnet-4-6",
		UserPrompt:  "hi",
		MaxTokens:   2048,
		Temperature: 0.7, // caller sets a temperature; thinking must override it
		Thinking:    &models.ThinkingConfig{Enabled: true, BudgetTokens: 1024},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When thinking is enabled the implementation silences the caller's temperature
	// (sets body.Temperature = 0 → omitempty drops it from JSON).
	// Anthropic treats an absent temperature as 1 for thinking requests.
	if _, present := reqBody["temperature"]; present {
		t.Errorf("temperature must not be sent when extended thinking is enabled, got %v", reqBody["temperature"])
	}
	// Thinking block must be present.
	thinking, ok := reqBody["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "enabled" {
		t.Errorf("thinking block missing or malformed: %v", reqBody["thinking"])
	}
	// max_tokens must still be present (always required for Claude).
	if _, ok := reqBody["max_tokens"]; !ok {
		t.Error("max_tokens must always be present in Anthropic requests")
	}
}

func TestAnthropicProvider_ServerOverload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{
		Model: "claude-sonnet-4-6", UserPrompt: "hi", MaxTokens: 10,
	})
	if _, ok := err.(*ServerOverloadError); !ok {
		t.Errorf("expected ServerOverloadError for 529, got %T: %v", err, err)
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
