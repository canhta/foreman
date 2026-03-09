// internal/pipeline/consistency_review_test.go
package pipeline

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConsistencyDB implements ConsistencyReviewDB for testing.
type mockConsistencyDB struct {
	savePatternErr error
	savedPatterns  []*models.ProgressPattern
	mu             sync.Mutex
	savedPatternsN int32
}

func (m *mockConsistencyDB) SaveProgressPattern(_ context.Context, p *models.ProgressPattern) error {
	atomic.AddInt32(&m.savedPatternsN, 1)
	m.mu.Lock()
	m.savedPatterns = append(m.savedPatterns, p)
	m.mu.Unlock()
	return m.savePatternErr
}

// mockErrorLLM always returns an error.
type mockErrorLLM struct{}

func (m *mockErrorLLM) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	return nil, errors.New("LLM unavailable")
}

func (m *mockErrorLLM) ProviderName() string                { return "mock_error" }
func (m *mockErrorLLM) HealthCheck(_ context.Context) error { return nil }

// mockConsistencyLLM returns a configurable response.
type mockConsistencyLLM struct {
	err      error
	response string
}

func (m *mockConsistencyLLM) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &models.LlmResponse{
		Content:      m.response,
		Model:        "test-model",
		StopReason:   models.StopReasonEndTurn,
		TokensInput:  100,
		TokensOutput: 50,
	}, nil
}

func (m *mockConsistencyLLM) ProviderName() string                { return "mock_consistency" }
func (m *mockConsistencyLLM) HealthCheck(_ context.Context) error { return nil }

// errGitProvider is a git provider whose Diff always fails.
type errGitProvider struct {
	realMockGitProvider
}

func (e *errGitProvider) Diff(_ context.Context, _, _, _ string) (string, error) {
	return "", errors.New("git diff failed")
}

func (e *errGitProvider) DiffWorking(_ context.Context, _ string) (string, error) {
	return "", errors.New("git diff failed")
}

// Compile-time check.
var _ git.GitProvider = (*errGitProvider)(nil)

// newConsistencyAdapter is a helper that builds a DAGTaskAdapter for consistency
// review tests and sets completedCount via Store (required since it is atomic.Int64).
func newConsistencyAdapter(
	ticketID string,
	llm LLMProvider,
	db ConsistencyReviewDB,
	gitProv git.GitProvider,
	interval int,
	completedCount int64,
) *DAGTaskAdapter {
	a := &DAGTaskAdapter{
		ticketID: ticketID,
		llm:      llm,
		cdb:      db,
		git:      gitProv,
		config: TaskRunnerConfig{
			WorkDir:                    "/tmp",
			IntermediateReviewInterval: interval,
		},
	}
	a.completedCount.Store(completedCount)
	return a
}

// --- Tests ---

// TestIntermediateConsistencyReview_TriggeredAtInterval verifies the check fires
// when completedCount % interval == 0 and interval > 0.
func TestIntermediateConsistencyReview_TriggeredAtInterval(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{
		response: `[{"pattern": "error_handling", "file": "foo.go", "line": 10, "suggestion": "wrap error"}]`,
	}

	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+code"}, 3, 3)
	adapter.runIntermediateConsistencyReview(context.Background())

	assert.EqualValues(t, 1, atomic.LoadInt32(&db.savedPatternsN),
		"should save 1 violation pattern")
	assert.Equal(t, "ticket-1", db.savedPatterns[0].TicketID)
	assert.Equal(t, "error_handling", db.savedPatterns[0].PatternKey)
}

// TestIntermediateConsistencyReview_NotTriggeredBetweenIntervals verifies the check
// does NOT fire when completedCount % interval != 0.
func TestIntermediateConsistencyReview_NotTriggeredBetweenIntervals(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{response: `[]`}

	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+code"}, 3, 0)

	// completedCount = 1 → should NOT trigger (not a multiple of 3)
	adapter.completedCount.Store(1)
	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 0, atomic.LoadInt32(&db.savedPatternsN))

	// completedCount = 2 → should NOT trigger
	adapter.completedCount.Store(2)
	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 0, atomic.LoadInt32(&db.savedPatternsN))
}

// TestIntermediateConsistencyReview_NeverBlocksOnLLMError verifies that LLM errors
// are logged and do not block execution or return errors.
func TestIntermediateConsistencyReview_NeverBlocksOnLLMError(t *testing.T) {
	db := &mockConsistencyDB{}
	adapter := newConsistencyAdapter("ticket-1", &mockErrorLLM{}, db, &realMockGitProvider{diffOutput: "+code"}, 3, 3)

	// Must not panic or return an error (non-blocking by design).
	assert.NotPanics(t, func() {
		adapter.runIntermediateConsistencyReview(context.Background())
	})
	// No patterns should be saved on LLM failure.
	assert.EqualValues(t, 0, atomic.LoadInt32(&db.savedPatternsN))
}

