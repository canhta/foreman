# OpenCode Deep Adoption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Adopt OpenCode's battle-tested tool robustness, permission system, agent modes, skill discovery, and session infrastructure into Foreman to make it production-grade.

**Architecture:** Each feature is a self-contained module. Edit strategies enhance the existing edit tool. Permissions add a new layer on top of tool registry. Agent modes extend the existing agent runner. All changes are additive.

**Tech Stack:** Go 1.25+, zerolog, existing interfaces, SQLite (for sessions)

**Depends on:** Independent from Plans 1 & 2. Can be executed in parallel.

---

### Task 1: Edit tool — add fallback matching strategies

**Files:**
- Create: `internal/agent/tools/edit_strategies.go`
- Create: `internal/agent/tools/edit_strategies_test.go`
- Modify: `internal/agent/tools/fs.go` (Edit tool handler)

**Step 1: Write the failing test**

```go
// internal/agent/tools/edit_strategies_test.go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimpleReplacer(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "func main() {\n\tfmt.Println(\"hello\")\n}"
	new := "func main() {\n\tfmt.Println(\"world\")\n}"

	result, ok := SimpleReplace(content, old, new)
	assert.True(t, ok)
	assert.Contains(t, result, "world")
}

func TestLineTrimmedReplacer(t *testing.T) {
	// LLM often sends with different leading/trailing whitespace per line
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "func main() {\n  fmt.Println(\"hello\")\n}" // spaces instead of tab

	result, ok := LineTrimmedReplace(content, old, "func main() {\n\tfmt.Println(\"world\")\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "world")
}

func TestIndentationFlexibleReplacer(t *testing.T) {
	content := "\t\tfunc inner() {\n\t\t\treturn nil\n\t\t}"
	old := "func inner() {\n\treturn nil\n}" // different indentation level

	result, ok := IndentFlexibleReplace(content, old, "func inner() {\n\treturn true\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "return true")
}

func TestWhitespaceNormalizedReplacer(t *testing.T) {
	content := "if   err != nil {\n\treturn   err\n}"
	old := "if err != nil {\n\treturn err\n}" // normalized whitespace

	result, ok := WhitespaceNormalizedReplace(content, old, "if err != nil {\n\treturn fmt.Errorf(\"wrap: %w\", err)\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "wrap")
}

func TestBlockAnchorReplacer(t *testing.T) {
	content := "func foo() {\n\tline1\n\tline2\n\tline3\n\tline4\n}"
	// Only first and last line match — middle is different (LLM summarized)
	old := "func foo() {\n\t// ... middle ...\n}"

	result, ok := BlockAnchorReplace(content, old, "func bar() {\n\tnewcode\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "bar")
}

func TestFindBestMatch_Levenshtein(t *testing.T) {
	content := "func handleRequest(w http.ResponseWriter, r *http.Request) {"
	search := "func handleReqeust(w http.ResponseWriter, r *http.Request) {" // typo

	match, similarity := FindBestMatch(content, search)
	assert.NotEmpty(t, match)
	assert.Greater(t, similarity, 0.8)
}

func TestApplyEditWithFallback(t *testing.T) {
	content := "func main() {\n  fmt.Println(\"hello\")\n}"
	old := "func main() {\n\tfmt.Println(\"hello\")\n}" // tab vs spaces
	newStr := "func main() {\n\tfmt.Println(\"world\")\n}"

	result, strategy, err := ApplyEditWithFallback(content, old, newStr)
	assert.NoError(t, err)
	assert.Contains(t, result, "world")
	assert.NotEqual(t, "simple", strategy) // should have used a fallback
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestSimpleReplacer -v`
Expected: FAIL — functions undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/tools/edit_strategies.go
package tools

import (
	"strings"
	"unicode"
)

// ApplyEditWithFallback tries multiple replacement strategies in order.
// Returns the result, strategy name used, and error.
func ApplyEditWithFallback(content, old, new string) (string, string, error) {
	strategies := []struct {
		name string
		fn   func(string, string, string) (string, bool)
	}{
		{"simple", SimpleReplace},
		{"line_trimmed", LineTrimmedReplace},
		{"block_anchor", BlockAnchorReplace},
		{"whitespace_normalized", WhitespaceNormalizedReplace},
		{"indent_flexible", IndentFlexibleReplace},
	}

	for _, s := range strategies {
		if result, ok := s.fn(content, old, new); ok {
			return result, s.name, nil
		}
	}

	// Last resort: find best match and suggest
	match, sim := FindBestMatch(content, old)
	if sim > 0.8 && match != "" {
		// Auto-apply with high-confidence fuzzy match
		result := strings.Replace(content, match, new, 1)
		return result, "fuzzy", nil
	}

	return "", "", fmt.Errorf("no match found for old_string (best similarity: %.0f%%)", sim*100)
}

// SimpleReplace does exact string replacement.
func SimpleReplace(content, old, new string) (string, bool) {
	if !strings.Contains(content, old) {
		return "", false
	}
	return strings.Replace(content, old, new, 1), true
}

// LineTrimmedReplace trims each line before matching.
func LineTrimmedReplace(content, old, new string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

	idx := findLineTrimmedMatch(contentLines, oldLines)
	if idx < 0 {
		return "", false
	}

	// Replace the matched lines with new content
	before := strings.Join(contentLines[:idx], "\n")
	after := strings.Join(contentLines[idx+len(oldLines):], "\n")
	result := before
	if before != "" {
		result += "\n"
	}
	result += new
	if after != "" {
		result += "\n" + after
	}
	return result, true
}

