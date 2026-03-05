# Phase 2: Full Pipeline — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up the full pipeline: give a ticket description → get multi-task commits. Planner decomposes tickets into tasks, YAML parser handles LLM output, TDD verifier catches invalid RED, feedback loops retry failed tasks, context assembler builds surgical LLM context, native git ops manage branches and commits.

**Architecture:** The pipeline orchestrator is a sequential state machine that drives a ticket through planning → validation → per-task execution (implement → TDD verify → lint/test → commit) with tiered feedback. Context assembly scores and ranks files within a token budget. Native git CLI handles all git operations. All new packages depend on Phase 1 foundations (models, db, llm, runner, config, output_parser, secrets_scanner, token_budget).

**Tech Stack:** Go 1.26, SQLite (go-sqlite3), pongo2 (templates), tiktoken-go (token counting), zerolog (logging), strutil (fuzzy matching), gopkg.in/yaml.v3 (YAML parsing)

---

### Task 1: YAML Parser for Planner Output

**Files:**
- Create: `internal/pipeline/yaml_parser.go`
- Test: `internal/pipeline/yaml_parser_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/yaml_parser_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlannerOutput_StrictYAML(t *testing.T) {
	raw := `status: OK
message: ""

codebase_patterns:
  language: "go"
  framework: "stdlib"
  test_runner: "go test"
  style_notes: "standard go conventions"

tasks:
  - title: "Add user model"
    description: "Create the user model struct with validation."
    acceptance_criteria:
      - "User struct has Name, Email, ID fields"
    test_assertions:
      - "TestNewUser creates valid user"
    files_to_read:
      - "internal/models/ticket.go"
    files_to_modify:
      - "internal/models/user.go (new)"
    estimated_complexity: "simple"
    depends_on: []
`
	result, err := ParsePlannerOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "Add user model", result.Tasks[0].Title)
	assert.Equal(t, "simple", result.Tasks[0].EstimatedComplexity)
	assert.Equal(t, "go", result.CodebasePatterns.Language)
}

func TestParsePlannerOutput_MarkdownFenced(t *testing.T) {
	raw := "Here is the plan:\n\n```yaml\nstatus: OK\nmessage: \"\"\n\ntasks:\n  - title: \"Fix bug\"\n    description: \"Fix the nil pointer.\"\n    acceptance_criteria:\n      - \"No panic on nil input\"\n    test_assertions:\n      - \"TestNilInput returns error\"\n    files_to_read: []\n    files_to_modify:\n      - \"internal/handler.go\"\n    estimated_complexity: \"simple\"\n    depends_on: []\n```\n\nLet me know if you need changes."
	result, err := ParsePlannerOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "Fix bug", result.Tasks[0].Title)
}

func TestParsePlannerOutput_ClarificationNeeded(t *testing.T) {
	raw := "I need more information.\n\nCLARIFICATION_NEEDED: What database should the user model use? PostgreSQL or SQLite?"
	result, err := ParsePlannerOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, "CLARIFICATION_NEEDED", result.Status)
	assert.Contains(t, result.Message, "What database")
}

func TestParsePlannerOutput_TicketTooLarge(t *testing.T) {
	raw := "TICKET_TOO_LARGE: This ticket requires 25+ tasks. Break it into smaller tickets."
	result, err := ParsePlannerOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, "TICKET_TOO_LARGE", result.Status)
	assert.Contains(t, result.Message, "25+ tasks")
}

func TestParsePlannerOutput_TotalFailure(t *testing.T) {
	raw := "random garbage with no structure at all $$$$"
	_, err := ParsePlannerOutput(raw)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestParsePlannerOutput -v`
Expected: FAIL — `ParsePlannerOutput` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/yaml_parser.go
package pipeline

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlannerResult is the parsed output from the planner LLM call.
type PlannerResult struct {
	Status           string           `yaml:"status"`
	Message          string           `yaml:"message"`
	CodebasePatterns CodebasePatterns `yaml:"codebase_patterns"`
	Tasks            []PlannedTask    `yaml:"tasks"`
}

type CodebasePatterns struct {
	Language   string `yaml:"language"`
	Framework  string `yaml:"framework"`
	TestRunner string `yaml:"test_runner"`
	StyleNotes string `yaml:"style_notes"`
}

type PlannedTask struct {
	Title               string   `yaml:"title"`
	Description         string   `yaml:"description"`
	AcceptanceCriteria  []string `yaml:"acceptance_criteria"`
	TestAssertions      []string `yaml:"test_assertions"`
	FilesToRead         []string `yaml:"files_to_read"`
	FilesToModify       []string `yaml:"files_to_modify"`
	EstimatedComplexity string   `yaml:"estimated_complexity"`
	DependsOn           []string `yaml:"depends_on"`
}

// ParsePlannerOutput parses LLM planner output using a strict → permissive → partial fallback chain.
func ParsePlannerOutput(raw string) (*PlannerResult, error) {
	// Strategy 1: Strict YAML parse
	result, err := parseStrictYAML(raw)
	if err == nil && result.Status != "" {
		return result, nil
	}

	// Strategy 2: Strip markdown fences and prose, then parse
	cleaned := stripMarkdownFences(raw)
	result, err = parseStrictYAML(cleaned)
	if err == nil && result.Status != "" {
		return result, nil
	}

	// Strategy 3: Look for status field anywhere in text
	if idx := strings.Index(cleaned, "status:"); idx != -1 {
		result, err = parseStrictYAML(cleaned[idx:])
		if err == nil && result.Status != "" {
			return result, nil
		}
	}

	// Strategy 4: Extract special statuses from prose
	if strings.Contains(raw, "CLARIFICATION_NEEDED") {
		question := extractAfterKey(raw, "CLARIFICATION_NEEDED")
		return &PlannerResult{
			Status:  "CLARIFICATION_NEEDED",
			Message: question,
		}, nil
	}
	if strings.Contains(raw, "TICKET_TOO_LARGE") {
		message := extractAfterKey(raw, "TICKET_TOO_LARGE")
		return &PlannerResult{
			Status:  "TICKET_TOO_LARGE",
			Message: message,
		}, nil
	}

	return nil, fmt.Errorf("failed to parse planner output (all strategies failed), raw length: %d", len(raw))
}

func parseStrictYAML(raw string) (*PlannerResult, error) {
	var result PlannerResult
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(false)
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

var fencePattern = regexp.MustCompile("(?s)```(?:yaml|yml)?\\s*\n(.*?)```")

func stripMarkdownFences(raw string) string {
	matches := fencePattern.FindStringSubmatch(raw)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return raw
}

func extractAfterKey(raw, key string) string {
	idx := strings.Index(raw, key)
	if idx == -1 {
		return ""
	}
	after := raw[idx+len(key):]
	// Skip colon and whitespace
	after = strings.TrimLeft(after, ": \t")
	// Take until end of line or end of string
	if nlIdx := strings.Index(after, "\n"); nlIdx != -1 {
		return strings.TrimSpace(after[:nlIdx])
	}
	return strings.TrimSpace(after)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestParsePlannerOutput -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/yaml_parser.go internal/pipeline/yaml_parser_test.go
git commit -m "feat: add YAML parser for planner output with strict/permissive/partial fallback"
```

---

### Task 2: Plan Validator

**Files:**
- Create: `internal/pipeline/plan_validator.go`
- Test: `internal/pipeline/plan_validator_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/plan_validator_test.go
package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePlan_ValidPlan(t *testing.T) {
	workDir := t.TempDir()
	// Create files that the plan references
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal/models"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/models/user.go"), []byte("package models"), 0o644))

	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Add validation",
				FilesToRead:         []string{"internal/models/user.go"},
				FilesToModify:       []string{"internal/models/user.go"},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{
		MaxTasksPerTicket: 20,
	}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidatePlan_NonExistentFileToRead(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Read missing file",
				FilesToRead:         []string{"does/not/exist.go"},
				FilesToModify:       []string{},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "non-existent file")
}

func TestValidatePlan_NewFileMarker(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Create new file",
				FilesToModify:       []string{"internal/new_file.go (new)"},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid)
}

func TestValidatePlan_CyclicDependencies(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Task A", DependsOn: []string{"Task B"}, EstimatedComplexity: "simple"},
			{Title: "Task B", DependsOn: []string{"Task A"}, EstimatedComplexity: "simple"},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "cycle")
}

func TestValidatePlan_TooManyTasks(t *testing.T) {
	workDir := t.TempDir()
	tasks := make([]PlannedTask, 25)
	for i := range tasks {
		tasks[i] = PlannedTask{Title: "task", EstimatedComplexity: "simple"}
	}
	plan := &PlannerResult{Status: "OK", Tasks: tasks}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "exceeding limit")
}

func TestValidatePlan_SharedFileWithoutOrdering(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))

	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Task A", FilesToModify: []string{"internal/handler.go"}, EstimatedComplexity: "simple"},
			{Title: "Task B", FilesToModify: []string{"internal/handler.go"}, EstimatedComplexity: "simple"},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid) // Warnings don't make it invalid
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "Multiple tasks modify")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestValidatePlan -v`
Expected: FAIL — `ValidatePlan` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/plan_validator.go
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// PlanValidation holds the result of validating a planner's output.
type PlanValidation struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

func (v *PlanValidation) addError(format string, args ...interface{}) {
	v.Errors = append(v.Errors, fmt.Sprintf(format, args...))
	v.Valid = false
}

func (v *PlanValidation) addWarning(format string, args ...interface{}) {
	v.Warnings = append(v.Warnings, fmt.Sprintf(format, args...))
}

// ValidatePlan checks a planner result for issues before execution.
func ValidatePlan(plan *PlannerResult, workDir string, config *models.LimitsConfig) *PlanValidation {
	v := &PlanValidation{Valid: true}

	// 1. Check task count limit
	if len(plan.Tasks) > config.MaxTasksPerTicket {
		v.addError("Plan has %d tasks, exceeding limit of %d", len(plan.Tasks), config.MaxTasksPerTicket)
	}

	// 2. Check file paths exist
	for _, task := range plan.Tasks {
		for _, path := range task.FilesToRead {
			if !fileExistsAt(workDir, path) {
				v.addError("Task '%s' references non-existent file: %s", task.Title, path)
			}
		}
		for _, path := range task.FilesToModify {
			if strings.HasSuffix(path, "(new)") {
				continue // New files don't need to exist
			}
			if !fileExistsAt(workDir, path) {
				v.addError("Task '%s' modifies non-existent file: %s", task.Title, path)
			}
		}
	}

	// 3. Check for dependency cycles
	if hasDependencyCycle(plan.Tasks) {
		v.addError("Task dependencies contain a cycle")
	}

	// 4. Warn about shared files without explicit ordering
	fileOwners := map[string][]string{}
	for _, task := range plan.Tasks {
		for _, path := range task.FilesToModify {
			cleanPath := strings.TrimSuffix(path, " (new)")
			fileOwners[cleanPath] = append(fileOwners[cleanPath], task.Title)
		}
	}
	for path, owners := range fileOwners {
		if len(owners) > 1 {
			if !hasOrderingBetween(plan.Tasks, owners) {
				v.addWarning("Multiple tasks modify '%s' without explicit ordering: %v", path, owners)
			}
		}
	}

	return v
}

func fileExistsAt(workDir, path string) bool {
	_, err := os.Stat(filepath.Join(workDir, path))
	return err == nil
}

func hasDependencyCycle(tasks []PlannedTask) bool {
	// Build adjacency map by title
	graph := map[string][]string{}
	for _, t := range tasks {
		graph[t.Title] = t.DependsOn
	}

	visited := map[string]bool{}
	inStack := map[string]bool{}

	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		inStack[node] = true
		for _, dep := range graph[node] {
			if !visited[dep] {
				if dfs(dep) {
					return true
				}
			} else if inStack[dep] {
				return true
			}
		}
		inStack[node] = false
		return false
	}

	for _, t := range tasks {
		if !visited[t.Title] {
			if dfs(t.Title) {
				return true
			}
		}
	}
	return false
}

