# Phase 3: Review + Quality + Skills — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the review loop that makes Foreman's output production-quality: spec reviewer checks acceptance criteria, quality reviewer checks code quality, final reviewer gates the whole PR, partial PR support ships incomplete work gracefully, and YAML skills engine enables extensible pipeline hooks.

**Architecture:** Three reviewer stages wrap around the Phase 2 implementer. Each reviewer is a stateless LLM call that parses a structured APPROVED/REJECTED response. Failed reviews feed back into the implementer via the feedback accumulator (Phase 2). The PR creator abstracts GitHub/GitLab/Bitbucket behind the existing git interface. The skills engine is a ~500 LOC YAML interpreter that executes at three hook points (post_lint, pre_pr, post_pr) using four primitives: llm_call, run_command, file_write, git_diff.

**Tech Stack:** Go 1.26, existing Phase 1+2 packages (llm, runner, db, models, context, pipeline, git), gopkg.in/yaml.v3, go-resty/v2 (HTTP client for PR APIs), text/template (skill template rendering)

---

### Task 1: Review Output Parser

**Files:**
- Create: `internal/pipeline/review_parser.go`
- Test: `internal/pipeline/review_parser_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/review_parser_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReviewOutput_Approved(t *testing.T) {
	raw := `STATUS: APPROVED

CRITERIA:
- [pass] User struct has Name, Email, ID fields
- [pass] Validation returns error on empty name

ISSUES:
- None

EXTRAS:
- None`

	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Empty(t, result.Issues)
}

func TestParseReviewOutput_Rejected(t *testing.T) {
	raw := `STATUS: REJECTED

CRITERIA:
- [pass] User struct has Name, Email, ID fields
- [fail] Validation returns error on empty name

ISSUES:
- Missing validation for empty name in user.go:NewUser()
- No test for the empty-name case

EXTRAS:
- None`

	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.Len(t, result.Issues, 2)
	assert.Contains(t, result.Issues[0], "Missing validation")
}

func TestParseReviewOutput_QualityApproved(t *testing.T) {
	raw := `STATUS: APPROVED

ISSUES:
- [MINOR] handler.go: could extract helper function for repetitive validation

STRENGTHS:
- Clean error handling
- Consistent naming`

	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Len(t, result.Issues, 1)
}

func TestParseReviewOutput_QualityChangesRequested(t *testing.T) {
	raw := `STATUS: CHANGES_REQUESTED

ISSUES:
- [CRITICAL] handler.go: SQL injection in query builder — use parameterized queries
- [IMPORTANT] user.go: password stored in plaintext

STRENGTHS:
- Good test coverage`

	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.Len(t, result.Issues, 2)
	assert.True(t, result.HasCritical)
}

func TestParseReviewOutput_FinalReview(t *testing.T) {
	raw := `STATUS: APPROVED
SUMMARY: Implementation correctly addresses all ticket requirements.
CHANGES: Added user model, handler, and tests.
CONCERNS: None significant.
REVIEW_NOTES: Consider adding pagination in a follow-up ticket.`

	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Contains(t, result.Summary, "correctly addresses")
	assert.Contains(t, result.ReviewNotes, "pagination")
}

func TestParseReviewOutput_Garbage(t *testing.T) {
	raw := "Here's my review: looks good to me!"
	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	// Permissive: if no STATUS found, default to not approved
	assert.False(t, result.Approved)
}

func TestParseReviewOutput_PermissiveApproved(t *testing.T) {
	raw := "I've reviewed the code.\n\nSTATUS: APPROVED\n\nLooks great, no issues."
	result, err := ParseReviewOutput(raw)
	require.NoError(t, err)
	assert.True(t, result.Approved)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestParseReviewOutput -v`
Expected: FAIL — `ParseReviewOutput` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/review_parser.go
package pipeline

import (
	"regexp"
	"strings"
)

// ReviewResult holds the parsed output from any reviewer (spec, quality, final).
type ReviewResult struct {
	Approved    bool
	Issues      []string
	HasCritical bool
	Summary     string
	ReviewNotes string
	RawOutput   string
}

// ParseReviewOutput parses STATUS: APPROVED|REJECTED|CHANGES_REQUESTED from reviewer LLM output.
func ParseReviewOutput(raw string) (*ReviewResult, error) {
	result := &ReviewResult{RawOutput: raw}

	// Extract STATUS line
	statusRe := regexp.MustCompile(`(?i)STATUS:\s*(APPROVED|REJECTED|CHANGES_REQUESTED)`)
	if m := statusRe.FindStringSubmatch(raw); len(m) > 1 {
		status := strings.ToUpper(m[1])
		result.Approved = status == "APPROVED"
	}

	// Extract ISSUES section
	result.Issues = extractListSection(raw, "ISSUES")

	// Check for CRITICAL severity
	for _, issue := range result.Issues {
		if strings.Contains(strings.ToUpper(issue), "[CRITICAL]") {
			result.HasCritical = true
			break
		}
	}

	// Extract SUMMARY (final reviewer)
	if m := extractSingleLine(raw, "SUMMARY"); m != "" {
		result.Summary = m
	}

	// Extract REVIEW_NOTES (final reviewer)
	if m := extractSingleLine(raw, "REVIEW_NOTES"); m != "" {
		result.ReviewNotes = m
	}

	return result, nil
}

// IssuesText returns all issues as a single string for feedback.
func (r *ReviewResult) IssuesText() string {
	return strings.Join(r.Issues, "\n")
}

func extractListSection(raw, header string) []string {
	// Find the header line, then collect "- " prefixed lines
	headerRe := regexp.MustCompile(`(?i)^` + header + `:\s*$`)
	lines := strings.Split(raw, "\n")
	var items []string
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if headerRe.MatchString(trimmed) {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "- ") {
				item := strings.TrimPrefix(trimmed, "- ")
				// Skip "None" entries
				if strings.ToLower(item) != "none" {
					items = append(items, item)
				}
			} else if trimmed == "" {
				// Blank line might still be in section
				continue
			} else if !strings.HasPrefix(trimmed, "- ") && trimmed != "" {
				// Non-list line ends the section
				inSection = false
			}
		}
	}
	return items
}

