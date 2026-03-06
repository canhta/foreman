//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Tracker ---

type mockIntegrationTracker struct {
	readyTickets []tracker.Ticket
	comments     []string
	mu           sync.Mutex
}

func (m *mockIntegrationTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readyTickets, nil
}

func (m *mockIntegrationTracker) CreateTicket(_ context.Context, _ tracker.CreateTicketRequest) (*tracker.Ticket, error) {
	return &tracker.Ticket{}, nil
}

func (m *mockIntegrationTracker) GetTicket(_ context.Context, _ string) (*tracker.Ticket, error) {
	return &tracker.Ticket{}, nil
}

func (m *mockIntegrationTracker) UpdateStatus(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockIntegrationTracker) AddComment(_ context.Context, _ string, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, comment)
	return nil
}

func (m *mockIntegrationTracker) AttachPR(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockIntegrationTracker) AddLabel(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockIntegrationTracker) RemoveLabel(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockIntegrationTracker) HasLabel(_ context.Context, _ string, _ string) (bool, error) {
	return false, nil
}

func (m *mockIntegrationTracker) ProviderName() string {
	return "mock"
}

// --- Mock Git Provider ---

type mockIntegrationGit struct{}

func (m *mockIntegrationGit) EnsureRepo(_ context.Context, _ string) error {
	return nil
}

func (m *mockIntegrationGit) CreateBranch(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockIntegrationGit) Commit(_ context.Context, _, _ string) (string, error) {
	return "abc123", nil
}

