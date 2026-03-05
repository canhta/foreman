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
	raw := "Here is the plan:\n\n" + "```yaml\nstatus: OK\nmessage: \"\"\n\ntasks:\n  - title: \"Fix bug\"\n    description: \"Fix the nil pointer.\"\n    acceptance_criteria:\n      - \"No panic on nil input\"\n    test_assertions:\n      - \"TestNilInput returns error\"\n    files_to_read: []\n    files_to_modify:\n      - \"internal/handler.go\"\n    estimated_complexity: \"simple\"\n    depends_on: []\n```\n\nLet me know if you need changes."
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