func hasOrderingBetween(tasks []PlannedTask, titles []string) bool {
	titleSet := map[string]bool{}
	for _, t := range titles {
		titleSet[t] = true
	}
	for _, task := range tasks {
		if !titleSet[task.Title] {
			continue
		}
		for _, dep := range task.DependsOn {
			if titleSet[dep] {
				return true
			}
		}
	}
	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestValidatePlan -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/plan_validator.go internal/pipeline/plan_validator_test.go
git commit -m "feat: add plan validator with file path, cycle, and limit checks"
```

---

### Task 3: TDD Verifier

**Files:**
- Create: `internal/pipeline/tdd_verifier.go`
- Test: `internal/pipeline/tdd_verifier_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/tdd_verifier_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTestFailure_Assertion(t *testing.T) {
	stdout := "--- FAIL: TestAdd (0.00s)\n    add_test.go:10: expected 4, got 0"
	stderr := ""
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureAssertion, result)
}

func TestClassifyTestFailure_Compile(t *testing.T) {
	stdout := ""
	stderr := "# mypackage\n./add.go:5:2: undefined: SomeFunc"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureCompile, result)
}

func TestClassifyTestFailure_Import(t *testing.T) {
	stdout := ""
	stderr := "cannot find module providing package github.com/foo/bar"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureImport, result)
}

func TestClassifyTestFailure_Unknown(t *testing.T) {
	stdout := "some random output"
	stderr := "something went wrong"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureUnknown, result)
}

func TestIsTestFile(t *testing.T) {
	assert.True(t, IsTestFile("internal/foo/bar_test.go"))
	assert.True(t, IsTestFile("src/utils.test.ts"))
	assert.True(t, IsTestFile("tests/test_handler.py"))
	assert.True(t, IsTestFile("spec/models/user_spec.rb"))
	assert.False(t, IsTestFile("internal/foo/bar.go"))
	assert.False(t, IsTestFile("src/utils.ts"))
}

func TestNewTDDResult_ValidRed(t *testing.T) {
	result := &TDDResult{Valid: true}
	assert.True(t, result.Valid)
}

