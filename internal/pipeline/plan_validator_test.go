package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePlan_ValidPlan(t *testing.T) {
	workDir := t.TempDir()
	// Create files that the plan references
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal/models"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/models/user.go"), []byte("package models"), 0o644))

	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Add validation",
				FilesToRead:         []string{"internal/models/user.go"},
				FilesToModify:       []string{"internal/models/user.go"},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{
		MaxTasksPerTicket: 20,
	}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidatePlan_NonExistentFileToRead(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Read missing file",
				FilesToRead:         []string{"does/not/exist.go"},
				FilesToModify:       []string{},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "non-existent file")
}

func TestValidatePlan_NewFileMarker(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{
				Title:               "Create new file",
				FilesToModify:       []string{"internal/new_file.go (new)"},
				EstimatedComplexity: "simple",
			},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid)
}

func TestValidatePlan_CyclicDependencies(t *testing.T) {
	workDir := t.TempDir()
	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Task A", DependsOn: []string{"Task B"}, EstimatedComplexity: "simple"},
			{Title: "Task B", DependsOn: []string{"Task A"}, EstimatedComplexity: "simple"},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "cycle")
}

func TestValidatePlan_TooManyTasks(t *testing.T) {
	workDir := t.TempDir()
	tasks := make([]PlannedTask, 25)
	for i := range tasks {
		tasks[i] = PlannedTask{Title: "task", EstimatedComplexity: "simple"}
	}
	plan := &PlannerResult{Status: "OK", Tasks: tasks}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Errors[0], "exceeding limit")
}

func TestEstimateTicketCost(t *testing.T) {
	pricing := map[string]models.PricingConfig{
		"anthropic:claude-sonnet-4-6": {Input: 3.00, Output: 15.00},
		"anthropic:claude-haiku-4-5":  {Input: 0.80, Output: 4.00},
	}

	tasks := []PlannedTask{
		{EstimatedComplexity: "simple"},
		{EstimatedComplexity: "medium"},
		{EstimatedComplexity: "complex"},
	}

	cost, missing := EstimateTicketCost(tasks, pricing, "anthropic:claude-sonnet-4-6", "anthropic:claude-haiku-4-5")
	assert.Empty(t, missing, "no unknown models expected")
	// Expected cost breakdown (sonnet impl, haiku review):
	//   simple:  1*(0.060+0.060) + 1*(0.008+0.004) = 0.132
	//   medium:  2*(0.120+0.120) + 2*(0.016+0.008) = 0.528
	//   complex: 3*(0.180+0.180) + 3*(0.024+0.012) = 1.188
	//   total: 1.848
	assert.InDelta(t, 1.848, cost, 0.001)
	assert.Greater(t, cost, 0.0)
}

func TestValidatePlan_SharedFileWithoutOrdering(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))

	plan := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Task A", FilesToModify: []string{"internal/handler.go"}, EstimatedComplexity: "simple"},
			{Title: "Task B", FilesToModify: []string{"internal/handler.go"}, EstimatedComplexity: "simple"},
		},
	}
	config := &models.LimitsConfig{MaxTasksPerTicket: 20}

	result := ValidatePlan(plan, workDir, config)
	assert.True(t, result.Valid) // Warnings don't make it invalid
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "Multiple tasks modify")
}
