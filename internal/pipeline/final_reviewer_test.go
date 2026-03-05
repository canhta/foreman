// internal/pipeline/final_reviewer_test.go
package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalReviewer_Approved(t *testing.T) {
	mock := &specMock{response: `STATUS: APPROVED
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
	mock := &specMock{response: `STATUS: REJECTED
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
