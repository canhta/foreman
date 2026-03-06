package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// TaskRunner is the interface that task executors must implement.
type TaskRunner interface {
	Run(ctx context.Context, taskID string) TaskResult
}

// TaskResult holds the outcome of a single task execution.
type TaskResult struct {
	Error  error
	TaskID string
	Status models.TaskStatus
}

// DAGTask represents a task node in the dependency graph.
type DAGTask struct {
	ID        string
	DependsOn []string
}

// DAGExecutor runs tasks respecting their dependency graph with bounded concurrency.
type DAGExecutor struct {
	runner      TaskRunner
	maxWorkers  int
	taskTimeout time.Duration
}

// NewDAGExecutor creates a new DAGExecutor.
func NewDAGExecutor(runner TaskRunner, maxWorkers int, taskTimeout time.Duration) *DAGExecutor {
	return &DAGExecutor{
		runner:      runner,
		maxWorkers:  maxWorkers,
		taskTimeout: taskTimeout,
	}
}

// Execute runs all tasks in the DAG respecting dependencies.
// Returns a map of taskID -> TaskResult for every task.
func (e *DAGExecutor) Execute(ctx context.Context, tasks []DAGTask) map[string]TaskResult {
	if len(tasks) == 0 {
		return map[string]TaskResult{}
	}

	// Build adjacency (parent -> children) and in-degree maps.
	inDegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))
	taskSet := make(map[string]bool, len(tasks))

	for _, t := range tasks {
		taskSet[t.ID] = true
		inDegree[t.ID] = len(t.DependsOn)
		for _, dep := range t.DependsOn {
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	results := make(map[string]TaskResult, len(tasks))

	readyChan := make(chan string, len(tasks))
	resultChan := make(chan TaskResult, len(tasks))

	// Seed ready tasks (in-degree == 0).
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			readyChan <- t.ID
		}
	}

	// Start worker pool.
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	var workerWg sync.WaitGroup
	for i := 0; i < e.maxWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case taskID, ok := <-readyChan:
					if !ok {
						return
					}
					taskCtx, taskCancel := context.WithTimeout(workerCtx, e.taskTimeout)
					result := e.runner.Run(taskCtx, taskID)
					taskCancel()
					resultChan <- result
				}
			}
		}()
	}

	// Coordinator: collect results, propagate failures, push ready tasks.
	remaining := len(tasks)
	for remaining > 0 {
		select {
		case <-ctx.Done():
			// Context cancelled: mark all unfinished tasks as skipped.
			for id := range taskSet {
				if _, done := results[id]; !done {
					results[id] = TaskResult{TaskID: id, Status: models.TaskStatusSkipped, Error: ctx.Err()}
				}
			}
			remaining = 0
		case res := <-resultChan:
			results[res.TaskID] = res
			remaining--

			if res.Status == models.TaskStatusDone {
				// Check dependents; push newly ready ones.
				for _, child := range dependents[res.TaskID] {
					inDegree[child]--
					if inDegree[child] == 0 {
						readyChan <- child
					}
				}
			} else {
				// BFS failure propagation: skip all transitive dependents.
				queue := []string{res.TaskID}
				for len(queue) > 0 {
					cur := queue[0]
					queue = queue[1:]
					for _, child := range dependents[cur] {
						if _, done := results[child]; !done {
							results[child] = TaskResult{
								TaskID: child,
								Status: models.TaskStatusSkipped,
							}
							remaining--
							queue = append(queue, child)
						}
					}
				}
			}
		}
	}

	workerCancel()
	close(readyChan)
	workerWg.Wait()

	return results
}
