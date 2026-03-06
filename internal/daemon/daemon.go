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
	RunnerMode             string
	PollIntervalSecs       int
	IdlePollIntervalSecs   int
	MaxParallelTickets     int
	MaxParallelTasks       int
	TaskTimeoutMinutes     int
	MergeCheckIntervalSecs int
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
	startedAt  time.Time
	db         db.Database
	prChecker  git.PRChecker
	hookRunner *skills.HookRunner
	tracker    tracker.IssueTracker
	config     DaemonConfig
	mu         sync.Mutex
	running    atomic.Bool
	paused     atomic.Bool
	active     atomic.Int32
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
			// Poll cycle — to be wired to tracker + pipeline in integration
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
