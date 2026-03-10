package project

import (
	"context"
	"testing"
	"time"
)

func TestProjectWorker_StartStop(t *testing.T) {
	w := &Worker{
		ID:     "test-worker",
		Name:   "TestProject",
		status: StatusStopped,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start sets status to running
	go w.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	if w.Status() != StatusRunning {
		t.Errorf("status = %q, want %q", w.Status(), StatusRunning)
	}

	// Cancel stops the worker
	cancel()
	time.Sleep(50 * time.Millisecond)

	if w.Status() != StatusStopped {
		t.Errorf("status after cancel = %q, want %q", w.Status(), StatusStopped)
	}
}
