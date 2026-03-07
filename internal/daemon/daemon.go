package daemon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// DaemonConfig holds daemon configuration.
type DaemonConfig struct {
	RunnerMode                string
	ClarificationLabel        string
	PollIntervalSecs          int
	IdlePollIntervalSecs      int
	MaxParallelTickets        int
	MaxParallelTasks          int
	TaskTimeoutMinutes        int
	MergeCheckIntervalSecs    int
	ClarificationTimeoutHours int
	LockTTLSeconds            int
}

// defaultLockTTLSeconds is the fallback lock TTL when not set via config.
const defaultLockTTLSeconds = 3600

// DefaultDaemonConfig returns sensible defaults.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollIntervalSecs:       60,
		IdlePollIntervalSecs:   300,
		MaxParallelTickets:     3,
		MaxParallelTasks:       3,
		TaskTimeoutMinutes:     15,
		MergeCheckIntervalSecs: 300,
		LockTTLSeconds:         defaultLockTTLSeconds,
	}
}

// DaemonStatus holds the current state of the daemon.
type DaemonStatus struct {
	StartedAt       time.Time
	State           string
	ActivePipelines int
	Uptime          time.Duration
}

// Daemon is the main 24/7 event loop.
type Daemon struct {
	db            db.Database
	prChecker     git.PRChecker
	tracker       tracker.IssueTracker
	orchestrator  TicketProcessor
	channel       channel.Channel
	channelRouter channel.InboundHandler
	hookRunner    *skills.HookRunner
	scheduler     *Scheduler
	tickets       chan struct{}
	startedAt     time.Time
	config        DaemonConfig
	wg            sync.WaitGroup
	mu            sync.Mutex
	running       atomic.Bool
	paused        atomic.Bool
}

// NewDaemon creates a new daemon.
func NewDaemon(config DaemonConfig) *Daemon {
	maxTickets := config.MaxParallelTickets
	if maxTickets <= 0 {
		maxTickets = 1
	}
	return &Daemon{
		config:  config,
		tickets: make(chan struct{}, maxTickets),
	}
}

// SetDB attaches a database instance to the daemon for startup cleanup tasks.
func (d *Daemon) SetDB(database db.Database) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.db = database
}

// SetPRChecker attaches a PR checker for merge monitoring.
func (d *Daemon) SetPRChecker(checker git.PRChecker) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prChecker = checker
}

// SetScheduler attaches a scheduler for file reservation orphan cleanup.
func (d *Daemon) SetScheduler(s *Scheduler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scheduler = s
}

// SetHookRunner attaches a hook runner for post_merge hooks.
func (d *Daemon) SetHookRunner(runner *skills.HookRunner) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hookRunner = runner
}

// SetTracker attaches a tracker for parent ticket completion.
func (d *Daemon) SetTracker(tr tracker.IssueTracker) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tracker = tr
}

// SetOrchestrator attaches a ticket processor for the poll loop.
func (d *Daemon) SetOrchestrator(tp TicketProcessor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.orchestrator = tp
}

// SetChannel attaches a messaging channel to the daemon.
func (d *Daemon) SetChannel(ch channel.Channel) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channel = ch
}

// SetChannelRouter wires the channel router after construction.
func (d *Daemon) SetChannelRouter(router channel.InboundHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channelRouter = router
}

// WaitForDrain blocks until all active pipelines finish or ctx expires.
func (d *Daemon) WaitForDrain(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Info().Msg("all active pipelines drained")
	case <-ctx.Done():
		log.Warn().Msg("drain timeout reached, forcing shutdown")
	}
}

// Validate returns an error if required daemon dependencies are not set.
// Call before Start to surface misconfigurations at startup (BUG-M13).
func (d *Daemon) Validate() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db == nil {
		return fmt.Errorf("daemon: db is required")
	}
	if d.orchestrator == nil {
		return fmt.Errorf("daemon: orchestrator is required")
	}
	return nil
}

