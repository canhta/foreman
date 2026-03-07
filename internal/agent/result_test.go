package agent

import (
	"strings"
	"testing"
)

// TestAgentResult_ZeroValue verifies that a zero-value AgentResult is valid with no nil panics.
func TestAgentResult_ZeroValue(t *testing.T) {
	var r AgentResult
	if r.Output != "" {
		t.Fatalf("expected empty Output, got %q", r.Output)
	}
	if r.FileChanges != nil {
		t.Fatalf("expected nil FileChanges, got %v", r.FileChanges)
	}
	if r.ReviewResult != nil {
		t.Fatalf("expected nil ReviewResult, got %v", r.ReviewResult)
	}
	if r.Metadata != nil {
		t.Fatalf("expected nil Metadata, got %v", r.Metadata)
	}
	// Ensure nil slices/maps can be ranged safely.
	for range r.FileChanges {
		t.Fatal("unexpected element in nil FileChanges")
	}
	for range r.Metadata {
		t.Fatal("unexpected element in nil Metadata")
	}
}

// TestFileChange_Construction verifies FileChange fields can be set and read correctly.
func TestFileChange_Construction(t *testing.T) {
	fc := FileChange{
		Path:       "internal/foo/bar.go",
		OldContent: "old",
		NewContent: "new",
		IsDiff:     false,
	}
	if fc.Path != "internal/foo/bar.go" {
		t.Fatalf("unexpected path: %s", fc.Path)
	}
	if fc.OldContent != "old" {
		t.Fatalf("unexpected OldContent: %s", fc.OldContent)
	}
	if fc.NewContent != "new" {
		t.Fatalf("unexpected NewContent: %s", fc.NewContent)
	}
	if fc.IsDiff {
		t.Fatal("expected IsDiff=false")
	}

	diff := FileChange{
		Path:       "README.md",
		NewContent: "@@ -1,3 +1,4 @@",
		IsDiff:     true,
	}
	if !diff.IsDiff {
		t.Fatal("expected IsDiff=true")
	}
}

// TestReviewResult_Construction verifies ReviewResult fields can be set and read correctly.
func TestReviewResult_Construction(t *testing.T) {
	rr := ReviewResult{
		Approved: true,
		Severity: "none",
		Issues:   []string{"issue a", "issue b"},
		Summary:  "looks good",
	}
	if !rr.Approved {
		t.Fatal("expected Approved=true")
	}
	if rr.Severity != "none" {
		t.Fatalf("unexpected Severity: %s", rr.Severity)
	}
	if len(rr.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(rr.Issues))
	}
	if rr.Summary != "looks good" {
		t.Fatalf("unexpected Summary: %s", rr.Summary)
	}
}

// TestAgentResult_Metadata verifies that Metadata map works as expected.
func TestAgentResult_Metadata(t *testing.T) {
	r := AgentResult{
		Output:   "done",
		Metadata: map[string]string{"runner": "builtin", "model": "claude-3"},
	}
	if r.Metadata["runner"] != "builtin" {
		t.Fatalf("expected runner=builtin, got %q", r.Metadata["runner"])
	}
	if r.Metadata["model"] != "claude-3" {
		t.Fatalf("expected model=claude-3, got %q", r.Metadata["model"])
	}
	// Setting a new key
	r.Metadata["extra"] = "value"
	if r.Metadata["extra"] != "value" {
		t.Fatal("expected Metadata to accept new keys")
	}
}

// TestParseFileChanges_NewFile verifies NEW FILE block parsing.
func TestParseFileChanges_NewFile(t *testing.T) {
	raw := `=== NEW FILE: internal/foo/bar.go ===
package foo

func Hello() string { return "hello" }
=== END FILE ===`

	changes := parseFileChanges(raw)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	fc := changes[0]
	if fc.Path != "internal/foo/bar.go" {
		t.Fatalf("unexpected path: %q", fc.Path)
	}
	if fc.IsDiff {
		t.Fatal("expected IsDiff=false for new file")
	}
	if !strings.Contains(fc.NewContent, "func Hello") {
		t.Fatalf("NewContent missing expected content: %q", fc.NewContent)
	}
	if fc.OldContent != "" {
		t.Fatalf("expected empty OldContent for new file, got %q", fc.OldContent)
	}
}