func TestNewTDDResult_InvalidRed_TestsPassed(t *testing.T) {
	result := &TDDResult{
		Valid:  false,
		Phase:  "red",
		Reason: "Tests passed without implementation code",
	}
	assert.False(t, result.Valid)
	assert.Equal(t, "red", result.Phase)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run "TestClassify|TestIsTest|TestNewTDD" -v`
Expected: FAIL — types and functions not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/tdd_verifier.go
package pipeline

import (
	"path/filepath"
	"strings"
)

// TDDResult holds the outcome of mechanical TDD verification.
type TDDResult struct {
	Valid  bool
	Reason string
	Phase  string // "red" or "green"
}

// TestFailureType categorizes how a test failed.
type TestFailureType string

const (
	FailureAssertion TestFailureType = "assertion" // Valid RED
	FailureCompile   TestFailureType = "compile"   // Invalid RED
	FailureImport    TestFailureType = "import"    // Invalid RED
	FailureRuntime   TestFailureType = "runtime"   // Ambiguous — treat as invalid
	FailureUnknown   TestFailureType = "unknown"
)

// ClassifyTestFailure parses test output to determine failure type.
func ClassifyTestFailure(stdout, stderr string) TestFailureType {
	combined := stdout + "\n" + stderr
	lower := strings.ToLower(combined)

	// Check for import errors first (more specific than compile)
	importPatterns := []string{
		"cannot find module",
		"module not found",
		"importerror",
		"import error",
		"no such file or directory",
		"could not resolve",
		"cannot resolve",
	}
	for _, p := range importPatterns {
		if strings.Contains(lower, p) {
			return FailureImport
		}
	}

	// Check for compile/syntax errors
	compilePatterns := []string{
		"syntaxerror",
		"syntax error",
		"compilation failed",
		"build failed",
		"error ts",
		"type error",
		"undefined: ",
		"cannot find symbol",
		"error[e",
	}
	for _, p := range compilePatterns {
		if strings.Contains(lower, p) {
			return FailureCompile
		}
	}

	// Check for assertion failures (valid RED)
	assertionPatterns := []string{
		"assertionerror",
		"assertion failed",
		"expect(",
		"expected",
		"assert.",
		"assertequal",
		"fail:",
		"failed:",
		"not equal",
		"not to equal",
		"tobetruthy",
		"tobefalsy",
		"should have",
		"should be",
	}
	for _, p := range assertionPatterns {
		if strings.Contains(lower, p) {
			return FailureAssertion
		}
	}

	return FailureUnknown
}

// IsTestFile returns true if the file path looks like a test file.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)

	// Go: _test.go
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	// JS/TS: .test.ts, .test.js, .spec.ts, .spec.js
	for _, suffix := range []string{".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	// Python: test_*.py or *_test.py
	if strings.HasSuffix(lower, ".py") && (strings.HasPrefix(lower, "test_") || strings.HasSuffix(strings.TrimSuffix(lower, ".py"), "_test")) {
		return true
	}
	// Ruby: _spec.rb
	if strings.HasSuffix(lower, "_spec.rb") {
		return true
	}
	// Directory-based: tests/, test/, spec/
	dir := filepath.Dir(path)
	parts := strings.Split(dir, string(filepath.Separator))
	for _, part := range parts {
		if part == "tests" || part == "test" || part == "spec" || part == "__tests__" {
			return true
		}
	}
	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run "TestClassify|TestIsTest|TestNewTDD" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/tdd_verifier.go internal/pipeline/tdd_verifier_test.go
git commit -m "feat: add TDD verifier with failure classification and test file detection"
```

---

### Task 4: Feedback Accumulator

**Files:**
- Create: `internal/pipeline/feedback.go`
- Test: `internal/pipeline/feedback_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/feedback_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFeedbackAccumulator_Empty(t *testing.T) {
	fb := NewFeedbackAccumulator()
	assert.False(t, fb.HasFeedback())
	assert.Equal(t, 0, fb.Attempt())
}

func TestFeedbackAccumulator_AddLintError(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddLintError("main.go:10: unused variable 'x'")
	assert.True(t, fb.HasFeedback())
	assert.Equal(t, 1, fb.Attempt())
	rendered := fb.Render()
	assert.Contains(t, rendered, "Lint errors")
	assert.Contains(t, rendered, "unused variable")
}

func TestFeedbackAccumulator_AddTestError(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddTestError("--- FAIL: TestFoo (0.00s)\n    foo_test.go:5: expected 1 got 2")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Test failures")
}

func TestFeedbackAccumulator_AddSpecFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddSpecFeedback("Missing error handling for nil input")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Spec review")
	assert.Contains(t, rendered, "nil input")
}

func TestFeedbackAccumulator_AddQualityFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddQualityFeedback("[CRITICAL] SQL injection in query builder")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Quality review")
	assert.Contains(t, rendered, "SQL injection")
}

func TestFeedbackAccumulator_AddTDDFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddTDDFeedback("Tests passed without implementation — tests are not verifying new behavior")
	rendered := fb.Render()
	assert.Contains(t, rendered, "TDD verification")
}

func TestFeedbackAccumulator_MultipleFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddLintError("error1")
	fb.AddTestError("error2")
	fb.AddSpecFeedback("spec issue")
	assert.Equal(t, 3, fb.Attempt())
	rendered := fb.Render()
	assert.Contains(t, rendered, "Lint errors")
	assert.Contains(t, rendered, "Test failures")
	assert.Contains(t, rendered, "Spec review")
}

func TestFeedbackAccumulator_Truncation(t *testing.T) {
	fb := NewFeedbackAccumulator()
	longError := make([]byte, 5000)
	for i := range longError {
		longError[i] = 'a'
	}
	fb.AddTestError(string(longError))
	rendered := fb.Render()
	assert.Less(t, len(rendered), 4000) // Should be truncated
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestFeedbackAccumulator -v`
Expected: FAIL — `NewFeedbackAccumulator` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/feedback.go
package pipeline

import (
	"fmt"
	"strings"
)

const maxFeedbackLen = 2000

// FeedbackAccumulator collects feedback from various pipeline stages
// for retry prompts.
type FeedbackAccumulator struct {
	entries []feedbackEntry
}

type feedbackEntry struct {
	category string
	content  string
}

// NewFeedbackAccumulator creates an empty feedback accumulator.
func NewFeedbackAccumulator() *FeedbackAccumulator {
	return &FeedbackAccumulator{}
}

// HasFeedback returns true if any feedback has been recorded.
func (f *FeedbackAccumulator) HasFeedback() bool {
	return len(f.entries) > 0
}

// Attempt returns the number of feedback entries (correlates to retry count).
func (f *FeedbackAccumulator) Attempt() int {
	return len(f.entries)
}

// AddLintError adds lint failure output.
func (f *FeedbackAccumulator) AddLintError(output string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Lint errors",
		content:  truncate(output, maxFeedbackLen),
	})
}

// AddTestError adds test failure output.
func (f *FeedbackAccumulator) AddTestError(output string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Test failures",
		content:  truncate(output, maxFeedbackLen),
	})
}

