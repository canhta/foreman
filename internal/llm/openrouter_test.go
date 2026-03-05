package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestOpenRouterProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer or-key" {
			t.Errorf("expected Bearer or-key, got %s", r.Header.Get("Authorization"))
		}

		resp := map[string]interface{}{
			"id":    "gen-123",
			"model": "anthropic/claude-3.5-sonnet",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"role": "assistant", "content": "Routed!"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenRouterProvider("or-key", srv.URL)
	if provider.ProviderName() != "openrouter" {
		t.Errorf("expected openrouter, got %s", provider.ProviderName())
	}

	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "anthropic/claude-3.5-sonnet",
		UserPrompt: "Hi",
		MaxTokens:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Routed!" {
		t.Errorf("expected Routed!, got %s", resp.Content)
	}
}
