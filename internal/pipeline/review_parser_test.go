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