func extractSingleLine(raw, key string) string {
	re := regexp.MustCompile(`(?i)` + key + `:\s*(.+)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestParseReviewOutput -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/review_parser.go internal/pipeline/review_parser_test.go
git commit -m "feat: add review output parser for spec, quality, and final reviewers"
```

---

### Task 2: Spec Reviewer

**Files:**
- Create: `internal/pipeline/spec_reviewer.go`
- Test: `internal/pipeline/spec_reviewer_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/spec_reviewer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// llmMock is a simple mock LLM that returns canned responses.
type llmMock struct {
	response string
}

func (m *llmMock) Complete(_ context.Context, req llm.LlmRequest) (*llm.LlmResponse, error) {
	return &llm.LlmResponse{
		Content:      m.response,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        "test",
		StopReason:   "end_turn",
	}, nil
}
func (m *llmMock) ProviderName() string                   { return "mock" }
func (m *llmMock) HealthCheck(_ context.Context) error    { return nil }

func TestSpecReviewer_Approved(t *testing.T) {
	mock := &llmMock{response: `STATUS: APPROVED

CRITERIA:
- [pass] Handler returns 200
- [pass] Response is JSON

ISSUES:
- None

EXTRAS:
- None`}

	reviewer := NewSpecReviewer(mock)
	result, err := reviewer.Review(context.Background(), SpecReviewInput{
		TaskTitle:          "Add user handler",
		AcceptanceCriteria: []string{"Handler returns 200", "Response is JSON"},
		Diff:               "diff --git a/handler.go\n+func GetUsers() {}",
		TestOutput:         "PASS: all tests",
	})

	require.NoError(t, err)
	assert.True(t, result.Approved)
}

func TestSpecReviewer_Rejected(t *testing.T) {
	mock := &llmMock{response: `STATUS: REJECTED

CRITERIA:
- [pass] Handler returns 200
- [fail] Response is JSON

ISSUES:
- Handler returns plain text, not JSON. Need to set Content-Type header and use json.Marshal.

EXTRAS:
- None`}

	reviewer := NewSpecReviewer(mock)
	result, err := reviewer.Review(context.Background(), SpecReviewInput{
		TaskTitle:          "Add user handler",
		AcceptanceCriteria: []string{"Handler returns 200", "Response is JSON"},
		Diff:               "diff --git a/handler.go\n+func GetUsers() { w.Write([]byte(\"ok\")) }",
		TestOutput:         "PASS",
	})

	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.NotEmpty(t, result.Issues)
	assert.Contains(t, result.Issues[0], "plain text")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestSpecReviewer -v`
Expected: FAIL — `NewSpecReviewer` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/spec_reviewer.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
)

// SpecReviewInput is what the spec reviewer needs.
type SpecReviewInput struct {
	TaskTitle          string
	AcceptanceCriteria []string
	Diff               string
	TestOutput         string
}

// SpecReviewer checks if implementation meets acceptance criteria.
type SpecReviewer struct {
	llm llm.LlmProvider
}

// NewSpecReviewer creates a spec reviewer.
func NewSpecReviewer(provider llm.LlmProvider) *SpecReviewer {
	return &SpecReviewer{llm: provider}
}

// Review runs a spec review and returns the parsed result.
func (r *SpecReviewer) Review(ctx context.Context, input SpecReviewInput) (*ReviewResult, error) {
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
	user.WriteString(fmt.Sprintf("## Task\n%s\n\nCriteria:\n", input.TaskTitle))
	for _, c := range input.AcceptanceCriteria {
		user.WriteString(fmt.Sprintf("- %s\n", c))
	}
	user.WriteString(fmt.Sprintf("\n## Diff\n```diff\n%s\n```\n\n## Test Output\n```\n%s\n```\n", input.Diff, input.TestOutput))

	resp, err := r.llm.Complete(ctx, llm.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   user.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("spec review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestSpecReviewer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/spec_reviewer.go internal/pipeline/spec_reviewer_test.go
git commit -m "feat: add spec reviewer with acceptance criteria checking"
```

---

### Task 3: Quality Reviewer

**Files:**
- Create: `internal/pipeline/quality_reviewer.go`
- Test: `internal/pipeline/quality_reviewer_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/quality_reviewer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQualityReviewer_Approved(t *testing.T) {
	mock := &llmMock{response: `STATUS: APPROVED

ISSUES:
- [MINOR] handler.go: could use a named constant for status code

STRENGTHS:
- Clean error handling
- Good test coverage`}

	reviewer := NewQualityReviewer(mock)
	result, err := reviewer.Review(context.Background(), QualityReviewInput{
		Diff:             "diff --git a/handler.go\n+func GetUsers() {}",
		CodebasePatterns: "go, stdlib, standard go conventions",
	})

	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Len(t, result.Issues, 1)
	assert.False(t, result.HasCritical)
}

func TestQualityReviewer_ChangesRequested(t *testing.T) {
	mock := &llmMock{response: `STATUS: CHANGES_REQUESTED

ISSUES:
- [CRITICAL] handler.go: SQL injection in query — use parameterized queries
- [IMPORTANT] user.go: password stored in plaintext, use bcrypt

STRENGTHS:
- Tests are comprehensive`}

	reviewer := NewQualityReviewer(mock)
	result, err := reviewer.Review(context.Background(), QualityReviewInput{
		Diff:             "diff --git a/handler.go\n+db.Query(\"SELECT * WHERE id=\" + id)",
		CodebasePatterns: "go, stdlib",
	})

	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.True(t, result.HasCritical)
	assert.Len(t, result.Issues, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestQualityReviewer -v`
Expected: FAIL — `NewQualityReviewer` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/quality_reviewer.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
)

// QualityReviewInput is what the quality reviewer needs.
type QualityReviewInput struct {
	Diff             string
	CodebasePatterns string
}

// QualityReviewer checks code quality, not spec compliance.
type QualityReviewer struct {
	llm llm.LlmProvider
}

// NewQualityReviewer creates a quality reviewer.
func NewQualityReviewer(provider llm.LlmProvider) *QualityReviewer {
	return &QualityReviewer{llm: provider}
}

// Review runs a quality review and returns the parsed result.
func (r *QualityReviewer) Review(ctx context.Context, input QualityReviewInput) (*ReviewResult, error) {
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
	user.WriteString(fmt.Sprintf("## Codebase Patterns\n%s\n\n## Diff\n```diff\n%s\n```\n", input.CodebasePatterns, input.Diff))

	resp, err := r.llm.Complete(ctx, llm.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   user.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("quality review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestQualityReviewer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/quality_reviewer.go internal/pipeline/quality_reviewer_test.go
git commit -m "feat: add quality reviewer with severity classification"
```

---

### Task 4: Final Reviewer

**Files:**
- Create: `internal/pipeline/final_reviewer.go`
- Test: `internal/pipeline/final_reviewer_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/final_reviewer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalReviewer_Approved(t *testing.T) {
	mock := &llmMock{response: `STATUS: APPROVED
SUMMARY: Implementation correctly addresses all ticket requirements. Clean code with good test coverage.
CHANGES: Added user model with validation, REST handler, and comprehensive tests.
CONCERNS: None significant.
REVIEW_NOTES: Consider adding pagination in a follow-up ticket.`}

	reviewer := NewFinalReviewer(mock)
	result, err := reviewer.Review(context.Background(), FinalReviewInput{
		TicketTitle:       "Add user management",
		TicketDescription: "Create user CRUD endpoints",
		FullDiff:          "diff --git ...",
		TestOutput:        "PASS: all 12 tests passed",
		TaskSummaries: []TaskSummary{
			{Title: "Add user model", Status: "done"},
			{Title: "Add user handler", Status: "done"},
		},
	})

	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Contains(t, result.Summary, "correctly addresses")
	assert.Contains(t, result.ReviewNotes, "pagination")
}

func TestFinalReviewer_Rejected(t *testing.T) {
	mock := &llmMock{response: `STATUS: REJECTED
SUMMARY: Missing error handling in the handler causes 500 errors on invalid input.
CHANGES: User model and handler added but handler lacks input validation.
CONCERNS: Handler does not validate request body, leading to panics.
REVIEW_NOTES: Add input validation before merging.`}

	reviewer := NewFinalReviewer(mock)
	result, err := reviewer.Review(context.Background(), FinalReviewInput{
		TicketTitle:       "Add user management",
		TicketDescription: "Create user CRUD endpoints",
		FullDiff:          "diff --git ...",
		TestOutput:        "PASS",
		TaskSummaries: []TaskSummary{
			{Title: "Add user model", Status: "done"},
			{Title: "Add user handler", Status: "done"},
		},
	})

	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.Contains(t, result.Summary, "Missing error handling")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestFinalReviewer -v`
Expected: FAIL — `NewFinalReviewer` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/final_reviewer.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
)

// TaskSummary is a brief task status for the final reviewer.
type TaskSummary struct {
	Title  string
	Status string
}

// FinalReviewInput is what the final reviewer needs.
type FinalReviewInput struct {
	TicketTitle       string
	TicketDescription string
	FullDiff          string
	TestOutput        string
	TaskSummaries     []TaskSummary
}

// FinalReviewer performs a final review of the complete changeset before PR creation.
type FinalReviewer struct {
	llm llm.LlmProvider
}

// NewFinalReviewer creates a final reviewer.
func NewFinalReviewer(provider llm.LlmProvider) *FinalReviewer {
	return &FinalReviewer{llm: provider}
}

// Review runs the final review and returns the parsed result.
func (r *FinalReviewer) Review(ctx context.Context, input FinalReviewInput) (*ReviewResult, error) {
	system := `Final review of the complete changeset before PR creation.

## Check
1. Changes as a whole address the original ticket
2. Integration issues between tasks
3. Cross-cutting concerns (error handling consistency, migrations, etc.)

## Output Format
STATUS: APPROVED | REJECTED
SUMMARY: <2-3 sentences>
CHANGES: <key changes by area>
CONCERNS: <issues if any>
REVIEW_NOTES: <notes for human reviewer>`

	var user strings.Builder
	user.WriteString(fmt.Sprintf("## Ticket\n%s\n%s\n\n", input.TicketTitle, input.TicketDescription))
	user.WriteString(fmt.Sprintf("## Full Diff\n```diff\n%s\n```\n\n", input.FullDiff))

	user.WriteString("## Tasks\n")
	for i, t := range input.TaskSummaries {
		user.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, t.Title, t.Status))
	}

	user.WriteString(fmt.Sprintf("\n## Tests\n```\n%s\n```\n", input.TestOutput))

	resp, err := r.llm.Complete(ctx, llm.LlmRequest{
		SystemPrompt: system,
		UserPrompt:   user.String(),
		MaxTokens:    2048,
		Temperature:  0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("final review LLM call: %w", err)
	}

	return ParseReviewOutput(resp.Content)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestFinalReviewer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/final_reviewer.go internal/pipeline/final_reviewer_test.go
git commit -m "feat: add final reviewer for complete changeset gating before PR"
```

---

### Task 5: PR Creator (git/pr.go)

**Files:**
- Create: `internal/git/pr.go`
- Test: `internal/git/pr_test.go`

**Step 1: Write the failing test**

```go
// internal/git/pr_test.go
package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatPRBody_Full(t *testing.T) {
	body := FormatPRBody(PRBodyInput{
		TicketExternalID: "PROJ-123",
		TicketTitle:      "Add user management",
		TaskSummaries: []PRTaskSummary{
			{Title: "Add user model", Status: "done"},
			{Title: "Add user handler", Status: "done"},
		},
		ReviewNotes: "Consider adding pagination in a follow-up.",
		IsPartial:   false,
	})

	assert.Contains(t, body, "PROJ-123")
	assert.Contains(t, body, "Add user management")
	assert.Contains(t, body, "Add user model")
	assert.Contains(t, body, "Add user handler")
	assert.Contains(t, body, "pagination")
	assert.Contains(t, body, "Foreman")
}

func TestFormatPRBody_Partial(t *testing.T) {
	body := FormatPRBody(PRBodyInput{
		TicketExternalID: "PROJ-456",
		TicketTitle:      "Refactor auth",
		TaskSummaries: []PRTaskSummary{
			{Title: "Extract middleware", Status: "done"},
			{Title: "Add JWT validation", Status: "failed"},
			{Title: "Update routes", Status: "pending"},
		},
		IsPartial:      true,
		FailedTask:     "Add JWT validation",
		FailureReason:  "Tests failed after 2 retries",
	})

	assert.Contains(t, body, "PARTIAL")
	assert.Contains(t, body, "PROJ-456")
	assert.Contains(t, body, "failed")
	assert.Contains(t, body, "JWT validation")
	assert.Contains(t, body, "human developer")
}

func TestPRRequest_Defaults(t *testing.T) {
	req := NewPRRequest("PROJ-123", "Add users", "foreman/PROJ-123-add-users", "main", true, []string{"team-lead"})
	assert.Equal(t, "[Foreman] PROJ-123: Add users", req.Title)
	assert.Equal(t, "foreman/PROJ-123-add-users", req.HeadBranch)
	assert.Equal(t, "main", req.BaseBranch)
	assert.True(t, req.Draft)
	assert.Contains(t, req.Reviewers, "team-lead")
	assert.Contains(t, req.Labels, "foreman-generated")
}

func TestPRRequest_Partial(t *testing.T) {
	req := NewPartialPRRequest("PROJ-123", "Add users", "foreman/PROJ-123-add-users", "main", []string{})
	assert.Contains(t, req.Title, "[PARTIAL]")
	assert.True(t, req.Draft)
	assert.Contains(t, req.Labels, "partial")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run "TestFormatPRBody|TestPRRequest" -v`
Expected: FAIL — `FormatPRBody` not defined

**Step 3: Write minimal implementation**

```go
// internal/git/pr.go
package git

import (
	"fmt"
	"strings"
)

// PrRequest describes a pull request to create.
type PrRequest struct {
	Title      string
	Body       string
	HeadBranch string
	BaseBranch string
	Draft      bool
	Reviewers  []string
	Labels     []string
}

// PrResponse holds the created PR details.
type PrResponse struct {
	Number  int
	URL     string
	HTMLURL string
}

// PRTaskSummary is a brief task status for PR body formatting.
type PRTaskSummary struct {
	Title  string
	Status string
}

// PRBodyInput holds data for formatting a PR body.
type PRBodyInput struct {
	TicketExternalID string
	TicketTitle      string
	TaskSummaries    []PRTaskSummary
	ReviewNotes      string
	IsPartial        bool
	FailedTask       string
	FailureReason    string
}

// FormatPRBody generates a formatted PR description.
func FormatPRBody(input PRBodyInput) string {
	var sb strings.Builder

	if input.IsPartial {
		sb.WriteString("## ⚠️ PARTIAL Implementation\n\n")
		sb.WriteString("This PR contains **partial** implementation. A human developer should review and complete the remaining work.\n\n")
	}

	sb.WriteString(fmt.Sprintf("## %s: %s\n\n", input.TicketExternalID, input.TicketTitle))

	sb.WriteString("### Tasks\n\n")
	for _, t := range input.TaskSummaries {
		icon := "✅"
		switch t.Status {
		case "failed":
			icon = "❌"
		case "pending", "skipped":
			icon = "⏳"
		}
		sb.WriteString(fmt.Sprintf("- %s %s — %s\n", icon, t.Title, t.Status))
	}

	if input.IsPartial && input.FailedTask != "" {
		sb.WriteString(fmt.Sprintf("\n### Failed Task\n**%s**: %s\n", input.FailedTask, input.FailureReason))
		sb.WriteString("\nA human developer should complete the remaining tasks.\n")
	}

	if input.ReviewNotes != "" {
		sb.WriteString(fmt.Sprintf("\n### Review Notes\n%s\n", input.ReviewNotes))
	}

	sb.WriteString("\n---\n🤖 *Generated by [Foreman](https://github.com/canhta/foreman)*\n")

	return sb.String()
}

// NewPRRequest creates a standard PR request.
func NewPRRequest(externalID, title, headBranch, baseBranch string, draft bool, reviewers []string) PrRequest {
	return PrRequest{
		Title:      fmt.Sprintf("[Foreman] %s: %s", externalID, title),
		HeadBranch: headBranch,
		BaseBranch: baseBranch,
		Draft:      draft,
		Reviewers:  reviewers,
		Labels:     []string{"foreman-generated"},
	}
}

// NewPartialPRRequest creates a PR request for partial implementations.
func NewPartialPRRequest(externalID, title, headBranch, baseBranch string, reviewers []string) PrRequest {
	return PrRequest{
		Title:      fmt.Sprintf("[Foreman] [PARTIAL] %s: %s", externalID, title),
		HeadBranch: headBranch,
		BaseBranch: baseBranch,
		Draft:      true,
		Reviewers:  reviewers,
		Labels:     []string{"foreman-generated", "partial"},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run "TestFormatPRBody|TestPRRequest" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/pr.go internal/git/pr_test.go
git commit -m "feat: add PR creation helpers with full and partial PR body formatting"
```

---

### Task 6: GitHub PR API Client

**Files:**
- Create: `internal/git/github_pr.go`
- Test: `internal/git/github_pr_test.go`

**Step 1: Write the failing test**

```go
// internal/git/github_pr_test.go
package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubPRCreator_CreatePR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/repos/org/repo/pulls")
		assert.Equal(t, "token test-token", r.Header.Get("Authorization"))

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "[Foreman] PROJ-123: Add users", body["title"])
		assert.Equal(t, "foreman/PROJ-123", body["head"])
		assert.Equal(t, "main", body["base"])
		assert.Equal(t, true, body["draft"])

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number":   42,
			"url":      "https://api.github.com/repos/org/repo/pulls/42",
			"html_url": "https://github.com/org/repo/pull/42",
		})
	}))
	defer server.Close()

	client := NewGitHubPRCreator(server.URL, "test-token", "org", "repo")
	resp, err := client.CreatePR(context.Background(), PrRequest{
		Title:      "[Foreman] PROJ-123: Add users",
		Body:       "PR body",
		HeadBranch: "foreman/PROJ-123",
		BaseBranch: "main",
		Draft:      true,
	})

	require.NoError(t, err)
	assert.Equal(t, 42, resp.Number)
	assert.Equal(t, "https://github.com/org/repo/pull/42", resp.HTMLURL)
}

func TestGitHubPRCreator_CreatePR_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message": "Validation Failed"}`))
	}))
	defer server.Close()

	client := NewGitHubPRCreator(server.URL, "test-token", "org", "repo")
	_, err := client.CreatePR(context.Background(), PrRequest{
		Title:      "test",
		HeadBranch: "test-branch",
		BaseBranch: "main",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "422")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestGitHubPRCreator -v`
Expected: FAIL — `NewGitHubPRCreator` not defined

**Step 3: Write minimal implementation**

```go
// internal/git/github_pr.go
package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PRCreator abstracts PR creation across git hosts.
type PRCreator interface {
	CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error)
}

