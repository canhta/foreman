// internal/context/assembler.go
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/canhta/foreman/internal/models"
)

// AssembledContext holds the prompts ready to send to an LLM.
type AssembledContext struct {
	SystemPrompt string
	UserPrompt   string
}

// DynamicContextBudget scales a base token budget by task complexity.
// Complexity values: "low" -> 50%, "medium" -> 100%, "high" -> 150% (capped at maxBudget).
// maxBudget should be modelContextWindow - maxOutputTokens. Use 0 for no cap.
func DynamicContextBudget(base int, complexity string, maxBudget int) int {
	var budget int
	switch strings.ToLower(strings.TrimSpace(complexity)) {
	case "low", "simple":
		budget = base / 2
	case "high", "complex":
		budget = base * 3 / 2
	default: // medium or unknown
		budget = base
	}
	if maxBudget > 0 && budget > maxBudget {
		budget = maxBudget
	}
	return budget
}

// FeedbackContext holds retry information for implementer retries.
type FeedbackContext struct {
	PreviousError   string
	SpecFeedback    string
	QualityFeedback string
	TDDFeedback     string
	Attempt         int
	MaxAttempts     int
}

// AssemblePlannerContext builds the context for a planner LLM call.
// cache is optional (nil = no caching).
func AssemblePlannerContext(workDir string, ticket *models.Ticket, tokenBudget int, cache *ContextCache) (*AssembledContext, error) {
	repoInfo, err := GetOrAnalyzeRepo(cache, workDir)
	if err != nil {
		return nil, fmt.Errorf("analyzing repo: %w", err)
	}

	system := `You are a senior software engineer decomposing a ticket into implementation tasks.

## Your Job
Produce an ordered list of granular tasks. Each task: 2-5 minutes, completable by an
AI agent that has no memory between tasks and follows strict TDD.

## Rules
1. Each task MUST specify exact file paths to read and modify.
2. Each task MUST include test assertions.
3. Each task MUST have acceptance criteria verifiable from the diff alone.
4. Tasks are executed in strict sequential order.
5. Maximum 20 tasks. If more needed, output TICKET_TOO_LARGE with explanation.
6. Do NOT include setup tasks (branching, env). The system handles those.
7. Every task includes its own tests — do NOT have separate "write tests" tasks.
8. For EXISTING files, describe what to ADD or CHANGE, not the full file.
9. If the ticket lacks enough detail, output CLARIFICATION_NEEDED with a specific question.

## Output Format (YAML — strict)
status: OK | TICKET_TOO_LARGE | CLARIFICATION_NEEDED
message: "<explanation if not OK>"
codebase_patterns:
  language: "<detected>"
  framework: "<detected>"
  test_runner: "<detected>"
  style_notes: "<key conventions>"
tasks:
  - title: "<short title>"
    description: |
      <Detailed description>
    acceptance_criteria:
      - "<verifiable from diff>"
    test_assertions:
      - "<what the test should assert>"
    files_to_read:
      - "<path>"
    files_to_modify:
      - "<path> (new)" or "<existing path>"
    estimated_complexity: "simple|medium|complex"
    depends_on: []  # list of task TITLES this task depends on; use exact titles, never numbers or indices

Do NOT wrap the YAML in markdown fences. Output ONLY the YAML.`

	var user strings.Builder
	fmt.Fprintf(&user, "## Ticket\nTitle: %s\n\nDescription:\n%s\n\n", ticket.Title, ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&user, "Acceptance Criteria:\n%s\n\n", ticket.AcceptanceCriteria)
	}

	fmt.Fprintf(&user, "## Repository\n### File Tree\n```\n%s\n```\n\n", repoInfo.FileTree)

	// Read README if it exists
	if content, err := readFileTruncated(filepath.Join(workDir, "README.md"), 3000); err == nil {
		fmt.Fprintf(&user, "### README\n%s\n\n", content)
	}

	// Read .foreman-context.md if it exists
	if content, err := os.ReadFile(filepath.Join(workDir, ".foreman-context.md")); err == nil {
		fmt.Fprintf(&user, "## Project-Specific Context\n%s\n\n", string(content))
	}

	return &AssembledContext{
		SystemPrompt: system,
		UserPrompt:   user.String(),
	}, nil
}

