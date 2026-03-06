package daemon

import (
	"context"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
)

// MergeCheckerDB is the subset of db.Database needed by MergeChecker.
type MergeCheckerDB interface {
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
	GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error)
}

// MergeChecker polls open PRs and updates ticket lifecycle on merge/close.
type MergeChecker struct {
	db         MergeCheckerDB
	prChecker  git.PRChecker
	hookRunner *skills.HookRunner
	tracker    tracker.IssueTracker
	log        zerolog.Logger
}

// NewMergeChecker creates a new MergeChecker.
func NewMergeChecker(db MergeCheckerDB, prChecker git.PRChecker, hookRunner *skills.HookRunner, tr tracker.IssueTracker, log zerolog.Logger) *MergeChecker {
	return &MergeChecker{
		db:         db,
		prChecker:  prChecker,
		hookRunner: hookRunner,
		tracker:    tr,
		log:        log,
	}
}

// Start begins the merge check poll loop. Blocks until ctx is cancelled.
func (m *MergeChecker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

func (m *MergeChecker) checkAll(ctx context.Context) {
	tickets, err := m.db.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusAwaitingMerge},
	})
	if err != nil {
		m.log.Warn().Err(err).Msg("failed to list awaiting_merge tickets")
		return
	}

	for i := range tickets {
		ticket := &tickets[i]
		if ticket.PRNumber == 0 {
			continue
		}

		status, err := m.prChecker.GetPRStatus(ctx, ticket.PRNumber)
		if err != nil {
			m.log.Warn().Err(err).Str("ticket", ticket.ID).Int("pr", ticket.PRNumber).Msg("failed to check PR status")
			continue
		}

		switch status.State {
		case git.PRStateMerged:
			m.handleMerged(ctx, ticket)
		case git.PRStateClosed:
			m.handleClosed(ctx, ticket)
		}
	}
}

func (m *MergeChecker) handleMerged(ctx context.Context, ticket *models.Ticket) {
	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusMerged); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to merged")
		return
	}
	ticket.Status = models.TicketStatusMerged

	// Fire post_merge hooks
	if m.hookRunner != nil {
		sCtx := &skills.SkillContext{Ticket: ticket}
		for _, hr := range m.hookRunner.RunHook(ctx, "post_merge", sCtx) {
			if hr.Error != nil {
				m.log.Warn().Err(hr.Error).Str("ticket", ticket.ID).Str("skill", hr.SkillID).Msg("post_merge hook failed")
			}
		}
	}

	// Check parent completion if this is a child ticket
	if ticket.ParentTicketID != "" {
		m.checkParentCompletion(ctx, ticket.ParentTicketID)
	}
}

func (m *MergeChecker) handleClosed(ctx context.Context, ticket *models.Ticket) {
	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPRClosed); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to pr_closed")
	}
}

func (m *MergeChecker) checkParentCompletion(ctx context.Context, parentExternalID string) {
	parent, err := m.db.GetTicketByExternalID(ctx, parentExternalID)
	if err != nil || parent == nil {
		return
	}
	if parent.Status != models.TicketStatusDecomposed {
		return
	}

	children, err := m.db.GetChildTickets(ctx, parentExternalID)
	if err != nil {
		m.log.Warn().Err(err).Str("parent", parentExternalID).Msg("failed to get child tickets")
		return
	}

	for _, child := range children {
		if child.Status != models.TicketStatusMerged {
			return
		}
	}

	// All children merged — close parent
	if err := m.db.UpdateTicketStatus(ctx, parent.ID, models.TicketStatusDone); err != nil {
		m.log.Error().Err(err).Str("parent", parent.ID).Msg("failed to close parent ticket")
		return
	}

	if m.tracker != nil {
		if err := m.tracker.UpdateStatus(ctx, parent.ExternalID, "done"); err != nil {
			m.log.Warn().Err(err).Str("parent", parent.ExternalID).Msg("failed to update tracker status for parent ticket")
		}
	}
}
