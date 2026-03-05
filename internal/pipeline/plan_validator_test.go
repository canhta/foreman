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
