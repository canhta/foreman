package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQualityReviewer_Approved(t *testing.T) {
	mock := &specMock{response: `STATUS: APPROVED

ISSUES:
- [MINOR] handler.go: could use a named constant for status code

STRENGTHS:
- Clean error handling
- Good test coverage`}

	reviewer := NewQualityReviewer(mock, mustLoadTestRegistry(t))
	result, err := reviewer.Review(context.Background(), QualityReviewInput{
		Diff:             "diff --git a/handler.go\n+func GetUsers() {}",
		CodebasePatterns: "go, stdlib, standard go conventions",
	})

	require.NoError(t, err)
	assert.True(t, result.Approved)
	assert.Len(t, result.Issues, 1)
	assert.NotEqual(t, "critical", result.Severity)
}

func TestQualityReviewer_ChangesRequested(t *testing.T) {
	mock := &specMock{response: `STATUS: CHANGES_REQUESTED

ISSUES:
- [CRITICAL] handler.go: SQL injection in query — use parameterized queries
- [IMPORTANT] user.go: password stored in plaintext, use bcrypt

STRENGTHS:
- Tests are comprehensive`}

	reviewer := NewQualityReviewer(mock, mustLoadTestRegistry(t))
	result, err := reviewer.Review(context.Background(), QualityReviewInput{
		Diff:             "diff --git a/handler.go\n+db.Query(\"SELECT * WHERE id=\" + id)",
		CodebasePatterns: "go, stdlib",
	})

	require.NoError(t, err)
	assert.False(t, result.Approved)
	assert.Equal(t, "critical", result.Severity)
	assert.Len(t, result.Issues, 2)
}
