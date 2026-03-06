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
	var requestBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&requestBody)

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

	// Verify messages array structure: system message first, then user message.
	msgs, _ := requestBody["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	sys, _ := msgs[0].(map[string]interface{})
	usr, _ := msgs[1].(map[string]interface{})
	if sys["role"] != "system" || sys["content"] != "You are a helper." {
		t.Errorf("unexpected system message: %v", sys)
	}
	if usr["role"] != "user" || usr["content"] != "Hi" {
		t.Errorf("unexpected user message: %v", usr)
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
	// Use a server that is immediately closed so the port is guaranteed to refuse connections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	provider := NewOpenAIProvider("key", url)
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

func TestIsReasoningModel(t *testing.T) {
	cases := []struct {
		model    string
		expected bool
	}{
		// o-series
		{"o1", true},
		{"o1-mini", true},
		{"o1-preview", true},
		{"o3", true},
		{"o3-mini", true},
		{"o3-pro", true},
		{"o3-deep-research", true},
		{"o4-mini", true},
		{"o4-mini-deep-research", true},
		// gpt-5 family
		{"gpt-5", true},
		{"gpt-5.4", true},
		{"gpt-5.4-pro", true},
		{"gpt-5-mini", true},
		{"gpt-5-nano", true},
		{"gpt-5.3-codex", true},
		// gpt-5.1 is an official gpt-5 family model — must be true
		{"gpt-5.1", true},
		// non-reasoning
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"gpt-4", false},
		{"gpt-4.1", false},
		{"", false},
		{"o", false},
	}
	for _, tc := range cases {
		got := isReasoningModel(tc.model)
		if got != tc.expected {
			t.Errorf("isReasoningModel(%q) = %v, want %v", tc.model, got, tc.expected)
		}
	}
}

func testReasoningModelRequest(t *testing.T, modelName string) {
	t.Helper()
	var requestBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&requestBody)
		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": modelName,
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	_, err := provider.Complete(context.Background(), models.LlmRequest{Model: modelName, UserPrompt: "hi", MaxTokens: 4096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := requestBody["max_tokens"]; ok {
		t.Errorf("model %s: max_tokens must not be sent for reasoning models", modelName)
	}
	if v, ok := requestBody["max_completion_tokens"]; !ok {
		t.Errorf("model %s: max_completion_tokens must be sent for reasoning models", modelName)
	} else if int(v.(float64)) != 4096 {
		t.Errorf("model %s: max_completion_tokens = %v, want 4096", modelName, v)
	}
	if _, ok := requestBody["temperature"]; ok {
		t.Errorf("model %s: temperature must not be sent for reasoning models", modelName)
	}
}

func TestOpenAIProvider_ReasoningModel_UsesMaxCompletionTokens(t *testing.T) {
	for _, model := range []string{
		"o4-mini", "o1-mini", "o3", "o3-pro",
		"gpt-5", "gpt-5.4", "gpt-5.4-pro", "gpt-5-mini", "gpt-5-nano", "gpt-5.3-codex",
	} {
		t.Run(model, func(t *testing.T) { testReasoningModelRequest(t, model) })
	}
}

func TestOpenAIProvider_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	resp, err := provider.Complete(context.Background(), models.LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10})
	if err != nil {
		t.Fatalf("unexpected error on empty choices: %v", err)
	}
	// Empty choices → empty content and stop reason, not a panic.
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
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
