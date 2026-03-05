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