// TestParseFileChanges_ModifyFile verifies MODIFY FILE / SEARCH+REPLACE block parsing.
func TestParseFileChanges_ModifyFile(t *testing.T) {
	raw := `=== MODIFY FILE: internal/foo/bar.go ===
<<<< SEARCH
func Hello() string { return "hello" }
>>>>
<<<< REPLACE
func Hello() string { return "world" }
>>>>
=== END FILE ===`

	changes := parseFileChanges(raw)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	fc := changes[0]
	if fc.Path != "internal/foo/bar.go" {
		t.Fatalf("unexpected path: %q", fc.Path)
	}
	if !strings.Contains(fc.OldContent, "hello") {
		t.Fatalf("OldContent missing expected content: %q", fc.OldContent)
	}
	if !strings.Contains(fc.NewContent, "world") {
		t.Fatalf("NewContent missing expected content: %q", fc.NewContent)
	}
}

// TestParseFileChanges_Empty verifies that unstructured output yields no changes.
func TestParseFileChanges_Empty(t *testing.T) {
	raw := "No structured blocks here, just prose."
	changes := parseFileChanges(raw)
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
}

// TestParseReviewResult_Approved verifies STATUS: APPROVED parsing.
func TestParseReviewResult_Approved(t *testing.T) {
	raw := "The code looks good.\nSTATUS: APPROVED\nNo issues found."
	rr := parseReviewResult(raw)
	if rr == nil {
		t.Fatal("expected non-nil ReviewResult")
	}
	if !rr.Approved {
		t.Fatal("expected Approved=true")
	}
	if rr.Severity != "none" {
		t.Fatalf("expected severity=none, got %q", rr.Severity)
	}
}

// TestParseReviewResult_Rejected verifies STATUS: REJECTED parsing.
func TestParseReviewResult_Rejected(t *testing.T) {
	raw := "Found a serious problem.\nSTATUS: REJECTED\nSee issues below."
	rr := parseReviewResult(raw)
	if rr == nil {
		t.Fatal("expected non-nil ReviewResult")
	}
	if rr.Approved {
		t.Fatal("expected Approved=false for REJECTED")
	}
	if rr.Severity != "major" {
		t.Fatalf("expected severity=major, got %q", rr.Severity)
	}
}

// TestParseReviewResult_ChangesRequested verifies STATUS: CHANGES_REQUESTED parsing.
func TestParseReviewResult_ChangesRequested(t *testing.T) {
	raw := "Minor nits.\nSTATUS: CHANGES_REQUESTED\nNothing critical."
	rr := parseReviewResult(raw)
	if rr == nil {
		t.Fatal("expected non-nil ReviewResult")
	}
	if rr.Approved {
		t.Fatal("expected Approved=false for CHANGES_REQUESTED")
	}
	if rr.Severity != "minor" {
		t.Fatalf("expected severity=minor, got %q", rr.Severity)
	}
}

// TestParseReviewResult_Critical verifies that [CRITICAL] tag elevates severity.
func TestParseReviewResult_Critical(t *testing.T) {
	raw := "[CRITICAL] Security hole.\nSTATUS: CHANGES_REQUESTED"
	rr := parseReviewResult(raw)
	if rr == nil {
		t.Fatal("expected non-nil ReviewResult")
	}
	if rr.Severity != "critical" {
		t.Fatalf("expected severity=critical, got %q", rr.Severity)
	}
}

// TestParseReviewResult_NoStatus verifies that prose with no STATUS: line returns nil.
func TestParseReviewResult_NoStatus(t *testing.T) {
	raw := "Here is some output with no status line."
	rr := parseReviewResult(raw)
	if rr != nil {
		t.Fatalf("expected nil ReviewResult for output without STATUS:, got %+v", rr)
	}
}

// TestEnrichResult verifies that enrichResult populates FileChanges and ReviewResult.
func TestEnrichResult_WithApprovedReview(t *testing.T) {
	raw := "Great job.\nSTATUS: APPROVED"
	r := enrichResult(AgentResult{Output: raw, Usage: AgentUsage{NumTurns: 1}})
	if r.ReviewResult == nil {
		t.Fatal("expected non-nil ReviewResult after enrichment")
	}
	if !r.ReviewResult.Approved {
		t.Fatal("expected Approved=true")
	}
	if len(r.FileChanges) != 0 {
		t.Fatalf("expected 0 FileChanges, got %d", len(r.FileChanges))
	}
}
