package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixturesExist(t *testing.T) {
	fixtures := []string{
		"../fixtures/sample_repo/main.go",
		"../fixtures/sample_repo/go.mod",
		"../fixtures/sample_tickets/LOCAL-1.json",
	}
	for _, f := range fixtures {
		path := filepath.Join(".", f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("fixture missing: %s", f)
		}
	}
}

func TestCheckTicketClarity_ClarificationEnabled(t *testing.T) {
	p := pipeline.NewPipeline(pipeline.PipelineConfig{
		EnableClarification: true,
	})

	tests := []struct {
		name   string
		ticket *models.Ticket
		wantOK bool
	}{
		{
			name: "clear ticket with long description",
			ticket: &models.Ticket{
				Description:        "Create a REST endpoint for user management. GET /users returns a list of users from an in-memory store.",
				AcceptanceCriteria: "GET /users returns 200 with JSON array",
			},
			wantOK: true,
		},
		{
			name: "clear ticket with acceptance criteria only",
			ticket: &models.Ticket{
				Description:        "Short desc",
				AcceptanceCriteria: "GET /users returns 200",
			},
			wantOK: true,
		},
		{
			name: "unclear ticket - short description no criteria",
			ticket: &models.Ticket{
				Description:        "Fix the bug",
				AcceptanceCriteria: "",
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CheckTicketClarity(tt.ticket)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestCheckTicketClarity_ClarificationDisabled(t *testing.T) {
	p := pipeline.NewPipeline(pipeline.PipelineConfig{
		EnableClarification: false,
	})

	// Even a vague ticket should pass when clarification is disabled.
	ok, err := p.CheckTicketClarity(&models.Ticket{
		Description: "Fix it",
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTopologicalSort_LinearChain(t *testing.T) {
	tasks := []pipeline.PlannedTask{
		{Title: "Create model", DependsOn: nil},
		{Title: "Add handler", DependsOn: []string{"Create model"}},
		{Title: "Write tests", DependsOn: []string{"Add handler"}},
	}

	sorted, err := pipeline.TopologicalSort(tasks)
	require.NoError(t, err)
	require.Len(t, sorted, 3)

	// Build position map to verify ordering.
	pos := map[string]int{}
	for i, task := range sorted {
		pos[task.Title] = i
	}

	assert.Less(t, pos["Create model"], pos["Add handler"])
	assert.Less(t, pos["Add handler"], pos["Write tests"])
}

func TestTopologicalSort_DiamondDependency(t *testing.T) {
	// A -> B, A -> C, B -> D, C -> D
	tasks := []pipeline.PlannedTask{
		{Title: "A", DependsOn: nil},
		{Title: "B", DependsOn: []string{"A"}},
		{Title: "C", DependsOn: []string{"A"}},
		{Title: "D", DependsOn: []string{"B", "C"}},
	}

	sorted, err := pipeline.TopologicalSort(tasks)
	require.NoError(t, err)
	require.Len(t, sorted, 4)

	pos := map[string]int{}
	for i, task := range sorted {
		pos[task.Title] = i
	}

	assert.Equal(t, 0, pos["A"])
	assert.Less(t, pos["B"], pos["D"])
	assert.Less(t, pos["C"], pos["D"])
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	tasks := []pipeline.PlannedTask{
		{Title: "A", DependsOn: []string{"B"}},
		{Title: "B", DependsOn: []string{"A"}},
	}

	_, err := pipeline.TopologicalSort(tasks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestTopologicalSort_UnknownDependency(t *testing.T) {
	tasks := []pipeline.PlannedTask{
		{Title: "A", DependsOn: []string{"nonexistent"}},
	}

	_, err := pipeline.TopologicalSort(tasks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown task")
}

func TestTopologicalSort_IndependentTasks(t *testing.T) {
	tasks := []pipeline.PlannedTask{
		{Title: "A"},
		{Title: "B"},
		{Title: "C"},
	}

	sorted, err := pipeline.TopologicalSort(tasks)
	require.NoError(t, err)
	require.Len(t, sorted, 3)
}
