# Phase 7: Gaps Backfill — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Backfill missing features that belong to Phases 1-3 but were omitted from the original plans: pipeline implementer step, runner output parser, prompt templates, context helpers (rules, progress), go-git fallback, plan validator cost estimation, LLM call cap enforcement, forbidden file patterns, built-in skill YAML files, LLM-assisted rebase conflict resolution, and full test suite pre-PR gate.

**Architecture:** Each task is an independent module that plugs into existing interfaces. Prompt templates use pongo2 (already a dependency). The implementer step follows the same pattern as spec/quality reviewers. go-git implements the existing `git.GitProvider` interface.

**Tech Stack:** Go 1.23+, pongo2 (templates), go-git/v5 (fallback git), existing Phase 1-3 packages

---

### Task 1: Pipeline Implementer Step

**Files:**
- Create: `internal/pipeline/implementer.go`
- Create: `internal/pipeline/implementer_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/implementer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

type mockImplLLM struct {
	response string
}

func (m *mockImplLLM) Complete(_ context.Context, req llm.LlmRequest) (*llm.LlmResponse, error) {
	return &llm.LlmResponse{
		Content:      m.response,
		TokensInput:  500,
		TokensOutput: 300,
		Model:        req.Model,
		DurationMs:   1000,
		StopReason:   "end_turn",
	}, nil
}

func (m *mockImplLLM) ProviderName() string              { return "mock" }
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
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestImplementer -v`
Expected: FAIL — NewImplementer not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/implementer.go
package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

type Implementer struct {
	llm llm.LlmProvider
}

func NewImplementer(provider llm.LlmProvider) *Implementer {
	return &Implementer{llm: provider}
}

type ImplementerInput struct {
	Task         *models.Task
	ContextFiles map[string]string
	Model        string
	MaxTokens    int
	Attempt      int
	Feedback     string
}

type ImplementerResult struct {
	Response *llm.LlmResponse
}

