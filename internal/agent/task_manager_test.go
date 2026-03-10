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

func TestTaskManager_SequentialIDs(t *testing.T) {
	tm := NewTaskManager()
	id1 := tm.Create("task one", "")
	id2 := tm.Create("task two", "")
	assert.Equal(t, "task-1", id1)
	assert.Equal(t, "task-2", id2)
}

func TestTaskManager_Fail(t *testing.T) {
	tm := NewTaskManager()
	taskID := tm.Create("failing task", "details")
	tm.SetRunning(taskID)
	tm.Fail(taskID, "something went wrong")
	task, err := tm.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusFailed, task.Status)
	assert.Equal(t, "something went wrong", task.Error)
}

func TestTaskManager_GetUnknown(t *testing.T) {
	tm := NewTaskManager()
	_, err := tm.Get("task-999")
	assert.Error(t, err)
}

func TestTaskManager_List(t *testing.T) {
	tm := NewTaskManager()
	tm.Create("task A", "")
	tm.Create("task B", "")
	tm.Create("task C", "")
	list := tm.List()
	assert.Len(t, list, 3)
}