// AddSpecFeedback adds spec reviewer feedback.
func (f *FeedbackAccumulator) AddSpecFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Spec review issues",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// AddQualityFeedback adds quality reviewer feedback.
func (f *FeedbackAccumulator) AddQualityFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Quality review issues",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// AddTDDFeedback adds TDD verification feedback.
func (f *FeedbackAccumulator) AddTDDFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "TDD verification failed",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// Render produces the combined feedback string for inclusion in retry prompts.
func (f *FeedbackAccumulator) Render() string {
	if len(f.entries) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, e := range f.entries {
		sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", e.category, e.content))
	}
	return strings.TrimSpace(sb.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestFeedbackAccumulator -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/feedback.go internal/pipeline/feedback_test.go
git commit -m "feat: add feedback accumulator for tiered retry pipeline"
```

---

### Task 5: Dependency Change Detector

**Files:**
- Create: `internal/pipeline/dep_detector.go`
- Test: `internal/pipeline/dep_detector_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/dep_detector_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectDepChange_GoMod(t *testing.T) {
	modified := []string{"go.mod", "internal/handler.go"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "go.mod", result.File)
	assert.Equal(t, "go", result.Command)
	assert.Equal(t, []string{"mod", "download"}, result.Args)
}

func TestDetectDepChange_PackageJSON(t *testing.T) {
	modified := []string{"src/app.ts", "package.json"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "package.json", result.File)
	assert.Equal(t, "npm", result.Command)
}

func TestDetectDepChange_YarnLock(t *testing.T) {
	modified := []string{"yarn.lock"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "yarn", result.Command)
}

func TestDetectDepChange_CargoToml(t *testing.T) {
	modified := []string{"Cargo.toml"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "cargo", result.Command)
	assert.Equal(t, []string{"fetch"}, result.Args)
}

func TestDetectDepChange_RequirementsTxt(t *testing.T) {
	modified := []string{"requirements.txt"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "pip", result.Command)
}

func TestDetectDepChange_NoDepFiles(t *testing.T) {
	modified := []string{"internal/handler.go", "internal/handler_test.go"}
	result := DetectDepChange(modified)
	assert.False(t, result.Changed)
}

func TestDetectDepChange_NestedPackageJSON(t *testing.T) {
	modified := []string{"packages/api/package.json"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "package.json", result.File)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestDetectDepChange -v`
Expected: FAIL — `DetectDepChange` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/dep_detector.go
package pipeline

import (
	"path/filepath"
)

// DepChangeResult describes a detected dependency file change.
type DepChangeResult struct {
	Changed bool
	File    string   // The dependency file that changed
	Command string   // Install command to run
	Args    []string // Install command arguments
}

// depFileMapping maps dependency file basenames to install commands.
var depFileMapping = map[string]struct {
	Command string
	Args    []string
}{
	"package.json":      {Command: "npm", Args: []string{"install"}},
	"package-lock.json": {Command: "npm", Args: []string{"install"}},
	"yarn.lock":         {Command: "yarn", Args: []string{"install"}},
	"pnpm-lock.yaml":   {Command: "pnpm", Args: []string{"install"}},
	"go.mod":            {Command: "go", Args: []string{"mod", "download"}},
	"go.sum":            {Command: "go", Args: []string{"mod", "download"}},
	"Cargo.toml":        {Command: "cargo", Args: []string{"fetch"}},
	"Cargo.lock":        {Command: "cargo", Args: []string{"fetch"}},
	"requirements.txt":  {Command: "pip", Args: []string{"install", "-r", "requirements.txt"}},
	"pyproject.toml":    {Command: "poetry", Args: []string{"install"}},
	"poetry.lock":       {Command: "poetry", Args: []string{"install"}},
	"Gemfile":           {Command: "bundle", Args: []string{"install"}},
	"Gemfile.lock":      {Command: "bundle", Args: []string{"install"}},
}

// DetectDepChange checks if any modified files are dependency manifests
// and returns the install command to run if so.
func DetectDepChange(modifiedFiles []string) DepChangeResult {
	for _, path := range modifiedFiles {
		base := filepath.Base(path)
		if mapping, ok := depFileMapping[base]; ok {
			return DepChangeResult{
				Changed: true,
				File:    base,
				Command: mapping.Command,
				Args:    mapping.Args,
			}
		}
	}
	return DepChangeResult{Changed: false}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestDetectDepChange -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/dep_detector.go internal/pipeline/dep_detector_test.go
git commit -m "feat: add dependency change detector for automatic reinstall between tasks"
```

---

### Task 6: Repo Analyzer

**Files:**
- Create: `internal/context/repo_analyzer.go`
- Test: `internal/context/repo_analyzer_test.go`

**Step 1: Write the failing test**

```go
// internal/context/repo_analyzer_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeRepo_GoProject(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/foo\ngo 1.23"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "Makefile"), []byte("test:\n\tgo test ./..."), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "go", info.Language)
	assert.Contains(t, info.BuildCmd, "go")
	assert.Contains(t, info.TestCmd, "go test")
}

func TestAnalyzeRepo_NodeProject(t *testing.T) {
	workDir := t.TempDir()
	packageJSON := `{"name": "test", "scripts": {"test": "jest", "lint": "eslint .", "build": "tsc"}}`
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "package.json"), []byte(packageJSON), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "typescript", info.Language)
	assert.Contains(t, info.TestCmd, "npm test")
	assert.Contains(t, info.LintCmd, "npm run lint")
}

func TestAnalyzeRepo_PythonProject(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "requirements.txt"), []byte("flask\npytest"), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "python", info.Language)
	assert.Contains(t, info.TestCmd, "pytest")
}

func TestAnalyzeRepo_ForemanContext(t *testing.T) {
	workDir := t.TempDir()
	contextMD := `# Foreman Context

## Commands
- Test: ` + "`npm run test:unit`" + `
- Lint: ` + "`npm run lint`" + `
- Build: ` + "`npm run build`" + `
`
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".foreman-context.md"), []byte(contextMD), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "npm run test:unit", info.TestCmd)
	assert.Equal(t, "npm run lint", info.LintCmd)
	assert.Equal(t, "npm run build", info.BuildCmd)
}

func TestAnalyzeRepo_EmptyDir(t *testing.T) {
	workDir := t.TempDir()
	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "unknown", info.Language)
}

func TestAnalyzeRepo_FileTree(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "src/main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Contains(t, info.FileTree, "src/")
	assert.Contains(t, info.FileTree, "go.mod")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestAnalyzeRepo -v`
Expected: FAIL — `AnalyzeRepo` not defined

**Step 3: Write minimal implementation**

```go
// internal/context/repo_analyzer.go
package context

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RepoInfo holds detected information about a repository.
type RepoInfo struct {
	Language string
	Framework string
	TestCmd  string
	LintCmd  string
	BuildCmd string
	FileTree string
}

// AnalyzeRepo detects the language, framework, and commands for a repository.
// Priority 1: .foreman-context.md, Priority 2: config file detection.
func AnalyzeRepo(workDir string) (*RepoInfo, error) {
	info := &RepoInfo{Language: "unknown"}

	// Priority 1: Read .foreman-context.md
	contextPath := filepath.Join(workDir, ".foreman-context.md")
	if content, err := os.ReadFile(contextPath); err == nil {
		parseContextFile(info, string(content))
		// Still detect language if not set from context
		if info.Language == "unknown" {
			detectLanguage(workDir, info)
		}
	} else {
		// Priority 2: Auto-detect from config files
		detectLanguage(workDir, info)
		detectCommands(workDir, info)
	}

	// Always generate file tree
	info.FileTree = generateFileTree(workDir)

	return info, nil
}

func parseContextFile(info *RepoInfo, content string) {
	// Extract commands from backtick-wrapped values
	testRe := regexp.MustCompile("(?i)test:\\s*`([^`]+)`")
	lintRe := regexp.MustCompile("(?i)lint:\\s*`([^`]+)`")
	buildRe := regexp.MustCompile("(?i)build:\\s*`([^`]+)`")

	if m := testRe.FindStringSubmatch(content); len(m) > 1 {
		info.TestCmd = m[1]
	}
	if m := lintRe.FindStringSubmatch(content); len(m) > 1 {
		info.LintCmd = m[1]
	}
	if m := buildRe.FindStringSubmatch(content); len(m) > 1 {
		info.BuildCmd = m[1]
	}
}

func detectLanguage(workDir string, info *RepoInfo) {
	// Check for language markers in priority order
	if fileExists(workDir, "go.mod") {
		info.Language = "go"
	} else if fileExists(workDir, "Cargo.toml") {
		info.Language = "rust"
	} else if fileExists(workDir, "package.json") {
		// Check if TypeScript
		if fileExists(workDir, "tsconfig.json") {
			info.Language = "typescript"
		} else {
			// Check package.json for TS dependencies
			if content, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
				if strings.Contains(string(content), "typescript") || strings.Contains(string(content), "\"tsc\"") {
					info.Language = "typescript"
				} else {
					info.Language = "javascript"
				}
			} else {
				info.Language = "javascript"
			}
		}
	} else if fileExists(workDir, "pyproject.toml") || fileExists(workDir, "requirements.txt") || fileExists(workDir, "setup.py") {
		info.Language = "python"
	} else if fileExists(workDir, "Gemfile") {
		info.Language = "ruby"
	}
}

func detectCommands(workDir string, info *RepoInfo) {
	switch info.Language {
	case "go":
		info.TestCmd = "go test ./..."
		info.BuildCmd = "go build ./..."
		info.LintCmd = "go vet ./..."
	case "typescript", "javascript":
		info.TestCmd = "npm test"
		info.BuildCmd = "npm run build"
		// Parse package.json scripts for lint
		if content, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
			var pkg map[string]interface{}
			if json.Unmarshal(content, &pkg) == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					if _, ok := scripts["lint"]; ok {
						info.LintCmd = "npm run lint"
					}
				}
			}
		}
	case "python":
		info.TestCmd = "pytest"
		info.LintCmd = "ruff check ."
	case "rust":
		info.TestCmd = "cargo test"
		info.BuildCmd = "cargo build"
		info.LintCmd = "cargo clippy"
	case "ruby":
		info.TestCmd = "bundle exec rspec"
		info.LintCmd = "bundle exec rubocop"
	}
}

func fileExists(workDir, name string) bool {
	_, err := os.Stat(filepath.Join(workDir, name))
	return err == nil
}

// skipDirs are directories to exclude from the file tree.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, "__pycache__": true,
	".next": true, "dist": true, "build": true, "target": true,
	".claude": true, ".idea": true, ".vscode": true,
}

