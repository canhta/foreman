// internal/pipeline/implementer.go
package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
)

// Implementer generates code changes for a task via LLM using TDD.
type Implementer struct {
	llm      LLMProvider
	registry *prompts.Registry
}

// NewImplementer creates an Implementer with the given LLM provider and registry.
// Registry is required; NewImplementer panics if reg is nil.
func NewImplementer(provider LLMProvider, reg *prompts.Registry) *Implementer {
	if reg == nil {
		panic("implementer: registry must not be nil")
	}
	return &Implementer{llm: provider, registry: reg}
}

// ImplementerInput holds all parameters for a single implementer call.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type ImplementerInput struct {
	Task           *models.Task
	ContextFiles   map[string]string
	Model          string
	Feedback       string
	PromptVersion  string
	MaxTokens      int
	Attempt        int
	RetryErrorType ErrorType
}

// ImplementerResult holds the raw LLM response from the implementer.
type ImplementerResult struct {
	Response *models.LlmResponse
}

// Execute runs the implementer step and returns the LLM response.
func (impl *Implementer) Execute(ctx context.Context, input ImplementerInput) (*ImplementerResult, error) {
	roleName := "implementer"
	if input.Attempt > 1 {
		roleName = "implementer-retry"
	}

	vars := map[string]any{
		"task_title":          input.Task.Title,
		"task_description":    input.Task.Description,
		"acceptance_criteria": input.Task.AcceptanceCriteria,
		"context_files":       input.ContextFiles,
		"codebase_patterns":   "",
		"attempt":             input.Attempt,
		"max_attempts":        5,
		"retry_feedback":      input.Feedback,
		"retry_error_type":    string(input.RetryErrorType),
	}

	// Map structured feedback into template-expected variables for retry role.
	if input.Attempt > 1 && input.Feedback != "" {
		switch input.RetryErrorType {
		case ErrorTypeSpecViolation:
			vars["spec_review_feedback"] = input.Feedback
		case ErrorTypeQualityConcern:
			vars["quality_review_feedback"] = input.Feedback
		case ErrorTypeTestAssertion, ErrorTypeTestRuntime:
			vars["test_failure"] = input.Feedback
		default:
			vars["tdd_failure"] = input.Feedback
		}
	}

	rendered, err := impl.registry.Render(prompts.KindRole, roleName, vars)
	if err != nil {
		return nil, fmt.Errorf("render %s prompt: %w", roleName, err)
	}

	resp, err := impl.llm.Complete(ctx, models.LlmRequest{
		Model:             input.Model,
		SystemPrompt:      rendered,
		UserPrompt:        "Implement the task.",
		PromptVersion:     input.PromptVersion,
		Stage:             "implementing",
		MaxTokens:         input.MaxTokens,
		Temperature:       0.0,
		CacheSystemPrompt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("implementer LLM call: %w", err)
	}

	return &ImplementerResult{Response: resp}, nil
}

// retryErrorTypeLabel maps an ErrorType to its short label.
// Used only for metrics/logging — not for prompt generation.
func retryErrorTypeLabel(errType ErrorType) string {
	switch errType {
	case ErrorTypeCompile:
		return "Compile Error"
	case ErrorTypeTypeError:
		return "Type Error"
	case ErrorTypeLintStyle:
		return "Lint/Style"
	case ErrorTypeTestAssertion:
		return "Test Assertion"
	case ErrorTypeTestRuntime:
		return "Test Runtime"
	case ErrorTypeSpecViolation:
		return "Spec Violation"
	case ErrorTypeQualityConcern:
		return "Quality Concern"
	default:
		return ""
	}
}