func (m *mockIntegrationGit) Diff(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

func (m *mockIntegrationGit) DiffWorking(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockIntegrationGit) Push(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockIntegrationGit) RebaseOnto(_ context.Context, _, _ string) (*git.RebaseResult, error) {
	return &git.RebaseResult{Success: true}, nil
}

func (m *mockIntegrationGit) FileTree(_ context.Context, _ string) ([]git.FileEntry, error) {
	return nil, nil
}

func (m *mockIntegrationGit) Log(_ context.Context, _ string, _ int) ([]git.CommitEntry, error) {
	return nil, nil
}

func (m *mockIntegrationGit) StageAll(_ context.Context, _ string) error {
	return nil
}

func (m *mockIntegrationGit) CleanWorkingTree(_ context.Context, _ string) error {
	return nil
}

// --- Mock PR Creator ---

type mockIntegrationPRCreator struct {
	called bool
	mu     sync.Mutex
}

func (m *mockIntegrationPRCreator) CreatePR(_ context.Context, _ git.PrRequest) (*git.PrResponse, error) {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()
	return &git.PrResponse{HTMLURL: "https://github.com/test/repo/pull/1", Number: 1}, nil
}

func (m *mockIntegrationPRCreator) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

// --- Mock Planner ---

type mockIntegrationPlanner struct{}

func (m *mockIntegrationPlanner) Plan(_ context.Context, _ string, _ *models.Ticket) (*daemon.PlanResult, error) {
	return &daemon.PlanResult{
		Status: "OK",
		Tasks: []daemon.PlannedTask{
			{Title: "Task 1", Description: "First task", FilesToModify: []string{"main.go"}},
			{Title: "Task 2", Description: "Second task", FilesToModify: []string{"util.go"}},
		},
	}, nil
}

// --- Mock Clarity Checker ---

type mockIntegrationClarityChecker struct{}

func (m *mockIntegrationClarityChecker) CheckTicketClarity(_ *models.Ticket) (bool, error) {
	return true, nil
}

// --- Mock DAG Task Runner Factory ---

type mockIntegrationRunnerFactory struct{}

func (m *mockIntegrationRunnerFactory) Create(_ daemon.TaskRunnerFactoryInput) daemon.TaskRunner {
	return &mockIntegrationTaskRunner{}
}

type mockIntegrationTaskRunner struct{}

func (m *mockIntegrationTaskRunner) Run(_ context.Context, taskID string) daemon.TaskResult {
	return daemon.TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}

// randomID generates a short random hex ID for test entities.
func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// --- DB wrapper that auto-generates task IDs (the orchestrator doesn't set them) ---

type idGeneratingDB struct {
	db.Database
}

func (d *idGeneratingDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	for i := range tasks {
		if tasks[i].ID == "" {
			tasks[i].ID = randomID()
		}
	}
	return d.Database.CreateTasks(ctx, ticketID, tasks)
}

// --- Test ---

func TestDaemon_EndToEnd(t *testing.T) {
	// 1. Create SQLite in a temp file (in-memory doesn't work with WAL params)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.NewSQLiteDB(dbPath)
	require.NoError(t, err)
	defer func() {
		database.Close()
		os.Remove(dbPath)
	}()

	// 2. Pre-insert a queued ticket with a real ID so the daemon picks it up.
	//    The mock tracker returns no tickets — ingest is a no-op.
	ticketID := randomID()
	now := time.Now()
	ticket := &models.Ticket{
		ID:          ticketID,
		ExternalID:  "TEST-123",
		Title:       "Implement feature X",
		Description: "Add a new feature X to the system",
		Status:      models.TicketStatusQueued,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, database.CreateTicket(context.Background(), ticket))

	// Wrap DB to auto-generate task IDs (the orchestrator doesn't set them).
	wrappedDB := &idGeneratingDB{Database: database}

	mockTracker := &mockIntegrationTracker{}

	// 3. Create mock git + PR creator
	mockGit := &mockIntegrationGit{}
	mockPR := &mockIntegrationPRCreator{}

	// 4. Create real CostController with generous limits
	costCtrl := telemetry.NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 1000,
		MaxCostPerDayUSD:    10000,
		MaxCostPerMonthUSD:  100000,
	})

	// 5. Create real Scheduler with the real DB
	scheduler := daemon.NewScheduler(wrappedDB)

	// 6. Create mock adapters
	planner := &mockIntegrationPlanner{}
	clarityChecker := &mockIntegrationClarityChecker{}
	runnerFactory := &mockIntegrationRunnerFactory{}

	// 7. Build orchestrator
	logger := zerolog.Nop()
	orchestrator := daemon.NewOrchestrator(
		wrappedDB,
		mockTracker,
		mockGit,
		mockPR,
		costCtrl,
		scheduler,
		planner,
		clarityChecker,
		runnerFactory,
		logger,
		daemon.OrchestratorConfig{
			WorkDir:            t.TempDir(),
			DefaultBranch:      "main",
			BranchPrefix:       "foreman/",
			MaxParallelTasks:   2,
			TaskTimeoutMinutes: 5,
			RebaseBeforePR:     true,
			AutoPush:           true,
		},
	)

	// 8. Build daemon with short poll interval
	daemonConfig := daemon.DaemonConfig{
		PollIntervalSecs:   1,
		MaxParallelTickets: 1,
		TaskTimeoutMinutes: 5,
	}
	d := daemon.NewDaemon(daemonConfig)
	d.SetDB(wrappedDB)
	d.SetTracker(mockTracker)
	d.SetOrchestrator(orchestrator)

	// 9. Start daemon in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Start(ctx)

	// 10. Wait for ticket to reach awaiting_merge
	assert.Eventually(t, func() bool {
		tickets, err := database.ListTickets(context.Background(), models.TicketFilter{
			StatusIn: []models.TicketStatus{models.TicketStatusAwaitingMerge},
		})
		if err != nil {
			return false
		}
		return len(tickets) > 0
	}, 10*time.Second, 200*time.Millisecond, "ticket should reach awaiting_merge status")

	// 11. Assert PR creator was called
	assert.True(t, mockPR.wasCalled(), "PR creator should have been called")

	// 12. Verify the ticket in DB
	tickets, err := database.ListTickets(context.Background(), models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusAwaitingMerge},
	})
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "TEST-123", tickets[0].ExternalID)
	assert.Equal(t, "Implement feature X", tickets[0].Title)

	// 13. Verify tasks were created
	tasks, err := database.ListTasks(context.Background(), tickets[0].ID)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	// 14. Shutdown
	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	d.WaitForDrain(drainCtx)
}
