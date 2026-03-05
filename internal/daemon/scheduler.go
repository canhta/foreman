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
// files are held by another ticket.
func (s *Scheduler) TryReserve(ctx context.Context, ticketID string, files []string) error {
	reserved, err := s.db.GetReservedFiles(ctx)
	if err != nil {
		return fmt.Errorf("getting reserved files: %w", err)
	}

	var conflicts []string
	for _, f := range files {
		if owner, ok := reserved[f]; ok && owner != ticketID {
			conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", f, owner))
		}
	}

	if len(conflicts) > 0 {
		return &FileConflictError{Conflicts: conflicts}
	}

	return s.db.ReserveFiles(ctx, ticketID, files)
}

// Release removes all file reservations for a ticket.
func (s *Scheduler) Release(ctx context.Context, ticketID string) error {
	if err := s.db.ReleaseFiles(ctx, ticketID); err != nil {
		return fmt.Errorf("releasing files for ticket %s: %w", ticketID, err)
	}
	return nil
}
