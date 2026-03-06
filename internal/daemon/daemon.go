package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/tracker"
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
}

// DefaultDaemonConfig returns sensible defaults.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollIntervalSecs:       60,
		IdlePollIntervalSecs:   300,
		MaxParallelTickets:     3,
		MaxParallelTasks:       3,
		TaskTimeoutMinutes:     15,
		MergeCheckIntervalSecs: 300,
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
	startedAt    time.Time
	db           db.Database
	prChecker    git.PRChecker
	hookRunner   *skills.HookRunner
	tracker      tracker.IssueTracker
	orchestrator TicketProcessor
	config       DaemonConfig
	mu           sync.Mutex
	wg           sync.WaitGroup
	running      atomic.Bool
	paused       atomic.Bool
	active       atomic.Int32
}

// NewDaemon creates a new daemon.
func NewDaemon(config DaemonConfig) *Daemon {
	return &Daemon{config: config}
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

// Start begins the daemon's poll loop. Blocks until ctx is cancelled.
func (d *Daemon) Start(ctx context.Context) {
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
			dockerRunner := runner.NewDockerRunner("", false, "", "", "", false)
			if err := dockerRunner.CleanupOrphanContainers(ctx, activeIDs); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup orphan containers on startup")
			}
		}
	}

	// Start merge checker goroutine
	if prChecker != nil && database != nil {
		mc := NewMergeChecker(database, prChecker, hookRunner, tr, log.Logger)
		interval := time.Duration(d.config.MergeCheckIntervalSecs) * time.Second
		go mc.Start(ctx, interval)
	}

	pollInterval := time.Duration(d.config.PollIntervalSecs) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
		ActivePipelines: int(d.active.Load()),
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
		if int(d.active.Load()) >= d.config.MaxParallelTickets {
			break
		}
		d.active.Add(1)
		d.wg.Add(1)
		go func(t models.Ticket) {
			defer d.wg.Done()
			defer d.active.Add(-1)
			if err := d.orchestrator.ProcessTicket(ctx, t); err != nil {
				log.Error().Err(err).Str("ticket_id", t.ID).Msg("ticket processing failed")
			}
		}(ticket)
	}
}