// GitHubPRCreator creates PRs via the GitHub REST API.
type GitHubPRCreator struct {
	baseURL string
	token   string
	owner   string
	repo    string
	client  *http.Client
}

// NewGitHubPRCreator creates a GitHub PR client.
func NewGitHubPRCreator(baseURL, token, owner, repo string) *GitHubPRCreator {
	return &GitHubPRCreator{
		baseURL: baseURL,
		token:   token,
		owner:   owner,
		repo:    repo,
		client:  &http.Client{},
	}
}

type githubPRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type githubPRResponse struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	HTMLURL string `json:"html_url"`
}

// CreatePR creates a pull request on GitHub.
func (g *GitHubPRCreator) CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error) {
	ghReq := githubPRRequest{
		Title: req.Title,
		Body:  req.Body,
		Head:  req.HeadBranch,
		Base:  req.BaseBranch,
		Draft: req.Draft,
	}

	body, err := json.Marshal(ghReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling PR request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls", g.baseURL, g.owner, g.repo)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "token "+g.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing PR request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var ghResp githubPRResponse
	if err := json.Unmarshal(respBody, &ghResp); err != nil {
		return nil, fmt.Errorf("decoding PR response: %w", err)
	}

	return &PrResponse{
		Number:  ghResp.Number,
		URL:     ghResp.URL,
		HTMLURL: ghResp.HTMLURL,
	}, nil
}

// Ensure GitHubPRCreator implements PRCreator.
var _ PRCreator = (*GitHubPRCreator)(nil)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestGitHubPRCreator -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/github_pr.go internal/git/github_pr_test.go
git commit -m "feat: add GitHub PR creator with REST API integration"
```

---

### Task 7: Skills Loader

**Files:**
- Create: `internal/skills/loader.go`
- Test: `internal/skills/loader_test.go`

**Step 1: Write the failing test**

```go
// internal/skills/loader_test.go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSkill_Valid(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: write-changelog
description: "Generate changelog entry"
trigger: pre_pr
steps:
  - id: generate
    type: llm_call
    prompt_template: "prompts/changelog.md.j2"
    model: "anthropic:claude-haiku"
  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ .Steps.generate.output }}"
    mode: prepend
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "write-changelog.yml"), []byte(skillYAML), 0o644))

	skill, err := LoadSkill(filepath.Join(dir, "write-changelog.yml"))
	require.NoError(t, err)
	assert.Equal(t, "write-changelog", skill.ID)
	assert.Equal(t, "pre_pr", skill.Trigger)
	assert.Len(t, skill.Steps, 2)
	assert.Equal(t, "llm_call", skill.Steps[0].Type)
	assert.Equal(t, "file_write", skill.Steps[1].Type)
}