func findLineTrimmedMatch(content, search []string) int {
	for i := 0; i <= len(content)-len(search); i++ {
		match := true
		for j := 0; j < len(search); j++ {
			if strings.TrimSpace(content[i+j]) != strings.TrimSpace(search[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// WhitespaceNormalizedReplace collapses all whitespace runs to single space.
func WhitespaceNormalizedReplace(content, old, new string) (string, bool) {
	normalize := func(s string) string {
		var b strings.Builder
		prevSpace := false
		for _, r := range s {
			if unicode.IsSpace(r) && r != '\n' {
				if !prevSpace {
					b.WriteRune(' ')
				}
				prevSpace = true
			} else {
				b.WriteRune(r)
				prevSpace = false
			}
		}
		return b.String()
	}

	normContent := normalize(content)
	normOld := normalize(old)

	idx := strings.Index(normContent, normOld)
	if idx < 0 {
		return "", false
	}

	// Map normalized index back to original content position
	// Use line-based approach for safety
	return LineTrimmedReplace(content, old, new)
}

// IndentFlexibleReplace matches content ignoring indentation level differences.
func IndentFlexibleReplace(content, old, new string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

	if len(oldLines) == 0 {
		return "", false
	}

	// Find the indentation offset
	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		offset := detectIndentOffset(contentLines[i], oldLines[0])
		if offset < 0 {
			continue
		}

		match := true
		for j := 1; j < len(oldLines); j++ {
			if !matchWithIndentOffset(contentLines[i+j], oldLines[j], offset) {
				match = false
				break
			}
		}

		if match {
			// Apply new content with the detected indent offset
			newLines := strings.Split(new, "\n")
			var adjusted []string
			for _, line := range newLines {
				adjusted = append(adjusted, applyIndentOffset(line, offset))
			}

			before := strings.Join(contentLines[:i], "\n")
			after := strings.Join(contentLines[i+len(oldLines):], "\n")
			result := before
			if before != "" {
				result += "\n"
			}
			result += strings.Join(adjusted, "\n")
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}
	return "", false
}

// BlockAnchorReplace matches using first and last lines as anchors.
func BlockAnchorReplace(content, old, new string) (string, bool) {
	oldLines := strings.Split(old, "\n")
	if len(oldLines) < 2 {
		return "", false
	}

	contentLines := strings.Split(content, "\n")
	firstLine := strings.TrimSpace(oldLines[0])
	lastLine := strings.TrimSpace(oldLines[len(oldLines)-1])

	for i := 0; i < len(contentLines); i++ {
		if strings.TrimSpace(contentLines[i]) != firstLine {
			continue
		}
		// Search for last line
		for j := i + 1; j < len(contentLines); j++ {
			if strings.TrimSpace(contentLines[j]) != lastLine {
				continue
			}
			// Found anchor match from line i to j
			before := strings.Join(contentLines[:i], "\n")
			after := strings.Join(contentLines[j+1:], "\n")
			result := before
			if before != "" {
				result += "\n"
			}
			result += new
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}
	return "", false
}

// FindBestMatch finds the most similar substring using Levenshtein distance.
func FindBestMatch(content, search string) (string, float64) {
	contentLines := strings.Split(content, "\n")
	searchLines := strings.Split(search, "\n")
	searchLen := len(searchLines)

	if searchLen == 0 || len(contentLines) == 0 {
		return "", 0
	}

	bestSim := 0.0
	bestMatch := ""

	for i := 0; i <= len(contentLines)-searchLen; i++ {
		candidate := strings.Join(contentLines[i:i+searchLen], "\n")
		sim := similarity(candidate, search)
		if sim > bestSim {
			bestSim = sim
			bestMatch = candidate
		}
	}

	return bestMatch, bestSim
}

// similarity computes normalized Levenshtein similarity (0.0–1.0).
func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := max(len(a), len(b))
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Optimize: only keep two rows
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func detectIndentOffset(contentLine, searchLine string) int {
	contentIndent := countLeadingWhitespace(contentLine)
	searchIndent := countLeadingWhitespace(searchLine)
	trimC := strings.TrimSpace(contentLine)
	trimS := strings.TrimSpace(searchLine)
	if trimC != trimS {
		return -1
	}
	return contentIndent - searchIndent
}

func matchWithIndentOffset(contentLine, searchLine string, offset int) bool {
	trimC := strings.TrimSpace(contentLine)
	trimS := strings.TrimSpace(searchLine)
	return trimC == trimS
}

func applyIndentOffset(line string, offset int) string {
	if offset <= 0 || line == "" {
		return line
	}
	return strings.Repeat("\t", offset) + line
}

func countLeadingWhitespace(s string) int {
	count := 0
	for _, r := range s {
		if r == '\t' {
			count++
		} else if r == ' ' {
			count++
		} else {
			break
		}
	}
	return count
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run "TestSimple|TestLineTrimmed|TestIndentation|TestWhitespace|TestBlockAnchor|TestFindBest|TestApplyEdit" -v`
Expected: PASS

**Step 5: Wire into existing Edit tool handler**

In `fs.go`, replace the direct `strings.Replace` call in the Edit handler with `ApplyEditWithFallback()`. Log which strategy was used.

**Step 6: Run full tool tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/agent/tools/edit_strategies.go internal/agent/tools/edit_strategies_test.go internal/agent/tools/fs.go
git commit -m "feat(tools): add 5 fallback edit strategies with Levenshtein fuzzy matching"
```

---

### Task 2: Tool output truncation and save-to-disk

**Files:**
- Create: `internal/agent/tools/truncation.go`
- Create: `internal/agent/tools/truncation_test.go`
- Modify: `internal/agent/tools/registry.go` — wrap all tool executions

**Step 1: Write the failing test**

```go
// internal/agent/tools/truncation_test.go
package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateOutput_Small(t *testing.T) {
	output := "small output"
	result, truncated := TruncateOutput(output, 2000, 50000)
	assert.Equal(t, output, result)
	assert.False(t, truncated)
}

func TestTruncateOutput_TooManyLines(t *testing.T) {
	lines := make([]string, 3000)
	for i := range lines {
		lines[i] = "line content"
	}
	output := strings.Join(lines, "\n")

	result, truncated := TruncateOutput(output, 2000, 500000)
	assert.True(t, truncated)
	resultLines := strings.Split(result, "\n")
	// Should have 2000 lines + truncation notice
	assert.LessOrEqual(t, len(resultLines), 2005)
}

func TestTruncateOutput_TooLarge(t *testing.T) {
	output := strings.Repeat("x", 60000)

	result, truncated := TruncateOutput(output, 2000, 50000)
	assert.True(t, truncated)
	assert.LessOrEqual(t, len(result), 55000) // 50KB + notice
}

func TestSaveTruncatedOutput(t *testing.T) {
	dir := t.TempDir()
	output := strings.Repeat("x", 60000)

	path, err := SaveTruncatedOutput(dir, "read_abc123", output)
	require.NoError(t, err)
	assert.Contains(t, path, "read_abc123")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestTruncate -v`
Expected: FAIL — functions undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/tools/truncation.go
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024 // 50KB
	MaxLineLength   = 2000
)

// TruncateOutput truncates tool output that exceeds line or byte limits.
// Returns the truncated output and whether truncation occurred.
func TruncateOutput(output string, maxLines, maxBytes int) (string, bool) {
	if len(output) <= maxBytes {
		lines := strings.Split(output, "\n")
		if len(lines) <= maxLines {
			return output, false
		}
	}

	lines := strings.Split(output, "\n")

	// Truncate individual long lines
	for i, line := range lines {
		if len(line) > MaxLineLength {
			lines[i] = line[:MaxLineLength] + "..."
		}
	}

	// Truncate by line count (keep head)
	truncated := false
	if len(lines) > maxLines {
		omitted := len(lines) - maxLines
		lines = lines[:maxLines]
		lines = append(lines, fmt.Sprintf("\n... (%d lines omitted. Use Read with offset to see the rest.)", omitted))
		truncated = true
	}

	result := strings.Join(lines, "\n")

	// Truncate by byte count
	if len(result) > maxBytes {
		result = result[:maxBytes]
		result += "\n... (output truncated at byte limit. Full output saved to disk.)"
		truncated = true
	}

	return result, truncated
}

// SaveTruncatedOutput saves the full output to disk for later retrieval.
// Returns the path where it was saved.
func SaveTruncatedOutput(dataDir, toolCallID, output string) (string, error) {
	dir := filepath.Join(dataDir, "tool-output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, fmt.Sprintf("tool_%s.txt", toolCallID))
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// TruncateHint returns a hint message for the agent when output was truncated.
func TruncateHint(savedPath string, isSubagent bool) string {
	if isSubagent {
		return "Output was truncated. Delegate to an explore subagent to read the full content."
	}
	return fmt.Sprintf("Output was truncated. Full output saved to: %s\nUse Read with offset/limit or Grep to find specific content.", savedPath)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestTruncate -v`
Expected: PASS

**Step 5: Integrate into tool registry**

In `registry.go`, wrap all tool `Execute` functions with truncation. After execution, if output exceeds limits, truncate and save full output.

**Step 6: Run full tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/agent/tools/truncation.go internal/agent/tools/truncation_test.go internal/agent/tools/registry.go
git commit -m "feat(tools): add output truncation with save-to-disk for large tool results"
```

---

### Task 3: Batch tool — parallel tool execution

**Files:**
- Create: `internal/agent/tools/batch.go`
- Create: `internal/agent/tools/batch_test.go`
- Modify: `internal/agent/tools/registry.go` — register batch tool

**Step 1: Write the failing test**

```go
// internal/agent/tools/batch_test.go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchTool_ParseInput(t *testing.T) {
	input := `{
		"tool_calls": [
			{"tool": "Read", "input": {"path": "main.go"}},
			{"tool": "Glob", "input": {"pattern": "**/*.go"}}
		]
	}`

	calls, err := ParseBatchInput(input)
	require.NoError(t, err)
	assert.Len(t, calls, 2)
	assert.Equal(t, "Read", calls[0].Tool)
	assert.Equal(t, "Glob", calls[1].Tool)
}

func TestBatchTool_RejectsNesting(t *testing.T) {
	input := `{
		"tool_calls": [
			{"tool": "Batch", "input": {"tool_calls": []}}
		]
	}`

	_, err := ParseBatchInput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot nest")
}

func TestBatchTool_MaxCalls(t *testing.T) {
	calls := make([]BatchCall, 30)
	for i := range calls {
		calls[i] = BatchCall{Tool: "Read", Input: `{"path": "a.go"}`}
	}

	err := ValidateBatchCalls(calls)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum 25")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestBatch -v`
Expected: FAIL — functions undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/tools/batch.go
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const MaxBatchCalls = 25

type BatchCall struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type BatchInput struct {
	ToolCalls []BatchCall `json:"tool_calls"`
}

type BatchResult struct {
	Tool    string `json:"tool"`
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

func ParseBatchInput(raw string) ([]BatchCall, error) {
	var input BatchInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("invalid batch input: %w", err)
	}
	for _, call := range input.ToolCalls {
		if strings.EqualFold(call.Tool, "Batch") {
			return nil, fmt.Errorf("cannot nest Batch tool calls")
		}
	}
	if err := ValidateBatchCalls(input.ToolCalls); err != nil {
		return nil, err
	}
	return input.ToolCalls, nil
}

func ValidateBatchCalls(calls []BatchCall) error {
	if len(calls) > MaxBatchCalls {
		return fmt.Errorf("maximum %d tool calls per batch, got %d", MaxBatchCalls, len(calls))
	}
	return nil
}

// ExecuteBatch runs multiple tool calls in parallel using the provided executor.
func ExecuteBatch(calls []BatchCall, executor func(tool string, input json.RawMessage) (string, error)) []BatchResult {
	results := make([]BatchResult, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c BatchCall) {
			defer wg.Done()
			output, err := executor(c.Tool, c.Input)
			if err != nil {
				results[idx] = BatchResult{Tool: c.Tool, Success: false, Error: err.Error()}
			} else {
				results[idx] = BatchResult{Tool: c.Tool, Success: true, Output: output}
			}
		}(i, call)
	}

	wg.Wait()
	return results
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestBatch -v`
Expected: PASS

**Step 5: Register in tool registry**

Add Batch tool to `registerExec()` in `exec.go`. Schema has `tool_calls` array parameter. Execute function calls `ExecuteBatch` with the existing tool executor.

**Step 6: Commit**

```bash
git add internal/agent/tools/batch.go internal/agent/tools/batch_test.go internal/agent/tools/registry.go
git commit -m "feat(tools): add Batch tool for parallel tool execution (max 25 calls)"
```

---

### Task 4: ApplyPatch validation — validate hunks before applying

**Files:**
- Create: `internal/agent/tools/patch_validator.go`
- Create: `internal/agent/tools/patch_validator_test.go`
- Modify: `internal/agent/tools/fs.go` — integrate validation into ApplyPatch

**Step 1: Write the failing test**

```go
// internal/agent/tools/patch_validator_test.go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePatch_ValidHunk(t *testing.T) {
	original := "line1\nline2\nline3\n"
	patch := `--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
`

	errs := ValidatePatchHunks(original, patch)
	assert.Empty(t, errs)
}

func TestValidatePatch_InvalidContext(t *testing.T) {
	original := "line1\nline2\nline3\n"
	patch := `--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 lineX
-line2
+line2_modified
 line3
`
	// "lineX" doesn't match "line1" in original
	errs := ValidatePatchHunks(original, patch)
	assert.NotEmpty(t, errs)
}

func TestValidatePatch_FileDoesNotExist(t *testing.T) {
	patch := `--- /dev/null
+++ b/new_file.go
@@ -0,0 +1,3 @@
+package main
+
+func main() {}
`
	// New file — no original content to validate against
	errs := ValidatePatchHunks("", patch)
	assert.Empty(t, errs)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestValidatePatch -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/tools/patch_validator.go
package tools

import (
	"fmt"
	"strings"
)

// ValidatePatchHunks validates that patch context lines match the original file.
// Returns a list of validation errors (empty if valid).
func ValidatePatchHunks(original, patch string) []string {
	hunks := parsePatchHunks(patch)
	if len(hunks) == 0 {
		return nil
	}

	origLines := strings.Split(original, "\n")
	var errors []string

	for _, hunk := range hunks {
		if hunk.isNewFile {
			continue
		}

		for _, cl := range hunk.contextLines {
			if cl.lineNum < 1 || cl.lineNum > len(origLines) {
				errors = append(errors, fmt.Sprintf(
					"hunk at line %d: context line %d out of range (file has %d lines)",
					hunk.startLine, cl.lineNum, len(origLines),
				))
				continue
			}
			if strings.TrimRight(origLines[cl.lineNum-1], "\r") != strings.TrimRight(cl.content, "\r") {
				errors = append(errors, fmt.Sprintf(
					"hunk at line %d: context mismatch at line %d: expected %q, got %q",
					hunk.startLine, cl.lineNum, cl.content, origLines[cl.lineNum-1],
				))
			}
		}
	}

	return errors
}

type patchHunk struct {
	startLine    int
	isNewFile    bool
	contextLines []contextLine
}

type contextLine struct {
	lineNum int
	content string
}

func parsePatchHunks(patch string) []patchHunk {
	// Implementation parses unified diff format
	// Extracts context lines (lines starting with ' ') and their positions
	// ... (standard unified diff parser)
	return nil // stub — implement in Step 3
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestValidatePatch -v`
Expected: PASS

**Step 5: Integrate into ApplyPatch tool**

Before applying patch in `fs.go`, call `ValidatePatchHunks()`. If errors, return them to the LLM with guidance to fix the patch.

**Step 6: Commit**

```bash
git add internal/agent/tools/patch_validator.go internal/agent/tools/patch_validator_test.go internal/agent/tools/fs.go
git commit -m "feat(tools): validate patch hunks against file content before applying"
```

---

### Task 5: Permission system — rule-based tool access control

**Files:**
- Create: `internal/agent/permission.go`
- Create: `internal/agent/permission_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/permission_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermission_ExactAllow(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionAllow},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
}

func TestPermission_DenyPattern(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionAllow},
		{Permission: "read", Pattern: ".env*", Action: ActionDeny},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("read", ".env", rules))
	assert.Equal(t, ActionDeny, Evaluate("read", ".env.local", rules))
}

func TestPermission_WildcardPermission(t *testing.T) {
	rules := Ruleset{
		{Permission: "*", Pattern: "*", Action: ActionAllow},
		{Permission: "bash", Pattern: "*", Action: ActionDeny},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("bash", "rm -rf /", rules))
}

func TestPermission_EditToolMapping(t *testing.T) {
	rules := Ruleset{
		{Permission: "edit", Pattern: "*.go", Action: ActionAllow},
		{Permission: "edit", Pattern: "*.yml", Action: ActionDeny},
	}
	// write, edit, multiedit, apply_patch all map to "edit"
	assert.Equal(t, ActionAllow, Evaluate("Write", "main.go", rules))
	assert.Equal(t, ActionAllow, Evaluate("Edit", "main.go", rules))
	assert.Equal(t, ActionAllow, Evaluate("MultiEdit", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("Write", "config.yml", rules))
}

func TestPermission_ExternalDirectory(t *testing.T) {
	rules := Ruleset{
		{Permission: "external_directory", Pattern: "*", Action: ActionDeny},
		{Permission: "external_directory", Pattern: "/tmp/*", Action: ActionAllow},
	}
	assert.Equal(t, ActionDeny, Evaluate("external_directory", "/etc/passwd", rules))
	assert.Equal(t, ActionAllow, Evaluate("external_directory", "/tmp/output.txt", rules))
}

func TestPermission_DefaultAsk(t *testing.T) {
	rules := Ruleset{}
	// No matching rule → default to deny for safety (daemon is non-interactive)
	assert.Equal(t, ActionDeny, Evaluate("bash", "anything", rules))
}

func TestPermission_LastRuleWins(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionDeny},
		{Permission: "read", Pattern: "*", Action: ActionAllow}, // last wins
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
}

func TestPermission_MergeRulesets(t *testing.T) {
	defaults := Ruleset{
		{Permission: "*", Pattern: "*", Action: ActionAllow},
	}
	agent := Ruleset{
		{Permission: "bash", Pattern: "*", Action: ActionDeny},
	}
	merged := Merge(defaults, agent)
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", merged))
	assert.Equal(t, ActionDeny, Evaluate("bash", "rm -rf", merged))
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestPermission -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/permission.go
package agent

import (
	"path/filepath"
	"strings"
)

type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

type Rule struct {
	Permission string
	Pattern    string
	Action     Action
}

type Ruleset []Rule

// editTools maps tool names that modify files to the "edit" permission.
var editTools = map[string]bool{
	"Write":      true,
	"Edit":       true,
	"MultiEdit":  true,
	"ApplyPatch": true,
	"write":      true,
	"edit":       true,
	"multiedit":  true,
	"applypatch": true,
}

// Evaluate checks a permission request against rulesets.
// Returns the action from the last matching rule, or ActionDeny if no match.
func Evaluate(permission, pattern string, rulesets ...Ruleset) Action {
	// Normalize edit tools
	normalizedPerm := strings.ToLower(permission)
	if editTools[permission] {
		normalizedPerm = "edit"
	}

	// Flatten and check in reverse order (last match wins)
	var allRules []Rule
	for _, rs := range rulesets {
		allRules = append(allRules, rs...)
	}

	for i := len(allRules) - 1; i >= 0; i-- {
		rule := allRules[i]
		if matchPermission(normalizedPerm, strings.ToLower(rule.Permission)) &&
			matchPattern(pattern, rule.Pattern) {
			return rule.Action
		}
	}

	return ActionDeny // default for daemon (non-interactive)
}

// Merge combines multiple rulesets into one (order preserved).
func Merge(rulesets ...Ruleset) Ruleset {
	var result Ruleset
	for _, rs := range rulesets {
		result = append(result, rs...)
	}
	return result
}

func matchPermission(perm, rule string) bool {
	if rule == "*" {
		return true
	}
	return perm == rule
}

func matchPattern(path, pattern string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	if matched {
		return true
	}
	// Try full path match
	matched, _ = filepath.Match(pattern, path)
	return matched
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestPermission -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/permission.go internal/agent/permission_test.go
git commit -m "feat(agent): add rule-based permission system with wildcard matching"
```

---

### Task 6: Permission system — integrate into tool execution

**Files:**
- Modify: `internal/agent/builtin.go` — check permissions before tool execution
- Modify: `internal/agent/tools/registry.go` — add permission check hook
- Modify: `internal/models/config.go` — add permission config
- Test: `internal/agent/builtin_test.go`

**Step 1: Add permission config**

In config model, add permission section:
```go
type PermissionConfig struct {
	Default  map[string]string            `toml:"default"`  // tool → action
	Patterns map[string]map[string]string `toml:"patterns"` // tool → pattern → action
}
```

**Step 2: Load permissions from config**

Parse `[permissions]` section from foreman.toml into Ruleset. Support both flat (`read = "allow"`) and pattern-based (`[permissions.patterns.edit] "*.go" = "allow"`).

**Step 3: Check before tool execution**

In the builtin runner's tool execution loop, call `Evaluate()` before executing each tool. If denied, return error message to LLM. For file-based tools, extract the file path from input to use as the pattern.

**Step 4: Add per-agent permission overrides**

In agent runner config, add optional `permissions` field that overrides the default permission ruleset for that agent.

**Step 5: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/agent/builtin.go internal/agent/tools/registry.go internal/models/config.go internal/agent/builtin_test.go
git commit -m "feat(agent): integrate permission system into tool execution pipeline"
```

---

### Task 7: Agent modes — plan mode and explore mode

**Files:**
- Create: `internal/agent/modes.go`
- Create: `internal/agent/modes_test.go`
- Modify: `internal/agent/builtin.go` — support mode switching

**Step 1: Write the failing test**

```go
// internal/agent/modes_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanModePermissions(t *testing.T) {
	mode := PlanMode()
	// Plan mode denies all edit tools
	assert.Equal(t, ActionDeny, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("Write", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("bash", "make build", mode.Permissions))
	// But allows read-only tools
	assert.Equal(t, ActionAllow, Evaluate("Read", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Glob", "**/*.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Grep", "pattern", mode.Permissions))
}

func TestExploreModePermissions(t *testing.T) {
	mode := ExploreMode()
	// Explore mode only allows: Read, Glob, Grep, Bash(read-only), GetSymbol
	assert.Equal(t, ActionAllow, Evaluate("Read", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Glob", "**/*.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Grep", "pattern", mode.Permissions))
	// Denies all edit tools
	assert.Equal(t, ActionDeny, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("Write", "main.go", mode.Permissions))
}

func TestBuildModePermissions(t *testing.T) {
	mode := BuildMode()
	// Build mode allows everything by default
	assert.Equal(t, ActionAllow, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("bash", "go test", mode.Permissions))
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestPlanMode -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/modes.go
package agent

// AgentMode defines a pre-configured set of permissions and behavior.
type AgentMode struct {
	Name        string
	Description string
	Permissions Ruleset
	MaxTurns    int
	ReadOnly    bool
}

// PlanMode creates a read-only agent that can analyze but not modify.
func PlanMode() AgentMode {
	return AgentMode{
		Name:        "plan",
		Description: "Read-only planning mode for analysis before implementation",
		ReadOnly:    true,
		MaxTurns:    20,
		Permissions: Ruleset{
			// Allow read-only tools
			{Permission: "read", Pattern: "*", Action: ActionAllow},
			{Permission: "glob", Pattern: "*", Action: ActionAllow},
			{Permission: "grep", Pattern: "*", Action: ActionAllow},
			{Permission: "getsymbol", Pattern: "*", Action: ActionAllow},
			{Permission: "gettypedefinition", Pattern: "*", Action: ActionAllow},
			{Permission: "getdiff", Pattern: "*", Action: ActionAllow},
			{Permission: "getcommitlog", Pattern: "*", Action: ActionAllow},
			{Permission: "treesummary", Pattern: "*", Action: ActionAllow},
			{Permission: "todowrite", Pattern: "*", Action: ActionAllow},
			{Permission: "todoread", Pattern: "*", Action: ActionAllow},
			// Deny everything else
			{Permission: "edit", Pattern: "*", Action: ActionDeny},
			{Permission: "bash", Pattern: "*", Action: ActionDeny},
			{Permission: "subagent", Pattern: "*", Action: ActionDeny},
		},
	}
}

// ExploreMode creates a fast, read-only codebase search agent.
func ExploreMode() AgentMode {
	return AgentMode{
		Name:        "explore",
		Description: "Fast codebase exploration — read-only search and navigation",
		ReadOnly:    true,
		MaxTurns:    10,
		Permissions: Ruleset{
			{Permission: "read", Pattern: "*", Action: ActionAllow},
			{Permission: "glob", Pattern: "*", Action: ActionAllow},
			{Permission: "grep", Pattern: "*", Action: ActionAllow},
			{Permission: "getsymbol", Pattern: "*", Action: ActionAllow},
			{Permission: "gettypedefinition", Pattern: "*", Action: ActionAllow},
			{Permission: "treesummary", Pattern: "*", Action: ActionAllow},
			// Deny edits and execution
			{Permission: "edit", Pattern: "*", Action: ActionDeny},
			{Permission: "bash", Pattern: "*", Action: ActionDeny},
		},
	}
}

// BuildMode creates the default full-access agent.
func BuildMode() AgentMode {
	return AgentMode{
		Name:        "build",
		Description: "Full-access implementation mode",
		MaxTurns:    15,
		Permissions: Ruleset{
			{Permission: "*", Pattern: "*", Action: ActionAllow},
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run "TestPlanMode|TestExploreMode|TestBuildMode" -v`
Expected: PASS

**Step 5: Wire modes into builtin runner**

Add `Mode` field to `AgentRequest`. When set, merge mode permissions with agent permissions. When using subagent tool, allow specifying mode (e.g., `"mode": "explore"`).

**Step 6: Commit**

```bash
git add internal/agent/modes.go internal/agent/modes_test.go internal/agent/builtin.go
git commit -m "feat(agent): add plan, explore, and build agent modes with scoped permissions"
```

---

### Task 8: LSP tool — language server operations

**Files:**
- Create: `internal/agent/tools/lsp.go`
- Create: `internal/agent/tools/lsp_test.go`
- Modify: `internal/agent/tools/registry.go` — register LSP tool

**Step 1: Write the failing test**

```go
// internal/agent/tools/lsp_test.go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPToolSchema(t *testing.T) {
	schema := LSPToolSchema()
	assert.Contains(t, string(schema), "operation")
	assert.Contains(t, string(schema), "filePath")
	assert.Contains(t, string(schema), "line")
	assert.Contains(t, string(schema), "character")
}

func TestLSPOperations(t *testing.T) {
	// Validate supported operations
	ops := SupportedLSPOperations()
	assert.Contains(t, ops, "goToDefinition")
	assert.Contains(t, ops, "findReferences")
	assert.Contains(t, ops, "hover")
	assert.Contains(t, ops, "documentSymbol")
	assert.Contains(t, ops, "workspaceSymbol")
}

func TestBuildGoplsCommand(t *testing.T) {
	cmd := BuildLSPCommand("goToDefinition", "/path/to/file.go", 10, 5)
	require.NotNil(t, cmd)
	// gopls command should be constructed correctly
	assert.Contains(t, cmd.Args, "definition")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestLSP -v`
Expected: FAIL

**Step 3: Write implementation**

The LSP tool wraps `gopls` (Go language server) commands for code navigation. For Go projects, it supports:
- `goToDefinition` — find where a symbol is defined
- `findReferences` — find all usages of a symbol
- `hover` — get type/doc info at position
- `documentSymbol` — list all symbols in a file
- `workspaceSymbol` — search symbols across project

```go
// internal/agent/tools/lsp.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func SupportedLSPOperations() []string {
	return []string{
		"goToDefinition",
		"findReferences",
		"hover",
		"documentSymbol",
		"workspaceSymbol",
	}
}

func LSPToolSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol"],
				"description": "The LSP operation to perform"
			},
			"filePath": {
				"type": "string",
				"description": "Path to the file"
			},
			"line": {
				"type": "integer",
				"description": "1-based line number"
			},
			"character": {
				"type": "integer",
				"description": "0-based character offset"
			},
			"query": {
				"type": "string",
				"description": "Search query (for workspaceSymbol)"
			}
		},
		"required": ["operation"]
	}`)
}

func BuildLSPCommand(operation, filePath string, line, char int) *exec.Cmd {
	switch operation {
	case "goToDefinition":
		return exec.Command("gopls", "definition", fmt.Sprintf("%s:%d:%d", filePath, line, char))
	case "findReferences":
		return exec.Command("gopls", "references", fmt.Sprintf("%s:%d:%d", filePath, line, char))
	case "hover":
		return exec.Command("gopls", "hover", fmt.Sprintf("%s:%d:%d", filePath, line, char))
	case "documentSymbol":
		return exec.Command("gopls", "symbols", filePath)
	case "workspaceSymbol":
		return exec.Command("gopls", "workspace_symbol", filePath)
	default:
		return nil
	}
}

func ExecuteLSP(ctx context.Context, workDir, operation, filePath string, line, char int, query string) (string, error) {
	var cmd *exec.Cmd

	switch operation {
	case "workspaceSymbol":
		cmd = exec.CommandContext(ctx, "gopls", "workspace_symbol", query)
	default:
		cmd = BuildLSPCommand(operation, filePath, line, char)
	}

	if cmd == nil {
		return "", fmt.Errorf("unsupported LSP operation: %s", operation)
	}

	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", operation, err, strings.TrimSpace(string(out)))
	}

	return strings.TrimSpace(string(out)), nil
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestLSP -v`
Expected: PASS

**Step 5: Register in tool registry**

Add LSP tool to `registerCode()`. Only register if `gopls` is available on PATH.

**Step 6: Commit**

```bash
git add internal/agent/tools/lsp.go internal/agent/tools/lsp_test.go internal/agent/tools/registry.go
git commit -m "feat(tools): add LSP tool for code navigation via gopls"
```

---

### Task 9: Todo tool — session-specific progress tracking

**Files:**
- Create: `internal/agent/tools/todo.go`
- Create: `internal/agent/tools/todo_test.go`
- Modify: `internal/agent/tools/registry.go`

**Step 1: Write the failing test**

```go
// internal/agent/tools/todo_test.go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoStore_AddAndList(t *testing.T) {
	store := NewTodoStore()

	store.Add(Todo{ID: "1", Content: "Write tests", Status: "pending"})
	store.Add(Todo{ID: "2", Content: "Implement feature", Status: "pending"})

	todos := store.List()
	assert.Len(t, todos, 2)
}

func TestTodoStore_Update(t *testing.T) {
	store := NewTodoStore()
	store.Add(Todo{ID: "1", Content: "Write tests", Status: "pending"})

	err := store.Update("1", "completed")
	require.NoError(t, err)

	todos := store.List()
	assert.Equal(t, "completed", todos[0].Status)
}

func TestTodoStore_Replace(t *testing.T) {
	store := NewTodoStore()
	store.Add(Todo{ID: "1", Content: "Old", Status: "pending"})

	store.Replace([]Todo{
		{ID: "1", Content: "Updated", Status: "in_progress"},
		{ID: "2", Content: "New", Status: "pending"},
	})

	todos := store.List()
	assert.Len(t, todos, 2)
	assert.Equal(t, "Updated", todos[0].Content)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestTodoStore -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/tools/todo.go
package tools

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Todo struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"` // pending, in_progress, completed
	Priority int    `json:"priority,omitempty"`
}

type TodoStore struct {
	mu    sync.RWMutex
	todos []Todo
}

func NewTodoStore() *TodoStore {
	return &TodoStore{}
}

func (s *TodoStore) Add(t Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = append(s.todos, t)
}

func (s *TodoStore) Update(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.todos {
		if s.todos[i].ID == id {
			s.todos[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("todo %q not found", id)
}

func (s *TodoStore) Replace(todos []Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = todos
}

func (s *TodoStore) List() []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Todo, len(s.todos))
	copy(result, s.todos)
	return result
}

func (s *TodoStore) JSON() string {
	todos := s.List()
	data, _ := json.MarshalIndent(todos, "", "  ")
	return string(data)
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestTodoStore -v`
Expected: PASS

**Step 5: Register TodoWrite and TodoRead tools**

Add to `registerExec()`:
- **TodoWrite**: Input is `{"todos": [...]}` — replaces entire todo list
- **TodoRead**: No input — returns current todo list as JSON

The TodoStore is created per agent session and passed via context.

**Step 6: Commit**

```bash
git add internal/agent/tools/todo.go internal/agent/tools/todo_test.go internal/agent/tools/registry.go
git commit -m "feat(tools): add TodoWrite/TodoRead tools for session progress tracking"
```

---

### Task 10: Skill discovery — multi-directory with hierarchy

**Files:**
- Create: `internal/skills/discovery.go`
- Create: `internal/skills/discovery_test.go`
- Modify: `internal/skills/loader.go` — use new discovery

**Step 1: Write the failing test**

```go
// internal/skills/discovery_test.go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSkills_ProjectDir(t *testing.T) {
	root := t.TempDir()

	// Create skills in multiple locations
	createSkillFile(t, filepath.Join(root, "skills", "lint.yml"), "lint")
	createSkillFile(t, filepath.Join(root, ".foreman", "skills", "review.yml"), "review")

	paths := DiscoverSkillPaths(root, "")
	assert.GreaterOrEqual(t, len(paths), 2)
}

func TestDiscoverSkills_Hierarchy(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "packages", "api")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	// Root-level skill
	createSkillFile(t, filepath.Join(root, "skills", "root.yml"), "root")
	// Package-level skill
	createSkillFile(t, filepath.Join(subdir, ".foreman", "skills", "local.yml"), "local")

	// Discover from subdir — should find both
	paths := DiscoverSkillPaths(root, subdir)
	assert.GreaterOrEqual(t, len(paths), 2)
}

func TestDiscoverSkills_AdditionalPaths(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()

	createSkillFile(t, filepath.Join(extra, "custom.yml"), "custom")

	paths := DiscoverSkillPaths(root, "", extra)
	found := false
	for _, p := range paths {
		if filepath.Base(p) == "custom.yml" {
			found = true
		}
	}
	assert.True(t, found)
}

func createSkillFile(t *testing.T, path, id string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := fmt.Sprintf("id: %s\ndescription: test\ntrigger: post_lint\nsteps: []\n", id)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/skills/ -run TestDiscoverSkills -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/skills/discovery.go
package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoverSkillPaths finds all skill files from multiple locations.
// Scans in priority order: project skills/, .foreman/skills/, additional paths.
// If startDir is provided, walks up to rootDir collecting skills at each level.
func DiscoverSkillPaths(rootDir, startDir string, additionalPaths ...string) []string {
	seen := make(map[string]bool)
	var paths []string

	addPaths := func(dir string) {
		candidates := []string{
			filepath.Join(dir, "skills"),
			filepath.Join(dir, ".foreman", "skills"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				filepath.Walk(candidate, func(path string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return nil
					}
					ext := strings.ToLower(filepath.Ext(path))
					if ext == ".yml" || ext == ".yaml" || ext == ".md" {
						if !seen[path] {
							seen[path] = true
							paths = append(paths, path)
						}
					}
					return nil
				})
			}
		}
	}

	// Walk from startDir up to rootDir
	if startDir != "" && startDir != rootDir {
		dir := startDir
		for {
			addPaths(dir)
			if dir == rootDir || dir == filepath.Dir(dir) {
				break
			}
			dir = filepath.Dir(dir)
		}
	} else {
		addPaths(rootDir)
	}

	// Additional paths
	for _, extra := range additionalPaths {
		addPaths(extra)
		// Also check if it's a direct file
		if info, err := os.Stat(extra); err == nil && !info.IsDir() {
			if !seen[extra] {
				seen[extra] = true
				paths = append(paths, extra)
			}
		}
	}

	return paths
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/skills/ -run TestDiscoverSkills -v`
Expected: PASS

**Step 5: Integrate into loader**

Replace the single-directory skill loading in `loader.go` with `DiscoverSkillPaths()`. Add `skills.additional_paths` to config.

**Step 6: Commit**

```bash
git add internal/skills/discovery.go internal/skills/discovery_test.go internal/skills/loader.go
git commit -m "feat(skills): add multi-directory skill discovery with hierarchy walk"
```

---

### Task 11: WebFetch tool — HTTP fetch with HTML-to-markdown

**Files:**
- Create: `internal/agent/tools/webfetch.go`
- Create: `internal/agent/tools/webfetch_test.go`
- Modify: `internal/agent/tools/registry.go`

**Step 1: Write the failing test**

```go
// internal/agent/tools/webfetch_test.go
package tools

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebFetch_PlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World!"))
	}))
	defer server.Close()

	result, err := WebFetch(server.URL, "text", 10)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestWebFetch_HTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Title</h1><p>Content</p></body></html>"))
	}))
	defer server.Close()

	result, err := WebFetch(server.URL, "markdown", 10)
	require.NoError(t, err)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Content")
}

