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
		MaxTasksPerTicket:  20,
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
		MaxTasksPerTicket:  20,
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
		MaxTasksPerTicket:  20,
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
