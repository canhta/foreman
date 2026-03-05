package pipeline

import (
	"context"
	"errors"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/models"
)

// DAGTaskAdapter adapts PipelineTaskRunner to the daemon.TaskRunner interface,
// bridging task ID lookups with the full pipeline execution.
type DAGTaskAdapter struct {
	runner *PipelineTaskRunner
	db     TaskRunnerDB
}

// NewDAGTaskAdapter creates an adapter that connects PipelineTaskRunner to DAGExecutor.
func NewDAGTaskAdapter(runner *PipelineTaskRunner, db TaskRunnerDB) *DAGTaskAdapter {
	return &DAGTaskAdapter{runner: runner, db: db}
}

// Run implements daemon.TaskRunner. It looks up the task by ID from the DB
// task list and delegates to PipelineTaskRunner.RunTask.
func (a *DAGTaskAdapter) Run(ctx context.Context, taskID string) daemon.TaskResult {
	// Find the task in the DB. We need the ticket ID to list tasks,
	// but the DAG executor only gives us the task ID. Use the task directly
	// from the pre-loaded task map if available, or query by convention.
	task, err := a.findTask(ctx, taskID)
	if err != nil {
		return daemon.TaskResult{
			TaskID: taskID,
			Status: models.TaskStatusFailed,
			Error:  err,
		}
	}

	err = a.runner.RunTask(ctx, task)
	if err != nil {
		var escalation *EscalationError
		if errors.As(err, &escalation) {
			// Escalation is not a task failure — it's a signal to pause the ticket.
			// Return failed status so the DAG executor stops dependents,
			// but preserve the escalation error for the caller to handle.
			return daemon.TaskResult{
				TaskID: taskID,
				Status: models.TaskStatusFailed,
				Error:  err,
			}
		}
		return daemon.TaskResult{
			TaskID: taskID,
			Status: models.TaskStatusFailed,
			Error:  err,
		}
	}

	return daemon.TaskResult{
		TaskID: taskID,
		Status: models.TaskStatusDone,
	}
}

func (a *DAGTaskAdapter) findTask(ctx context.Context, taskID string) (*models.Task, error) {
	// The task ID is the DB task ID. We need to find which ticket it belongs to.
	// Since tasks are created with known ticket IDs, we search across recent tickets.
	// In practice, the orchestrator should pass the ticket ID when constructing the adapter.
	//
	// For now, iterate through the ticket's tasks. The adapter is constructed per-ticket,
	// so the runner's config.WorkDir is already scoped to the right ticket.
	tickets, err := a.db.ListTasks(ctx, "")
	if err != nil || len(tickets) == 0 {
		// Fallback: return a minimal task with just the ID.
		return &models.Task{ID: taskID}, nil
	}
	for i := range tickets {
		if tickets[i].ID == taskID {
			return &tickets[i], nil
		}
	}
	return &models.Task{ID: taskID}, nil
}

// Compile-time check.
var _ daemon.TaskRunner = (*DAGTaskAdapter)(nil)
