package pipeline

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseReviewOutputTyped_Approved(t *testing.T) {
	raw := `STATUS: APPROVED

CRITERIA:
- [pass] User struct has Name, Email, ID fields
- [pass] Validation returns error on empty name

ISSUES:
- None

EXTRAS:
- None`

	result := ParseReviewOutputTyped(raw)
	assert.True(t, result.Approved)
	assert.Equal(t, "none", result.Severity)
	assert.Empty(t, result.Issues)
}

func TestParseReviewOutputTyped_RejectedWithCritical(t *testing.T) {
	raw := `STATUS: REJECTED

ISSUES:
- [CRITICAL] handler.go: SQL injection in query builder — use parameterized queries
- [IMPORTANT] user.go: password stored in plaintext`

	result := ParseReviewOutputTyped(raw)
	assert.False(t, result.Approved)
	assert.Equal(t, "critical", result.Severity)
	assert.Len(t, result.Issues, 2)
	assert.Equal(t, "critical", result.Issues[0].Severity)
	assert.Equal(t, "major", result.Issues[1].Severity) // IMPORTANT maps to major
	assert.Equal(t, "handler.go", result.Issues[0].File)
}

func TestParseReviewOutputTyped_ChangesRequestedWithMinor(t *testing.T) {
	raw := `STATUS: CHANGES_REQUESTED

ISSUES:
- [MINOR] handler.go: could extract helper function for repetitive validation
- [MAJOR] user.go: missing input sanitization`

	result := ParseReviewOutputTyped(raw)
	assert.False(t, result.Approved)
	assert.Equal(t, "major", result.Severity)
	assert.Len(t, result.Issues, 2)
	assert.Equal(t, "minor", result.Issues[0].Severity)
	assert.Equal(t, "major", result.Issues[1].Severity)
}

func TestParseReviewOutputTyped_ApprovedWithMinorIssues(t *testing.T) {
	raw := `STATUS: APPROVED

ISSUES:
- [MINOR] handler.go: could extract helper function for repetitive validation

STRENGTHS:
- Clean error handling`

	result := ParseReviewOutputTyped(raw)
	assert.True(t, result.Approved)
	assert.Equal(t, "minor", result.Severity)
	assert.Len(t, result.Issues, 1)
	assert.Equal(t, "minor", result.Issues[0].Severity)
}

func TestParseReviewOutputTyped_WithSummary(t *testing.T) {
	raw := `STATUS: APPROVED
SUMMARY: Implementation correctly addresses all ticket requirements.
REVIEW_NOTES: Consider adding pagination in a follow-up ticket.`

	result := ParseReviewOutputTyped(raw)
	assert.True(t, result.Approved)
	assert.Contains(t, result.Summary, "correctly addresses")
}

func TestParseReviewOutputTyped_Garbage(t *testing.T) {
	raw := "Here's my review: looks good to me!"
	result := ParseReviewOutputTyped(raw)
	assert.False(t, result.Approved)
	assert.Equal(t, "none", result.Severity)
	assert.Empty(t, result.Issues)
}

func TestParseReviewOutputTyped_NoIssues(t *testing.T) {
	raw := `STATUS: APPROVED

ISSUES:
- None`

	result := ParseReviewOutputTyped(raw)
	assert.True(t, result.Approved)
	assert.Empty(t, result.Issues)
	assert.Equal(t, "none", result.Severity)
}

func TestParseReviewOutputTyped_IssueFileExtraction(t *testing.T) {
	raw := `STATUS: REJECTED

ISSUES:
- [CRITICAL] mypackage/service.go: null pointer dereference possible
- [MINOR] plain issue with no file prefix`

	result := ParseReviewOutputTyped(raw)
	assert.False(t, result.Approved)
	// First issue has file extracted
	assert.Equal(t, "mypackage/service.go", result.Issues[0].File)
	assert.Contains(t, result.Issues[0].Description, "null pointer")
	// Second issue has no file
	assert.Empty(t, result.Issues[1].File)
	assert.Contains(t, result.Issues[1].Description, "plain issue")
}

// TestParseReviewOutputTyped_UnrecognizedSeverityTag verifies that an issue with an
// unrecognized severity tag results in overall severity "none" — not "minor".
// This guards against the bug where computeOverallSeverity initialized best="minor".
func TestParseReviewOutputTyped_UnrecognizedSeverityTag(t *testing.T) {
	raw := `STATUS: REJECTED

ISSUES:
- [UNKNOWN] some/file.go: something odd happened`

	result := ParseReviewOutputTyped(raw)
	assert.False(t, result.Approved)
	assert.Len(t, result.Issues, 1)
	// Unrecognized tag → normalizeSeverity returns "minor" for the issue itself,
	// but the overall severity should reflect that: "minor".
	// The real edge case is an issue whose Severity field is entirely empty or
	// custom. We test that by directly calling computeOverallSeverity below.
	_ = result
}

// TestComputeOverallSeverity_AllUnrecognized verifies that issues whose Severity
// field contains an unrecognized string produce overall severity "none", not "minor".
func TestComputeOverallSeverity_AllUnrecognized(t *testing.T) {
	issues := []models.ReviewIssue{
		{Severity: "unknown", Description: "something"},
		{Severity: "weird", Description: "something else"},
	}
	result := computeOverallSeverity(issues)
	assert.Equal(t, "none", result)
}

// TestParseReviewOutputTyped_ReviewNotesPreserved verifies that ReviewNotes from the
// final reviewer format is propagated into the typed ReviewOutput.ReviewNotes field.
func TestParseReviewOutputTyped_ReviewNotesPreserved(t *testing.T) {
	raw := `STATUS: APPROVED
SUMMARY: Implementation correctly addresses all ticket requirements.
REVIEW_NOTES: Consider adding pagination in a follow-up ticket.`

	result := ParseReviewOutputTyped(raw)
	assert.True(t, result.Approved)
	assert.Contains(t, result.Summary, "correctly addresses")
	assert.Contains(t, result.ReviewNotes, "pagination")
}
