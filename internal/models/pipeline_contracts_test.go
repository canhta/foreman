package models_test

import (
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanOutputRoundTrip verifies JSON marshal/unmarshal of PlanOutput.
func TestPlanOutputRoundTrip(t *testing.T) {
	original := models.PlanOutput{
		Rationale: "decomposed into two focused tasks",
		Tasks: []models.PlanTask{
			{
				ID:            "task-1",
				Description:   "Add user model",
				FilesToModify: []string{"internal/models/user.go"},
				FilesToRead:   []string{"internal/models/ticket.go"},
				Dependencies:  nil,
			},
			{
				ID:            "task-2",
				Description:   "Add user handler",
				FilesToModify: []string{"internal/api/user.go"},
				FilesToRead:   []string{"internal/models/user.go"},
				Dependencies:  []string{"task-1"},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded models.PlanOutput
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.Rationale, decoded.Rationale)
	assert.Len(t, decoded.Tasks, 2)
	assert.Equal(t, original.Tasks[0].ID, decoded.Tasks[0].ID)
	assert.Equal(t, original.Tasks[0].Description, decoded.Tasks[0].Description)
	assert.Equal(t, original.Tasks[0].FilesToModify, decoded.Tasks[0].FilesToModify)
	assert.Equal(t, original.Tasks[0].FilesToRead, decoded.Tasks[0].FilesToRead)
	assert.Nil(t, decoded.Tasks[0].Dependencies)
	assert.Equal(t, original.Tasks[1].Dependencies, decoded.Tasks[1].Dependencies)
}

// TestPlanOutputFieldTags verifies correct JSON field names.
func TestPlanOutputFieldTags(t *testing.T) {
	out := models.PlanOutput{
		Rationale: "test",
		Tasks: []models.PlanTask{
			{ID: "t1", Description: "d1"},
		},
	}

	data, err := json.Marshal(out)
	require.NoError(t, err)
	raw := string(data)

	assert.Contains(t, raw, `"tasks"`)
	assert.Contains(t, raw, `"rationale"`)
	assert.Contains(t, raw, `"id"`)
	assert.Contains(t, raw, `"description"`)
}

// TestPlanTaskOmitemptyDependencies verifies Dependencies is omitted when nil.
func TestPlanTaskOmitemptyDependencies(t *testing.T) {
	task := models.PlanTask{
		ID:          "t1",
		Description: "no deps",
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)
	// Dependencies should be omitted when nil
	assert.NotContains(t, string(data), `"dependencies"`)
}

// TestImplementOutputRoundTrip verifies JSON marshal/unmarshal of ImplementOutput.
func TestImplementOutputRoundTrip(t *testing.T) {
	original := models.ImplementOutput{
		Summary: "added user model and tests",
		Changes: []models.ImplementedChange{
			{
				Path:       "internal/models/user.go",
				NewContent: "package models\n\ntype User struct{}",
				IsNew:      true,
			},
			{
				Path:       "internal/api/user.go",
				OldContent: "func old() {}",
				NewContent: "func new() {}",
				IsNew:      false,
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded models.ImplementOutput
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.Summary, decoded.Summary)
	assert.Len(t, decoded.Changes, 2)
	assert.Equal(t, original.Changes[0].Path, decoded.Changes[0].Path)
	assert.Equal(t, original.Changes[0].NewContent, decoded.Changes[0].NewContent)
	assert.True(t, decoded.Changes[0].IsNew)
	assert.False(t, decoded.Changes[1].IsNew)
	assert.Equal(t, original.Changes[1].OldContent, decoded.Changes[1].OldContent)
}

// TestImplementOutputFieldTags verifies correct JSON field names.
func TestImplementOutputFieldTags(t *testing.T) {
	out := models.ImplementOutput{
		Summary: "summary",
		Changes: []models.ImplementedChange{
			{Path: "foo.go", NewContent: "bar"},
		},
	}

	data, err := json.Marshal(out)
	require.NoError(t, err)
	raw := string(data)

	assert.Contains(t, raw, `"changes"`)
	assert.Contains(t, raw, `"summary"`)
	assert.Contains(t, raw, `"path"`)
	assert.Contains(t, raw, `"new_content"`)
}

// TestReviewOutputRoundTrip verifies JSON marshal/unmarshal of ReviewOutput.
func TestReviewOutputRoundTrip(t *testing.T) {
	original := models.ReviewOutput{
		Approved: true,
		Severity: "minor",
		Summary:  "overall looks good",
		Issues: []models.ReviewIssue{
			{
				Severity:    "minor",
				Description: "could extract helper function",
				File:        "handler.go",
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded models.ReviewOutput
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.Approved, decoded.Approved)
	assert.Equal(t, original.Severity, decoded.Severity)
	assert.Equal(t, original.Summary, decoded.Summary)
	assert.Len(t, decoded.Issues, 1)
	assert.Equal(t, original.Issues[0].Severity, decoded.Issues[0].Severity)
	assert.Equal(t, original.Issues[0].Description, decoded.Issues[0].Description)
	assert.Equal(t, original.Issues[0].File, decoded.Issues[0].File)
}

// TestReviewOutputRejected verifies rejected review round-trips.
func TestReviewOutputRejected(t *testing.T) {
	original := models.ReviewOutput{
		Approved: false,
		Severity: "critical",
		Summary:  "SQL injection found",
		Issues: []models.ReviewIssue{
			{
				Severity:    "critical",
				Description: "SQL injection in query builder — use parameterized queries",
				File:        "handler.go",
			},
			{
				Severity:    "major",
				Description: "password stored in plaintext",
				File:        "user.go",
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded models.ReviewOutput
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.False(t, decoded.Approved)
	assert.Equal(t, "critical", decoded.Severity)
	assert.Len(t, decoded.Issues, 2)
}

// TestReviewIssueFileOmitempty verifies File is omitted when empty.
func TestReviewIssueFileOmitempty(t *testing.T) {
	issue := models.ReviewIssue{
		Severity:    "minor",
		Description: "no file context",
	}

	data, err := json.Marshal(issue)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"file"`)
}

// TestReviewOutputFieldTags verifies correct JSON field names.
func TestReviewOutputFieldTags(t *testing.T) {
	out := models.ReviewOutput{
		Approved: true,
		Severity: "none",
		Issues:   []models.ReviewIssue{},
		Summary:  "ok",
	}

	data, err := json.Marshal(out)
	require.NoError(t, err)
	raw := string(data)

	assert.Contains(t, raw, `"approved"`)
	assert.Contains(t, raw, `"severity"`)
	assert.Contains(t, raw, `"issues"`)
	assert.Contains(t, raw, `"summary"`)
}

// TestImplementedChangeOmitemptyOldContent verifies OldContent is omitted when empty.
func TestImplementedChangeOmitemptyOldContent(t *testing.T) {
	change := models.ImplementedChange{
		Path:       "new_file.go",
		NewContent: "package main",
		IsNew:      true,
	}

	data, err := json.Marshal(change)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"old_content"`)
}