// TestIntermediateConsistencyReview_NeverBlocksOnGitError verifies that git diff errors
// are logged and do not block execution.
func TestIntermediateConsistencyReview_NeverBlocksOnGitError(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{response: `[]`}
	adapter := newConsistencyAdapter("ticket-1", llm, db, &errGitProvider{}, 3, 3)

	assert.NotPanics(t, func() {
		adapter.runIntermediateConsistencyReview(context.Background())
	})
	assert.EqualValues(t, 0, atomic.LoadInt32(&db.savedPatternsN))
}

// TestIntermediateConsistencyReview_ViolationsSavedAsProgressPatterns verifies that
// each JSON violation item is saved as a separate progress pattern.
func TestIntermediateConsistencyReview_ViolationsSavedAsProgressPatterns(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{
		response: `[
			{"pattern": "naming_convention", "file": "handler.go", "line": 5, "suggestion": "use camelCase"},
			{"pattern": "import_order", "file": "main.go", "line": 1, "suggestion": "group stdlib imports"},
			{"pattern": "error_wrapping", "file": "db.go", "line": 42, "suggestion": "use fmt.Errorf with %w"}
		]`,
	}

	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+some diff"}, 3, 3)
	adapter.runIntermediateConsistencyReview(context.Background())

	require.EqualValues(t, 3, atomic.LoadInt32(&db.savedPatternsN),
		"3 violations should be saved as 3 progress patterns")

	patterns := make(map[string]bool)
	for _, p := range db.savedPatterns {
		patterns[p.PatternKey] = true
		assert.Equal(t, "ticket-1", p.TicketID)
	}
	assert.True(t, patterns["naming_convention"])
	assert.True(t, patterns["import_order"])
	assert.True(t, patterns["error_wrapping"])
}

// TestIntermediateConsistencyReview_NeverBlocksOnDBError verifies that DB save errors
// are logged but do not block execution.
func TestIntermediateConsistencyReview_NeverBlocksOnDBError(t *testing.T) {
	db := &mockConsistencyDB{
		savePatternErr: errors.New("db write failed"),
	}
	llm := &mockConsistencyLLM{
		response: `[{"pattern": "naming", "file": "foo.go", "line": 1, "suggestion": "fix name"}]`,
	}
	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+code"}, 3, 3)

	assert.NotPanics(t, func() {
		adapter.runIntermediateConsistencyReview(context.Background())
	})
}

// TestIntermediateConsistencyReview_IntervalZeroDisabled verifies that interval=0
// disables the check entirely.
func TestIntermediateConsistencyReview_IntervalZeroDisabled(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{response: `[{"pattern": "test", "file": "a.go", "line": 1, "suggestion": "x"}]`}
	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+code"}, 0, 3)

	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 0, atomic.LoadInt32(&db.savedPatternsN),
		"interval=0 should disable the check")
}

// TestIntermediateConsistencyReview_TriggeredAtMultipleIntervals verifies the check
// fires again at each subsequent interval crossing (e.g. count=3, count=6), not just
// the first one.
func TestIntermediateConsistencyReview_TriggeredAtMultipleIntervals(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{
		response: `[{"pattern": "error_handling", "file": "foo.go", "line": 1, "suggestion": "wrap"}]`,
	}

	adapter := newConsistencyAdapter("ticket-1", llm, db, &realMockGitProvider{diffOutput: "+code"}, 3, 0)

	// First interval crossing at count=3.
	adapter.completedCount.Store(3)
	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 1, atomic.LoadInt32(&db.savedPatternsN),
		"should save 1 violation at first interval crossing (count=3)")

	// Second interval crossing at count=6.
	adapter.completedCount.Store(6)
	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 2, atomic.LoadInt32(&db.savedPatternsN),
		"should save another violation at second interval crossing (count=6)")

	// count=7 is between intervals — should NOT fire.
	adapter.completedCount.Store(7)
	adapter.runIntermediateConsistencyReview(context.Background())
	assert.EqualValues(t, 2, atomic.LoadInt32(&db.savedPatternsN),
		"should not fire between interval crossings (count=7)")
}

// TestIntermediateConsistencyReview_LastReviewedSHAAdvances verifies that after a
// successful review the lastReviewedSHA is updated to the current HEAD SHA, so the
// next review diffs only the new tasks (not replaying old commits).
func TestIntermediateConsistencyReview_LastReviewedSHAAdvances(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{
		response: `[{"pattern": "naming", "file": "a.go", "line": 1, "suggestion": "fix"}]`,
	}
	// Log() will return this SHA as HEAD.
	gitProv := &realMockGitProvider{
		diffOutput: "+code",
		commitSHA:  "sha-after-first-review",
		logEntries: []git.CommitEntry{{SHA: "sha-after-first-review"}},
	}

	adapter := newConsistencyAdapter("ticket-1", llm, db, gitProv, 3, 3)
	assert.Empty(t, adapter.lastReviewedSHA, "should start with empty lastReviewedSHA")

	adapter.runIntermediateConsistencyReview(context.Background())

	adapter.lastReviewedSHAMu.Lock()
	gotSHA := adapter.lastReviewedSHA
	adapter.lastReviewedSHAMu.Unlock()

	assert.Equal(t, "sha-after-first-review", gotSHA,
		"lastReviewedSHA should be updated to HEAD SHA after a successful review")
}