// Start begins the daemon's poll loop. Blocks until ctx is cancelled.
func (d *Daemon) Start(ctx context.Context) {
	d.mu.Lock()
	if d.db == nil {
		log.Warn().Msg("daemon started without database: tickets will not be processed")
	}
	if d.orchestrator == nil {
		log.Warn().Msg("daemon started without orchestrator: queued tickets will not be processed")
	}
	d.mu.Unlock()

	d.running.Store(true)
	d.mu.Lock()
	d.startedAt = time.Now()
	d.mu.Unlock()
	defer d.running.Store(false)

	// On startup, clean up orphaned Docker containers from previous crashes.
	d.mu.Lock()
	database := d.db
	runnerMode := d.config.RunnerMode
	prChecker := d.prChecker
	hookRunner := d.hookRunner
	tr := d.tracker
	d.mu.Unlock()

	if database != nil && runnerMode == "docker" {
		activeTickets, err := database.ListTickets(ctx, models.TicketFilter{
			StatusIn: []models.TicketStatus{
				models.TicketStatusPlanning,
				models.TicketStatusPlanValidating,
				models.TicketStatusImplementing,
				models.TicketStatusReviewing,
			},
		})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to list active tickets for Docker orphan cleanup")
		} else {
			activeIDs := make(map[string]bool, len(activeTickets))
			for _, t := range activeTickets {
				activeIDs[t.ID] = true
			}
			dockerRunner := runner.NewDockerRunner("", false, "", "", "", false, false)
			if err := dockerRunner.CleanupOrphanContainers(ctx, activeIDs); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup orphan containers on startup")
			}
		}
	}

	// Start merge checker goroutine
	if prChecker != nil && database != nil {
		mc := NewMergeChecker(database, prChecker, hookRunner, tr, log.Logger)
		interval := time.Duration(d.config.MergeCheckIntervalSecs) * time.Second
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			mc.Start(ctx, interval)
		}()
	}

	// Start channel listener
	if d.channel != nil && d.channelRouter != nil {
		ch := d.channel
		router := d.channelRouter
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			if err := ch.Start(ctx, router); err != nil {
				log.Error().Err(err).Msg("channel stopped with error")
			}
		}()
	}

	pollInterval := time.Duration(d.config.PollIntervalSecs) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if d.channel != nil {
				if err := d.channel.Stop(); err != nil {
					log.Error().Err(err).Msg("channel stop error")
				}
			}
			return
		case <-ticker.C:
			if d.paused.Load() {
				continue
			}

			// Check clarification timeouts
			if database != nil && tr != nil {
				checkClarificationTimeouts(ctx, log.Logger, database, tr,
					d.config.ClarificationTimeoutHours, d.config.ClarificationLabel)
			}

			// Clean up expired pairings
			if database != nil {
				if err := database.DeleteExpiredPairings(ctx); err != nil {
					log.Error().Err(err).Msg("failed to delete expired pairings")
				}
			}

			// Clean up orphan file reservations for tickets in terminal states (ARCH-F04).
			if database != nil && d.scheduler != nil {
				if released, cleanErr := d.scheduler.CleanupOrphanReservations(ctx, database); cleanErr != nil {
					log.Warn().Err(cleanErr).Msg("orphan reservation cleanup failed")
				} else if released > 0 {
					log.Info().Int("released", released).Msg("orphan file reservations released")
				}
			}

			// Fetch ready tickets from tracker and insert new ones as queued
			if tr != nil && database != nil {
				d.ingestFromTracker(ctx, database, tr)
			}

			// Process queued tickets from DB
			if database != nil && d.orchestrator != nil {
				d.processQueuedTickets(ctx, database)
			}
		}
	}
}

// IsRunning returns whether the daemon is currently running.
func (d *Daemon) IsRunning() bool {
	return d.running.Load()
}

// IsPaused returns whether the daemon is paused.
func (d *Daemon) IsPaused() bool {
	return d.paused.Load()
}

// Pause pauses the daemon's polling.
func (d *Daemon) Pause() {
	d.paused.Store(true)
}

// Resume resumes the daemon's polling.
func (d *Daemon) Resume() {
	d.paused.Store(false)
}

// Status returns the current daemon status.
func (d *Daemon) Status() DaemonStatus {
	d.mu.Lock()
	startedAt := d.startedAt
	d.mu.Unlock()

	isRunning := d.running.Load()
	state := "stopped"
	var uptime time.Duration
	if isRunning {
		uptime = time.Since(startedAt)
		if d.paused.Load() {
			state = "paused"
		} else {
			state = "running"
		}
	}

	return DaemonStatus{
		State:           state,
		ActivePipelines: len(d.tickets),
		StartedAt:       startedAt,
		Uptime:          uptime,
	}
}

// ingestFromTracker fetches ready tickets from the tracker and inserts new ones into the DB.
func (d *Daemon) ingestFromTracker(ctx context.Context, database db.Database, tr tracker.IssueTracker) {
	tickets, err := tr.FetchReadyTickets(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to fetch ready tickets from tracker")
		return
	}
	for _, t := range tickets {
		existing, _ := database.GetTicketByExternalID(ctx, t.ExternalID)
		if existing != nil {
			continue
		}
		dbTicket := &models.Ticket{
			ID:                 uuid.New().String(),
			ExternalID:         t.ExternalID,
			Title:              t.Title,
			Description:        t.Description,
			AcceptanceCriteria: t.AcceptanceCriteria,
			Priority:           t.Priority,
			Assignee:           t.Assignee,
			Reporter:           t.Reporter,
			Labels:             t.Labels,
			Status:             models.TicketStatusQueued,
		}
		if err := database.CreateTicket(ctx, dbTicket); err != nil {
			log.Warn().Err(err).Str("external_id", t.ExternalID).Msg("failed to insert ticket")
		}
	}
}

