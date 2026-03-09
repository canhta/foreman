package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskManager_CreateAndGet(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("explore codebase", "Find all API endpoints")
	assert.NotEmpty(t, taskID)
	task, err := tm.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, "explore codebase", task.Description)
	assert.Equal(t, TaskStatusPending, task.Status)
}

func TestTaskManager_UpdateStatus(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("test task", "details")
	tm.SetRunning(taskID)
	task, _ := tm.Get(taskID)
	assert.Equal(t, TaskStatusRunning, task.Status)
	tm.Complete(taskID, "result output")
	task, _ = tm.Get(taskID)
	assert.Equal(t, TaskStatusCompleted, task.Status)
	assert.Equal(t, "result output", task.Result)
}

func TestTaskManager_Resume(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("resumable task", "details")
	tm.Complete(taskID, "partial result")
	task, err := tm.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, "partial result", task.Result)
}
