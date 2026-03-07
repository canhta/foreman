package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/models"
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

// TicketStateChecker is the minimal DB interface needed for orphan cleanup.
type TicketStateChecker interface {
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
}

// terminalTicketStatuses are statuses at which file reservations should be released.
var terminalTicketStatuses = map[models.TicketStatus]bool{
	models.TicketStatusDone:          true,
	models.TicketStatusFailed:        true,
	models.TicketStatusMerged:        true,
	models.TicketStatusPRClosed:      true,
	models.TicketStatusAwaitingMerge: true,
	models.TicketStatusPartial:       true,
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

// CleanupOrphanReservations releases file reservations held by tickets that are
// in terminal states (merged, failed, done, pr_closed, etc.) (ARCH-F04).
// It is safe to call periodically; releasing an already-released reservation is a no-op.
func (s *Scheduler) CleanupOrphanReservations(ctx context.Context, tc TicketStateChecker) (int, error) {
	reserved, err := s.db.GetReservedFiles(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting reserved files: %w", err)
	}

	// Collect unique ticket IDs from the reserved map (value = ticketID).
	ticketIDs := make(map[string]bool, len(reserved))
	for _, tid := range reserved {
		ticketIDs[tid] = true
	}

	released := 0
	for tid := range ticketIDs {
		ticket, err := tc.GetTicket(ctx, tid)
		if err != nil || ticket == nil {
			// Unknown ticket — release to avoid indefinite blocking.
			if releaseErr := s.db.ReleaseFiles(ctx, tid); releaseErr == nil {
				released++
			}
			continue
		}
		if terminalTicketStatuses[ticket.Status] {
			if releaseErr := s.db.ReleaseFiles(ctx, tid); releaseErr == nil {
				released++
			}
		}
	}
	return released, nil
}