func generateFileTree(workDir string) string {
	var entries []string
	filepath.Walk(workDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		if rel == "." {
			return nil
		}
		// Skip hidden and excluded directories
		if fi.IsDir() {
			base := filepath.Base(rel)
			if skipDirs[base] || (strings.HasPrefix(base, ".") && base != ".") {
				return filepath.SkipDir
			}
			entries = append(entries, rel+"/")
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	sort.Strings(entries)
	if len(entries) > 500 {
		entries = entries[:500]
		entries = append(entries, fmt.Sprintf("... (%d+ files, truncated)", 500))
	}
	return strings.Join(entries, "\n")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestAnalyzeRepo -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/repo_analyzer.go internal/context/repo_analyzer_test.go
git commit -m "feat: add repo analyzer with language detection and .foreman-context.md support"
```

---

### Task 7: File Selector

**Files:**
- Create: `internal/context/file_selector.go`
- Test: `internal/context/file_selector_test.go`

**Step 1: Write the failing test**

```go
// internal/context/file_selector_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()

	files := map[string]string{
		"internal/handler.go":      "package internal\n\nimport \"internal/models\"\n\nfunc Handle() {}",
		"internal/handler_test.go": "package internal\n\nfunc TestHandle() {}",
		"internal/models/user.go":  "package models\n\ntype User struct{}",
		"internal/utils/helper.go": "package utils\n\nfunc Help() {}",
		"cmd/main.go":              "package main\n\nfunc main() {}",
		"go.mod":                   "module test\ngo 1.23",
	}
	for path, content := range files {
		fullPath := filepath.Join(workDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}
	return workDir
}

func TestSelectFilesForTask_ExplicitFiles(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToRead:   []string{"internal/models/user.go"},
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	paths := filePaths(files)
	assert.Contains(t, paths, "internal/models/user.go")
	assert.Contains(t, paths, "internal/handler.go")
}

func TestSelectFilesForTask_TestSibling(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)

	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
}

func TestSelectFilesForTask_ProximityBoost(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)

	// Files in the same directory should be included
	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
}

func TestSelectFilesForTask_ScoreOrdering(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToRead:   []string{"internal/models/user.go"},
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Explicit files should have highest scores
	assert.True(t, files[0].Score >= 90, "First file should have score >= 90, got %f", files[0].Score)
}

func TestSelectFilesForTask_TokenBudget(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// Very small budget should limit results
	files, err := SelectFilesForTask(task, workDir, 100)
	require.NoError(t, err)
	// Should include at least the explicit file
	assert.NotEmpty(t, files)
}

func filePaths(files []ScoredFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestSelectFilesForTask -v`
Expected: FAIL — `SelectFilesForTask` not defined

**Step 3: Write minimal implementation**

```go
// internal/context/file_selector.go
package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// ScoredFile is a file ranked by relevance to a task.
type ScoredFile struct {
	Path      string
	Score     float64
	Reason    string
	SizeBytes int64
}

// SelectFilesForTask returns the most relevant files within the token budget.
func SelectFilesForTask(task *models.Task, workDir string, tokenBudget int) ([]ScoredFile, error) {
	candidates := map[string]*ScoredFile{}

	// Signal 1: Explicit planner references (highest priority)
	for _, path := range task.FilesToRead {
		addCandidate(candidates, workDir, path, 100, "planner:read")
	}
	for _, path := range task.FilesToModify {
		cleanPath := strings.TrimSuffix(path, " (new)")
		addCandidate(candidates, workDir, cleanPath, 100, "planner:modify")
	}

	// Signal 2: Test file siblings
	for _, path := range task.FilesToModify {
		cleanPath := strings.TrimSuffix(path, " (new)")
		sibling := findTestSibling(workDir, cleanPath)
		if sibling != "" {
			addCandidate(candidates, workDir, sibling, 60, "test_sibling")
		}
	}

	// Signal 3: Directory proximity
	taskDirs := extractDirectories(task.FilesToModify)
	allFiles := listSourceFiles(workDir)
	for _, f := range allFiles {
		if _, exists := candidates[f]; exists {
			continue
		}
		if inAnyDirectory(f, taskDirs) {
			addCandidate(candidates, workDir, f, 30, "proximity")
		}
	}

	// Convert to slice and sort by score descending
	result := make([]ScoredFile, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, *c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	// Apply token budget cutoff
	selected := make([]ScoredFile, 0, len(result))
	tokensUsed := 0
	for _, c := range result {
		fileTokens := estimateFileTokens(c.SizeBytes)
		if tokensUsed+fileTokens > tokenBudget && len(selected) > 0 {
			continue
		}
		tokensUsed += fileTokens
		selected = append(selected, c)
	}

	return selected, nil
}

func addCandidate(candidates map[string]*ScoredFile, workDir, path string, score float64, reason string) {
	if existing, ok := candidates[path]; ok {
		if score > existing.Score {
			existing.Score = score
			existing.Reason = reason
		}
		return
	}
	fullPath := filepath.Join(workDir, path)
	fi, err := os.Stat(fullPath)
	if err != nil {
		return // File doesn't exist, skip
	}
	candidates[path] = &ScoredFile{
		Path:      path,
		Score:     score,
		Reason:    reason,
		SizeBytes: fi.Size(),
	}
}

func findTestSibling(workDir, path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	// Go: foo.go → foo_test.go
	if ext == ".go" {
		candidate := base + "_test.go"
		if _, err := os.Stat(filepath.Join(workDir, candidate)); err == nil {
			return candidate
		}
	}
	// JS/TS: foo.ts → foo.test.ts / foo.spec.ts
	for _, testExt := range []string{".test", ".spec"} {
		candidate := base + testExt + ext
		if _, err := os.Stat(filepath.Join(workDir, candidate)); err == nil {
			return candidate
		}
	}
	return ""
}

func extractDirectories(files []string) []string {
	dirs := map[string]bool{}
	for _, f := range files {
		cleanPath := strings.TrimSuffix(f, " (new)")
		dir := filepath.Dir(cleanPath)
		if dir != "." {
			dirs[dir] = true
		}
	}
	result := make([]string, 0, len(dirs))
	for d := range dirs {
		result = append(result, d)
	}
	return result
}

func inAnyDirectory(file string, dirs []string) bool {
	fileDir := filepath.Dir(file)
	for _, d := range dirs {
		if fileDir == d {
			return true
		}
	}
	return false
}

func listSourceFiles(workDir string) []string {
	var files []string
	filepath.Walk(workDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		files = append(files, rel)
		return nil
	})
	return files
}

func estimateFileTokens(sizeBytes int64) int {
	// Rough estimate: 1 token ≈ 4 bytes
	return int(sizeBytes / 4)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestSelectFilesForTask -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/file_selector.go internal/context/file_selector_test.go
git commit -m "feat: add file selector with scored multi-signal ranking and token budget"
```

---

### Task 8: Context Assembler

**Files:**
- Create: `internal/context/assembler.go`
- Test: `internal/context/assembler_test.go`

**Step 1: Write the failing test**

```go
// internal/context/assembler_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssemblePlannerContext(t *testing.T) {
	workDir := setupTestRepo(t)
	ticket := &models.Ticket{
		Title:              "Add user endpoint",
		Description:        "Create a REST endpoint for user management",
		AcceptanceCriteria: "GET /users returns list of users",
	}

	ctx, err := AssemblePlannerContext(workDir, ticket, 30000)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.NotEmpty(t, ctx.UserPrompt)
	assert.Contains(t, ctx.UserPrompt, "Add user endpoint")
	assert.Contains(t, ctx.UserPrompt, "REST endpoint")
}

func TestAssembleImplementerContext(t *testing.T) {
	workDir := setupTestRepo(t)

	task := &models.Task{
		Title:              "Add user handler",
		Description:        "Create the handler function for user endpoint",
		AcceptanceCriteria: []string{"Handler returns 200"},
		TestAssertions:     []string{"TestGetUsers returns status 200"},
		FilesToRead:        []string{"internal/models/user.go"},
		FilesToModify:      []string{"internal/handler.go"},
	}

	ctx, err := AssembleImplementerContext(workDir, task, nil, 60000)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.NotEmpty(t, ctx.UserPrompt)
	assert.Contains(t, ctx.UserPrompt, "Add user handler")
	assert.Contains(t, ctx.UserPrompt, "Handler returns 200")
}

func TestAssembleImplementerContext_WithFeedback(t *testing.T) {
	workDir := setupTestRepo(t)

	task := &models.Task{
		Title:              "Fix handler",
		Description:        "Fix the handler",
		AcceptanceCriteria: []string{"No error"},
		TestAssertions:     []string{"Test passes"},
		FilesToModify:      []string{"internal/handler.go"},
	}

	fb := &FeedbackContext{
		Attempt:    2,
		MaxAttempts: 3,
		PreviousError: "nil pointer dereference",
	}

	ctx, err := AssembleImplementerContext(workDir, task, fb, 60000)
	require.NoError(t, err)
	assert.Contains(t, ctx.UserPrompt, "RETRY")
	assert.Contains(t, ctx.UserPrompt, "nil pointer dereference")
	assert.Contains(t, ctx.UserPrompt, "attempt 2")
}

func TestAssembleSpecReviewerContext(t *testing.T) {
	ctx := AssembleSpecReviewerContext(
		"Add user handler",
		[]string{"Handler returns 200", "Handles errors"},
		"diff --git a/handler.go\n+func GetUsers() {}",
		"PASS: all tests",
	)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.UserPrompt, "Handler returns 200")
	assert.Contains(t, ctx.UserPrompt, "diff --git")
}

func TestAssembleQualityReviewerContext(t *testing.T) {
	ctx := AssembleQualityReviewerContext(
		"diff --git a/handler.go\n+func GetUsers() {}",
		"go, stdlib, standard go conventions",
	)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.UserPrompt, "diff --git")
	assert.Contains(t, ctx.UserPrompt, "standard go conventions")
}