func TestWebFetch_SizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 6*1024*1024)) // 6MB
	}))
	defer server.Close()

	_, err := WebFetch(server.URL, "text", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestWebFetch -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/tools/webfetch.go
package tools

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxResponseSize = 5 * 1024 * 1024 // 5MB
	defaultTimeout  = 30 * time.Second
)

// WebFetch fetches a URL and returns content in the specified format.
// Format: "text" (raw), "markdown" (HTML→markdown), "html" (raw HTML).
func WebFetch(url, format string, timeoutSecs int) (string, error) {
	timeout := defaultTimeout
	if timeoutSecs > 0 {
		timeout = time.Duration(timeoutSecs) * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Foreman/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	if len(body) > maxResponseSize {
		return "", fmt.Errorf("response too large (>5MB)")
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	switch format {
	case "markdown":
		if strings.Contains(contentType, "text/html") {
			return htmlToBasicMarkdown(content), nil
		}
		return content, nil
	case "html":
		return content, nil
	default: // "text"
		if strings.Contains(contentType, "text/html") {
			return stripHTML(content), nil
		}
		return content, nil
	}
}

// htmlToBasicMarkdown does a simple HTML→markdown conversion.
func htmlToBasicMarkdown(html string) string {
	// Basic conversion: strip tags, preserve structure
	result := html
	// Headers
	for i := 6; i >= 1; i-- {
		prefix := strings.Repeat("#", i) + " "
		result = strings.ReplaceAll(result, fmt.Sprintf("<h%d>", i), prefix)
		result = strings.ReplaceAll(result, fmt.Sprintf("</h%d>", i), "\n")
		result = strings.ReplaceAll(result, fmt.Sprintf("<H%d>", i), prefix)
		result = strings.ReplaceAll(result, fmt.Sprintf("</H%d>", i), "\n")
	}
	// Paragraphs and breaks
	result = strings.ReplaceAll(result, "<p>", "\n\n")
	result = strings.ReplaceAll(result, "</p>", "")
	result = strings.ReplaceAll(result, "<br>", "\n")
	result = strings.ReplaceAll(result, "<br/>", "\n")
	result = strings.ReplaceAll(result, "<br />", "\n")
	// Lists
	result = strings.ReplaceAll(result, "<li>", "\n- ")
	result = strings.ReplaceAll(result, "</li>", "")
	// Code
	result = strings.ReplaceAll(result, "<code>", "`")
	result = strings.ReplaceAll(result, "</code>", "`")
	result = strings.ReplaceAll(result, "<pre>", "\n```\n")
	result = strings.ReplaceAll(result, "</pre>", "\n```\n")
	// Strip remaining tags
	result = stripHTML(result)
	return strings.TrimSpace(result)
}

func stripHTML(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestWebFetch -v`
Expected: PASS

**Step 5: Register in tool registry**

Add WebFetch tool to `registerExec()`. Schema: `url` (required string), `format` (optional: text/markdown/html), `timeout` (optional int).

**Step 6: Commit**

```bash
git add internal/agent/tools/webfetch.go internal/agent/tools/webfetch_test.go internal/agent/tools/registry.go
git commit -m "feat(tools): add WebFetch tool for HTTP content retrieval with HTML-to-markdown"
```

---

### Task 12: Per-session cost tracking

**Files:**
- Create: `internal/agent/cost_tracker.go`
- Create: `internal/agent/cost_tracker_test.go`
- Modify: `internal/agent/builtin.go` — integrate tracking

**Step 1: Write the failing test**

```go
// internal/agent/cost_tracker_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostTracker_RecordAndTotal(t *testing.T) {
	ct := NewCostTracker()

	ct.Record(CostEntry{
		Model:         "claude-sonnet-4-20250514",
		InputTokens:   1000,
		OutputTokens:  500,
		CachedTokens:  200,
		CostUSD:       0.005,
	})

	ct.Record(CostEntry{
		Model:         "claude-sonnet-4-20250514",
		InputTokens:   2000,
		OutputTokens:  1000,
		CachedTokens:  500,
		CostUSD:       0.010,
	})

	summary := ct.Summary()
	assert.Equal(t, 3000, summary.TotalInputTokens)
	assert.Equal(t, 1500, summary.TotalOutputTokens)
	assert.Equal(t, 700, summary.TotalCachedTokens)
	assert.InDelta(t, 0.015, summary.TotalCostUSD, 0.001)
	assert.Equal(t, 2, summary.LLMCalls)
}

func TestCostTracker_BudgetExceeded(t *testing.T) {
	ct := NewCostTracker()
	ct.SetBudget(0.01)

	ct.Record(CostEntry{CostUSD: 0.008})
	assert.False(t, ct.BudgetExceeded())

	ct.Record(CostEntry{CostUSD: 0.005})
	assert.True(t, ct.BudgetExceeded())
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestCostTracker -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/cost_tracker.go
package agent

import "sync"

type CostEntry struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	CostUSD      float64
}

type CostSummary struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCachedTokens int
	TotalCostUSD      float64
	LLMCalls          int
	ByModel           map[string]float64
}

type CostTracker struct {
	mu      sync.Mutex
	entries []CostEntry
	budget  float64
}

func NewCostTracker() *CostTracker {
	return &CostTracker{}
}

func (ct *CostTracker) SetBudget(usd float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.budget = usd
}

func (ct *CostTracker) Record(entry CostEntry) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.entries = append(ct.entries, entry)
}

