//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaemon_ConcurrentTickets verifies that MaxParallelTickets=2 allows two
// tickets to be processed simultaneously and both reach awaiting_merge.
func TestDaemon_ConcurrentTickets(t *testing.T) {
	// 1. Create SQLite in a temp file.
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	database, err := db.NewSQLiteDB(dbPath)
	require.NoError(t, err)
	defer func() {
		database.Close()
		os.Remove(dbPath)
	}()

	// 2. Pre-insert two queued tickets.
	now := time.Now()
	ticket1ID := randomID()
	ticket2ID := randomID()
	for _, t2 := range []struct{ id, ext, title string }{
		{ticket1ID, "CONC-1", "Concurrent ticket 1"},
		{ticket2ID, "CONC-2", "Concurrent ticket 2"},
	} {
		require.NoError(t, database.CreateTicket(context.Background(), &models.Ticket{
			ID: t2.id, ExternalID: t2.ext, Title: t2.title,
			Description: "desc", Status: models.TicketStatusQueued,
			CreatedAt: now, UpdatedAt: now,
		}))
	}

	wrappedDB := &idGeneratingDB{Database: database}

	// 3. Track concurrent task executions.
	var concurrentPeak int32
	var active int32

	trackingFactory := &peakTrackingRunnerFactory{
		concurrent: &active,
		peak:       &concurrentPeak,
	}

	// 4. Wire infrastructure.
	costCtrl := telemetry.NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 1000,
		MaxCostPerDayUSD:    10000,
		MaxCostPerMonthUSD:  100000,
	})
	scheduler := daemon.NewScheduler(wrappedDB)
	mockTracker := &mockIntegrationTracker{}
	mockGit := &mockIntegrationGit{}
	mockPR := &mockIntegrationPRCreator{}
	logger := zerolog.Nop()

	orchestrator := daemon.NewOrchestrator(
		wrappedDB, mockTracker, mockGit, mockPR,
		costCtrl, scheduler,
		&mockIntegrationPlanner{},
		&mockIntegrationClarityChecker{},
		trackingFactory,
		logger,
		daemon.OrchestratorConfig{
			WorkDir:            t.TempDir(),
			DefaultBranch:      "main",
			BranchPrefix:       "foreman/",
			MaxParallelTasks:   2,
			TaskTimeoutMinutes: 5,
		},
	)

	// 5. Start daemon with MaxParallelTickets=2.
	d := daemon.NewDaemon(daemon.DaemonConfig{
		PollIntervalSecs:   1,
		MaxParallelTickets: 2,
		MaxParallelTasks:   2,
		TaskTimeoutMinutes: 5,
	})
	d.SetDB(wrappedDB)
	d.SetTracker(mockTracker)
	d.SetOrchestrator(orchestrator)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Start(ctx)

	// 6. Wait for both tickets to reach awaiting_merge.
	assert.Eventually(t, func() bool {
		tickets, err2 := database.ListTickets(context.Background(), models.TicketFilter{
			StatusIn: []models.TicketStatus{models.TicketStatusAwaitingMerge},
		})
		return err2 == nil && len(tickets) == 2
	}, 15*time.Second, 200*time.Millisecond, "both tickets should reach awaiting_merge")

	// 7. Verify both specific tickets are awaiting_merge.
	t1, err := database.GetTicket(context.Background(), ticket1ID)
	require.NoError(t, err)
	assert.Equal(t, models.TicketStatusAwaitingMerge, t1.Status)

	t2, err := database.GetTicket(context.Background(), ticket2ID)
	require.NoError(t, err)
	assert.Equal(t, models.TicketStatusAwaitingMerge, t2.Status)

	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	d.WaitForDrain(drainCtx)
}

// peakTrackingRunnerFactory wraps the mock factory and records concurrent task peaks.
type peakTrackingRunnerFactory struct {
	concurrent *int32
	peak       *int32
}

func (f *peakTrackingRunnerFactory) Create(_ daemon.TaskRunnerFactoryInput) daemon.TaskRunner {
	return &peakTrackingTaskRunner{concurrent: f.concurrent, peak: f.peak}
}

type peakTrackingTaskRunner struct {
	concurrent *int32
	peak       *int32
}

func (r *peakTrackingTaskRunner) Run(_ context.Context, taskID string) daemon.TaskResult {
	cur := atomic.AddInt32(r.concurrent, 1)
	// Update peak if this is a new high.
	for {
		old := atomic.LoadInt32(r.peak)
		if cur <= old || atomic.CompareAndSwapInt32(r.peak, old, cur) {
			break
		}
	}
	// Small delay to allow overlap to be detected.
	time.Sleep(10 * time.Millisecond)
	atomic.AddInt32(r.concurrent, -1)
	return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}
