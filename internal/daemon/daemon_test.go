package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
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

// ---------------------------------------------------------------------------
// Mocks for daemon poll-loop tests
// ---------------------------------------------------------------------------

// daemonMockDB is a simple mock db for daemon-level tests.
type daemonMockDB struct {
	orchMockDB // embed the full DB mock from orchestrator_test.go

	mu            sync.Mutex
	queuedTickets []models.Ticket // returned by ListTickets when filtering queued
}

func newDaemonMockDB() *daemonMockDB {
	return &daemonMockDB{
		orchMockDB: orchMockDB{
			reservedFiles:  make(map[string]string),
			ticketsByExtID: make(map[string]*models.Ticket),
		},
	}
}

func (m *daemonMockDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return queued tickets when asked for queued status.
	for _, s := range filter.StatusIn {
		if s == models.TicketStatusQueued {
			return m.queuedTickets, nil
		}
	}
	return nil, nil
}

func (m *daemonMockDB) GetTicketByExternalID(_ context.Context, extID string) (*models.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.ticketsByExtID[extID]; ok {
		return t, nil
	}
	return nil, nil
}

func (m *daemonMockDB) CreateTicket(_ context.Context, t *models.Ticket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdTickets = append(m.createdTickets, t)
	return nil
}

func (m *daemonMockDB) GetTeamStats(_ context.Context, _ time.Time) ([]models.TeamStat, error) {
	return nil, nil
}

func (m *daemonMockDB) GetRecentPRs(_ context.Context, _ int) ([]models.Ticket, error) {
	return nil, nil
}

func (m *daemonMockDB) GetTicketSummaries(_ context.Context, _ models.TicketFilter) ([]models.TicketSummary, error) {
	return nil, nil
}

func (m *daemonMockDB) GetGlobalEvents(_ context.Context, _, _ int) ([]models.EventRecord, error) {
	return nil, nil
}

// daemonMockTracker is a mock tracker for daemon-level ingestion tests.
type daemonMockTracker struct {
	orchMockTracker
	readyTickets []tracker.Ticket
}

func (m *daemonMockTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) {
	return m.readyTickets, nil
}

// daemonMockProcessor implements TicketProcessor for daemon tests.
type daemonMockProcessor struct {
	mu       sync.Mutex
	calls    []string // ticket IDs processed
	blocking chan struct{}
	started  atomic.Int32
}

func (m *daemonMockProcessor) ProcessTicket(_ context.Context, ticket models.Ticket) error {
	m.started.Add(1)
	m.mu.Lock()
	m.calls = append(m.calls, ticket.ID)
	m.mu.Unlock()
	if m.blocking != nil {
		<-m.blocking
	}
	return nil
}

// ---------------------------------------------------------------------------
// Daemon poll-loop tests
// ---------------------------------------------------------------------------

func TestDaemon_IngestFromTracker(t *testing.T) {
	mdb := newDaemonMockDB()
	mt := &daemonMockTracker{
		readyTickets: []tracker.Ticket{
			{ExternalID: "GH-100", Title: "First ticket"},
			{ExternalID: "GH-101", Title: "Second ticket"},
		},
	}

	d := NewDaemon(DefaultDaemonConfig())
	d.ingestFromTracker(context.Background(), mdb, mt)

	mdb.mu.Lock()
	defer mdb.mu.Unlock()

	require.Len(t, mdb.createdTickets, 2)
	assert.Equal(t, "GH-100", mdb.createdTickets[0].ExternalID)
	assert.Equal(t, "GH-101", mdb.createdTickets[1].ExternalID)
	assert.Equal(t, models.TicketStatusQueued, mdb.createdTickets[0].Status)
	assert.Equal(t, models.TicketStatusQueued, mdb.createdTickets[1].Status)
}

func TestDaemon_IngestDeduplicates(t *testing.T) {
	mdb := newDaemonMockDB()
	// Pre-populate an existing ticket in the DB
	mdb.ticketsByExtID["GH-200"] = &models.Ticket{
		ID:         "existing-1",
		ExternalID: "GH-200",
		Title:      "Already exists",
	}

	mt := &daemonMockTracker{
		readyTickets: []tracker.Ticket{
			{ExternalID: "GH-200", Title: "Already exists"},
		},
	}

	d := NewDaemon(DefaultDaemonConfig())
	d.ingestFromTracker(context.Background(), mdb, mt)

	mdb.mu.Lock()
	defer mdb.mu.Unlock()

	assert.Empty(t, mdb.createdTickets, "should not insert duplicate ticket")
}

func TestDaemon_ProcessQueuedTickets_RespectsMaxParallel(t *testing.T) {
	mdb := newDaemonMockDB()
	mdb.queuedTickets = []models.Ticket{
		{ID: "q-1", ExternalID: "GH-300", Title: "Ticket 1"},
		{ID: "q-2", ExternalID: "GH-301", Title: "Ticket 2"},
	}

	blocker := make(chan struct{})
	mp := &daemonMockProcessor{blocking: blocker}

	cfg := DefaultDaemonConfig()
	cfg.MaxParallelTickets = 1

	d := NewDaemon(cfg)
	d.SetDB(mdb)
	d.SetOrchestrator(mp)

	d.processQueuedTickets(context.Background(), mdb)

	// Wait for the first goroutine to start
	require.Eventually(t, func() bool {
		return mp.started.Load() >= 1
	}, time.Second, 10*time.Millisecond)

	// Only 1 should be active due to maxParallelTickets=1
	assert.Equal(t, int32(1), d.active.Load(), "only 1 ticket should be active")

	// Release the blocker
	close(blocker)

	// Wait for completion
	d.wg.Wait()

	mp.mu.Lock()
	defer mp.mu.Unlock()
	assert.Len(t, mp.calls, 1, "only 1 ticket should have been processed (maxParallel=1)")
}

func TestDaemon_SkipWhenPaused(t *testing.T) {
	mdb := newDaemonMockDB()
	mdb.queuedTickets = []models.Ticket{
		{ID: "q-p1", ExternalID: "GH-400", Title: "Should not run"},
	}

	mt := &daemonMockTracker{
		readyTickets: []tracker.Ticket{
			{ExternalID: "GH-401", Title: "Should not ingest"},
		},
	}

	mp := &daemonMockProcessor{}

	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 1

	d := NewDaemon(cfg)
	d.SetDB(mdb)
	d.SetTracker(mt)
	d.SetOrchestrator(mp)
	d.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	go d.Start(ctx)
	defer cancel()

	// Wait for 2 poll intervals to pass while paused
	time.Sleep(2500 * time.Millisecond)

	// Nothing should have been processed or ingested
	mdb.mu.Lock()
	ticketsCreated := len(mdb.createdTickets)
	mdb.mu.Unlock()

	mp.mu.Lock()
	processed := len(mp.calls)
	mp.mu.Unlock()

	assert.Equal(t, 0, ticketsCreated, "no tickets should be ingested while paused")
	assert.Equal(t, 0, processed, "no tickets should be processed while paused")
}

func TestDaemon_WaitForDrain(t *testing.T) {
	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 10 // Long interval so poll does not interfere

	d := NewDaemon(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	go d.Start(ctx)

	// Wait for daemon to be running
	require.Eventually(t, func() bool {
		return d.IsRunning()
	}, time.Second, 10*time.Millisecond)

	cancel()

	// WaitForDrain should return since there are no active pipelines
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()

	done := make(chan struct{})
	go func() {
		d.WaitForDrain(drainCtx)
		close(done)
	}()

	select {
	case <-done:
		// WaitForDrain returned successfully
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForDrain did not return in time")
	}
}
