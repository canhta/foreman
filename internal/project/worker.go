package project

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

// WorkerStatus represents the state of a ProjectWorker.
type WorkerStatus string

const (
	StatusStopped WorkerStatus = "stopped"
	StatusRunning WorkerStatus = "running"
	StatusPaused  WorkerStatus = "paused"
	StatusError   WorkerStatus = "error"
)

// Worker runs an independent goroutine group for a single project.
// It owns its own database, orchestrator, tracker, and git provider.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type Worker struct {
	ID         string
	Name       string
	Dir        string         // project directory path
	Config     *models.Config // merged config (global + project)
	ProjConfig *ProjectConfig // raw project config
	Database   db.Database

	cancel context.CancelFunc
	status WorkerStatus
	paused atomic.Bool
	mu     sync.RWMutex
	log    zerolog.Logger
	err    error
}

// Status returns the current worker status.
func (w *Worker) Status() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) setStatus(s WorkerStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = s
}

// Error returns the last error if status is StatusError.
func (w *Worker) Error() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.err
}

// SetError records an error and transitions the worker to StatusError.
func (w *Worker) SetError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.err = err
	w.status = StatusError
}

// Start begins the worker's event loop. Blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	w.setStatus(StatusRunning)
	defer w.setStatus(StatusStopped)

	w.log.Info().Str("project", w.Name).Msg("project worker started")

	<-ctx.Done()

	w.log.Info().Str("project", w.Name).Msg("project worker stopped")
}

// Pause pauses the worker's polling loop.
func (w *Worker) Pause() {
	w.paused.Store(true)
	w.setStatus(StatusPaused)
}

// Resume resumes the worker's polling loop.
func (w *Worker) Resume() {
	w.paused.Store(false)
	w.setStatus(StatusRunning)
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}
