// internal/pipeline/implementer_test.go
package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockImplLLM struct {
	response        string
	capturedRequest *models.LlmRequest
}

func (m *mockImplLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.capturedRequest = &req
	return &models.LlmResponse{
		Content:      m.response,
		TokensInput:  500,
		TokensOutput: 300,
		Model:        req.Model,
		DurationMs:   1000,
		StopReason:   models.StopReasonEndTurn,
	}, nil
}

func (m *mockImplLLM) ProviderName() string                { return "mock" }
func (m *mockImplLLM) HealthCheck(_ context.Context) error { return nil }

func TestImplementer_Execute(t *testing.T) {
	llmProvider := &mockImplLLM{
		response: `--- SEARCH/REPLACE ---
<<<< SEARCH
func Add(a, b int) int {
	return 0
}
==== REPLACE
func Add(a, b int) int {
	return a + b
}
>>>> END

--- TEST ---
` + "```" + `go
func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("expected 5")
	}
}
` + "```",
	}

	impl := NewImplementer(llmProvider)
	result, err := impl.Execute(context.Background(), ImplementerInput{
		Task: &models.Task{
			ID:    "task-1",
			Title: "Implement Add function",
		},
		ContextFiles: map[string]string{
			"math.go": "package main\n\nfunc Add(a, b int) int {\n\treturn 0\n}\n",
		},
		Model:     "test-model",
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response")
	}
	if result.Response.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestImplementer_ExecuteRetry(t *testing.T) {
	llmProvider := &mockImplLLM{response: "retry response with fixes"}

	impl := NewImplementer(llmProvider)
	result, err := impl.Execute(context.Background(), ImplementerInput{
		Task:         &models.Task{ID: "task-1", Title: "Fix bug"},
		ContextFiles: map[string]string{"main.go": "package main"},
		Model:        "test-model",
		MaxTokens:    4096,
		Attempt:      2,
		Feedback:     "Tests failed: expected 5 got 0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response")
	}

	req := llmProvider.capturedRequest
	if req == nil {
		t.Fatal("expected LLM request to be captured")
	}

	// Verify retry section appears in user prompt with correct attempt number and feedback.
	if !strings.Contains(req.UserPrompt, "RETRY (attempt 2)") {
		t.Errorf("expected user prompt to contain 'RETRY (attempt 2)', got:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.UserPrompt, "Tests failed: expected 5 got 0") {
		t.Errorf("expected user prompt to contain feedback text, got:\n%s", req.UserPrompt)
	}
}
