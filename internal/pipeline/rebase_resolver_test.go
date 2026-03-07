// internal/pipeline/rebase_resolver_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockRebaseLLM struct {
	response string
}

func (m *mockRebaseLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response, TokensInput: 200, TokensOutput: 100, Model: req.Model, StopReason: models.StopReasonEndTurn}, nil
}
func (m *mockRebaseLLM) ProviderName() string                { return "mock" }
func (m *mockRebaseLLM) HealthCheck(_ context.Context) error { return nil }

func TestResolveConflict(t *testing.T) {
	llmProvider := &mockRebaseLLM{
		response: `<<<< RESOLVED
func Add(a, b int) int {
	return a + b
}
>>>> END`,
	}

	conflictDiff := `<<<<<<< HEAD
func Add(a, b int) int {
	return a + b
}
=======
func Add(a, b int) int {
	return a - b
}
>>>>>>> feature`

	result, err := AttemptConflictResolution(context.Background(), llmProvider, conflictDiff, "", "", "test-model", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolved == "" {
		t.Error("expected resolved content")
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestResolveConflict_Failure(t *testing.T) {
	llmProvider := &mockRebaseLLM{
		response: "I cannot resolve this conflict automatically.",
	}

	result, err := AttemptConflictResolution(context.Background(), llmProvider, "some conflict", "", "", "test-model", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure — LLM didn't produce RESOLVED block")
	}
}