// processQueuedTickets picks up queued tickets from the DB and launches goroutines to process them.
func (d *Daemon) processQueuedTickets(ctx context.Context, database db.Database) {
	queued, err := database.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusQueued},
	})
	if err != nil {
		log.Warn().Err(err).Msg("failed to list queued tickets")
		return
	}
	for _, ticket := range queued {
		select {
		case d.tickets <- struct{}{}: // Acquire slot (non-blocking)
		default:
			return // All slots full
		}

		// Acquire a distributed lock for this ticket to prevent concurrent pickup
		// across multiple Foreman instances (BUG-M15).
		lockName := fmt.Sprintf("ticket:%s", ticket.ID)
		lockTTL := d.config.LockTTLSeconds
		if lockTTL <= 0 {
			lockTTL = defaultLockTTLSeconds
		}
		acquired, err := database.AcquireLock(ctx, lockName, lockTTL)
		if err != nil {
			<-d.tickets // Release slot on failure
			log.Warn().Err(err).Str("ticket_id", ticket.ID).Msg("failed to acquire distributed lock for ticket")
			continue
		}
		if !acquired {
			<-d.tickets // Release slot — another instance picked it up
			continue
		}

		// Mark planning BEFORE launching goroutine to prevent double pickup
		if err := database.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPlanning); err != nil {
			<-d.tickets // Release on failure
			if relErr := database.ReleaseLock(ctx, lockName); relErr != nil {
				log.Warn().Err(relErr).Str("lock_name", lockName).Msg("failed to release distributed lock")
			}
			log.Warn().Err(err).Str("ticket_id", ticket.ID).Msg("failed to mark ticket planning")
			continue
		}

		d.wg.Add(1)
		go func(t models.Ticket, lk string) {
			defer d.wg.Done()
			defer func() { <-d.tickets }()
			defer func() {
				if err := database.ReleaseLock(ctx, lk); err != nil {
					log.Warn().Err(err).Str("lock_name", lk).Msg("failed to release distributed lock")
				}
			}()
			if err := d.orchestrator.ProcessTicket(ctx, t); err != nil {
				log.Error().Err(err).Str("ticket_id", t.ID).Msg("ticket processing failed")
			}
		}(ticket, lockName)
	}
}

// --- CommandHandler implementation (for channel commands) ---

// DaemonCommandHandler wraps Daemon to implement channel.CommandHandler.
type DaemonCommandHandler struct {
	d *Daemon
}

// NewDaemonCommandHandler creates a CommandHandler adapter for the daemon.
func NewDaemonCommandHandler(d *Daemon) *DaemonCommandHandler {
	return &DaemonCommandHandler{d: d}
}

// Compile-time check.
var _ channel.CommandHandler = (*DaemonCommandHandler)(nil)

// Status returns a summary of active tickets.
func (h *DaemonCommandHandler) Status(ctx context.Context) (string, error) {
	h.d.mu.Lock()
	database := h.d.db
	h.d.mu.Unlock()
	if database == nil {
		return "Daemon has no database configured.", nil
	}

	tickets, err := database.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{
			models.TicketStatusPlanning,
			models.TicketStatusImplementing,
			models.TicketStatusReviewing,
		},
	})
	if err != nil {
		return "", err
	}
	if len(tickets) == 0 {
		return "No active tickets.", nil
	}
	result := fmt.Sprintf("%d active ticket(s):\n", len(tickets))
	for _, t := range tickets {
		result += fmt.Sprintf("  #%s %s (%s)\n", t.ID, t.Title, t.Status)
	}
	return result, nil
}

// Pause pauses the daemon.
func (h *DaemonCommandHandler) Pause(_ context.Context) (string, error) {
	h.d.paused.Store(true)
	return "Daemon paused. No new tickets will be picked up.", nil
}

// Resume resumes the daemon.
func (h *DaemonCommandHandler) Resume(_ context.Context) (string, error) {
	h.d.paused.Store(false)
	return "Daemon resumed. Picking up tickets again.", nil
}

// Cost returns daily and monthly cost.
func (h *DaemonCommandHandler) Cost(ctx context.Context) (string, error) {
	h.d.mu.Lock()
	database := h.d.db
	h.d.mu.Unlock()
	if database == nil {
		return "No database configured.", nil
	}

	now := time.Now()
	daily, err := database.GetDailyCost(ctx, now.Format("2006-01-02"))
	if err != nil {
		return "", err
	}
	monthly, err := database.GetMonthlyCost(ctx, now.Format("2006-01"))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Today: $%.2f\nThis month: $%.2f", daily, monthly), nil
}