func (ct *CostTracker) Summary() CostSummary {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	s := CostSummary{ByModel: make(map[string]float64)}
	for _, e := range ct.entries {
		s.TotalInputTokens += e.InputTokens
		s.TotalOutputTokens += e.OutputTokens
		s.TotalCachedTokens += e.CachedTokens
		s.TotalCostUSD += e.CostUSD
		s.LLMCalls++
		s.ByModel[e.Model] += e.CostUSD
	}
	return s
}

func (ct *CostTracker) BudgetExceeded() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.budget <= 0 {
		return false
	}
	var total float64
	for _, e := range ct.entries {
		total += e.CostUSD
	}
	return total >= ct.budget
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestCostTracker -v`
Expected: PASS

**Step 5: Integrate into builtin runner**

Create `CostTracker` in `Run()`. After each LLM call, record the cost entry. Check budget before each turn. Return cost summary in `AgentResult`.

**Step 6: Commit**

```bash
git add internal/agent/cost_tracker.go internal/agent/cost_tracker_test.go internal/agent/builtin.go
git commit -m "feat(agent): add per-session cost tracking with budget enforcement"
```

---

### Task 13: Subagent improvements — task resumption

**Files:**
- Modify: `internal/agent/tools/exec.go` — enhance Subagent tool
- Create: `internal/agent/task_manager.go`
- Create: `internal/agent/task_manager_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/task_manager_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskManager_CreateAndGet(t *testing.T) {
	tm := NewTaskManager()

	taskID := tm.Create("explore codebase", "Find all API endpoints")
	assert.NotEmpty(t, taskID)

	task, err := tm.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, "explore codebase", task.Description)
	assert.Equal(t, TaskStatusPending, task.Status)
}