func (impl *Implementer) Execute(ctx context.Context, input ImplementerInput) (*ImplementerResult, error) {
	systemPrompt := buildImplementerSystemPrompt()
	userPrompt := buildImplementerUserPrompt(input)

	resp, err := impl.llm.Complete(ctx, llm.LlmRequest{
		Model:        input.Model,
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    input.MaxTokens,
		Temperature:  0.0,
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
		for path, content := range input.ContextFiles {
			prompt += fmt.Sprintf("### %s\n```\n%s\n```\n\n", path, content)
		}
	}

	if input.Attempt > 1 && input.Feedback != "" {
		prompt += fmt.Sprintf("## RETRY (attempt %d)\n\n%s\n\n", input.Attempt, input.Feedback)
	}

	return prompt
}
```

**Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestImplementer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/implementer.go internal/pipeline/implementer_test.go
git commit -m "feat(pipeline): add implementer step with TDD prompt"
```

---

### Task 2: Runner Output Parser

**Files:**
- Create: `internal/runner/output_parser.go`
- Create: `internal/runner/output_parser_test.go`

**Step 1: Write the failing test**

```go
// internal/runner/output_parser_test.go
package runner

import (
	"testing"
)

func TestParseTestOutput_GoPass(t *testing.T) {
	output := `=== RUN   TestAdd
--- PASS: TestAdd (0.00s)
PASS
ok  	example.com/pkg	0.003s`

	result := ParseTestOutput(output, "go")
	if !result.Passed {
		t.Error("expected passed")
	}
	if result.TotalTests != 1 {
		t.Errorf("expected 1 test, got %d", result.TotalTests)
	}
	if result.PassedTests != 1 {
		t.Errorf("expected 1 passed, got %d", result.PassedTests)
	}
}

func TestParseTestOutput_GoFail(t *testing.T) {
	output := `=== RUN   TestAdd
--- FAIL: TestAdd (0.00s)
    add_test.go:8: expected 5, got 0
FAIL
FAIL	example.com/pkg	0.003s`

	result := ParseTestOutput(output, "go")
	if result.Passed {
		t.Error("expected failed")
	}
	if result.FailedTests != 1 {
		t.Errorf("expected 1 failed, got %d", result.FailedTests)
	}
	if len(result.Failures) == 0 {
		t.Error("expected failure details")
	}
}

func TestParseLintOutput_Clean(t *testing.T) {
	result := ParseLintOutput("", "go")
	if !result.Clean {
		t.Error("expected clean lint")
	}
}

func TestParseLintOutput_WithErrors(t *testing.T) {
	output := `main.go:10:5: undefined: foo
main.go:15:2: syntax error`

	result := ParseLintOutput(output, "go")
	if result.Clean {
		t.Error("expected lint errors")
	}
	if len(result.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(result.Issues))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run "TestParseTestOutput|TestParseLintOutput" -v`
Expected: FAIL — ParseTestOutput not defined

**Step 3: Write minimal implementation**

```go
// internal/runner/output_parser.go
package runner

import (
	"regexp"
	"strings"
)

type TestResult struct {
	Passed      bool
	TotalTests  int
	PassedTests int
	FailedTests int
	Failures    []TestFailure
	RawOutput   string
}

type TestFailure struct {
	TestName string
	Message  string
	File     string
	Line     int
}

type LintResult struct {
	Clean  bool
	Issues []LintIssue
}

type LintIssue struct {
	File    string
	Line    int
	Message string
}

var (
	goPassRe = regexp.MustCompile(`--- PASS: (\S+)`)
	goFailRe = regexp.MustCompile(`--- FAIL: (\S+)`)
	goFailMsgRe = regexp.MustCompile(`\s+(\S+\.go:\d+): (.+)`)
	lintIssueRe = regexp.MustCompile(`^(\S+\.go):(\d+):\d+: (.+)$`)
)

func ParseTestOutput(output, lang string) TestResult {
	result := TestResult{RawOutput: output}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if goPassRe.MatchString(line) {
			result.PassedTests++
			result.TotalTests++
		}
		if goFailRe.MatchString(line) {
			result.FailedTests++
			result.TotalTests++
			matches := goFailRe.FindStringSubmatch(line)
			failure := TestFailure{TestName: matches[1]}
			result.Failures = append(result.Failures, failure)
		}
	}

	// Check for failure messages
	for i, f := range result.Failures {
		for _, line := range lines {
			if goFailMsgRe.MatchString(line) && strings.Contains(output, f.TestName) {
				matches := goFailMsgRe.FindStringSubmatch(line)
				result.Failures[i].Message = matches[2]
				break
			}
		}
	}

	result.Passed = result.FailedTests == 0 && !strings.Contains(output, "FAIL")
	return result
}

func ParseLintOutput(output, lang string) LintResult {
	result := LintResult{Clean: true}
	if strings.TrimSpace(output) == "" {
		return result
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if lintIssueRe.MatchString(line) {
			matches := lintIssueRe.FindStringSubmatch(line)
			result.Issues = append(result.Issues, LintIssue{
				File:    matches[1],
				Message: matches[3],
			})
			result.Clean = false
		}
	}
	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/runner/ -run "TestParseTestOutput|TestParseLintOutput" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runner/output_parser.go internal/runner/output_parser_test.go
git commit -m "feat(runner): add test and lint output parser"
```

---

### Task 3: Prompt Templates (pongo2)

**Files:**
- Create: `prompts/planner.md.j2`
- Create: `prompts/implementer.md.j2`
- Create: `prompts/implementer_retry.md.j2`
- Create: `prompts/spec_reviewer.md.j2`
- Create: `prompts/quality_reviewer.md.j2`
- Create: `prompts/final_reviewer.md.j2`
- Create: `prompts/clarifier.md.j2`
- Create: `internal/pipeline/prompt_renderer.go`
- Create: `internal/pipeline/prompt_renderer_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/prompt_renderer_test.go
package pipeline

import (
	"testing"
)

func TestRenderPrompt_Planner(t *testing.T) {
	ctx := PromptContext{
		TicketTitle:       "Add login page",
		TicketDescription: "Build a login page with email and password.",
		FileTree:          "src/\n  main.go\n  auth/\n    login.go",
	}

	result, err := RenderPrompt("planner", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
	if !containsAll(result, "Add login page", "login page with email") {
		t.Error("expected ticket context in rendered prompt")
	}
}

func TestRenderPrompt_Implementer(t *testing.T) {
	ctx := PromptContext{
		TaskTitle:       "Implement login handler",
		TaskDescription: "Handle POST /login with email/password validation.",
		ContextFiles: map[string]string{
			"auth/login.go": "package auth\n",
		},
	}

	result, err := RenderPrompt("implementer", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
}

func TestRenderPrompt_Unknown(t *testing.T) {
	_, err := RenderPrompt("nonexistent", PromptContext{})
	if err == nil {
		t.Error("expected error for unknown template")
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestRenderPrompt -v`
Expected: FAIL — RenderPrompt not defined

**Step 3: Create prompt template files**

```
{# prompts/planner.md.j2 #}
## Your Job

Decompose this ticket into implementation tasks. Each task should be a single, testable unit of work.

## Rules

1. Maximum {{ max_tasks }} tasks
2. Each task must have clear acceptance criteria
3. Each task must list files to read and files to modify
4. Order tasks by dependency (independent tasks first)
5. Estimate complexity: simple | medium | complex
6. If the ticket is unclear, respond with: CLARIFICATION_NEEDED: <your question>

## Output Format (YAML — strict, parse failure = pipeline failure)

```yaml
tasks:
  - title: "Short descriptive title"
    description: "What this task does"
    acceptance_criteria:
      - "Criterion 1"
    files_to_read:
      - "path/to/file.go"
    files_to_modify:
      - "path/to/file.go"
    test_assertions:
      - "Test that X returns Y"
    estimated_complexity: "simple"
    depends_on: []
```

## Ticket

**Title:** {{ ticket_title }}

**Description:** {{ ticket_description }}

{% if acceptance_criteria %}
**Acceptance Criteria:** {{ acceptance_criteria }}
{% endif %}

## Repository

**File Tree:**
```
{{ file_tree }}
```

{% if project_context %}
## Project-Specific Context
{{ project_context }}
{% endif %}
```

```
{# prompts/implementer.md.j2 #}
You are an expert software engineer implementing a single task using TDD.

## TDD Rules (MANDATORY)

1. Write tests FIRST that capture the acceptance criteria
2. Tests must be runnable and fail for the right reason
3. Then write the minimal implementation to make tests pass
4. Never modify tests to make them pass — fix the implementation

## Output Format

Use SEARCH/REPLACE blocks for file modifications:

--- SEARCH/REPLACE ---
<<<< SEARCH file="path/to/file.go"
existing code to find
==== REPLACE
new replacement code
>>>> END

For new test files, use:
--- NEW FILE path/to/file_test.go ---
file contents here
--- END FILE ---

## Task

**{{ task_title }}**
{{ task_description }}

{% if acceptance_criteria %}
**Acceptance Criteria:**
{% for ac in acceptance_criteria %}
- {{ ac }}
{% endfor %}
{% endif %}

## Codebase Context

{% for path, content in context_files.items() %}
### {{ path }}
```
{{ content }}
```

{% endfor %}

{% if codebase_patterns %}
## Codebase Patterns
{{ codebase_patterns }}
{% endif %}
```

```
{# prompts/implementer_retry.md.j2 #}
{% include "implementer.md.j2" %}

## RETRY (attempt {{ attempt }}/{{ max_attempts }})

The previous implementation failed. Here is the feedback:

{% if spec_review_feedback %}
## SPEC REVIEWER FOUND ISSUES
{{ spec_review_feedback }}
{% endif %}

{% if quality_review_feedback %}
## QUALITY REVIEWER FOUND ISSUES
{{ quality_review_feedback }}
{% endif %}

{% if tdd_failure %}
## TDD VERIFICATION FAILED
{{ tdd_failure }}
{% endif %}

{% if test_failure %}
## TEST FAILURE
{{ test_failure }}
{% endif %}

Fix the issues above. Do NOT repeat the same mistake.
```

```
{# prompts/spec_reviewer.md.j2 #}
You are reviewing code changes for spec compliance. Check that the implementation matches the task requirements exactly.

## Review Checklist
1. Does the code implement ALL acceptance criteria?
2. Are there any missing edge cases?
3. Does the code follow the project conventions?
4. Are tests comprehensive and meaningful?

## Task
**{{ task_title }}**
{{ task_description }}

**Acceptance Criteria:**
{% for ac in acceptance_criteria %}
- {{ ac }}
{% endfor %}

## Changes (diff)
```diff
{{ diff }}
```

## Output Format
If approved: APPROVED
If issues found:
ISSUES:
- Issue 1 description
- Issue 2 description
```

```
{# prompts/quality_reviewer.md.j2 #}
You are reviewing code changes for quality. Focus on maintainability, performance, and best practices.

## Review Checklist
1. Code readability and naming
2. Error handling completeness
3. Performance concerns
4. Security issues (injection, XSS, etc.)
5. Test quality — are edge cases covered?

## Changes (diff)
```diff
{{ diff }}
```

## Codebase Patterns
{{ codebase_patterns }}

## Output Format
If approved: APPROVED
If issues found:
ISSUES:
- Issue 1 description
- Issue 2 description
```

```
{# prompts/final_reviewer.md.j2 #}
You are performing a final review of all changes in this ticket before creating a PR.

## Ticket
**{{ ticket_title }}**
{{ ticket_description }}

## All Changes (full diff against default branch)
```diff
{{ full_diff }}
```

## Tasks Completed
{% for task in completed_tasks %}
- {{ task.title }} ({{ task.status }})
{% endfor %}

## Output Format
If approved: APPROVED: <one-line summary for PR description>
If issues found:
ISSUES:
- Issue 1 description
```

```
{# prompts/clarifier.md.j2 #}
The following ticket needs clarification before we can plan the implementation.

## Ticket
**{{ ticket_title }}**
{{ ticket_description }}

## What's Missing
Generate a clear, specific question that would help us understand what to implement.
Focus on the most critical ambiguity. Ask ONE question, not multiple.

## Output Format
CLARIFICATION_NEEDED: <your specific question>
```

**Step 4: Create prompt renderer**

```go
// internal/pipeline/prompt_renderer.go
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flosch/pongo2/v6"
)

type PromptContext struct {
	// Ticket-level
	TicketTitle        string
	TicketDescription  string
	AcceptanceCriteria string
	FileTree           string
	ProjectContext     string
	FullDiff           string

	// Task-level
	TaskTitle       string
	TaskDescription string
	ContextFiles    map[string]string
	CodebasePatterns string
	Diff            string

	// Retry
	Attempt             int
	MaxAttempts          int
	SpecReviewFeedback   string
	QualityReviewFeedback string
	TDDFailure           string
	TestFailure          string

	// Final review
	CompletedTasks []struct {
		Title  string
		Status string
	}

	// Config
	MaxTasks int
}

// promptsDir is the directory containing prompt templates.
// Default is "prompts/" relative to working directory.
var promptsDir = "prompts"

func RenderPrompt(templateName string, ctx PromptContext) (string, error) {
	path := filepath.Join(promptsDir, templateName+".md.j2")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("prompt template not found: %s", path)
	}

	tpl, err := pongo2.FromFile(path)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", templateName, err)
	}

	pongoCtx := pongo2.Context{
		"ticket_title":           ctx.TicketTitle,
		"ticket_description":     ctx.TicketDescription,
		"acceptance_criteria":    ctx.AcceptanceCriteria,
		"file_tree":             ctx.FileTree,
		"project_context":       ctx.ProjectContext,
		"full_diff":             ctx.FullDiff,
		"task_title":            ctx.TaskTitle,
		"task_description":      ctx.TaskDescription,
		"context_files":         ctx.ContextFiles,
		"codebase_patterns":     ctx.CodebasePatterns,
		"diff":                  ctx.Diff,
		"attempt":               ctx.Attempt,
		"max_attempts":          ctx.MaxAttempts,
		"spec_review_feedback":  ctx.SpecReviewFeedback,
		"quality_review_feedback": ctx.QualityReviewFeedback,
		"tdd_failure":           ctx.TDDFailure,
		"test_failure":          ctx.TestFailure,
		"completed_tasks":       ctx.CompletedTasks,
		"max_tasks":             ctx.MaxTasks,
	}

	result, err := tpl.Execute(pongoCtx)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", templateName, err)
	}

	return result, nil
}
```

**Step 5: Run tests**

Run: `go test ./internal/pipeline/ -run TestRenderPrompt -v`
Expected: PASS

**Step 6: Commit**

```bash
git add prompts/ internal/pipeline/prompt_renderer.go internal/pipeline/prompt_renderer_test.go
git commit -m "feat(pipeline): add pongo2 prompt templates and renderer"
```

---

### Task 4: Context Rules (`context/rules.go`)

**Files:**
- Create: `internal/context/rules.go`
- Create: `internal/context/rules_test.go`

**Step 1: Write the failing test**

```go
// internal/context/rules_test.go
package context

import (
	"testing"
)

func TestLoadRules_Default(t *testing.T) {
	rules := LoadDirectoryRules("")
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if rules.TestCommand == "" {
		t.Error("expected default test command")
	}
}

func TestLoadRules_Go(t *testing.T) {
	rules := LoadDirectoryRules("go")
	if rules.TestCommand != "go test ./..." {
		t.Errorf("expected go test command, got %s", rules.TestCommand)
	}
	if rules.LintCommand != "go vet ./..." {
		t.Errorf("expected go vet command, got %s", rules.LintCommand)
	}
}

func TestLoadRules_Node(t *testing.T) {
	rules := LoadDirectoryRules("node")
	if rules.TestCommand != "npm test" {
		t.Errorf("expected npm test, got %s", rules.TestCommand)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		files    []string
		expected string
	}{
		{[]string{"go.mod", "main.go"}, "go"},
		{[]string{"package.json", "index.js"}, "node"},
		{[]string{"Cargo.toml", "src/main.rs"}, "rust"},
		{[]string{"requirements.txt", "main.py"}, "python"},
		{[]string{"unknown.xyz"}, ""},
	}
	for _, tt := range tests {
		got := DetectLanguage(tt.files)
		if got != tt.expected {
			t.Errorf("DetectLanguage(%v) = %q, want %q", tt.files, got, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run "TestLoadRules|TestDetectLanguage" -v`
Expected: FAIL — LoadDirectoryRules not defined

**Step 3: Write minimal implementation**

```go
// internal/context/rules.go
package context

// DirectoryRules holds conventions for a detected language/framework.
type DirectoryRules struct {
	Language    string
	TestCommand string
	LintCommand string
	BuildCommand string
	PackageFile string
}

var languageRules = map[string]DirectoryRules{
	"go": {
		Language:    "go",
		TestCommand: "go test ./...",
		LintCommand: "go vet ./...",
		BuildCommand: "go build ./...",
		PackageFile: "go.mod",
	},
	"node": {
		Language:    "node",
		TestCommand: "npm test",
		LintCommand: "npx eslint .",
		BuildCommand: "npm run build",
		PackageFile: "package.json",
	},
	"rust": {
		Language:    "rust",
		TestCommand: "cargo test",
		LintCommand: "cargo clippy",
		BuildCommand: "cargo build",
		PackageFile: "Cargo.toml",
	},
	"python": {
		Language:    "python",
		TestCommand: "pytest",
		LintCommand: "ruff check .",
		BuildCommand: "",
		PackageFile: "requirements.txt",
	},
}

var defaultRules = DirectoryRules{
	TestCommand: "make test",
	LintCommand: "make lint",
}

func LoadDirectoryRules(language string) *DirectoryRules {
	if language == "" {
		r := defaultRules
		return &r
	}
	if rules, ok := languageRules[language]; ok {
		return &rules
	}
	r := defaultRules
	return &r
}

var languageDetectors = map[string][]string{
	"go":     {"go.mod"},
	"node":   {"package.json"},
	"rust":   {"Cargo.toml"},
	"python": {"requirements.txt", "pyproject.toml", "setup.py"},
}

func DetectLanguage(files []string) string {
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for lang, markers := range languageDetectors {
		for _, marker := range markers {
			if fileSet[marker] {
				return lang
			}
		}
	}
	return ""
}
```

**Step 4: Run tests**

Run: `go test ./internal/context/ -run "TestLoadRules|TestDetectLanguage" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/rules.go internal/context/rules_test.go
git commit -m "feat(context): add directory rules and language detection"
```

---

### Task 5: Context Progress (`context/progress.go`)

**Files:**
- Create: `internal/context/progress.go`
- Create: `internal/context/progress_test.go`

**Step 1: Write the failing test**

```go
// internal/context/progress_test.go
package context

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type mockProgressDB struct {
	patterns []models.ProgressPattern
}

func (m *mockProgressDB) GetProgressPatterns(_ context.Context, ticketID string, dirs []string) ([]models.ProgressPattern, error) {
	return m.patterns, nil
}

func TestGetPrunedPatterns(t *testing.T) {
	db := &mockProgressDB{
		patterns: []models.ProgressPattern{
			{PatternKey: "import_style", PatternValue: "ESM imports", CreatedAt: time.Now()},
			{PatternKey: "import_style", PatternValue: "CommonJS", CreatedAt: time.Now().Add(-time.Hour)},
			{PatternKey: "error_handling", PatternValue: "try/catch with custom errors", CreatedAt: time.Now()},
		},
	}

	result, err := GetPrunedPatterns(context.Background(), db, "ticket-1", []string{"src/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should deduplicate by key, keeping most recent
	if len(result) != 2 {
		t.Errorf("expected 2 patterns after pruning, got %d", len(result))
	}
}

func TestFormatPatternsForPrompt(t *testing.T) {
	patterns := []models.ProgressPattern{
		{PatternKey: "import_style", PatternValue: "ESM imports, no semicolons"},
		{PatternKey: "error_handling", PatternValue: "Wrap with fmt.Errorf"},
	}

	result := FormatPatternsForPrompt(patterns)
	if result == "" {
		t.Error("expected non-empty formatted patterns")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run "TestGetPrunedPatterns|TestFormatPatternsForPrompt" -v`
Expected: FAIL — GetPrunedPatterns not defined

**Step 3: Write minimal implementation**

```go
// internal/context/progress.go
package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// ProgressStore is the subset of db.Database needed for progress patterns.
type ProgressStore interface {
	GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error)
}

// GetPrunedPatterns retrieves patterns and deduplicates by key, keeping the most recent.
func GetPrunedPatterns(ctx context.Context, db ProgressStore, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	patterns, err := db.GetProgressPatterns(ctx, ticketID, directories)
	if err != nil {
		return nil, err
	}

	// Deduplicate by key — keep most recent (patterns are returned in order)
	seen := make(map[string]bool)
	var pruned []models.ProgressPattern
	for _, p := range patterns {
		if !seen[p.PatternKey] {
			seen[p.PatternKey] = true
			pruned = append(pruned, p)
		}
	}
	return pruned, nil
}

// FormatPatternsForPrompt converts progress patterns into a human-readable
// string for inclusion in LLM prompts.
func FormatPatternsForPrompt(patterns []models.ProgressPattern) string {
	if len(patterns) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Discovered Patterns\n\n")
	for _, p := range patterns {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", p.PatternKey, p.PatternValue))
	}
	return sb.String()
}
```

**Step 4: Run tests**

Run: `go test ./internal/context/ -run "TestGetPrunedPatterns|TestFormatPatternsForPrompt" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/progress.go internal/context/progress_test.go
git commit -m "feat(context): add progress pattern pruning and prompt formatting"
```

---

### Task 6: go-git Fallback Provider

**Files:**
- Create: `internal/git/gogit.go`
- Create: `internal/git/gogit_test.go`

**Step 1: Write the failing test**

```go
// internal/git/gogit_test.go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoGitProvider_EnsureRepo(t *testing.T) {
	dir := t.TempDir()
	// Initialize a bare repo to clone from
	bareDir := filepath.Join(dir, "bare.git")
	exec.Command("git", "init", "--bare", bareDir).Run()

	workDir := filepath.Join(dir, "work")
	os.MkdirAll(workDir, 0o755)

	provider := NewGoGitProvider()
	err := provider.EnsureRepo(context.Background(), workDir)
	// go-git EnsureRepo on an empty dir should init
	if err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	// Verify .git exists
	if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
		t.Error("expected .git directory")
	}
}

func TestGoGitProvider_FileTree(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	provider := NewGoGitProvider()
	files, err := provider.FileTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("FileTree failed: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected at least one file")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestGoGit -v`
Expected: FAIL — NewGoGitProvider not defined

**Step 3: Write minimal implementation**

```go
// internal/git/gogit.go
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GoGitProvider implements GitProvider using go-git (pure Go).
// Used as fallback when native git CLI is not available.
type GoGitProvider struct{}

func NewGoGitProvider() *GoGitProvider {
	return &GoGitProvider{}
}

func (g *GoGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	_, err := gogit.PlainOpen(workDir)
	if err == nil {
		return nil // Already a repo
	}
	_, err = gogit.PlainInit(workDir, false)
	if err != nil {
		return fmt.Errorf("go-git init: %w", err)
	}
	return nil
}

func (g *GoGitProvider) CreateBranch(ctx context.Context, workDir, branchName string) error {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return err
	}
	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("go-git head: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branchName), headRef.Hash())
	return repo.Storer.SetReference(ref)
}

func (g *GoGitProvider) Commit(ctx context.Context, workDir, message string) (string, error) {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Foreman",
			Email: "foreman@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("go-git commit: %w", err)
	}
	return hash.String(), nil
}

func (g *GoGitProvider) Diff(ctx context.Context, workDir, base, head string) (string, error) {
	// go-git diff is complex; delegate to native git if available, return stub otherwise
	return "", fmt.Errorf("go-git diff not fully implemented — use native git")
}

func (g *GoGitProvider) DiffWorking(ctx context.Context, workDir string) (string, error) {
	return "", fmt.Errorf("go-git DiffWorking not fully implemented — use native git")
}

func (g *GoGitProvider) Push(ctx context.Context, workDir, branchName string) error {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return err
	}
	return repo.Push(&gogit.PushOptions{})
}

func (g *GoGitProvider) RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error) {
	// go-git does not support rebase natively
	return nil, fmt.Errorf("go-git does not support rebase — use native git")
}

func (g *GoGitProvider) CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error) {
	return nil, fmt.Errorf("go-git CreatePR not implemented — requires API client")
}

func (g *GoGitProvider) FileTree(ctx context.Context, workDir string) ([]FileEntry, error) {
	var entries []FileEntry
	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(workDir, path)
		entries = append(entries, FileEntry{
			Path:      rel,
			IsDir:     info.IsDir(),
			SizeBytes: info.Size(),
		})
		return nil
	})
	return entries, err
}

func (g *GoGitProvider) Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error) {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, err
	}

	var entries []CommitEntry
	for i := 0; i < count; i++ {
		commit, err := iter.Next()
		if err != nil {
			break
		}
		entries = append(entries, CommitEntry{
			SHA:     commit.Hash.String(),
			Message: commit.Message,
			Author:  commit.Author.Name,
			Date:    commit.Author.When,
		})
	}
	return entries, nil
}

func (g *GoGitProvider) CheckFileOverlap(ctx context.Context, workDir, branchA string, filesB []string) ([]string, error) {
	return nil, fmt.Errorf("go-git CheckFileOverlap not implemented — use native git")
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -run TestGoGit -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/gogit.go internal/git/gogit_test.go
git commit -m "feat(git): add go-git fallback provider"
```

---

### Task 7: Plan Validator — Token-Aware Cost Estimation

**Files:**
- Modify: `internal/pipeline/plan_validator.go`
- Modify: `internal/pipeline/plan_validator_test.go`

**Step 1: Write the failing test**

```go
// Add to plan_validator_test.go
func TestEstimateTicketCost(t *testing.T) {
	pricing := map[string]models.PricingConfig{
		"anthropic:claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00},
		"anthropic:claude-haiku-4-5-20251001":  {Input: 0.80, Output: 4.00},
	}

	tasks := []PlanTask{
		{EstimatedComplexity: "simple"},
		{EstimatedComplexity: "medium"},
		{EstimatedComplexity: "complex"},
	}

	cost := EstimateTicketCost(tasks, pricing, "anthropic:claude-sonnet-4-5-20250929", "anthropic:claude-haiku-4-5-20251001")
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
	// Simple: ~2 calls, Medium: ~4 calls, Complex: ~6 calls
	// With average token usage, cost should be in reasonable range
	if cost > 50.0 {
		t.Errorf("cost seems too high: %f", cost)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestEstimateTicketCost -v`
Expected: FAIL — EstimateTicketCost not defined

**Step 3: Write implementation**

```go
// Add to plan_validator.go

// EstimateTicketCost estimates the total cost for a set of planned tasks
// based on complexity tiers and model pricing.
func EstimateTicketCost(tasks []PlanTask, pricing map[string]models.PricingConfig, implModel, reviewModel string) float64 {
	// Complexity tiers determine estimated LLM calls and token budgets per task
	type tier struct {
		llmCalls       int
		avgInputTokens int
		avgOutputTokens int
	}
	tiers := map[string]tier{
		"simple":  {llmCalls: 2, avgInputTokens: 20000, avgOutputTokens: 4000},
		"medium":  {llmCalls: 4, avgInputTokens: 40000, avgOutputTokens: 8000},
		"complex": {llmCalls: 6, avgInputTokens: 60000, avgOutputTokens: 12000},
	}

	var totalCost float64
	for _, task := range tasks {
		t, ok := tiers[task.EstimatedComplexity]
		if !ok {
			t = tiers["medium"]
		}
		// Implementation calls use implModel
		implCalls := t.llmCalls / 2
		if implCalls < 1 {
			implCalls = 1
		}
		totalCost += float64(implCalls) * estimateCallCost(implModel, t.avgInputTokens, t.avgOutputTokens, pricing)
		// Review calls use reviewModel
		reviewCalls := t.llmCalls - implCalls
		totalCost += float64(reviewCalls) * estimateCallCost(reviewModel, t.avgInputTokens/2, t.avgOutputTokens/4, pricing)
	}
	return totalCost
}

func estimateCallCost(model string, inputTokens, outputTokens int, pricing map[string]models.PricingConfig) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1_000_000)*p.Input + (float64(outputTokens)/1_000_000)*p.Output
}
```

**Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestEstimateTicketCost -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/plan_validator.go internal/pipeline/plan_validator_test.go
git commit -m "feat(pipeline): add token-aware cost estimation to plan validator"
```

---

### Task 8: LLM Call Cap Enforcement

**Files:**
- Create: `internal/pipeline/call_cap.go`
- Create: `internal/pipeline/call_cap_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/call_cap_test.go
package pipeline

import (
	"context"
	"fmt"
	"testing"
)

type mockCapDB struct {
	counts map[string]int
}

func (m *mockCapDB) IncrementTaskLlmCalls(_ context.Context, id string) (int, error) {
	m.counts[id]++
	return m.counts[id], nil
}

func TestCheckTaskCallCap_UnderLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t1": 3}}
	err := CheckTaskCallCap(context.Background(), db, "t1", 8)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCheckTaskCallCap_AtLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t2": 7}}
	err := CheckTaskCallCap(context.Background(), db, "t2", 8)
	if err != nil {
		t.Errorf("expected no error at limit, got %v", err)
	}
}