func TestLoadSkill_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte("{{invalid yaml"), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
}

func TestLoadSkill_InvalidStepType(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: bad-skill
description: "Invalid"
trigger: post_lint
steps:
  - id: bad
    type: execute_arbitrary_code
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(skillYAML), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step type")
}

func TestLoadSkill_InvalidTrigger(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `id: bad-trigger
description: "Invalid trigger"
trigger: on_deploy
steps:
  - id: ok
    type: run_command
    command: "echo"
    args: ["hello"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(skillYAML), 0o644))

	_, err := LoadSkill(filepath.Join(dir, "bad.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid trigger")
}

func TestLoadSkillsDir(t *testing.T) {
	dir := t.TempDir()
	skill1 := `id: skill-a
description: "Skill A"
trigger: post_lint
steps:
  - id: run
    type: run_command
    command: "echo"
    args: ["a"]
`
	skill2 := `id: skill-b
description: "Skill B"
trigger: pre_pr
steps:
  - id: run
    type: run_command
    command: "echo"
    args: ["b"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skill-a.yml"), []byte(skill1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skill-b.yml"), []byte(skill2), 0o644))
	// Non-YAML file should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0o644))

	skills, err := LoadSkillsDir(dir)
	require.NoError(t, err)
	assert.Len(t, skills, 2)
	assert.Contains(t, skillIDs(skills), "skill-a")
	assert.Contains(t, skillIDs(skills), "skill-b")
}

