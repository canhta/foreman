package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
)

// MergeCheckerDB is the subset of db.Database needed by MergeChecker.
type MergeCheckerDB interface {
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	UpdateTicketStatusIfEquals(ctx context.Context, ticketID string, newStatus models.TicketStatus, requiredCurrentStatus models.TicketStatus) (bool, error)
	SetTicketPRHeadSHA(ctx context.Context, ticketID, sha string) error
	RecordEvent(ctx context.Context, e *models.EventRecord) error
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
	GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error)
}

// MergeChecker polls open PRs and updates ticket lifecycle on merge/close.
type MergeChecker struct {
	db         MergeCheckerDB
	prChecker  git.PRChecker
	hookRunner *skills.HookRunner
	tracker    tracker.IssueTracker
	notify     func(ctx context.Context, ticket *models.Ticket, msg string)
	log        zerolog.Logger

	// Optional pipeline context accessors for skill hook injection (REQ-OBS-002).
	handoffDB    skills.HandoffAccessor
	progressDB   skills.ProgressAccessor
	eventEmitter skills.SkillEventEmitter
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

// SetSkillContextAccessors attaches optional database accessors and an event emitter
// that are forwarded into SkillContext when triggering hook runners (REQ-OBS-002).
// All three arguments are optional; pass nil to leave any accessor unset.
func (m *MergeChecker) SetSkillContextAccessors(handoffDB skills.HandoffAccessor, progressDB skills.ProgressAccessor, emitter skills.SkillEventEmitter) {
	m.handoffDB = handoffDB
	m.progressDB = progressDB
	m.eventEmitter = emitter
}

// SetNotify attaches a notification callback invoked when the pr_updated status is set.
// The callback receives the affected ticket and a human-readable message.
// SetNotify must be called before Start; it is not safe to call concurrently with the health loop.
func (m *MergeChecker) SetNotify(fn func(ctx context.Context, ticket *models.Ticket, msg string)) {
	m.notify = fn
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
		case git.PRStateOpen:
			m.handleOpen(ctx, ticket, status.HeadSHA)
		}
	}
}

func (m *MergeChecker) handleMerged(ctx context.Context, ticket *models.Ticket) {
	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusMerged); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to merged")
		return
	}
	ticket.Status = models.TicketStatusMerged

	// Fire post_merge hooks, injecting pipeline context so agentsdk steps can
	// read/write handoffs and emit structured events (REQ-OBS-002).
	if m.hookRunner != nil {
		// Extract the trace ID from the context if one was set upstream.
		tc := telemetry.TraceFromContext(ctx)
		sCtx := &skills.SkillContext{
			Ticket: ticket,
			PipelineCtx: &telemetry.PipelineContext{
				TraceID:  tc.TraceID,
				TicketID: ticket.ID,
				Stage:    "post_merge",
			},
			HandoffDB:    m.handoffDB,
			ProgressDB:   m.progressDB,
			EventEmitter: m.eventEmitter,
		}
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

// handleOpen compares the stored PR HEAD SHA to the current one.
// If they differ and a stored SHA exists, the ticket is marked pr_updated.
// If no SHA is stored yet (first poll), the SHA is initialized without changing status.
func (m *MergeChecker) handleOpen(ctx context.Context, ticket *models.Ticket, currentHeadSHA string) {
	if currentHeadSHA == "" {
		return
	}

	storedSHA := ticket.PRHeadSHA

	if storedSHA == "" {
		// First time we see this ticket's HEAD SHA — store it as baseline.
		if err := m.db.SetTicketPRHeadSHA(ctx, ticket.ID, currentHeadSHA); err != nil {
			m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to store initial pr_head_sha")
		}
		return
	}

	if storedSHA == currentHeadSHA {
		// No external push detected.
		return
	}

	// HEAD SHA changed — external push detected.
	m.log.Info().
		Str("ticket", ticket.ID).
		Str("old_sha", storedSHA).
		Str("new_sha", currentHeadSHA).
		Msg("PR branch updated externally; marking ticket as pr_updated")

	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPRUpdated); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to pr_updated")
		return
	}

	if err := m.db.SetTicketPRHeadSHA(ctx, ticket.ID, currentHeadSHA); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update pr_head_sha after pr_updated")
	}

	if err := m.db.RecordEvent(ctx, &models.EventRecord{
		ID:        fmt.Sprintf("evt-%s-pr_updated-%d", ticket.ID, time.Now().UnixNano()),
		TicketID:  ticket.ID,
		EventType: "pr_updated",
		Message:   fmt.Sprintf("PR branch updated: %s -> %s", storedSHA, currentHeadSHA),
		Details:   fmt.Sprintf("old_sha=%s new_sha=%s", storedSHA, currentHeadSHA),
		CreatedAt: time.Now(),
	}); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to record pr_updated event")
	}

	if m.notify != nil {
		msg := fmt.Sprintf(
			"PR for ticket %s was updated externally (new SHA: %s). Manual re-labeling required to re-enter pipeline.",
			ticket.ExternalID, currentHeadSHA,
		)
		m.notify(ctx, ticket, msg)
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

	// All children merged — attempt to close parent using a conditional update
	// to prevent concurrent goroutines from firing side effects twice (ARCH-F06).
	updated, err := m.db.UpdateTicketStatusIfEquals(ctx, parent.ID, models.TicketStatusDone, models.TicketStatusDecomposed)
	if err != nil {
		m.log.Error().Err(err).Str("parent", parent.ID).Msg("failed to close parent ticket")
		return
	}
	if !updated {
		// Another goroutine already closed the parent; skip side effects.
		return
	}

	if m.tracker != nil {
		if err := m.tracker.UpdateStatus(ctx, parent.ExternalID, "done"); err != nil {
			m.log.Warn().Err(err).Str("parent", parent.ExternalID).Msg("failed to update tracker status for parent ticket")
		}
	}
}
