package integration

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTaskRunner implements daemon.TaskRunner for testing.
type mockTaskRunner struct {
	runFunc func(ctx context.Context, taskID string) daemon.TaskResult
}

func (m *mockTaskRunner) Run(ctx context.Context, taskID string) daemon.TaskResult {
	return m.runFunc(ctx, taskID)
}

func successRunner() *mockTaskRunner {
	return &mockTaskRunner{
		runFunc: func(_ context.Context, taskID string) daemon.TaskResult {
			return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
		},
	}
}

func TestDAGExecutor_AllTasksSucceed(t *testing.T) {
	executor := daemon.NewDAGExecutor(successRunner(), 4, 10*time.Second)

	tasks := []daemon.DAGTask{
		{ID: "t1"},
		{ID: "t2", DependsOn: []string{"t1"}},
		{ID: "t3", DependsOn: []string{"t1"}},
		{ID: "t4", DependsOn: []string{"t2", "t3"}},
	}

	results := executor.Execute(context.Background(), tasks)

	require.Len(t, results, 4)
	for _, r := range results {
		assert.Equal(t, models.TaskStatusDone, r.Status, "task %s should be done", r.TaskID)
		assert.NoError(t, r.Error)
	}
}

func TestDAGExecutor_FailurePropagation(t *testing.T) {
	runner := &mockTaskRunner{
		runFunc: func(_ context.Context, taskID string) daemon.TaskResult {
			if taskID == "t2" {
				return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: fmt.Errorf("build failed")}
			}
			return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
		},
	}

	executor := daemon.NewDAGExecutor(runner, 4, 10*time.Second)

	// t1 -> t2 -> t3 -> t4
	tasks := []daemon.DAGTask{
		{ID: "t1"},
		{ID: "t2", DependsOn: []string{"t1"}},
		{ID: "t3", DependsOn: []string{"t2"}},
		{ID: "t4", DependsOn: []string{"t3"}},
	}

	results := executor.Execute(context.Background(), tasks)

	assert.Equal(t, models.TaskStatusDone, results["t1"].Status)
	assert.Equal(t, models.TaskStatusFailed, results["t2"].Status)
	assert.Equal(t, models.TaskStatusSkipped, results["t3"].Status, "t3 should be skipped due to t2 failure")
	assert.Equal(t, models.TaskStatusSkipped, results["t4"].Status, "t4 should be skipped due to t2 failure")
}

func TestDAGExecutor_ParallelExecution(t *testing.T) {
	var maxConcurrent atomic.Int32
	var current atomic.Int32

	runner := &mockTaskRunner{
		runFunc: func(_ context.Context, taskID string) daemon.TaskResult {
			cur := current.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			current.Add(-1)
			return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
		},
	}

	executor := daemon.NewDAGExecutor(runner, 4, 10*time.Second)

	// 4 independent tasks — should run in parallel.
	tasks := []daemon.DAGTask{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d"},
	}

	results := executor.Execute(context.Background(), tasks)
	require.Len(t, results, 4)
	assert.Greater(t, maxConcurrent.Load(), int32(1), "independent tasks should run concurrently")
}

func TestDAGExecutor_EmptyTasks(t *testing.T) {
	executor := daemon.NewDAGExecutor(successRunner(), 4, 10*time.Second)
	results := executor.Execute(context.Background(), nil)
	assert.Empty(t, results)
}

func TestDAGExecutor_ContextCancellation(t *testing.T) {
	runner := &mockTaskRunner{
		runFunc: func(ctx context.Context, taskID string) daemon.TaskResult {
			select {
			case <-ctx.Done():
				return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusSkipped, Error: ctx.Err()}
			case <-time.After(5 * time.Second):
				return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
			}
		},
	}

	executor := daemon.NewDAGExecutor(runner, 2, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tasks := []daemon.DAGTask{
		{ID: "slow1"},
		{ID: "slow2"},
	}

	results := executor.Execute(ctx, tasks)
	require.Len(t, results, 2)

	for _, r := range results {
		assert.NotEqual(t, models.TaskStatusDone, r.Status, "tasks should not complete when context is cancelled")
	}
}

func TestDAGExecutor_PartialFailureIsolation(t *testing.T) {
	// Two independent branches: t1->t2 and t3->t4.
	// t1 fails, t3 succeeds. t2 should be skipped, t4 should succeed.
	runner := &mockTaskRunner{
		runFunc: func(_ context.Context, taskID string) daemon.TaskResult {
			if taskID == "t1" {
				return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: fmt.Errorf("failed")}
			}
			return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
		},
	}

	executor := daemon.NewDAGExecutor(runner, 4, 10*time.Second)

	tasks := []daemon.DAGTask{
		{ID: "t1"},
		{ID: "t2", DependsOn: []string{"t1"}},
		{ID: "t3"},
		{ID: "t4", DependsOn: []string{"t3"}},
	}

	results := executor.Execute(context.Background(), tasks)

	assert.Equal(t, models.TaskStatusFailed, results["t1"].Status)
	assert.Equal(t, models.TaskStatusSkipped, results["t2"].Status)
	assert.Equal(t, models.TaskStatusDone, results["t3"].Status)
	assert.Equal(t, models.TaskStatusDone, results["t4"].Status)
}