func TestLoadSkillsDir_Empty(t *testing.T) {
	dir := t.TempDir()
	skills, err := LoadSkillsDir(dir)
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestLoadSkillsDir_NonExistent(t *testing.T) {
	skills, err := LoadSkillsDir("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func skillIDs(skills []*Skill) []string {
	ids := make([]string, len(skills))
	for i, s := range skills {
		ids[i] = s.ID
	}
	return ids
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run "TestLoadSkill" -v`
Expected: FAIL — `LoadSkill` not defined

**Step 3: Write minimal implementation**

```go
// internal/skills/loader.go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is a YAML workflow definition.
type Skill struct {
	ID          string      `yaml:"id"`
	Description string      `yaml:"description"`
	Trigger     string      `yaml:"trigger"`
	Steps       []SkillStep `yaml:"steps"`
}

// SkillStep is one step within a skill.
type SkillStep struct {
	ID             string            `yaml:"id"`
	Type           string            `yaml:"type"`
	PromptTemplate string            `yaml:"prompt_template,omitempty"`
	Model          string            `yaml:"model,omitempty"`
	Context        map[string]string `yaml:"context,omitempty"`
	Command        string            `yaml:"command,omitempty"`
	Args           []string          `yaml:"args,omitempty"`
	AllowFailure   bool              `yaml:"allow_failure,omitempty"`
	Path           string            `yaml:"path,omitempty"`
	Content        string            `yaml:"content,omitempty"`
	Mode           string            `yaml:"mode,omitempty"`
}

// StepResult holds the output of an executed skill step.
type StepResult struct {
	Output   string
	Stderr   string
	ExitCode int
	Error    string
}

var validTriggers = map[string]bool{
	"post_lint": true,
	"pre_pr":    true,
	"post_pr":   true,
}

var validStepTypes = map[string]bool{
	"llm_call":    true,
	"run_command": true,
	"file_write":  true,
	"git_diff":    true,
}

// LoadSkill loads and validates a single skill file.
func LoadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading skill file %s: %w", path, err)
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return nil, fmt.Errorf("parsing skill file %s: %w", path, err)
	}

	// Validate trigger
	if !validTriggers[skill.Trigger] {
		return nil, fmt.Errorf("invalid trigger '%s' in skill '%s' (valid: post_lint, pre_pr, post_pr)", skill.Trigger, skill.ID)
	}

	// Validate step types
	for _, step := range skill.Steps {
		if !validStepTypes[step.Type] {
			return nil, fmt.Errorf("unknown step type '%s' in skill '%s' step '%s'", step.Type, skill.ID, step.ID)
		}
	}

	return &skill, nil
}

// LoadSkillsDir loads all .yml/.yaml files from a directory.
func LoadSkillsDir(dir string) ([]*Skill, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory %s: %w", dir, err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() {
			// Recurse into subdirectories (e.g., community/)
			subSkills, err := LoadSkillsDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			skills = append(skills, subSkills...)
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		skill, err := LoadSkill(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}

	return skills, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run "TestLoadSkill" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/loader.go internal/skills/loader_test.go
git commit -m "feat: add YAML skills loader with validation for triggers and step types"
```

---

### Task 8: Skills Engine

**Files:**
- Create: `internal/skills/engine.go`
- Test: `internal/skills/engine_test.go`

**Step 1: Write the failing test**

```go
// internal/skills/engine_test.go
package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLLMProvider struct {
	response string
}

func (m *mockLLMProvider) Complete(_ context.Context, req llm.LlmRequest) (*llm.LlmResponse, error) {
	return &llm.LlmResponse{Content: m.response, TokensInput: 10, TokensOutput: 5}, nil
}
func (m *mockLLMProvider) ProviderName() string                { return "mock" }
func (m *mockLLMProvider) HealthCheck(_ context.Context) error { return nil }

type mockRunner struct {
	stdout   string
	exitCode int
}

func (m *mockRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stdout: m.stdout, ExitCode: m.exitCode}, nil
}
func (m *mockRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestEngine_ExecuteRunCommand(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "hello", exitCode: 0}, workDir, "main")

	skill := &Skill{
		ID:      "test-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo", Args: []string{"hello"}},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "hello", sCtx.Steps["run"].Output)
}

