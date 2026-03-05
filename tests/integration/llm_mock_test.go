package integration

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

type MockLlmProvider struct {
	responses map[string]string
}

func (m *MockLlmProvider) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	content, ok := m.responses[req.Model]
	if !ok {
		content = "mock response"
	}
	return &models.LlmResponse{
		Content:      content,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        req.Model,
		DurationMs:   100,
		StopReason:   "end_turn",
	}, nil
}

func (m *MockLlmProvider) ProviderName() string                { return "mock" }
func (m *MockLlmProvider) HealthCheck(_ context.Context) error { return nil }

// Ensure MockLlmProvider satisfies the llm.LlmProvider interface.
var _ llm.LlmProvider = (*MockLlmProvider)(nil)

func TestMockLlmProvider(t *testing.T) {
	mock := &MockLlmProvider{responses: map[string]string{
		"test-model": "hello world",
	}}
	resp, err := mock.Complete(context.Background(), models.LlmRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected hello world, got %s", resp.Content)
	}
}