func TestAssembleContext_SecretsFiltered(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".env"), []byte("API_KEY=sk-ant-secret123456789"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	task := &models.Task{
		FilesToRead:   []string{".env"},
		FilesToModify: []string{"internal/handler.go"},
	}

	ctx, err := AssembleImplementerContext(workDir, task, nil, 60000)
	require.NoError(t, err)
	// .env should not appear in the context
	assert.NotContains(t, ctx.UserPrompt, "sk-ant-secret")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run "TestAssemble" -v`
Expected: FAIL — `AssemblePlannerContext` not defined

**Step 3: Write minimal implementation**

```go
// internal/context/assembler.go
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// AssembledContext holds the prompts ready to send to an LLM.
type AssembledContext struct {
	SystemPrompt string
	UserPrompt   string
}

// FeedbackContext holds retry information for implementer retries.
type FeedbackContext struct {
	Attempt       int
	MaxAttempts   int
	PreviousError string
	SpecFeedback  string
	QualityFeedback string
	TDDFeedback   string
}

// AssemblePlannerContext builds the context for a planner LLM call.
func AssemblePlannerContext(workDir string, ticket *models.Ticket, tokenBudget int) (*AssembledContext, error) {
	repoInfo, err := AnalyzeRepo(workDir)
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
    depends_on: []

Do NOT wrap the YAML in markdown fences. Output ONLY the YAML.`

	var user strings.Builder
	user.WriteString(fmt.Sprintf("## Ticket\nTitle: %s\n\nDescription:\n%s\n\n", ticket.Title, ticket.Description))
	if ticket.AcceptanceCriteria != "" {
		user.WriteString(fmt.Sprintf("Acceptance Criteria:\n%s\n\n", ticket.AcceptanceCriteria))
	}

	user.WriteString(fmt.Sprintf("## Repository\n### File Tree\n```\n%s\n```\n\n", repoInfo.FileTree))

	// Read README if it exists
	if content, err := readFileTruncated(filepath.Join(workDir, "README.md"), 3000); err == nil {
		user.WriteString(fmt.Sprintf("### README\n%s\n\n", content))
	}

	// Read .foreman-context.md if it exists
	if content, err := os.ReadFile(filepath.Join(workDir, ".foreman-context.md")); err == nil {
		user.WriteString(fmt.Sprintf("## Project-Specific Context\n%s\n\n", string(content)))
	}

	return &AssembledContext{
		SystemPrompt: system,
		UserPrompt:   user.String(),
	}, nil
}

// AssembleImplementerContext builds the context for an implementer LLM call.
func AssembleImplementerContext(workDir string, task *models.Task, feedback *FeedbackContext, tokenBudget int) (*AssembledContext, error) {
	repoInfo, err := AnalyzeRepo(workDir)
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
	files, err := SelectFilesForTask(task, workDir, tokenBudget/2)
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
		user.WriteString(fmt.Sprintf("## RETRY (attempt %d/%d)\n", feedback.Attempt, feedback.MaxAttempts))
		if feedback.PreviousError != "" {
			user.WriteString(fmt.Sprintf("Previous error:\n```\n%s\n```\n\n", feedback.PreviousError))
		}
		if feedback.SpecFeedback != "" {
			user.WriteString(fmt.Sprintf("## SPEC REVIEWER FOUND ISSUES\n%s\n\n", feedback.SpecFeedback))
		}
		if feedback.QualityFeedback != "" {
			user.WriteString(fmt.Sprintf("## QUALITY REVIEWER FOUND ISSUES\n%s\n\n", feedback.QualityFeedback))
		}
		if feedback.TDDFeedback != "" {
			user.WriteString(fmt.Sprintf("## TDD VERIFICATION FAILED\n%s\n\n", feedback.TDDFeedback))
		}
	}

	// Task section
	user.WriteString(fmt.Sprintf("## Task\nTitle: %s\nDescription:\n%s\n\n", task.Title, task.Description))
	user.WriteString("Acceptance Criteria:\n")
	for _, c := range task.AcceptanceCriteria {
		user.WriteString(fmt.Sprintf("- %s\n", c))
	}
	user.WriteString("\nTest Assertions:\n")
	for _, a := range task.TestAssertions {
		user.WriteString(fmt.Sprintf("- %s\n", a))
	}

	// Commands
	user.WriteString(fmt.Sprintf("\n## Commands\nBuild: `%s`  Test: `%s`  Lint: `%s`\n\n",
		repoInfo.BuildCmd, repoInfo.TestCmd, repoInfo.LintCmd))

	// File contents
	user.WriteString("## Files\n")
	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(workDir, f.Path))
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(filepath.Ext(f.Path), ".")
		user.WriteString(fmt.Sprintf("### %s\n```%s\n%s\n```\n\n", f.Path, ext, string(content)))
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
	user.WriteString(fmt.Sprintf("## Task\n%s\n\nCriteria:\n", taskTitle))
	for _, c := range criteria {
		user.WriteString(fmt.Sprintf("- %s\n", c))
	}
	user.WriteString(fmt.Sprintf("\n## Diff\n```diff\n%s\n```\n\n## Test Output\n```\n%s\n```\n", diff, testOutput))

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
	user.WriteString(fmt.Sprintf("## Codebase Patterns\n%s\n\n## Diff\n```diff\n%s\n```\n", codebasePatterns, diff))

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
	if len(content) > maxBytes {
		return string(content[:maxBytes]) + "\n... (truncated)", nil
	}
	return string(content), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/context/ -run "TestAssemble" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/assembler.go internal/context/assembler_test.go
git commit -m "feat: add context assembler for planner, implementer, and reviewer prompts"
```

---

### Task 9: Native Git Provider

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/native.go`
- Test: `internal/git/native_test.go`

**Step 1: Write the failing test**

```go
// internal/git/native_test.go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command failed: %s %v\noutput: %s", name, args, out)
}

func TestNativeGitProvider_CreateBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	err := git.CreateBranch(context.Background(), dir, "feature/test")
	require.NoError(t, err)

	// Verify we're on the new branch
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "feature/test", trimNewline(string(out)))
}

func TestNativeGitProvider_Commit(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main"), 0o644))
	run(t, dir, "git", "add", ".")

	sha, err := git.Commit(context.Background(), dir, "test commit")
	require.NoError(t, err)
	assert.NotEmpty(t, sha)
	assert.Len(t, sha, 40) // Full SHA
}

