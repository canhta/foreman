// internal/git/pr_test.go
package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		IsPartial:     true,
		FailedTask:    "Add JWT validation",
		FailureReason: "Tests failed after 2 retries",
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
