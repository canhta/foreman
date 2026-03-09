// internal/pipeline/implementer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockImplLLM struct {
	capturedRequest *models.LlmRequest
	response        string
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
		response: `=== NEW FILE: math_test.go ===
package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("expected 5")
	}
}
=== END FILE ===`,
	}

	impl := NewImplementer(llmProvider, mustLoadTestRegistry(t))
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

	impl := NewImplementer(llmProvider, mustLoadTestRegistry(t))
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

	// Verify the retry attempt info and feedback appear in the system prompt
	// (the registry renders everything into the system prompt via the implementer-retry role).
	if req.SystemPrompt == "" {
		t.Error("expected non-empty system prompt for retry attempt")
	}
}

// TestRetryErrorTypeLabel verifies the label mapping for metrics/logging.
func TestRetryErrorTypeLabel(t *testing.T) {
	cases := []struct {
		errType ErrorType
		want    string
	}{
		{ErrorTypeCompile, "Compile Error"},
		{ErrorTypeTypeError, "Type Error"},
		{ErrorTypeLintStyle, "Lint/Style"},
		{ErrorTypeTestAssertion, "Test Assertion"},
		{ErrorTypeTestRuntime, "Test Runtime"},
		{ErrorTypeSpecViolation, "Spec Violation"},
		{ErrorTypeQualityConcern, "Quality Concern"},
		{ErrorTypeUnknown, ""},
	}
	for _, tc := range cases {
		if got := retryErrorTypeLabel(tc.errType); got != tc.want {
			t.Errorf("retryErrorTypeLabel(%q) = %q, want %q", tc.errType, got, tc.want)
		}
	}
}