// AssembleImplementerContext builds the context for an implementer LLM call.
// cache is optional (nil = no caching).
// fq is an optional FeedbackQuerier for REQ-CTX-003 feedback boosting; pass nil to disable.
// feedbackBoost is the score multiplier applied to files that appeared in prior similar tasks.
func AssembleImplementerContext(workDir string, task *models.Task, feedback *FeedbackContext, tokenBudget int, cache *ContextCache, fq FeedbackQuerier, feedbackBoost float64) (*AssembledContext, error) {
	repoInfo, err := GetOrAnalyzeRepo(cache, workDir)
	if err != nil {
		return nil, fmt.Errorf("analyzing repo: %w", err)
	}

	system := `You are implementing a single task using strict Test-Driven Development.

## TDD Rules (MANDATORY)
1. RED: Write a failing test first. The test MUST compile and run, but FAIL on assertions.
   - Do NOT write tests that fail due to import errors or missing modules.
   - The test must execute and produce assertion failures, not compile errors.
2. GREEN: Write MINIMAL code to make the test pass.
3. Do NOT add anything not in the task spec.

## Output Format
For NEW files, use:
=== NEW FILE: path/to/file.ext ===
<complete contents>
=== END FILE ===

For EXISTING files, use search-and-replace blocks:
=== MODIFY FILE: path/to/file.ext ===
<<<< SEARCH
<exact lines to find — include at least 3 lines of context>
>>>>
<<<< REPLACE
<replacement lines>
>>>>
=== END FILE ===

IMPORTANT:
- Each SEARCH block must include at least 3 lines of surrounding context.
- Match the existing file's indentation and whitespace exactly.
- ALWAYS output test files BEFORE implementation files.`

	// Select files for context
	files, err := SelectFilesForTask(task, workDir, tokenBudget/2, cache, fq, feedbackBoost)
	if err != nil {
		return nil, fmt.Errorf("selecting files: %w", err)
	}

	// Filter secrets
	scanner := NewSecretsScanner(&models.SecretsConfig{
		Enabled:       true,
		AlwaysExclude: []string{".env", ".env.*", "*.pem", "*.key"},
	})
	files = filterSecrets(scanner, files, workDir)

	var user strings.Builder

	// Feedback section for retries
	if feedback != nil {
		fmt.Fprintf(&user, "## RETRY (attempt %d/%d)\n", feedback.Attempt, feedback.MaxAttempts)
		if feedback.PreviousError != "" {
			fmt.Fprintf(&user, "Previous error:\n```\n%s\n```\n\n", feedback.PreviousError)
		}
		if feedback.SpecFeedback != "" {
			fmt.Fprintf(&user, "## SPEC REVIEWER FOUND ISSUES\n%s\n\n", feedback.SpecFeedback)
		}
		if feedback.QualityFeedback != "" {
			fmt.Fprintf(&user, "## QUALITY REVIEWER FOUND ISSUES\n%s\n\n", feedback.QualityFeedback)
		}
		if feedback.TDDFeedback != "" {
			fmt.Fprintf(&user, "## TDD VERIFICATION FAILED\n%s\n\n", feedback.TDDFeedback)
		}
	}

	// Task section
	fmt.Fprintf(&user, "## Task\nTitle: %s\nDescription:\n%s\n\n", task.Title, task.Description)
	user.WriteString("Acceptance Criteria:\n")
	for _, c := range task.AcceptanceCriteria {
		fmt.Fprintf(&user, "- %s\n", c)
	}
	user.WriteString("\nTest Assertions:\n")
	for _, a := range task.TestAssertions {
		fmt.Fprintf(&user, "- %s\n", a)
	}

	// Commands
	fmt.Fprintf(&user, "\n## Commands\nBuild: `%s`  Test: `%s`  Lint: `%s`\n\n",
		repoInfo.BuildCmd, repoInfo.TestCmd, repoInfo.LintCmd)

	// File contents — enforce remaining token budget (second half)
	fileBudget := NewTokenBudget(tokenBudget / 2)
	user.WriteString("## Files\n")
	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(workDir, f.Path))
		if err != nil {
			continue
		}
		tokens := EstimateTokens(string(content))
		if !fileBudget.CanFit(tokens) {
			continue
		}
		fileBudget.Add(tokens)
		ext := strings.TrimPrefix(filepath.Ext(f.Path), ".")
		fmt.Fprintf(&user, "### %s\n```%s\n%s\n```\n\n", f.Path, ext, string(content))
	}

	return &AssembledContext{
		SystemPrompt: system,
		UserPrompt:   user.String(),
	}, nil
}

