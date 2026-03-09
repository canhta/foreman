// internal/pipeline/prompt_builder_test.go
package pipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

// mockLLMProvider is a simple mock for LLMProvider used by PromptBuilder tests.
// It returns a canned response or a fixed error.
type mockLLMProvider struct {
	response *models.LlmResponse
	err      error
}

func (m *mockLLMProvider) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockLLMProvider) ProviderName() string                { return "mock" }
func (m *mockLLMProvider) HealthCheck(_ context.Context) error { return nil }

func TestPromptBuilder_Build_BasicTask(t *testing.T) {
	pb := NewPromptBuilder(nil) // no LLM = skip criteria reformulation
	task := &models.Task{
		Title:              "Add user validation",
		Description:        "Validate email format on signup",
		AcceptanceCriteria: []string{"Invalid emails are rejected with 400"},
		FilesToModify:      []string{"internal/api/handler.go"},
	}
	config := PromptBuilderConfig{
		TestCommand:      "go test ./...",
		CodebasePatterns: "Go, stdlib net/http",
	}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "Add user validation")
	assert.Contains(t, prompt, "Validate email format on signup")
	assert.Contains(t, prompt, "Invalid emails are rejected with 400")
	assert.Contains(t, prompt, "go test ./...")
	assert.Contains(t, prompt, "internal/api/handler.go")
}

func TestPromptBuilder_Build_WithContextFiles(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Fix bug",
		Description: "Null pointer in handler",
	}
	contextFiles := map[string]string{
		"handler.go": "package api\nfunc Handle() {}",
	}
	config := PromptBuilderConfig{}

	prompt := pb.Build(task, contextFiles, config)

	assert.Contains(t, prompt, "handler.go")
	assert.Contains(t, prompt, "package api")
}

func TestPromptBuilder_Build_WithRetryFeedback(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Fix bug",
		Description: "Null pointer",
	}
	config := PromptBuilderConfig{
		Attempt:        2,
		RetryFeedback:  "test failed: nil pointer dereference",
		RetryErrorType: ErrorTypeTestRuntime,
	}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "RETRY")
	assert.Contains(t, prompt, "nil pointer dereference")
	assert.Contains(t, prompt, "runtime")
}

func TestPromptBuilder_Build_NoRetryOnFirstAttempt(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Add feature",
		Description: "New endpoint",
	}
	config := PromptBuilderConfig{Attempt: 1}

	prompt := pb.Build(task, nil, config)

	assert.NotContains(t, prompt, "RETRY")
}

func TestPromptBuilder_Build_WithLLMReformulation(t *testing.T) {
	mockLLM := &mockLLMProvider{
		response: &models.LlmResponse{Content: "- VERIFY: POST /signup with invalid email returns HTTP 400"},
	}
	pb := NewPromptBuilder(mockLLM)
	task := &models.Task{
		Title:              "Add validation",
		Description:        "Validate emails",
		AcceptanceCriteria: []string{"Invalid emails rejected"},
	}
	config := PromptBuilderConfig{CriteriaModel: "claude-haiku-4-5-20251001"}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "VERIFY: POST /signup")
}

func TestPromptBuilder_Build_LLMFailureFallsBackToRawCriteria(t *testing.T) {
	mockLLM := &mockLLMProvider{err: fmt.Errorf("API error")}
	pb := NewPromptBuilder(mockLLM)
	task := &models.Task{
		Title:              "Add validation",
		Description:        "Validate emails",
		AcceptanceCriteria: []string{"Invalid emails rejected"},
	}
	config := PromptBuilderConfig{CriteriaModel: "claude-haiku-4-5-20251001"}

	prompt := pb.Build(task, nil, config)

	// Falls back to raw criteria
	assert.Contains(t, prompt, "Invalid emails rejected")
}
