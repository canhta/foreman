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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}

		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Hello!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("test-key", srv.URL)

	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:        "gpt-4o",
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Hi",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected Hello!, got %s", resp.Content)
	}
	if resp.TokensInput != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.TokensInput)
	}
}

func TestOpenAIProvider_ProviderName(t *testing.T) {
	p := NewOpenAIProvider("key", "")
	if p.ProviderName() != "openai" {
		t.Errorf("expected openai, got %s", p.ProviderName())
	}
}

func TestOpenAIProvider_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10})

	if _, ok := err.(*RateLimitError); !ok {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}
