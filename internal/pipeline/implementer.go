// internal/pipeline/implementer.go
package pipeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/canhta/foreman/internal/models"
)

// Implementer generates code changes for a task via LLM using TDD.
type Implementer struct {
	llm LLMProvider
}

// NewImplementer creates an Implementer with the given LLM provider.
func NewImplementer(provider LLMProvider) *Implementer {
	return &Implementer{llm: provider}
}

// ImplementerInput holds all parameters for a single implementer call.
type ImplementerInput struct {
	Task          *models.Task
	ContextFiles  map[string]string
	Model         string
	Feedback      string
	PromptVersion string
	MaxTokens     int
	Attempt       int
}

// ImplementerResult holds the raw LLM response from the implementer.
type ImplementerResult struct {
	Response *models.LlmResponse
}

// Execute runs the implementer step and returns the LLM response.
func (impl *Implementer) Execute(ctx context.Context, input ImplementerInput) (*ImplementerResult, error) {
	systemPrompt := buildImplementerSystemPrompt()
	userPrompt := buildImplementerUserPrompt(input)

	resp, err := impl.llm.Complete(ctx, models.LlmRequest{
		Model:         input.Model,
		SystemPrompt:  systemPrompt,
		UserPrompt:    userPrompt,
		PromptVersion: input.PromptVersion,
		Stage:         "implementing",
		MaxTokens:     input.MaxTokens,
		Temperature:   0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("implementer LLM call: %w", err)
	}

	return &ImplementerResult{Response: resp}, nil
}

func buildImplementerSystemPrompt() string {
	return `You are an expert software engineer implementing a task using TDD.

## TDD Rules (MANDATORY)
1. Write tests FIRST that capture the acceptance criteria
2. Tests must be runnable and fail for the right reason before implementation
3. Write minimal implementation to make tests pass
4. Never skip writing tests

## Output Format
Use SEARCH/REPLACE blocks for modifications and TEST blocks for new tests.`
}

func buildImplementerUserPrompt(input ImplementerInput) string {
	prompt := fmt.Sprintf("## Task\n**%s**\n\n", input.Task.Title)
	if input.Task.Description != "" {
		prompt += fmt.Sprintf("**Description:** %s\n\n", input.Task.Description)
	}

	if len(input.ContextFiles) > 0 {
		prompt += "## Codebase Context\n\n"
		keys := make([]string, 0, len(input.ContextFiles))
		for k := range input.ContextFiles {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, path := range keys {
			prompt += fmt.Sprintf("### %s\n```\n%s\n```\n\n", path, input.ContextFiles[path])
		}
	}

	if input.Attempt > 1 && input.Feedback != "" {
		prompt += fmt.Sprintf("## RETRY (attempt %d)\n\n%s\n\n", input.Attempt, input.Feedback)
	}

	return prompt
}
