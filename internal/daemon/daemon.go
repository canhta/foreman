package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// DaemonConfig holds daemon configuration.
type DaemonConfig struct {
	PollIntervalSecs     int
	IdlePollIntervalSecs int
	MaxParallelTickets   int
}

// DefaultDaemonConfig returns sensible defaults.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollIntervalSecs:     60,
		IdlePollIntervalSecs: 300,
		MaxParallelTickets:   3,
	}
}

// DaemonStatus holds the current state of the daemon.
type DaemonStatus struct {
	State           string // "running", "paused", "stopped"
	ActivePipelines int
	StartedAt       time.Time
	Uptime          time.Duration
}

// Daemon is the main 24/7 event loop.
type Daemon struct {
	config    DaemonConfig
	running   atomic.Bool
	paused    atomic.Bool
	startedAt time.Time
	active    atomic.Int32
	mu        sync.Mutex
}

// NewDaemon creates a new daemon.
func NewDaemon(config DaemonConfig) *Daemon {
	return &Daemon{config: config}
}

// Start begins the daemon's poll loop. Blocks until ctx is cancelled.
func (d *Daemon) Start(ctx context.Context) {
	d.running.Store(true)
	d.mu.Lock()
	d.startedAt = time.Now()
	d.mu.Unlock()
	defer d.running.Store(false)

	// On startup, clean up orphaned Docker containers from previous crashes.
	// TODO: wire db and runner from full config during integration.
	// When runner.Mode == "docker", fetch active tickets from db and call
	// dockerRunner.CleanupOrphanContainers(ctx, activeIDs). Stub logged here
	// so the intent is visible before full wiring.
	log.Debug().Msg("daemon starting — Docker orphan cleanup requires integration wiring")

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
