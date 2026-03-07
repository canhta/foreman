package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/models"
)

// DAGTaskAdapter adapts PipelineTaskRunner to the daemon.TaskRunner interface,
// bridging task ID lookups with the full pipeline execution.
type DAGTaskAdapter struct {
	runner   *PipelineTaskRunner
	db       TaskRunnerDB
	ticketID string
}

// NewDAGTaskAdapter creates an adapter that connects PipelineTaskRunner to DAGExecutor.
func NewDAGTaskAdapter(runner *PipelineTaskRunner, db TaskRunnerDB, ticketID string) *DAGTaskAdapter {
	return &DAGTaskAdapter{runner: runner, db: db, ticketID: ticketID}
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
			// BUG-M01: Escalation is not a task failure — it's a signal to pause the ticket.
			// Return TaskStatusEscalated so the DB status set in RunTask is consistent
			// with the result reported to the orchestrator. Dependents are still skipped
			// because the DAG executor treats any non-Done status as a failure for propagation.
			return daemon.TaskResult{
				TaskID: taskID,
				Status: models.TaskStatusEscalated,
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
	tasks, err := a.db.ListTasks(ctx, a.ticketID)
	if err != nil {
		return nil, fmt.Errorf("listing tasks for ticket %s: %w", a.ticketID, err)
	}
	for i := range tasks {
		if tasks[i].ID == taskID {
			return &tasks[i], nil
		}
	}
	return nil, fmt.Errorf("task %s not found in ticket %s", taskID, a.ticketID)
}

// Compile-time check.
var _ daemon.TaskRunner = (*DAGTaskAdapter)(nil)