func TestEngine_ExecuteFileWrite(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")

	skill := &Skill{
		ID:      "write-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "write", Type: "file_write", Path: "output.txt", Content: "hello world", Mode: "overwrite"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "output.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestEngine_ExecuteFileWrite_Prepend(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "log.txt"), []byte("existing content"), 0o644))

	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")
	skill := &Skill{
		ID:      "prepend-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "prepend", Type: "file_write", Path: "log.txt", Content: "new header", Mode: "prepend"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "log.txt"))
	require.NoError(t, err)
	assert.True(t, len(string(content)) > 0)
	assert.Contains(t, string(content), "new header")
	assert.Contains(t, string(content), "existing content")
}

func TestEngine_ExecuteLLMCall(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(
		&mockLLMProvider{response: "Generated changelog entry"},
		&mockRunner{},
		workDir,
		"main",
	)

	skill := &Skill{
		ID:      "llm-skill",
		Trigger: "pre_pr",
		Steps: []SkillStep{
			{ID: "generate", Type: "llm_call", Model: "test-model"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "Generated changelog entry", sCtx.Steps["generate"].Output)
}

func TestEngine_AllowFailure(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

	skill := &Skill{
		ID:      "failing-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "may-fail", Type: "run_command", Command: "false", AllowFailure: true},
			{ID: "after", Type: "run_command", Command: "echo", Args: []string{"ok"}},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	require.NoError(t, err)
	// First step failed but allowed, second step ran
	assert.NotEmpty(t, sCtx.Steps["may-fail"].Error)
	assert.NotNil(t, sCtx.Steps["after"])
}

func TestEngine_StepFailureStopsExecution(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

	skill := &Skill{
		ID:      "strict-skill",
		Trigger: "post_lint",
		Steps: []SkillStep{
			{ID: "must-pass", Type: "run_command", Command: "false", AllowFailure: false},
			{ID: "never-runs", Type: "run_command", Command: "echo"},
		},
	}

	sCtx := NewSkillContext()
	err := engine.Execute(context.Background(), skill, sCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must-pass")
	// Second step should not have run
	assert.Nil(t, sCtx.Steps["never-runs"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestEngine -v`
Expected: FAIL — `NewEngine` not defined

**Step 3: Write minimal implementation**

```go
// internal/skills/engine.go
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/runner"
)

// SkillContext holds the execution context for a skill run.
type SkillContext struct {
	Ticket   interface{}            // Current ticket (set by caller)
	Diff     string                 // Current branch diff
	FileTree string                 // Repo file tree
	Models   map[string]string      // Model routing config
	Steps    map[string]*StepResult // Results of previous steps
}

// NewSkillContext creates an empty skill execution context.
func NewSkillContext() *SkillContext {
	return &SkillContext{
		Steps: make(map[string]*StepResult),
	}
}

// Engine executes YAML skill definitions.
type Engine struct {
	llm           llm.LlmProvider
	runner        runner.CommandRunner
	workDir       string
	defaultBranch string
}

// NewEngine creates a skill engine.
func NewEngine(llmProvider llm.LlmProvider, cmdRunner runner.CommandRunner, workDir, defaultBranch string) *Engine {
	return &Engine{
		llm:           llmProvider,
		runner:        cmdRunner,
		workDir:       workDir,
		defaultBranch: defaultBranch,
	}
}

// Execute runs all steps in a skill sequentially.
func (e *Engine) Execute(ctx context.Context, skill *Skill, sCtx *SkillContext) error {
	for _, step := range skill.Steps {
		result, err := e.executeStep(ctx, step, sCtx)
		if err != nil {
			if step.AllowFailure {
				sCtx.Steps[step.ID] = &StepResult{Error: err.Error()}
				continue
			}
			return fmt.Errorf("skill '%s' step '%s' failed: %w", skill.ID, step.ID, err)
		}
		sCtx.Steps[step.ID] = result
	}
	return nil
}

func (e *Engine) executeStep(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	switch step.Type {
	case "llm_call":
		return e.executeLLMCall(ctx, step, sCtx)
	case "run_command":
		return e.executeRunCommand(ctx, step, sCtx)
	case "file_write":
		return e.executeFileWrite(step, sCtx)
	case "git_diff":
		return e.executeGitDiff(ctx)
	default:
		return nil, fmt.Errorf("unknown step type: %s", step.Type)
	}
}

func (e *Engine) executeLLMCall(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	// Build a simple prompt from step context or template reference
	prompt := step.Content
	if prompt == "" {
		prompt = fmt.Sprintf("Execute skill step: %s", step.ID)
	}

	resp, err := e.llm.Complete(ctx, llm.LlmRequest{
		Model:      step.Model,
		UserPrompt: prompt,
		MaxTokens:  4096,
	})
	if err != nil {
		return nil, err
	}
	return &StepResult{Output: resp.Content}, nil
}

func (e *Engine) executeRunCommand(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	out, err := e.runner.Run(ctx, e.workDir, step.Command, step.Args, 120)
	if err != nil {
		return nil, err
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("command '%s' failed (exit %d): %s", step.Command, out.ExitCode, out.Stderr)
	}
	return &StepResult{Output: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode}, nil
}

func (e *Engine) executeFileWrite(step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	path := filepath.Join(e.workDir, step.Path)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", step.Path, err)
	}

	content := step.Content

	switch step.Mode {
	case "prepend":
		existing, _ := os.ReadFile(path)
		content = content + "\n" + string(existing)
	case "append":
		existing, _ := os.ReadFile(path)
		content = string(existing) + "\n" + content
	// default: overwrite
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing file %s: %w", step.Path, err)
	}
	return &StepResult{Output: path}, nil
}

func (e *Engine) executeGitDiff(ctx context.Context) (*StepResult, error) {
	// For now, return empty diff — full git integration happens via the git provider
	// which will be injected when the pipeline wires this up
	return &StepResult{Output: ""}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run TestEngine -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/engine.go internal/skills/engine_test.go
git commit -m "feat: add YAML skill engine with llm_call, run_command, file_write, git_diff primitives"
```

---

### Task 9: Pipeline Hooks

**Files:**
- Create: `internal/skills/hooks.go`
- Test: `internal/skills/hooks_test.go`

**Step 1: Write the failing test**

```go
// internal/skills/hooks_test.go
package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHookRunner_RunHook_PostLint(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "security-scan", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "scan", Type: "run_command", Command: "echo", Args: []string{"scanned"}},
		}},
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{
			{ID: "gen", Type: "run_command", Command: "echo", Args: []string{"changelog"}},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "security-scan", results[0].SkillID)
	assert.NoError(t, results[0].Error)
}

func TestHookRunner_RunHook_PrePR(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{
			{ID: "gen", Type: "run_command", Command: "echo", Args: []string{"changelog"}},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "pre_pr", NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "changelog", results[0].SkillID)
	assert.NoError(t, results[0].Error)
}

func TestHookRunner_RunHook_NoMatchingSkills(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{}, workDir, "main")

	skills := []*Skill{
		{ID: "changelog", Trigger: "pre_pr", Steps: []SkillStep{}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	assert.Empty(t, results)
}

func TestHookRunner_RunHook_SkillFailure(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{exitCode: 1, stdout: "error"}, workDir, "main")

	skills := []*Skill{
		{ID: "broken", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "fail", Type: "run_command", Command: "false"},
		}},
		{ID: "working", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "ok", Type: "run_command", Command: "echo"},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHook(context.Background(), "post_lint", NewSkillContext())

	// Both skills attempted — failure doesn't block
	assert.Len(t, results, 2)
	assert.Error(t, results[0].Error)
	// Second skill also fails because mock always returns exitCode=1
	// The important thing is both ran
}

func TestHookRunner_FilterByNames(t *testing.T) {
	workDir := t.TempDir()
	engine := NewEngine(&mockLLMProvider{}, &mockRunner{stdout: "ok", exitCode: 0}, workDir, "main")

	skills := []*Skill{
		{ID: "skill-a", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo"},
		}},
		{ID: "skill-b", Trigger: "post_lint", Steps: []SkillStep{
			{ID: "run", Type: "run_command", Command: "echo"},
		}},
	}

	hookRunner := NewHookRunner(engine, skills)
	results := hookRunner.RunHookByNames(context.Background(), "post_lint", []string{"skill-a"}, NewSkillContext())

	assert.Len(t, results, 1)
	assert.Equal(t, "skill-a", results[0].SkillID)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestHookRunner -v`
Expected: FAIL — `NewHookRunner` not defined

**Step 3: Write minimal implementation**

```go
// internal/skills/hooks.go
package skills

import (
	"context"
)

// HookResult holds the outcome of running a single skill at a hook point.
type HookResult struct {
	SkillID string
	Error   error
}

// HookRunner executes skills at pipeline hook points.
type HookRunner struct {
	engine *Engine
	skills []*Skill
}

// NewHookRunner creates a hook runner with all loaded skills.
func NewHookRunner(engine *Engine, skills []*Skill) *HookRunner {
	return &HookRunner{engine: engine, skills: skills}
}

// RunHook executes all skills matching the given trigger.
// Failures are recorded but do not block execution of subsequent skills.
func (h *HookRunner) RunHook(ctx context.Context, trigger string, sCtx *SkillContext) []HookResult {
	var results []HookResult
	for _, skill := range h.skills {
		if skill.Trigger != trigger {
			continue
		}
		err := h.engine.Execute(ctx, skill, sCtx)
		results = append(results, HookResult{
			SkillID: skill.ID,
			Error:   err,
		})
	}
	return results
}

// RunHookByNames executes only the named skills matching the trigger.
// Used when the pipeline config specifies which skills to run at a hook point.
func (h *HookRunner) RunHookByNames(ctx context.Context, trigger string, names []string, sCtx *SkillContext) []HookResult {
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var results []HookResult
	for _, skill := range h.skills {
		if skill.Trigger != trigger || !nameSet[skill.ID] {
			continue
		}
		err := h.engine.Execute(ctx, skill, sCtx)
		results = append(results, HookResult{
			SkillID: skill.ID,
			Error:   err,
		})
	}
	return results
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run TestHookRunner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/hooks.go internal/skills/hooks_test.go
git commit -m "feat: add pipeline hook runner for post_lint, pre_pr, post_pr skill execution"
```

---

### Task 10: Partial PR Handler

**Files:**
- Create: `internal/pipeline/partial_pr.go`
- Test: `internal/pipeline/partial_pr_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/partial_pr_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldCreatePartialPR_WithCompletedTasks(t *testing.T) {
	assert.True(t, ShouldCreatePartialPR(5, 3, true))
}

func TestShouldCreatePartialPR_NoCompletedTasks(t *testing.T) {
	assert.False(t, ShouldCreatePartialPR(5, 0, true))
}

func TestShouldCreatePartialPR_Disabled(t *testing.T) {
	assert.False(t, ShouldCreatePartialPR(5, 3, false))
}

func TestShouldCreatePartialPR_AllComplete(t *testing.T) {
	// Not partial if all tasks are done
	assert.False(t, ShouldCreatePartialPR(5, 5, true))
}

func TestFormatPartialPRComment(t *testing.T) {
	comment := FormatPartialPRComment(42, 3, 5, "Add JWT validation", "Tests failed after 2 retries", []string{"Update routes", "Add middleware"})
	assert.Contains(t, comment, "PR #42")
	assert.Contains(t, comment, "3/5")
	assert.Contains(t, comment, "JWT validation")
	assert.Contains(t, comment, "Tests failed")
	assert.Contains(t, comment, "Update routes")
	assert.Contains(t, comment, "human developer")
}

func TestFormatRemainingTasks(t *testing.T) {
	tasks := []string{"Task A", "Task B", "Task C"}
	formatted := FormatRemainingTasks(tasks)
	assert.Contains(t, formatted, "- Task A")
	assert.Contains(t, formatted, "- Task B")
	assert.Contains(t, formatted, "- Task C")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run "TestShouldCreate|TestFormatPartial|TestFormatRemaining" -v`
Expected: FAIL — functions not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/partial_pr.go
package pipeline

import (
	"fmt"
	"strings"
)

// ShouldCreatePartialPR determines whether to create a partial PR.
func ShouldCreatePartialPR(totalTasks, completedTasks int, enabled bool) bool {
	if !enabled {
		return false
	}
	if completedTasks == 0 {
		return false
	}
	if completedTasks >= totalTasks {
		return false // Not partial if all done
	}
	return true
}

// FormatPartialPRComment creates the issue tracker comment for a partial PR.
func FormatPartialPRComment(prNumber, completed, total int, failedTask, failureReason string, remainingTasks []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⚠️ PR #%d opened with **partial** implementation (%d/%d tasks complete).\n\n", prNumber, completed, total))
	sb.WriteString(fmt.Sprintf("**Failed task:** %s\n**Reason:** %s\n\n", failedTask, failureReason))
	sb.WriteString(fmt.Sprintf("**Remaining tasks:**\n%s\n\n", FormatRemainingTasks(remainingTasks)))
	sb.WriteString("A human developer should review the PR and complete the remaining work.")
	return sb.String()
}

// FormatRemainingTasks formats a list of remaining task names as markdown.
func FormatRemainingTasks(tasks []string) string {
	var sb strings.Builder
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("- %s\n", t))
	}
	return strings.TrimSpace(sb.String())
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run "TestShouldCreate|TestFormatPartial|TestFormatRemaining" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/partial_pr.go internal/pipeline/partial_pr_test.go
git commit -m "feat: add partial PR support with conditional creation and formatted comments"
```