// TestIntermediateConsistencyReview_UsesLastReviewedSHAForDiff verifies that on the
// second interval crossing the diff is from lastReviewedSHA to HEAD, not HEAD~interval.
func TestIntermediateConsistencyReview_UsesLastReviewedSHAForDiff(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{response: `[]`}

	var capturedBase string
	trackingGit := &diffTrackingGitProvider{
		logSHA: "head-sha-2",
		onDiff: func(base, head string) {
			capturedBase = base
		},
	}

	adapter := newConsistencyAdapter("ticket-1", llm, db, trackingGit, 3, 0)
	// Simulate having already done one review at "prev-sha".
	adapter.lastReviewedSHAMu.Lock()
	adapter.lastReviewedSHA = "prev-sha"
	adapter.lastReviewedSHAMu.Unlock()

	adapter.completedCount.Store(6)
	adapter.runIntermediateConsistencyReview(context.Background())

	assert.Equal(t, "prev-sha", capturedBase,
		"second review should diff from lastReviewedSHA, not HEAD~interval")
}

// diffTrackingGitProvider records the base argument passed to Diff.
type diffTrackingGitProvider struct {
	onDiff func(base, head string)
	logSHA string
	realMockGitProvider
}

func (d *diffTrackingGitProvider) Diff(_ context.Context, _, base, head string) (string, error) {
	if d.onDiff != nil {
		d.onDiff(base, head)
	}
	return "+tracked diff", nil
}

func (d *diffTrackingGitProvider) Log(_ context.Context, _ string, _ int) ([]git.CommitEntry, error) {
	if d.logSHA != "" {
		return []git.CommitEntry{{SHA: d.logSHA}}, nil
	}
	return nil, nil
}

var _ git.GitProvider = (*diffTrackingGitProvider)(nil)

// TestIntermediateConsistencyReview_ConcurrentSafety verifies that concurrent calls
// to Run() with multiple goroutines do not cause data races on completedCount or
// lastReviewedSHA. Run with -race to detect violations.
func TestIntermediateConsistencyReview_ConcurrentSafety(t *testing.T) {
	db := &mockConsistencyDB{}
	llm := &mockConsistencyLLM{response: `[]`}
	gitProv := &realMockGitProvider{
		diffOutput: "+concurrent diff",
		logEntries: []git.CommitEntry{{SHA: "concurrent-sha"}},
	}

	adapter := newConsistencyAdapter("ticket-concurrent", llm, db, gitProv, 3, 0)

	// Simulate concurrent increments as DAGExecutor would do.
	const goroutines = 9
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			interval := int64(adapter.config.IntermediateReviewInterval)
			newCount := adapter.completedCount.Add(1)
			if interval > 0 && newCount%interval == 0 {
				adapter.runIntermediateConsistencyReview(context.Background())
			}
		}()
	}
	wg.Wait()

	// With interval=3 and 9 completions, reviews should fire at counts 3, 6, 9.
	assert.Equal(t, int64(9), adapter.completedCount.Load(),
		"completedCount should be 9 after 9 concurrent increments")
}

// TestDAGTaskAdapter_Run_IncrementsCompletedCount verifies that completedCount is
// incremented after a successful task run.
func TestDAGTaskAdapter_Run_IncrementsCompletedCount(t *testing.T) {
	tasks := []models.Task{{ID: "task-1", Title: "Do the thing"}}
	db := newMockAdapterDB(tasks)

	llm := &mockLLM{
		responses: map[string]string{
			"implementer":      buildNewFileResponse("hello.go", "package main\n"),
			"spec_reviewer":    "STATUS: APPROVED\nCRITERIA:\n- [pass] all good\nISSUES:\n- None",
			"quality_reviewer": "STATUS: APPROVED\nISSUES:\n- None",
		},
	}
	g := &realMockGitProvider{diffOutput: "+package main", commitSHA: "abc123"}
	cmd := &realMockCmdRunner{exitCode: 0}

	workDir := t.TempDir()
	r := NewPipelineTaskRunner(llm, db, g, cmd, TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 1,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
		SearchReplaceSimilarity:  0.8,
	}).WithRegistry(mustLoadTestRegistry(t))

	cdb := &mockConsistencyDB{}
	adapter := &DAGTaskAdapter{
		runner:   r,
		db:       db,
		ticketID: "ticket-1",
		llm:      llm,
		cdb:      cdb,
		git:      g,
		config: TaskRunnerConfig{
			WorkDir:                    workDir,
			IntermediateReviewInterval: 3, // won't fire at count=1
		},
	}

	result := adapter.Run(context.Background(), "task-1")
	assert.Equal(t, models.TaskStatusDone, result.Status)
	assert.Equal(t, int64(1), adapter.completedCount.Load(),
		"completedCount should be 1 after one successful task")
}
