package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestLocalProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":    "local-1",
			"model": "llama3",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"role": "assistant", "content": "Local reply"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewLocalProvider(srv.URL)
	if provider.ProviderName() != "local" {
		t.Errorf("expected local, got %s", provider.ProviderName())
	}

	resp, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:      "llama3",
		UserPrompt: "Hi",
		MaxTokens:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Local reply" {
		t.Errorf("expected Local reply, got %s", resp.Content)
	}
}