func TestTaskManager_UpdateStatus(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("test task", "details")

	tm.SetRunning(taskID)
	task, _ := tm.Get(taskID)
	assert.Equal(t, TaskStatusRunning, task.Status)

	tm.Complete(taskID, "result output")
	task, _ = tm.Get(taskID)
	assert.Equal(t, TaskStatusCompleted, task.Status)
	assert.Equal(t, "result output", task.Result)
}

func TestTaskManager_Resume(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("resumable task", "details")
	tm.Complete(taskID, "partial result")

	// Resume should return the previous result
	task, err := tm.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, "partial result", task.Result)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestTaskManager -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/task_manager.go
package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID          string
	Description string
	Prompt      string
	Status      TaskStatus
	Result      string
	Error       string
	Mode        string // agent mode
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: make(map[string]*Task)}
}

func (tm *TaskManager) Create(description, prompt string) string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	id := uuid.New().String()[:8]
	tm.tasks[id] = &Task{
		ID:          id,
		Description: description,
		Prompt:      prompt,
		Status:      TaskStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	return id
}

func (tm *TaskManager) Get(id string) (*Task, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	task, ok := tm.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return task, nil
}

func (tm *TaskManager) SetRunning(id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusRunning
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) Complete(id, result string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusCompleted
		t.Result = result
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) Fail(id, errMsg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusFailed
		t.Error = errMsg
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) List() []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		result = append(result, t)
	}
	return result
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestTaskManager -v`
Expected: PASS

**Step 5: Enhance Subagent tool**

In `exec.go`, modify the Subagent tool to:
- Accept optional `task_id` for resumption
- Accept `mode` parameter (plan/explore/build)
- Return `task_id` in output for future resumption
- Use `TaskManager` for lifecycle tracking

**Step 6: Commit**

```bash
git add internal/agent/task_manager.go internal/agent/task_manager_test.go internal/agent/tools/exec.go
git commit -m "feat(agent): add task manager for subagent resumption and lifecycle tracking"
```

---

### Task 14: File change tracking — per-session diffs

**Files:**
- Create: `internal/agent/diff_tracker.go`
- Create: `internal/agent/diff_tracker_test.go`
- Modify: `internal/agent/builtin.go` — track changes per session

**Step 1: Write the failing test**

```go
// internal/agent/diff_tracker_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffTracker_RecordChange(t *testing.T) {
	dt := NewDiffTracker()

	dt.RecordChange("main.go", ChangeModified, 10, 5)
	dt.RecordChange("util.go", ChangeCreated, 20, 0)
	dt.RecordChange("main.go", ChangeModified, 3, 2) // second edit to same file

	summary := dt.Summary()
	assert.Equal(t, 2, summary.FilesChanged)
	assert.Equal(t, 33, summary.TotalAdditions)
	assert.Equal(t, 7, summary.TotalDeletions)
}

