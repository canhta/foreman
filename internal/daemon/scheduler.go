package daemon

import (
	"context"
	"fmt"
	"strings"
)

// FileReserver is the database subset needed by the scheduler.
type FileReserver interface {
	GetReservedFiles(ctx context.Context) (map[string]string, error)
	ReserveFiles(ctx context.Context, ticketID string, paths []string) error
	ReleaseFiles(ctx context.Context, ticketID string) error
	// TryReserveFiles atomically checks for conflicts and reserves files in a
	// single transaction. It returns a list of conflicting paths (each formatted
	// as "path (held by owner)") and a nil error when conflicts are found, or
	// an empty slice and nil error on success. A non-nil error indicates a DB
	// failure.
	TryReserveFiles(ctx context.Context, ticketID string, paths []string) (conflicts []string, err error)
}

// FileConflictError indicates file reservation conflicts.
type FileConflictError struct {
	Conflicts []string
}

func (e *FileConflictError) Error() string {
	return fmt.Sprintf("file reservation conflict: %s", strings.Join(e.Conflicts, ", "))
}

// Scheduler manages file reservations for parallel ticket processing.
type Scheduler struct {
	db FileReserver
}

// NewScheduler creates a scheduler.
func NewScheduler(db FileReserver) *Scheduler {
	return &Scheduler{db: db}
}

// TryReserve attempts to reserve files for a ticket. Returns FileConflictError if any
// files are held by another ticket. The check-and-reserve is performed atomically
// inside a DB transaction to prevent race conditions.
func (s *Scheduler) TryReserve(ctx context.Context, ticketID string, files []string) error {
	conflicts, err := s.db.TryReserveFiles(ctx, ticketID, files)
	if err != nil {
		return fmt.Errorf("reserving files: %w", err)
	}
	if len(conflicts) > 0 {
		return &FileConflictError{Conflicts: conflicts}
	}
	return nil
}

// Release removes all file reservations for a ticket.
func (s *Scheduler) Release(ctx context.Context, ticketID string) error {
	if err := s.db.ReleaseFiles(ctx, ticketID); err != nil {
		return fmt.Errorf("releasing files for ticket %s: %w", ticketID, err)
	}
	return nil
}