// AssembleSpecReviewerContext builds context for spec review.
func AssembleSpecReviewerContext(taskTitle string, criteria []string, diff, testOutput string) *AssembledContext {
	system := `You verify that the implementation satisfies every acceptance criterion. Nothing more.

## Rules
1. Check EVERY criterion. Mark pass or fail.
2. Flag any extra functionality not requested (YAGNI).
3. Do NOT comment on code quality or style.
4. Be specific — say exactly what's missing and where.

## Output Format
STATUS: APPROVED | REJECTED
CRITERIA:
- [pass/fail] <criterion>
ISSUES:
- <what's missing, which file, what's needed>
EXTRAS:
- <anything not requested>`

	var user strings.Builder
	fmt.Fprintf(&user, "## Task\n%s\n\nCriteria:\n", taskTitle)
	for _, c := range criteria {
		fmt.Fprintf(&user, "- %s\n", c)
	}
	fmt.Fprintf(&user, "\n## Diff\n```diff\n%s\n```\n\n## Test Output\n```\n%s\n```\n", diff, testOutput)

	return &AssembledContext{
		SystemPrompt: system,
		UserPrompt:   user.String(),
	}
}

// AssembleQualityReviewerContext builds context for quality review.
func AssembleQualityReviewerContext(diff, codebasePatterns string) *AssembledContext {
	system := `You review code quality only. Do NOT check spec compliance.

## Check
- Style matches codebase patterns
- Naming consistency
- Error handling
- No obvious bugs/edge cases
- DRY
- Tests are meaningful
- No security issues (hardcoded secrets, injection, XSS)
- No performance anti-patterns

## Severity
- CRITICAL: Must fix. Security, data loss, production breakage.
- IMPORTANT: Should fix. Code smell, subtle bug.
- MINOR: Nice to fix. Does NOT block approval.

## Output Format
STATUS: APPROVED | CHANGES_REQUESTED
ISSUES:
- [CRITICAL|IMPORTANT|MINOR] <file, issue, fix suggestion>
STRENGTHS:
- <what was done well>`

	var user strings.Builder
	fmt.Fprintf(&user, "## Codebase Patterns\n%s\n\n## Diff\n```diff\n%s\n```\n", codebasePatterns, diff)

	return &AssembledContext{
		SystemPrompt: system,
		UserPrompt:   user.String(),
	}
}

func filterSecrets(scanner *SecretsScanner, files []ScoredFile, workDir string) []ScoredFile {
	filtered := make([]ScoredFile, 0, len(files))
	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(workDir, f.Path))
		if err != nil {
			continue
		}
		result := scanner.ScanFile(f.Path, string(content))
		if result.HasSecrets {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

func readFileTruncated(path string, maxBytes int) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(content) <= maxBytes {
		return string(content), nil
	}
	// Truncate at a valid UTF-8 boundary to avoid splitting multi-byte sequences.
	truncated := content[:maxBytes]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return string(truncated) + "\n... (truncated)", nil
}
