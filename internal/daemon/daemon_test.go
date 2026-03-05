package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonConfig_Defaults(t *testing.T) {
	cfg := DefaultDaemonConfig()
	assert.Equal(t, 60, cfg.PollIntervalSecs)
	assert.Equal(t, 300, cfg.IdlePollIntervalSecs)
	assert.Equal(t, 3, cfg.MaxParallelTickets)
}

func TestDaemon_NewDaemon(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)
	require.NotNil(t, d)
	assert.False(t, d.IsRunning())
}

func TestDaemon_StartStop(t *testing.T) {
	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 1 // Fast for tests
	d := NewDaemon(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	go d.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	assert.True(t, d.IsRunning())

	cancel()
	time.Sleep(100 * time.Millisecond)
	assert.False(t, d.IsRunning())
}

func TestDaemon_Pause_Resume(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)

	assert.False(t, d.IsPaused())
	d.Pause()
	assert.True(t, d.IsPaused())
	d.Resume()
	assert.False(t, d.IsPaused())
}

func TestDaemon_Status(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)

	status := d.Status()
	assert.Equal(t, "stopped", status.State)
	assert.Equal(t, 0, status.ActivePipelines)
}

func TestDaemon_Status_Running(t *testing.T) {
	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 1
	d := NewDaemon(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	status := d.Status()
	assert.Equal(t, "running", status.State)
	assert.Greater(t, status.Uptime, time.Duration(0))
}

func TestDaemon_Status_Paused(t *testing.T) {
	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 1
	d := NewDaemon(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	d.Pause()

	status := d.Status()
	assert.Equal(t, "paused", status.State)
}
