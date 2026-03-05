package pipeline

import (
	"context"
	"fmt"
)

// CallCapDB is the subset of db.Database needed for call cap checks.
type CallCapDB interface {
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)
}

// CallCapExceededError is returned when a task exceeds its LLM call limit.
type CallCapExceededError struct {
	TaskID  string
	Current int
	Max     int
}

func (e *CallCapExceededError) Error() string {
	return fmt.Sprintf("task %s exceeded LLM call cap: %d/%d", e.TaskID, e.Current, e.Max)
}

// CheckTaskCallCap increments the call counter and returns an error if the cap is exceeded.
// Call this BEFORE every LLM call for a task.
func CheckTaskCallCap(ctx context.Context, db CallCapDB, taskID string, maxCalls int) error {
	count, err := db.IncrementTaskLlmCalls(ctx, taskID)
	if err != nil {
		return fmt.Errorf("increment call count: %w", err)
	}
	if count > maxCalls {
		return &CallCapExceededError{TaskID: taskID, Current: count, Max: maxCalls}
	}
	return nil
}
