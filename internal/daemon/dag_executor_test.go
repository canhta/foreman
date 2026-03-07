package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTaskRunner is a test double for TaskRunner.
type mockTaskRunner struct {
	failTasks   map[string]bool
	execOrder   []string
	runDuration time.Duration
	mu          sync.Mutex
	activeCnt   int32
	maxConcur   int32
}

func newMockRunner(duration time.Duration, failTasks map[string]bool) *mockTaskRunner {
	if failTasks == nil {
		failTasks = map[string]bool{}
	}
	return &mockTaskRunner{
		failTasks:   failTasks,
		runDuration: duration,
	}
}

func (m *mockTaskRunner) Run(ctx context.Context, taskID string) TaskResult {
	cur := atomic.AddInt32(&m.activeCnt, 1)
	// Track max concurrency
	for {
		old := atomic.LoadInt32(&m.maxConcur)
		if cur <= old || atomic.CompareAndSwapInt32(&m.maxConcur, old, cur) {
			break
		}
	}

	select {
	case <-time.After(m.runDuration):
	case <-ctx.Done():
		atomic.AddInt32(&m.activeCnt, -1)
		return TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: ctx.Err()}
	}

	atomic.AddInt32(&m.activeCnt, -1)

	m.mu.Lock()
	m.execOrder = append(m.execOrder, taskID)
	m.mu.Unlock()

	if m.failTasks[taskID] {
		return TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: assert.AnError}
	}
	return TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}

func TestDAGExecutor_SingleTask(t *testing.T) {
	runner := newMockRunner(10*time.Millisecond, nil)
	exec := NewDAGExecutor(runner, 2, 5*time.Second)

	tasks := []DAGTask{{ID: "A"}}
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 1)
	assert.Equal(t, models.TaskStatusDone, results["A"].Status)
	assert.Nil(t, results["A"].Error)
}

func TestDAGExecutor_ParallelIndependent(t *testing.T) {
	runner := newMockRunner(50*time.Millisecond, nil)
	exec := NewDAGExecutor(runner, 3, 5*time.Second)

	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B"},
		{ID: "C"},
	}
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 3)
	for _, id := range []string{"A", "B", "C"} {
		assert.Equal(t, models.TaskStatusDone, results[id].Status)
	}
	assert.Equal(t, int32(3), atomic.LoadInt32(&runner.maxConcur), "all 3 independent tasks should run concurrently")
}

func TestDAGExecutor_DependencyChain(t *testing.T) {
	runner := newMockRunner(10*time.Millisecond, nil)
	exec := NewDAGExecutor(runner, 3, 5*time.Second)

	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"B"}},
	}
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 3)
	for _, id := range []string{"A", "B", "C"} {
		assert.Equal(t, models.TaskStatusDone, results[id].Status)
	}

	// Verify ordering: A before B before C
	runner.mu.Lock()
	order := runner.execOrder
	runner.mu.Unlock()

	idxA, idxB, idxC := -1, -1, -1
	for i, id := range order {
		switch id {
		case "A":
			idxA = i
		case "B":
			idxB = i
		case "C":
			idxC = i
		}
	}
	assert.True(t, idxA < idxB, "A should complete before B")
	assert.True(t, idxB < idxC, "B should complete before C")
}

func TestDAGExecutor_BFSFailurePropagation(t *testing.T) {
	runner := newMockRunner(10*time.Millisecond, map[string]bool{"A": true})
	exec := NewDAGExecutor(runner, 3, 5*time.Second)

	// DAG: A -> B -> D
	//      A -> C -> D
	//      E (independent)
	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"A"}},
		{ID: "D", DependsOn: []string{"B", "C"}},
		{ID: "E"},
	}
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 5)
	assert.Equal(t, models.TaskStatusFailed, results["A"].Status)
	assert.Equal(t, models.TaskStatusSkipped, results["B"].Status)
	assert.Equal(t, models.TaskStatusSkipped, results["C"].Status)
	assert.Equal(t, models.TaskStatusSkipped, results["D"].Status)
	assert.Equal(t, models.TaskStatusDone, results["E"].Status)
}

func TestDAGExecutor_BoundedConcurrency(t *testing.T) {
	runner := newMockRunner(50*time.Millisecond, nil)
	exec := NewDAGExecutor(runner, 2, 5*time.Second)

	tasks := make([]DAGTask, 6)
	for i := 0; i < 6; i++ {
		tasks[i] = DAGTask{ID: string(rune('A' + i))}
	}
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 6)
	for _, r := range results {
		assert.Equal(t, models.TaskStatusDone, r.Status)
	}
	assert.LessOrEqual(t, atomic.LoadInt32(&runner.maxConcur), int32(2), "concurrency should not exceed maxWorkers=2")
}

func TestDAGExecutor_ContextCancellation(t *testing.T) {
	runner := newMockRunner(500*time.Millisecond, nil)
	exec := NewDAGExecutor(runner, 3, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B"},
		{ID: "C"},
	}
	results := exec.Execute(ctx, tasks)

	require.Len(t, results, 3)
	// All tasks should be present in results (failed or skipped due to cancellation)
	for _, id := range []string{"A", "B", "C"} {
		_, ok := results[id]
		assert.True(t, ok, "task %s should be in results", id)
	}
}

// mockDAGStateStore is a test double for dagStateStore that records SaveDAGState calls.
type mockDAGStateStore struct {
	mu      sync.Mutex
	calls   []db.DAGState
	saveErr error
}

func (m *mockDAGStateStore) SaveDAGState(_ context.Context, _ string, state db.DAGState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, db.DAGState{
		TicketID:       state.TicketID,
		CompletedTasks: append([]string(nil), state.CompletedTasks...),
	})
	return m.saveErr
}

func (m *mockDAGStateStore) savedStates() []db.DAGState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]db.DAGState, len(m.calls))
	copy(out, m.calls)
	return out
}

// TestDAGExecutor_WithDAGStore_PersistsStateAfterEachTask verifies that
// persistDAGState is called after each completed task and that the saved state
// contains the correct ticketID and accumulated completedIDs.
func TestDAGExecutor_WithDAGStore_PersistsStateAfterEachTask(t *testing.T) {
	runner := newMockRunner(5*time.Millisecond, nil)

	store := &mockDAGStateStore{}
	const ticketID = "ticket-persist-test"

	// Two independent tasks so both complete.
	tasks := []DAGTask{
		{ID: "task-1"},
		{ID: "task-2"},
	}

	exec := NewDAGExecutor(runner, 2, 5*time.Second).
		WithDAGStore(store, ticketID)

	results := exec.Execute(context.Background(), tasks)

	// Both tasks must succeed.
	require.Len(t, results, 2)
	assert.Equal(t, models.TaskStatusDone, results["task-1"].Status)
	assert.Equal(t, models.TaskStatusDone, results["task-2"].Status)

	// SaveDAGState must have been called at least twice (once per completed task).
	saved := store.savedStates()
	require.GreaterOrEqual(t, len(saved), 2, "SaveDAGState must be called after each completed task")

	// Every saved snapshot must have the correct ticketID.
	for i, s := range saved {
		assert.Equal(t, ticketID, s.TicketID, "snapshot[%d] has wrong ticketID", i)
	}

	// The final snapshot must contain both task IDs (order-independent).
	last := saved[len(saved)-1]
	assert.ElementsMatch(t, []string{"task-1", "task-2"}, last.CompletedTasks,
		"final snapshot must include all completed task IDs")
}