func TestDiffTracker_FileList(t *testing.T) {
	dt := NewDiffTracker()

	dt.RecordChange("a.go", ChangeModified, 1, 0)
	dt.RecordChange("b.go", ChangeCreated, 5, 0)
	dt.RecordChange("c.go", ChangeDeleted, 0, 10)

	files := dt.ChangedFiles()
	assert.Len(t, files, 3)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestDiffTracker -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/diff_tracker.go
package agent

import "sync"

type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

type FileChange struct {
	Path      string
	Type      ChangeType
	Additions int
	Deletions int
}

type DiffSummary struct {
	FilesChanged   int
	TotalAdditions int
	TotalDeletions int
	Files          []FileChange
}

type DiffTracker struct {
	mu      sync.Mutex
	changes map[string]*FileChange
}

func NewDiffTracker() *DiffTracker {
	return &DiffTracker{changes: make(map[string]*FileChange)}
}

func (dt *DiffTracker) RecordChange(path string, changeType ChangeType, additions, deletions int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if existing, ok := dt.changes[path]; ok {
		existing.Additions += additions
		existing.Deletions += deletions
		if changeType == ChangeDeleted {
			existing.Type = ChangeDeleted
		}
	} else {
		dt.changes[path] = &FileChange{
			Path:      path,
			Type:      changeType,
			Additions: additions,
			Deletions: deletions,
		}
	}
}

func (dt *DiffTracker) Summary() DiffSummary {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	s := DiffSummary{FilesChanged: len(dt.changes)}
	for _, c := range dt.changes {
		s.TotalAdditions += c.Additions
		s.TotalDeletions += c.Deletions
		s.Files = append(s.Files, *c)
	}
	return s
}

func (dt *DiffTracker) ChangedFiles() []string {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	files := make([]string, 0, len(dt.changes))
	for path := range dt.changes {
		files = append(files, path)
	}
	return files
}
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestDiffTracker -v`
Expected: PASS

**Step 5: Integrate into builtin runner**

After each Write/Edit/ApplyPatch/MultiEdit tool execution, call `DiffTracker.RecordChange()` with the file path and change stats. Return `DiffSummary` in `AgentResult`.

**Step 6: Commit**

```bash
git add internal/agent/diff_tracker.go internal/agent/diff_tracker_test.go internal/agent/builtin.go
git commit -m "feat(agent): add per-session file change tracking with diff summaries"
```

---

### Task 15: Final integration and verification

**Step 1: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1`
Expected: All PASS

**Step 2: Build**

Run: `cd /Users/canh/Projects/Indies/Foreman && make build`
Expected: Clean build

**Step 3: Verify new tool registration**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/tools/ -run TestRegistryAllTools -v`
Verify: Batch, LSP, TodoWrite, TodoRead, WebFetch tools are registered alongside existing tools.

**Step 4: Update AGENTS.md**

Document all new tools, agent modes, permission system, and skill discovery in the project AGENTS.md.

**Step 5: Commit**

```bash
git add -A
git commit -m "docs: update AGENTS.md with new tools, modes, permissions from deep OpenCode adoption"
```
