// internal/pipeline/implementer.go
package pipeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
)

// Implementer generates code changes for a task via LLM using TDD.
type Implementer struct {
	llm      LLMProvider
	registry *prompts.Registry
}

// NewImplementer creates an Implementer with the given LLM provider.
func NewImplementer(provider LLMProvider) *Implementer {
	return &Implementer{llm: provider}
}

// WithRegistry attaches a prompt registry so the implementer uses registry.Render()
// for the system prompt instead of the hardcoded buildImplementerSystemPrompt().
func (impl *Implementer) WithRegistry(reg *prompts.Registry) *Implementer {
	impl.registry = reg
	return impl
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
	var systemPrompt string
	if impl.registry != nil {
		rendered, err := impl.registry.Render(prompts.KindRole, "implementer", map[string]any{
			"task_title":          input.Task.Title,
			"task_description":    input.Task.Description,
			"acceptance_criteria": input.Task.AcceptanceCriteria,
			"context_files":       input.ContextFiles,
			"codebase_patterns":   "",
		})
		if err != nil {
			return nil, fmt.Errorf("render implementer prompt: %w", err)
		}
		systemPrompt = rendered
	} else {
		systemPrompt = buildImplementerSystemPrompt()
	}
	userPrompt := buildImplementerUserPrompt(input)

	resp, err := impl.llm.Complete(ctx, models.LlmRequest{
		Model:             input.Model,
		SystemPrompt:      systemPrompt,
		UserPrompt:        userPrompt,
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

// retryLabelAndGuidance returns the short label (e.g. "Compile Error") and the
// per-error-type guidance paragraph for a retry prompt section. Both
// retryHeadingAndGuidance (implementer) and promptBuilderRetryHeadingAndGuidance
// (prompt_builder) delegate here to avoid duplicating the case switch.
func retryLabelAndGuidance(errType ErrorType) (label, guidance string) {
	switch errType {
	case ErrorTypeCompile:
		return "Compile Error",
			"Focus on fixing the build error. Check import paths, undefined symbols, and missing return statements. Do not refactor unrelated code."
	case ErrorTypeTypeError:
		return "Type Error",
			"Focus on fixing the type mismatch. Verify interface implementations, check function signatures, and ensure correct type assertions."
	case ErrorTypeLintStyle:
		return "Lint/Style",
			"Focus on fixing the lint/style issues listed below. Do not rewrite working logic."
	case ErrorTypeTestAssertion:
		return "Test Assertion",
			"Focus on making the failing test assertions pass. Read the expected vs actual values carefully and adjust implementation, not tests."
	case ErrorTypeTestRuntime:
		return "Test Runtime",
			"Focus on preventing the runtime panic. Check nil pointer dereferences, slice/map bounds, and error returns before use."
	case ErrorTypeSpecViolation:
		return "Spec Violation",
			"Focus on satisfying the acceptance criteria listed below. Do not change code unrelated to the failing criteria."
	case ErrorTypeQualityConcern:
		return "Quality Concern",
			"Focus on addressing the quality concerns listed below. Refactor only the flagged areas."
	default:
		return "", ""
	}
}

// retryHeadingAndGuidance returns the markdown heading and per-error-type guidance
// paragraph for a retry prompt section. For unknown/zero-value error types it
// returns the legacy generic heading and no guidance (backward compatible).
func retryHeadingAndGuidance(errType ErrorType, attempt int) (heading, guidance string) {
	label, guidance := retryLabelAndGuidance(errType)
	if label == "" {
		// ErrorTypeUnknown or zero value: preserve the original generic header.
		return fmt.Sprintf("## RETRY (attempt %d)\n\n", attempt), ""
	}
	return fmt.Sprintf("## RETRY — %s (attempt %d)\n\n", label, attempt), guidance
}

func buildImplementerSystemPrompt() string {
	return `You are an expert software engineer implementing a task using TDD.

## TDD Rules (MANDATORY)
1. Write tests FIRST that capture the acceptance criteria
2. Tests must be runnable and fail for the right reason before implementation
3. Write minimal implementation to make tests pass
4. Never skip writing tests

## Output Format
Output ONLY machine-parseable file blocks. Do not include explanations.

For NEW files:
=== NEW FILE: path/to/file.ext ===
<complete file content>
=== END FILE ===

For EXISTING files:
=== MODIFY FILE: path/to/file.ext ===
<<<< SEARCH
<exact existing lines>
>>>>
<<<< REPLACE
<replacement lines>
>>>>
=== END FILE ===

Rules:
- Output test files before implementation files.
- Include at least 3 lines in each SEARCH block.
- Preserve indentation/whitespace exactly.
- Do not output markdown fences.`
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
		heading, guidance := retryHeadingAndGuidance(input.RetryErrorType, input.Attempt)
		prompt += heading
		if guidance != "" {
			prompt += guidance + "\n\n"
		}
		prompt += input.Feedback + "\n\n"
	}

	return prompt
}
