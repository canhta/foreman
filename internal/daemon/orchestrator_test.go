package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
)

// ---------------------------------------------------------------------------
// Mock: db.Database (also satisfies FileReserver)
// ---------------------------------------------------------------------------

type orchMockDB struct {
	reserveErr     error
	releaseErr     error
	ticketsByExtID map[string]*models.Ticket
	reservedFiles  map[string]string
	statusUpdates  []struct {
		id     string
		status models.TicketStatus
	}
	createdTasks   []models.Task
	createdTickets []*models.Ticket
	tasks          []models.Task
	dailyCost      float64
	monthlyCost    float64
}

func newOrchMockDB() *orchMockDB {
	return &orchMockDB{
		reservedFiles:  make(map[string]string),
		ticketsByExtID: make(map[string]*models.Ticket),
	}
}

func (m *orchMockDB) CreateTicket(_ context.Context, t *models.Ticket) error {
	m.createdTickets = append(m.createdTickets, t)
	return nil
}
func (m *orchMockDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.statusUpdates = append(m.statusUpdates, struct {
		id     string
		status models.TicketStatus
	}{id, status})
	return nil
}
func (m *orchMockDB) GetTicket(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (m *orchMockDB) GetTicketByExternalID(_ context.Context, extID string) (*models.Ticket, error) {
	if t, ok := m.ticketsByExtID[extID]; ok {
		return t, nil
	}
	return nil, nil
}
func (m *orchMockDB) ListTickets(_ context.Context, _ models.TicketFilter) ([]models.Ticket, error) {
	return nil, nil
}
func (m *orchMockDB) GetChildTickets(_ context.Context, _ string) ([]models.Ticket, error) {
	return nil, nil
}
func (m *orchMockDB) SetLastCompletedTask(_ context.Context, _ string, _ int) error { return nil }
func (m *orchMockDB) CreateTasks(_ context.Context, _ string, tasks []models.Task) error {
	m.createdTasks = append(m.createdTasks, tasks...)
	return nil
}
func (m *orchMockDB) UpdateTaskStatus(_ context.Context, _ string, _ models.TaskStatus) error {
	return nil
}
func (m *orchMockDB) IncrementTaskLlmCalls(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (m *orchMockDB) ListTasks(_ context.Context, _ string) ([]models.Task, error) {
	return m.tasks, nil
}
func (m *orchMockDB) RecordLlmCall(_ context.Context, _ *models.LlmCallRecord) error { return nil }
func (m *orchMockDB) ListLlmCalls(_ context.Context, _ string) ([]models.LlmCallRecord, error) {
	return nil, nil
}
func (m *orchMockDB) SetHandoff(_ context.Context, _ *models.HandoffRecord) error { return nil }
func (m *orchMockDB) GetHandoffs(_ context.Context, _, _ string) ([]models.HandoffRecord, error) {
	return nil, nil
}
func (m *orchMockDB) SaveProgressPattern(_ context.Context, _ *models.ProgressPattern) error {
	return nil
}
func (m *orchMockDB) GetProgressPatterns(_ context.Context, _ string, _ []string) ([]models.ProgressPattern, error) {
	return nil, nil
}
func (m *orchMockDB) ReserveFiles(_ context.Context, ticketID string, paths []string) error {
	if m.reserveErr != nil {
		return m.reserveErr
	}
	for _, p := range paths {
		m.reservedFiles[p] = ticketID
	}
	return nil
}
func (m *orchMockDB) ReleaseFiles(_ context.Context, ticketID string) error {
	if m.releaseErr != nil {
		return m.releaseErr
	}
	for k, v := range m.reservedFiles {
		if v == ticketID {
			delete(m.reservedFiles, k)
		}
	}
	return nil
}
func (m *orchMockDB) GetReservedFiles(_ context.Context) (map[string]string, error) {
	return m.reservedFiles, nil
}
func (m *orchMockDB) TryReserveFiles(_ context.Context, ticketID string, paths []string) ([]string, error) {
	var conflicts []string
	for _, p := range paths {
		if owner, ok := m.reservedFiles[p]; ok && owner != ticketID {
			conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", p, owner))
		}
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}
	if m.reserveErr != nil {
		return nil, m.reserveErr
	}
	for _, p := range paths {
		m.reservedFiles[p] = ticketID
	}
	return nil, nil
}
func (m *orchMockDB) GetTicketCost(_ context.Context, _ string) (float64, error) { return 0, nil }
func (m *orchMockDB) GetDailyCost(_ context.Context, _ string) (float64, error) {
	return m.dailyCost, nil
}
func (m *orchMockDB) GetMonthlyCost(_ context.Context, _ string) (float64, error) {
	return m.monthlyCost, nil
}
func (m *orchMockDB) RecordDailyCost(_ context.Context, _ string, _ float64) error { return nil }
func (m *orchMockDB) RecordEvent(_ context.Context, _ *models.EventRecord) error   { return nil }
func (m *orchMockDB) GetEvents(_ context.Context, _ string, _ int) ([]models.EventRecord, error) {
	return nil, nil
}
func (m *orchMockDB) CreateAuthToken(_ context.Context, _, _ string) error        { return nil }
func (m *orchMockDB) ValidateAuthToken(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *orchMockDB) CreatePairing(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}
func (m *orchMockDB) GetPairing(_ context.Context, _ string) (*models.Pairing, error) {
	return nil, nil
}
func (m *orchMockDB) DeletePairing(_ context.Context, _ string) error { return nil }
func (m *orchMockDB) ListPairings(_ context.Context, _ string) ([]models.Pairing, error) {
	return nil, nil
}
func (m *orchMockDB) DeleteExpiredPairings(_ context.Context) error { return nil }
func (m *orchMockDB) FindActiveClarification(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (m *orchMockDB) GetTeamStats(_ context.Context, _ time.Time) ([]models.TeamStat, error) {
	return nil, nil
}
func (m *orchMockDB) GetRecentPRs(_ context.Context, _ int) ([]models.Ticket, error) {
	return nil, nil
}
func (m *orchMockDB) GetTicketSummaries(_ context.Context, _ models.TicketFilter) ([]models.TicketSummary, error) {
	return nil, nil
}
func (m *orchMockDB) GetGlobalEvents(_ context.Context, _, _ int) ([]models.EventRecord, error) {
	return nil, nil
}
func (m *orchMockDB) DeleteTicket(_ context.Context, _ string) error           { return nil }
func (m *orchMockDB) SetTaskErrorType(_ context.Context, _, _ string) error    { return nil }
func (m *orchMockDB) StoreCallDetails(_ context.Context, _, _, _ string) error { return nil }
func (m *orchMockDB) GetCallDetails(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}
func (m *orchMockDB) Close() error { return nil }

func (m *orchMockDB) lastStatus(id string) models.TicketStatus {
	for i := len(m.statusUpdates) - 1; i >= 0; i-- {
		if m.statusUpdates[i].id == id {
			return m.statusUpdates[i].status
		}
	}
	return ""
}

func (m *orchMockDB) statusSequence(id string) []models.TicketStatus {
	var seq []models.TicketStatus
	for _, u := range m.statusUpdates {
		if u.id == id {
			seq = append(seq, u.status)
		}
	}
	return seq
}

// ---------------------------------------------------------------------------
// Mock: tracker.IssueTracker
// ---------------------------------------------------------------------------

type orchMockTracker struct {
	comments []struct {
		externalID string
		comment    string
	}
	labels []struct {
		externalID string
		label      string
	}
	prURLs []string
}

func (m *orchMockTracker) CreateTicket(_ context.Context, _ tracker.CreateTicketRequest) (*tracker.Ticket, error) {
	return nil, nil
}
func (m *orchMockTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) {
	return nil, nil
}
func (m *orchMockTracker) GetTicket(_ context.Context, _ string) (*tracker.Ticket, error) {
	return nil, nil
}
func (m *orchMockTracker) UpdateStatus(_ context.Context, _, _ string) error { return nil }
func (m *orchMockTracker) AddComment(_ context.Context, externalID, comment string) error {
	m.comments = append(m.comments, struct {
		externalID string
		comment    string
	}{externalID, comment})
	return nil
}
func (m *orchMockTracker) AttachPR(_ context.Context, _ string, prURL string) error {
	m.prURLs = append(m.prURLs, prURL)
	return nil
}
func (m *orchMockTracker) AddLabel(_ context.Context, externalID, label string) error {
	m.labels = append(m.labels, struct {
		externalID string
		label      string
	}{externalID, label})
	return nil
}
func (m *orchMockTracker) RemoveLabel(_ context.Context, _, _ string) error      { return nil }
func (m *orchMockTracker) HasLabel(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (m *orchMockTracker) ProviderName() string                                  { return "mock" }

// ---------------------------------------------------------------------------
// Mock: git.GitProvider
// ---------------------------------------------------------------------------

type orchMockGit struct {
	ensureErr    error
	branchErr    error
	pushErr      error
	rebaseResult *git.RebaseResult
	rebaseErr    error
}

func (m *orchMockGit) EnsureRepo(_ context.Context, _ string) error      { return m.ensureErr }
func (m *orchMockGit) CreateBranch(_ context.Context, _, _ string) error { return m.branchErr }
func (m *orchMockGit) Commit(_ context.Context, _, _ string) (string, error) {
	return "abc123", nil
}
func (m *orchMockGit) Diff(_ context.Context, _, _, _ string) (string, error)  { return "", nil }
func (m *orchMockGit) DiffWorking(_ context.Context, _ string) (string, error) { return "", nil }
func (m *orchMockGit) Push(_ context.Context, _, _ string) error               { return m.pushErr }
func (m *orchMockGit) RebaseOnto(_ context.Context, _, _ string) (*git.RebaseResult, error) {
	if m.rebaseErr != nil {
		return nil, m.rebaseErr
	}
	if m.rebaseResult != nil {
		return m.rebaseResult, nil
	}
	return &git.RebaseResult{Success: true}, nil
}
func (m *orchMockGit) FileTree(_ context.Context, _ string) ([]git.FileEntry, error) {
	return nil, nil
}
func (m *orchMockGit) Log(_ context.Context, _ string, _ int) ([]git.CommitEntry, error) {
	return nil, nil
}
func (m *orchMockGit) StageAll(_ context.Context, _ string) error         { return nil }
func (m *orchMockGit) CleanWorkingTree(_ context.Context, _ string) error { return nil }

// ---------------------------------------------------------------------------
// Mock: git.PRCreator
// ---------------------------------------------------------------------------

type orchMockPRCreator struct {
	response *git.PrResponse
	err      error
	called   bool
}

func (m *orchMockPRCreator) CreatePR(_ context.Context, _ git.PrRequest) (*git.PrResponse, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// ---------------------------------------------------------------------------
// Mock: TicketPlanner
// ---------------------------------------------------------------------------

type orchMockPlanner struct {
	result *PlanResult
	err    error
}

func (m *orchMockPlanner) Plan(_ context.Context, _ string, _ *models.Ticket) (*PlanResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// ---------------------------------------------------------------------------
// Mock: ClarityChecker
// ---------------------------------------------------------------------------

type orchMockClarity struct {
	err   error
	clear bool
}

func (m *orchMockClarity) CheckTicketClarity(_ *models.Ticket) (bool, error) {
	return m.clear, m.err
}

// ---------------------------------------------------------------------------
// Mock: DAGTaskRunnerFactory + TaskRunner
// ---------------------------------------------------------------------------

type orchMockRunnerFactory struct {
	runner TaskRunner
}

func (m *orchMockRunnerFactory) Create(_ TaskRunnerFactoryInput) TaskRunner {
	return m.runner
}

type orchMockTaskRunner struct {
	results map[string]TaskResult
}

func (m *orchMockTaskRunner) Run(_ context.Context, taskID string) TaskResult {
	if res, ok := m.results[taskID]; ok {
		return res
	}
	return TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}

// ---------------------------------------------------------------------------
// Fixture helper
// ---------------------------------------------------------------------------

type orchFixture struct {
	db        *orchMockDB
	tracker   *orchMockTracker
	gitProv   *orchMockGit
	prCreator *orchMockPRCreator
	planner   *orchMockPlanner
	clarity   *orchMockClarity
	factory   *orchMockRunnerFactory
	runner    *orchMockTaskRunner
	orch      *Orchestrator
}

func newOrchFixture() *orchFixture {
	mdb := newOrchMockDB()
	mdb.tasks = []models.Task{
		{ID: "task-1", Title: "Task 1", Status: models.TaskStatusPending, Sequence: 1},
	}

	mt := &orchMockTracker{}
	mg := &orchMockGit{}
	mpr := &orchMockPRCreator{
		response: &git.PrResponse{
			URL:     "https://api.github.com/repos/test/test/pulls/1",
			HTMLURL: "https://github.com/test/test/pull/1",
			Number:  1,
		},
	}

	mr := &orchMockTaskRunner{
		results: map[string]TaskResult{
			"task-1": {TaskID: "task-1", Status: models.TaskStatusDone},
		},
	}
	mf := &orchMockRunnerFactory{runner: mr}

	mp := &orchMockPlanner{
		result: &PlanResult{
			Status: "OK",
			Tasks: []PlannedTask{
				{
					Title:         "Task 1",
					Description:   "Do stuff",
					FilesToModify: []string{"main.go"},
				},
			},
		},
	}

	mc := &orchMockClarity{clear: true}

	costCtrl := telemetry.NewCostController(models.CostConfig{
		MaxCostPerDayUSD:   100,
		MaxCostPerMonthUSD: 1000,
	})

	scheduler := NewScheduler(mdb)

	log := zerolog.New(io.Discard)

	config := OrchestratorConfig{
		WorkDir:             "/tmp/test-repo",
		DefaultBranch:       "main",
		BranchPrefix:        "foreman/",
		AutoPush:            true,
		RebaseBeforePR:      true,
		EnableClarification: true,
		ClarificationLabel:  "needs-clarification",
		MaxParallelTasks:    2,
		TaskTimeoutMinutes:  5,
	}

	orch := NewOrchestrator(mdb, mt, mg, mpr, costCtrl, scheduler, mp, mc, mf, log, config)

	return &orchFixture{
		db:        mdb,
		tracker:   mt,
		gitProv:   mg,
		prCreator: mpr,
		planner:   mp,
		clarity:   mc,
		factory:   mf,
		runner:    mr,
		orch:      orch,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestProcessTicket_HappyPath(t *testing.T) {
	f := newOrchFixture()

	ticket := models.Ticket{
		ID:         "t-1",
		ExternalID: "GH-42",
		Title:      "Add feature X",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.NoError(t, err)

	// Status transitions: planning -> implementing -> awaiting_merge
	seq := f.db.statusSequence("t-1")
	require.GreaterOrEqual(t, len(seq), 3)
	assert.Equal(t, models.TicketStatusPlanning, seq[0])
	assert.Equal(t, models.TicketStatusImplementing, seq[1])
	assert.Equal(t, models.TicketStatusAwaitingMerge, seq[len(seq)-1])

	// "picked up" comment posted
	pickedUp := false
	for _, c := range f.tracker.comments {
		if c.externalID == "GH-42" && strings.Contains(c.comment, "picked up") {
			pickedUp = true
			break
		}
	}
	assert.True(t, pickedUp, "expected 'picked up' comment on tracker")

	// PR URL comment posted
	prComment := false
	for _, c := range f.tracker.comments {
		if c.externalID == "GH-42" && strings.Contains(c.comment, "https://github.com/test/test/pull/1") {
			prComment = true
			break
		}
	}
	assert.True(t, prComment, "expected PR URL comment on tracker")

	// PR created
	assert.True(t, f.prCreator.called, "PR should have been created")

	// Scheduler released (reserved files empty after success)
	assert.Empty(t, f.db.reservedFiles, "file reservations should be released after PR")
}

func TestProcessTicket_ClarificationNeeded(t *testing.T) {
	f := newOrchFixture()
	f.clarity.clear = false

	ticket := models.Ticket{
		ID:         "t-2",
		ExternalID: "GH-43",
		Title:      "Vague ticket",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.NoError(t, err)

	// Status set to clarification_needed
	last := f.db.lastStatus("t-2")
	assert.Equal(t, models.TicketStatusClarificationNeeded, last)

	// Clarification label added
	labelFound := false
	for _, l := range f.tracker.labels {
		if l.externalID == "GH-43" && l.label == "needs-clarification" {
			labelFound = true
			break
		}
	}
	assert.True(t, labelFound, "expected clarification label on tracker")

	// No PR created
	assert.False(t, f.prCreator.called)

	// No error returned
	// (already asserted by require.NoError above)
}

func TestProcessTicket_FileConflict(t *testing.T) {
	f := newOrchFixture()

	// Pre-reserve the file that the planned task wants to modify
	f.db.reservedFiles["main.go"] = "other-ticket"

	ticket := models.Ticket{
		ID:         "t-3",
		ExternalID: "GH-44",
		Title:      "Conflicting ticket",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.NoError(t, err)

	// Status set back to queued
	last := f.db.lastStatus("t-3")
	assert.Equal(t, models.TicketStatusQueued, last)

	// No PR created
	assert.False(t, f.prCreator.called)
}

func TestProcessTicket_PlanFailure(t *testing.T) {
	f := newOrchFixture()
	f.planner.err = errors.New("LLM unavailable")

	ticket := models.Ticket{
		ID:         "t-4",
		ExternalID: "GH-45",
		Title:      "Plan fails",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "planning")

	// Status set to failed (via defer)
	last := f.db.lastStatus("t-4")
	assert.Equal(t, models.TicketStatusFailed, last)

	// Error comment posted to tracker
	errorComment := false
	for _, c := range f.tracker.comments {
		if c.externalID == "GH-45" && strings.Contains(c.comment, "error") {
			errorComment = true
			break
		}
	}
	assert.True(t, errorComment, "expected error comment posted to tracker")
}

func TestProcessTicket_CostBudgetExceeded(t *testing.T) {
	f := newOrchFixture()
	f.db.dailyCost = 200.0 // exceeds $100 daily budget

	ticket := models.Ticket{
		ID:         "t-5",
		ExternalID: "GH-46",
		Title:      "Expensive ticket",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "budget")

	// Status set to failed
	last := f.db.lastStatus("t-5")
	assert.Equal(t, models.TicketStatusFailed, last)

	// Budget error in tracker comment
	budgetComment := false
	for _, c := range f.tracker.comments {
		if c.externalID == "GH-46" && strings.Contains(c.comment, "budget") {
			budgetComment = true
			break
		}
	}
	assert.True(t, budgetComment, "expected budget error in tracker comment")
}

func TestProcessTicket_DAGAllFailed(t *testing.T) {
	f := newOrchFixture()

	f.runner.results = map[string]TaskResult{
		"task-1": {TaskID: "task-1", Status: models.TaskStatusFailed, Error: fmt.Errorf("compilation error")},
	}

	ticket := models.Ticket{
		ID:         "t-6",
		ExternalID: "GH-47",
		Title:      "All tasks fail",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")

	// Status set to failed
	last := f.db.lastStatus("t-6")
	assert.Equal(t, models.TicketStatusFailed, last)
}

// TestProcessTicket_ReleaseFailDoesNotBlockPR verifies BUG-M02:
// A failed Release() call after a successful PR creation must NOT cause the
// orchestrator to return an error. The PR was already created; leaking
// reservations is only cosmetic (CleanupOrphanReservations will handle it).
func TestProcessTicket_ReleaseFailDoesNotBlockPR(t *testing.T) {
	f := newOrchFixture()
	// Inject a release error so Release() fails.
	f.db.releaseErr = errors.New("db connection lost")

	ticket := models.Ticket{
		ID:         "t-rel-fail",
		ExternalID: "GH-99",
		Title:      "Release fails after PR",
	}

	err := f.orch.ProcessTicket(context.Background(), ticket)
	// Must succeed despite release failure.
	require.NoError(t, err, "release error must not propagate after PR is created")

	// Final status should be awaiting_merge, not failed.
	last := f.db.lastStatus("t-rel-fail")
	assert.Equal(t, models.TicketStatusAwaitingMerge, last,
		"ticket must reach awaiting_merge even when Release() fails")

	// PR was still created.
	assert.True(t, f.prCreator.called, "PR should have been created")
}
