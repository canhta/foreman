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

func TestOpenAIProvider_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()
	provider := NewOpenAIProvider("bad-key", srv.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10})
	if _, ok := err.(*AuthError); !ok {
		t.Errorf("expected AuthError, got %T: %v", err, err)
	}
}

func TestOpenAIProvider_ConnectionError(t *testing.T) {
	provider := NewOpenAIProvider("key", "http://localhost:19999") // nothing listening
	_, err := provider.Complete(context.Background(), models.LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10})
	if _, ok := err.(*ConnectionError); !ok {
		t.Errorf("expected ConnectionError, got %T: %v", err, err)
	}
}

func TestOpenAIProvider_HealthCheck_UsesDefaultModel(t *testing.T) {
	var requestedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		requestedModel, _ = body["model"].(string)
		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": requestedModel,
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestedModel != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", requestedModel)
	}
}

func TestLocalProvider_HealthCheck_UsesDefaultModel(t *testing.T) {
	var requestedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		requestedModel, _ = body["model"].(string)
		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": requestedModel,
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewLocalProvider(srv.URL)
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestedModel != "llama3" {
		t.Errorf("expected llama3, got %s", requestedModel)
	}
}

func TestOpenAIProvider_TemperatureZeroNotOmitted(t *testing.T) {
	var requestBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&requestBody)
		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10, Temperature: 0.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := requestBody["temperature"]; !ok {
		t.Error("temperature field was omitted from request body, expected it to be present with value 0")
	}
}