func TestNativeGitProvider_DiffWorking(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Updated"), 0o644))

	diff, err := git.DiffWorking(context.Background(), dir)
	require.NoError(t, err)
	assert.Contains(t, diff, "Updated")
}

func TestNativeGitProvider_FileTree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/app.go"), []byte("package src"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add src")

	entries, err := git.FileTree(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	assert.Contains(t, paths, "README.md")
	assert.Contains(t, paths, "src/app.go")
}

func TestNativeGitProvider_Log(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	commits, err := git.Log(context.Background(), dir, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, commits)
	assert.Equal(t, "initial", commits[0].Message)
}

func trimNewline(s string) string {
	return s[:len(s)-1]
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestNativeGitProvider -v`
Expected: FAIL — `NewNativeGitProvider` not defined

**Step 3: Write the git interface**

```go
// internal/git/git.go
package git

import (
	"context"
	"time"
)

// GitProvider abstracts git operations.
type GitProvider interface {
	EnsureRepo(ctx context.Context, workDir string) error
	CreateBranch(ctx context.Context, workDir, branchName string) error
	Commit(ctx context.Context, workDir, message string) (sha string, err error)
	Diff(ctx context.Context, workDir, base, head string) (string, error)
	DiffWorking(ctx context.Context, workDir string) (string, error)
	Push(ctx context.Context, workDir, branchName string) error
	RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error)
	FileTree(ctx context.Context, workDir string) ([]FileEntry, error)
	Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error)
}

// RebaseResult holds rebase outcome.
type RebaseResult struct {
	Success       bool
	ConflictFiles []string
	ConflictDiff  string
}

// FileEntry represents a file in the repo tree.
type FileEntry struct {
	Path      string
	IsDir     bool
	SizeBytes int64
}

// CommitEntry represents a git commit.
type CommitEntry struct {
	SHA     string
	Message string
	Author  string
	Date    time.Time
}
```

**Step 4: Write the native git implementation**

```go
// internal/git/native.go
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NativeGitProvider shells out to the native git CLI.
type NativeGitProvider struct{}

// NewNativeGitProvider creates a native git provider.
func NewNativeGitProvider() *NativeGitProvider {
	return &NativeGitProvider{}
}

func (g *NativeGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, "git", "status")
	return err
}

func (g *NativeGitProvider) CreateBranch(ctx context.Context, workDir, branchName string) error {
	_, err := g.run(ctx, workDir, "git", "checkout", "-b", branchName)
	return err
}

func (g *NativeGitProvider) Commit(ctx context.Context, workDir, message string) (string, error) {
	_, err := g.run(ctx, workDir, "git", "commit", "-m", message)
	if err != nil {
		return "", err
	}
	out, err := g.run(ctx, workDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *NativeGitProvider) Diff(ctx context.Context, workDir, base, head string) (string, error) {
	out, err := g.run(ctx, workDir, "git", "diff", base+"..."+head)
	if err != nil {
		return "", err
	}
	return out, nil
}

func (g *NativeGitProvider) DiffWorking(ctx context.Context, workDir string) (string, error) {
	out, err := g.run(ctx, workDir, "git", "diff")
	if err != nil {
		return "", err
	}
	return out, nil
}

func (g *NativeGitProvider) Push(ctx context.Context, workDir, branchName string) error {
	_, err := g.run(ctx, workDir, "git", "push", "-u", "origin", branchName)
	return err
}

func (g *NativeGitProvider) RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error) {
	_, err := g.run(ctx, workDir, "git", "rebase", targetBranch)
	if err != nil {
		// Check for conflicts
		out, _ := g.run(ctx, workDir, "git", "diff", "--name-only", "--diff-filter=U")
		conflicts := strings.Split(strings.TrimSpace(out), "\n")
		if len(conflicts) == 1 && conflicts[0] == "" {
			conflicts = nil
		}
		diffOut, _ := g.run(ctx, workDir, "git", "diff")
		return &RebaseResult{
			Success:       false,
			ConflictFiles: conflicts,
			ConflictDiff:  diffOut,
		}, nil
	}
	return &RebaseResult{Success: true}, nil
}

func (g *NativeGitProvider) FileTree(ctx context.Context, workDir string) ([]FileEntry, error) {
	out, err := g.run(ctx, workDir, "git", "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	files := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		entries = append(entries, FileEntry{Path: f})
	}
	return entries, nil
}

func (g *NativeGitProvider) Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error) {
	out, err := g.run(ctx, workDir, "git", "log",
		fmt.Sprintf("-n%d", count),
		"--format=%H|%s|%an|%aI",
	)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	entries := make([]CommitEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, parts[3])
		entries = append(entries, CommitEntry{
			SHA:     parts[0],
			Message: parts[1],
			Author:  parts[2],
			Date:    date,
		})
	}
	return entries, nil
}

func (g *NativeGitProvider) run(ctx context.Context, workDir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\noutput: %s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// Ensure NativeGitProvider implements GitProvider.
var _ GitProvider = (*NativeGitProvider)(nil)

// Helper for strconv import usage avoidance
var _ = strconv.Itoa
```

Wait — remove that unused strconv line. The implementation doesn't need it.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestNativeGitProvider -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/git/git.go internal/git/native.go internal/git/native_test.go
git commit -m "feat: add git provider interface and native CLI implementation"
```

---

### Task 10: Pipeline Orchestrator

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/pipeline_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM returns canned responses for testing.
type mockLLM struct {
	responses map[string]string // role → response
}

func (m *mockLLM) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	// Determine role from system prompt content
	role := "implementer"
	if contains(req.SystemPrompt, "decomposing a ticket") {
		role = "planner"
	} else if contains(req.SystemPrompt, "verify that the implementation satisfies") {
		role = "spec_reviewer"
	} else if contains(req.SystemPrompt, "review code quality") {
		role = "quality_reviewer"
	}

	response, ok := m.responses[role]
	if !ok {
		response = "STATUS: APPROVED"
	}

	return &models.LlmResponse{
		Content:      response,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        "test-model",
		StopReason:   models.StopReasonEndTurn,
	}, nil
}

func (m *mockLLM) ProviderName() string       { return "mock" }
func (m *mockLLM) HealthCheck(ctx context.Context) error { return nil }

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline(PipelineConfig{
		MaxImplementationRetries: 2,
		MaxSpecReviewCycles:      2,
		MaxQualityReviewCycles:   1,
		MaxLlmCallsPerTask:      8,
		EnableTDDVerification:    false,
	})
	require.NotNil(t, p)
}

func TestPipeline_CheckTicketClarity_Sufficient(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: true})
	ticket := &models.Ticket{
		Description:        "Add a REST endpoint that returns a list of users from the database. Support pagination with limit and offset query params.",
		AcceptanceCriteria: "GET /api/users returns JSON array",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.True(t, clear)
}

func TestPipeline_CheckTicketClarity_TooVague(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: true})
	ticket := &models.Ticket{
		Description:        "fix bug",
		AcceptanceCriteria: "",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.False(t, clear)
}

func TestPipeline_CheckTicketClarity_Disabled(t *testing.T) {
	p := NewPipeline(PipelineConfig{EnableClarification: false})
	ticket := &models.Ticket{
		Description:        "fix",
		AcceptanceCriteria: "",
	}
	clear, _ := p.CheckTicketClarity(ticket)
	assert.True(t, clear) // Always clear when disabled
}

func TestPipeline_TopologicalSort(t *testing.T) {
	tasks := []PlannedTask{
		{Title: "Task C", DependsOn: []string{"Task A", "Task B"}},
		{Title: "Task A", DependsOn: []string{}},
		{Title: "Task B", DependsOn: []string{"Task A"}},
	}
	sorted, err := TopologicalSort(tasks)
	require.NoError(t, err)
	assert.Equal(t, "Task A", sorted[0].Title)
	assert.Equal(t, "Task B", sorted[1].Title)
	assert.Equal(t, "Task C", sorted[2].Title)
}

