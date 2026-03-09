// internal/pipeline/prompt_builder.go
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// PromptBuilderConfig holds configuration for prompt generation.
type PromptBuilderConfig struct {
	TestCommand      string
	LintCommand      string
	CodebasePatterns string
	CriteriaModel    string
	RetryFeedback    string
	RetryErrorType   ErrorType
	Attempt          int
}

// PromptBuilder generates structured prompts for external agent runners.
type PromptBuilder struct {
	llm LLMProvider // optional — used for criteria reformulation
}

// NewPromptBuilder creates a PromptBuilder. llm may be nil to skip reformulation.
func NewPromptBuilder(llm LLMProvider) *PromptBuilder {
	return &PromptBuilder{llm: llm}
}

// Build generates a self-contained prompt from task, context files, and config.
func (pb *PromptBuilder) Build(task *models.Task, contextFiles map[string]string, config PromptBuilderConfig) string {
	var b strings.Builder

	// Task section
	fmt.Fprintf(&b, "## Task\n**%s**\n\n", task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "**Description:** %s\n\n", task.Description)
	}

	// Expected Behaviors (acceptance criteria)
	if len(task.AcceptanceCriteria) > 0 {
		criteria := pb.reformulateCriteria(task.AcceptanceCriteria, config.CriteriaModel)
		b.WriteString("## Expected Behaviors\n")
		for _, c := range criteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	// Affected Area Hints
	if len(task.FilesToRead) > 0 || len(task.FilesToModify) > 0 {
		b.WriteString("## Affected Area Hints\n")
		for _, f := range task.FilesToModify {
			fmt.Fprintf(&b, "- Modify: `%s`\n", f)
		}
		for _, f := range task.FilesToRead {
			fmt.Fprintf(&b, "- Reference: `%s`\n", f)
		}
		b.WriteString("\n")
	}

	// Codebase Context
	if len(contextFiles) > 0 {
		b.WriteString("## Codebase Context\n")
		paths := make([]string, 0, len(contextFiles))
		for p := range contextFiles {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", p, contextFiles[p])
		}
	}

	// Constraints
	if config.CodebasePatterns != "" {
		fmt.Fprintf(&b, "## Constraints\n%s\n\n", config.CodebasePatterns)
	}

	// Definition of Done
	b.WriteString("## Definition of Done\n")
	if config.TestCommand != "" {
		fmt.Fprintf(&b, "- All tests pass: `%s`\n", config.TestCommand)
	}
	if config.LintCommand != "" {
		fmt.Fprintf(&b, "- Lint passes: `%s`\n", config.LintCommand)
	}
	if config.TestCommand == "" && config.LintCommand == "" {
		b.WriteString("- Code compiles and works correctly\n")
	}
	b.WriteString("\n")

	// Retry Context (attempt > 1 only)
	if config.Attempt > 1 && config.RetryFeedback != "" {
		heading, guidance := promptBuilderRetryHeadingAndGuidance(config.RetryErrorType)
		fmt.Fprintf(&b, "## RETRY — %s (attempt %d)\n\n", heading, config.Attempt)
		if guidance != "" {
			fmt.Fprintf(&b, "%s\n\n", guidance)
		}
		fmt.Fprintf(&b, "Previous failure:\n%s\n", config.RetryFeedback)
	}

	return b.String()
}

// reformulateCriteria uses LLM to transform raw acceptance criteria into
// mechanically verifiable assertions. Falls back to raw criteria on failure.
func (pb *PromptBuilder) reformulateCriteria(criteria []string, model string) []string {
	if pb.llm == nil || model == "" {
		return criteria
	}

	prompt := "Reformulate each acceptance criterion into a mechanically verifiable assertion. " +
		"Each should start with 'VERIFY:' and describe an observable, testable behavior.\n\n"
	for _, c := range criteria {
		prompt += "- " + c + "\n"
	}

	resp, err := pb.llm.Complete(context.Background(), models.LlmRequest{
		Model:        model,
		SystemPrompt: "You reformulate acceptance criteria into precise, testable assertions.",
		UserPrompt:   prompt,
		MaxTokens:    1024,
		Temperature:  0.0,
		Stage:        "criteria_reformulation",
	})
	if err != nil {
		log.Warn().Err(err).Msg("prompt_builder: criteria reformulation failed, using raw criteria")
		return criteria
	}

	// Parse response lines
	lines := strings.Split(strings.TrimSpace(resp.Content), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if line != "" {
			result = append(result, line)
		}
	}
	if len(result) == 0 {
		return criteria
	}
	return result
}

// promptBuilderRetryHeadingAndGuidance returns the heading label and per-error-type
// guidance text for use in PromptBuilder's retry section. Unlike the implementer's
// retryHeadingAndGuidance, this returns just the label (e.g. "Test Runtime") so the
// caller can format the full heading with an attempt number.
func promptBuilderRetryHeadingAndGuidance(errType ErrorType) (heading, guidance string) {
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
		return "Failure", ""
	}
}