func TestCheckTaskCallCap_OverLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t3": 8}}
	err := CheckTaskCallCap(context.Background(), db, "t3", 8)
	if err == nil {
		t.Error("expected error when over limit")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestCheckTaskCallCap -v`
Expected: FAIL — CheckTaskCallCap not defined

**Step 3: Write implementation**

```go
// internal/pipeline/call_cap.go
package pipeline

import (
	"context"
	"fmt"
)

// CallCapDB is the subset of db.Database needed for call cap checks.
type CallCapDB interface {
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)
}

// CallCapExceededError is returned when a task exceeds its LLM call limit.
type CallCapExceededError struct {
	TaskID  string
	Current int
	Max     int
}

func (e *CallCapExceededError) Error() string {
	return fmt.Sprintf("task %s exceeded LLM call cap: %d/%d", e.TaskID, e.Current, e.Max)
}

// CheckTaskCallCap increments the call counter and returns an error if the cap is exceeded.
// Call this BEFORE every LLM call for a task.
func CheckTaskCallCap(ctx context.Context, db CallCapDB, taskID string, maxCalls int) error {
	count, err := db.IncrementTaskLlmCalls(ctx, taskID)
	if err != nil {
		return fmt.Errorf("increment call count: %w", err)
	}
	if count > maxCalls {
		return &CallCapExceededError{TaskID: taskID, Current: count, Max: maxCalls}
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestCheckTaskCallCap -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/call_cap.go internal/pipeline/call_cap_test.go
git commit -m "feat(pipeline): add LLM call cap enforcement per task"
```

---

### Task 9: Forbidden File Patterns Check

**Files:**
- Create: `internal/git/forbidden.go`
- Create: `internal/git/forbidden_test.go`

**Step 1: Write the failing test**

```go
// internal/git/forbidden_test.go
package git

import (
	"testing"
)

func TestCheckForbiddenFiles_Clean(t *testing.T) {
	files := []string{"main.go", "auth/login.go", "README.md"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 0 {
		t.Errorf("expected no forbidden files, got %v", forbidden)
	}
}

func TestCheckForbiddenFiles_Secrets(t *testing.T) {
	files := []string{"main.go", ".env", "certs/server.key", "config.pem"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 3 {
		t.Errorf("expected 3 forbidden files, got %d: %v", len(forbidden), forbidden)
	}
}

func TestCheckForbiddenFiles_SSHDir(t *testing.T) {
	files := []string{".ssh/id_rsa", ".aws/credentials"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 2 {
		t.Errorf("expected 2 forbidden files, got %d: %v", len(forbidden), forbidden)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestCheckForbiddenFiles -v`
Expected: FAIL — CheckForbiddenFiles not defined

**Step 3: Write implementation**

```go
// internal/git/forbidden.go
package git

import (
	"path/filepath"
	"strings"
)

// DefaultForbiddenPatterns are file patterns that must never be committed.
var DefaultForbiddenPatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	".ssh/*",
	".aws/*",
	".gnupg/*",
	"*credentials*",
	"*.pfx",
}

// CheckForbiddenFiles returns any files from the given list that match forbidden patterns.
// This should be called before every git commit.
func CheckForbiddenFiles(files []string, patterns []string) []string {
	var forbidden []string
	for _, f := range files {
		base := filepath.Base(f)
		for _, pattern := range patterns {
			// Check both full path and base name
			matched, _ := filepath.Match(pattern, f)
			if !matched {
				matched, _ = filepath.Match(pattern, base)
			}
			// Also check directory prefix patterns like ".ssh/*"
			if !matched && strings.Contains(pattern, "/") {
				dir := strings.TrimSuffix(pattern, "/*")
				if strings.HasPrefix(f, dir+"/") || strings.HasPrefix(f, dir+"\\") {
					matched = true
				}
			}
			if matched {
				forbidden = append(forbidden, f)
				break
			}
		}
	}
	return forbidden
}
```

**Step 4: Run tests**

Run: `go test ./internal/git/ -run TestCheckForbiddenFiles -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/forbidden.go internal/git/forbidden_test.go
git commit -m "feat(git): add forbidden file patterns check before commit"
```

---

### Task 10: Built-In Skill YAML Files

**Files:**
- Create: `skills/feature-dev.yml`
- Create: `skills/bug-fix.yml`
- Create: `skills/refactor.yml`
- Create: `skills/community/write-changelog.yml`
- Create: `skills/community/security-scan.yml`

**Step 1: Create skill files**

```yaml
# skills/feature-dev.yml
id: feature-dev
description: "Default feature development workflow — no extra steps"
trigger: post_lint
steps: []
# This is a placeholder skill that demonstrates the format.
# Feature development uses the default pipeline with no extra hooks.
```

```yaml
# skills/bug-fix.yml
id: bug-fix
description: "Bug fixing workflow — emphasizes regression tests"
trigger: post_lint
steps:
  - id: regression-check
    type: llm_call
    prompt_template: |
      Review this bug fix diff and check:
      1. Does the fix address the root cause, not just symptoms?
      2. Is there a regression test that would catch this bug if reintroduced?
      3. Are there related areas that might have the same bug?
      Respond with APPROVED or ISSUES: followed by a bullet list.
    model: "{{ .Models.QualityReviewer }}"
    context:
      diff: "{{ .Diff }}"
      ticket: "{{ .Ticket }}"
```

```yaml
# skills/refactor.yml
id: refactor
description: "Refactoring workflow — ensures behavior preservation"
trigger: post_lint
steps:
  - id: behavior-check
    type: run_command
    command: "{{ .Rules.TestCommand }}"
    args: []
    allow_failure: false
```

```yaml
# skills/community/write-changelog.yml
id: write-changelog
description: "Generate a changelog entry from the PR diff and ticket context"
trigger: pre_pr
steps:
  - id: generate
    type: llm_call
    prompt_template: |
      Generate a concise changelog entry for this change.
      Format: "- [TYPE] Description (ticket reference)"
      Types: Added, Changed, Fixed, Removed
      Diff: {{ .Diff }}
      Ticket: {{ .Ticket.Title }}
    model: "{{ .Models.Clarifier }}"
    context:
      diff: "{{ .Diff }}"
      ticket: "{{ .Ticket }}"
  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ .Steps.generate.output }}"
    mode: prepend
```

```yaml
# skills/community/security-scan.yml
id: security-scan
description: "Run a security-focused LLM review after lint"
trigger: post_lint
steps:
  - id: scan
    type: llm_call
    prompt_template: |
      Review this diff for security vulnerabilities:
      - SQL injection, XSS, command injection
      - Hardcoded secrets or credentials
      - Insecure deserialization
      - Path traversal
      Respond with APPROVED or ISSUES: followed by findings.
    model: "{{ .Models.QualityReviewer }}"
    context:
      diff: "{{ .Diff }}"
      file_tree: "{{ .FileTree }}"
```

**Step 2: Verify YAML is valid**

Run: `python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['skills/feature-dev.yml','skills/bug-fix.yml','skills/refactor.yml','skills/community/write-changelog.yml','skills/community/security-scan.yml']]" && echo "OK"`
Expected: OK

**Step 3: Commit**

```bash
mkdir -p skills/community
git add skills/
git commit -m "feat(skills): add built-in skill YAML files"
```

---

### Task 11: LLM-Assisted Rebase Conflict Resolution

**Files:**
- Create: `internal/pipeline/rebase_resolver.go`
- Create: `internal/pipeline/rebase_resolver_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/rebase_resolver_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/llm"
)

type mockRebaseLLM struct {
	response string
}

func (m *mockRebaseLLM) Complete(_ context.Context, req llm.LlmRequest) (*llm.LlmResponse, error) {
	return &llm.LlmResponse{Content: m.response, TokensInput: 200, TokensOutput: 100, Model: req.Model, StopReason: "end_turn"}, nil
}
func (m *mockRebaseLLM) ProviderName() string              { return "mock" }
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

	result, err := AttemptConflictResolution(context.Background(), llmProvider, conflictDiff, "test-model")
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

	result, err := AttemptConflictResolution(context.Background(), llmProvider, "some conflict", "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure — LLM didn't produce RESOLVED block")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestResolveConflict -v`
Expected: FAIL — AttemptConflictResolution not defined

**Step 3: Write implementation**

```go
// internal/pipeline/rebase_resolver.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
)

type ConflictResolution struct {
	Success  bool
	Resolved string
}

// AttemptConflictResolution asks the LLM to resolve a git merge conflict.
// Returns the resolved content if successful, or Success=false if the LLM
// cannot produce a valid resolution.
func AttemptConflictResolution(ctx context.Context, provider llm.LlmProvider, conflictDiff, model string) (*ConflictResolution, error) {
	resp, err := provider.Complete(ctx, llm.LlmRequest{
		Model: model,
		SystemPrompt: `You are resolving a git merge conflict. Analyze both sides and produce the correct merged result.

Output format:
<<<< RESOLVED
<the correct merged code>
>>>> END

If you cannot confidently resolve the conflict, say "CANNOT_RESOLVE" and explain why.`,
		UserPrompt: fmt.Sprintf("## Conflict\n```\n%s\n```\n\nResolve this conflict:", conflictDiff),
		MaxTokens:  4096,
	})
	if err != nil {
		return nil, fmt.Errorf("conflict resolution LLM call: %w", err)
	}

	// Parse the RESOLVED block
	content := resp.Content
	startMarker := "<<<< RESOLVED"
	endMarker := ">>>> END"

	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return &ConflictResolution{Success: false}, nil
	}

	resolved := strings.TrimSpace(content[startIdx+len(startMarker) : endIdx])
	return &ConflictResolution{
		Success:  true,
		Resolved: resolved,
	}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestResolveConflict -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/rebase_resolver.go internal/pipeline/rebase_resolver_test.go
git commit -m "feat(pipeline): add LLM-assisted rebase conflict resolution"
```

---

### Task 12: Full Test Suite Pre-PR Gate

**Files:**
- Create: `internal/pipeline/full_test_gate.go`
- Create: `internal/pipeline/full_test_gate_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/full_test_gate_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/runner"
)

type mockGateRunner struct {
	output *runner.CommandOutput
}

func (m *mockGateRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return m.output, nil
}

func (m *mockGateRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestFullTestGate_Pass(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		Stdout:   "PASS\nok example.com 0.5s",
		ExitCode: 0,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected passed")
	}
}

func TestFullTestGate_Fail(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		Stdout:   "FAIL\nFAIL example.com 0.5s",
		Stderr:   "test failed",
		ExitCode: 1,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected failed")
	}
}

func TestFullTestGate_Timeout(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		TimedOut: true,
		ExitCode: -1,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected failed on timeout")
	}
	if !result.TimedOut {
		t.Error("expected timeout flag")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestFullTestGate -v`
Expected: FAIL — RunFullTestSuite not defined

**Step 3: Write implementation**

```go
// internal/pipeline/full_test_gate.go
package pipeline

import (
	"context"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

type FullTestResult struct {
	Passed   bool
	TimedOut bool
	Output   string
	ExitCode int
}

// RunFullTestSuite executes the full test suite as a pre-PR gate.
// This runs after all tasks are committed and rebased onto the target branch.
func RunFullTestSuite(ctx context.Context, cmdRunner runner.CommandRunner, workDir, testCommand string, timeoutSecs int) (*FullTestResult, error) {
	parts := strings.Fields(testCommand)
	if len(parts) == 0 {
		return &FullTestResult{Passed: true, Output: "no test command configured"}, nil
	}

	cmd := parts[0]
	args := parts[1:]

	output, err := cmdRunner.Run(ctx, workDir, cmd, args, timeoutSecs)
	if err != nil {
		return nil, err
	}

	result := &FullTestResult{
		Output:   output.Stdout + "\n" + output.Stderr,
		ExitCode: output.ExitCode,
		TimedOut: output.TimedOut,
	}

	result.Passed = output.ExitCode == 0 && !output.TimedOut
	return result, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestFullTestGate -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/full_test_gate.go internal/pipeline/full_test_gate_test.go
git commit -m "feat(pipeline): add full test suite pre-PR gate"
```

---

### Task 13: Install go-git Dependency

**Step 1: Install the dependency**

```bash
cd /Users/canh/Projects/Indies/Foreman
go get github.com/go-git/go-git/v5@v5.12.0
go get github.com/flosch/pongo2/v6@v6.0.0
go mod tidy
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add go-git and pongo2 dependencies"
```

> **Note:** Run this task BEFORE Tasks 3 and 6, since they depend on these packages.