func TestPipeline_TopologicalSort_Cycle(t *testing.T) {
	tasks := []PlannedTask{
		{Title: "A", DependsOn: []string{"B"}},
		{Title: "B", DependsOn: []string{"A"}},
	}
	_, err := TopologicalSort(tasks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run "TestNewPipeline|TestPipeline_Check|TestPipeline_Topological" -v`
Expected: FAIL — `NewPipeline` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/pipeline.go
package pipeline

import (
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// PipelineConfig holds pipeline-specific configuration.
type PipelineConfig struct {
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask      int
	EnableTDDVerification    bool
	EnableClarification      bool
	EnablePartialPR          bool
	ContextTokenBudget       int
}

// Pipeline orchestrates the execution of a ticket through the full pipeline.
type Pipeline struct {
	config PipelineConfig
}

// NewPipeline creates a new pipeline orchestrator.
func NewPipeline(config PipelineConfig) *Pipeline {
	return &Pipeline{config: config}
}

// CheckTicketClarity determines if a ticket has enough detail to plan.
func (p *Pipeline) CheckTicketClarity(ticket *models.Ticket) (bool, error) {
	if !p.config.EnableClarification {
		return true, nil
	}

	// Heuristic checks (no LLM needed)
	if len(ticket.Description) < 50 && ticket.AcceptanceCriteria == "" {
		return false, nil
	}

	return true, nil
}

// TopologicalSort orders tasks by their dependency graph.
func TopologicalSort(tasks []PlannedTask) ([]PlannedTask, error) {
	// Build index by title
	taskMap := map[string]*PlannedTask{}
	for i := range tasks {
		taskMap[tasks[i].Title] = &tasks[i]
	}

	// Kahn's algorithm
	inDegree := map[string]int{}
	graph := map[string][]string{} // task → dependents

	for _, t := range tasks {
		if _, ok := inDegree[t.Title]; !ok {
			inDegree[t.Title] = 0
		}
		for _, dep := range t.DependsOn {
			graph[dep] = append(graph[dep], t.Title)
			inDegree[t.Title]++
		}
	}

	// Find all tasks with no dependencies
	var queue []string
	for _, t := range tasks {
		if inDegree[t.Title] == 0 {
			queue = append(queue, t.Title)
		}
	}

	var sorted []PlannedTask
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, *taskMap[current])

		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(tasks) {
		return nil, fmt.Errorf("task dependencies contain a cycle")
	}

	return sorted, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run "TestNewPipeline|TestPipeline_Check|TestPipeline_Topological" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go
git commit -m "feat: add pipeline orchestrator with clarity check and topological sort"
```

---

### Task 11: Planner Step

**Files:**
- Create: `internal/pipeline/planner.go`
- Test: `internal/pipeline/planner_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/planner_test.go
package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanner_Plan(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test\ngo 1.23"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))

	plannerYAML := `status: OK
message: ""
codebase_patterns:
  language: "go"
  framework: "stdlib"
  test_runner: "go test"
  style_notes: "standard go"
tasks:
  - title: "Add endpoint"
    description: "Add a GET /users endpoint"
    acceptance_criteria:
      - "GET /users returns 200"
    test_assertions:
      - "TestGetUsers returns status 200"
    files_to_read:
      - "internal/handler.go"
    files_to_modify:
      - "internal/handler.go"
    estimated_complexity: "simple"
    depends_on: []`

	llm := &mockLLM{responses: map[string]string{"planner": plannerYAML}}

	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket: 20,
		ContextTokenBudget: 30000,
	})

	ticket := &models.Ticket{
		Title:       "Add users endpoint",
		Description: "Create a REST endpoint for user management that returns a list of users.",
	}

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "Add endpoint", result.Tasks[0].Title)
}

func TestPlanner_Plan_ClarificationNeeded(t *testing.T) {
	llm := &mockLLM{responses: map[string]string{
		"planner": "CLARIFICATION_NEEDED: What authentication method should be used?",
	}}
	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket: 20,
		ContextTokenBudget: 30000,
	})

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	ticket := &models.Ticket{
		Title:       "Add auth",
		Description: "Add authentication to the app.",
	}

	result, err := planner.Plan(context.Background(), workDir, ticket)
	require.NoError(t, err)
	assert.Equal(t, "CLARIFICATION_NEEDED", result.Status)
	assert.Contains(t, result.Message, "authentication")
}

func TestPlanner_Plan_ValidationFails(t *testing.T) {
	plannerYAML := `status: OK
tasks:
  - title: "Read missing file"
    description: "test"
    acceptance_criteria: ["test"]
    test_assertions: ["test"]
    files_to_read:
      - "nonexistent/path.go"
    files_to_modify: []
    estimated_complexity: "simple"
    depends_on: []`

	llm := &mockLLM{responses: map[string]string{"planner": plannerYAML}}
	planner := NewPlanner(llm, &models.LimitsConfig{
		MaxTasksPerTicket: 20,
		ContextTokenBudget: 30000,
	})

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	ticket := &models.Ticket{
		Title:       "Test",
		Description: "Test ticket with sufficient description for the planner to work with.",
	}

	_, err := planner.Plan(context.Background(), workDir, ticket)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestPlanner -v`
Expected: FAIL — `NewPlanner` not defined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/planner.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/models"
)

// LLMProvider is the interface for making LLM calls (matches llm.LlmProvider).
type LLMProvider interface {
	Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

// Planner decomposes a ticket into implementation tasks via LLM.
type Planner struct {
	llm    LLMProvider
	limits *models.LimitsConfig
}

// NewPlanner creates a planner with the given LLM provider and limits.
func NewPlanner(llm LLMProvider, limits *models.LimitsConfig) *Planner {
	return &Planner{llm: llm, limits: limits}
}

// Plan generates a task plan for the given ticket.
func (p *Planner) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlannerResult, error) {
	// Assemble planner context
	assembled, err := appcontext.AssemblePlannerContext(workDir, ticket, p.limits.ContextTokenBudget)
	if err != nil {
		return nil, fmt.Errorf("assembling planner context: %w", err)
	}

	// Make LLM call
	resp, err := p.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: assembled.SystemPrompt,
		UserPrompt:   assembled.UserPrompt,
		MaxTokens:    4096,
		Temperature:  0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("planner LLM call: %w", err)
	}

	// Parse the output
	result, err := ParsePlannerOutput(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parsing planner output: %w", err)
	}

	// If not OK, return early (clarification needed, too large, etc.)
	if result.Status != "OK" {
		return result, nil
	}

	// Validate the plan
	validation := ValidatePlan(result, workDir, p.limits)
	if !validation.Valid {
		return nil, fmt.Errorf("plan validation failed: %s", strings.Join(validation.Errors, "; "))
	}

	// Sort tasks topologically
	sorted, err := TopologicalSort(result.Tasks)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}
	result.Tasks = sorted

	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestPlanner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/planner.go internal/pipeline/planner_test.go
git commit -m "feat: add planner step with LLM call, YAML parse, validation, and topological sort"
```

---

### Task 12: Install New Dependencies

**Step 1: Add testify and yaml.v3 to go.mod**

```bash
cd /Users/canh/Projects/Indies/Foreman
go get github.com/stretchr/testify@latest
go get gopkg.in/yaml.v3@latest
go mod tidy
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add testify and yaml.v3 dependencies for phase 2"
```

> **Note:** This task should be run FIRST before all other tasks, since they depend on `testify` and `yaml.v3`. The executor should reorder this to run before Task 1.

---

### Task 13: Add LlmRequest/LlmResponse to models (if not present)

**Files:**
- Modify: `internal/models/ticket.go` (check if LlmRequest/LlmResponse exist)

**Step 1: Check if LlmRequest exists in models**

Run: `grep -r "LlmRequest" internal/models/`

If already present, skip this task. If not:

**Step 2: Add to models**

The `LLMProvider` interface in the planner (Task 11) references `models.LlmRequest` and `models.LlmResponse`. Verify these exist in `internal/llm/provider.go` and adjust the planner's interface to match the actual types. This may require the planner to import from `internal/llm` instead of `internal/models`.

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit (if changes made)**

```bash
git add internal/
git commit -m "fix: align planner LLM interface with existing provider types"
```
