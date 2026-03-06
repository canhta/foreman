# Production Deployment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire the daemon poll loop end-to-end and ship production-ready deployment infrastructure so Foreman runs autonomously on a VPS.

**Architecture:** Orchestrator pattern — daemon owns scheduling/concurrency, orchestrator owns per-ticket lifecycle (pickup through PR). CLI stubs wired to existing DB layer. Both Docker Compose and systemd deployment paths.

**Tech Stack:** Go 1.24, SQLite, zerolog, cobra, prometheus, testify

---

### Task 1: Orchestrator — Interface and Struct

**Files:**
- Create: `internal/daemon/orchestrator.go`

**Step 1: Define TicketProcessor interface and Orchestrator struct**

```go
// internal/daemon/orchestrator.go
package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
)

// TicketProcessor processes a single ticket end-to-end.
type TicketProcessor interface {
	ProcessTicket(ctx context.Context, ticket models.Ticket) error
}

// OrchestratorConfig holds configuration for ticket processing.
type OrchestratorConfig struct {
	WorkDir                string
	DefaultBranch          string
	BranchPrefix           string
	PRDraft                bool
	PRReviewers            []string
	RebaseBeforePR         bool
	AutoPush               bool
	MaxParallelTasks       int
	TaskTimeoutMinutes     int
	ClarificationLabel     string
	EnablePartialPR        bool
	EnableTDDVerification  bool
	EnableClarification    bool
	MaxLlmCallsPerTask    int
	MaxImplementRetries    int
	MaxSpecReviewCycles    int
	MaxQualityReviewCycles int
	ContextTokenBudget     int
	TestCommand            string
	Models                 models.ModelsConfig
}

// Orchestrator processes a single ticket through the full pipeline lifecycle.
type Orchestrator struct {
	db        db.Database
	tracker   tracker.IssueTracker
	git       git.GitProvider
	prCreator git.PRCreator
	llm       pipeline.LLMProvider
	cmdRunner runner.CommandRunner
	costCtrl  *telemetry.CostController
	scheduler *Scheduler
	log       zerolog.Logger
	config    OrchestratorConfig
}

// NewOrchestrator creates an orchestrator with all dependencies.
func NewOrchestrator(
	database db.Database,
	tr tracker.IssueTracker,
	gitProv git.GitProvider,
	prCreator git.PRCreator,
	llm pipeline.LLMProvider,
	cmdRunner runner.CommandRunner,
	costCtrl *telemetry.CostController,
	scheduler *Scheduler,
	log zerolog.Logger,
	config OrchestratorConfig,
) *Orchestrator {
	return &Orchestrator{
		db:        database,
		tracker:   tr,
		git:       gitProv,
		prCreator: prCreator,
		llm:       llm,
		cmdRunner: cmdRunner,
		costCtrl:  costCtrl,
		scheduler: scheduler,
		log:       log,
		config:    config,
	}
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./internal/daemon/...`
Expected: Success (no errors)

**Step 3: Commit**

```bash
git add internal/daemon/orchestrator.go
git commit -m "feat(daemon): add Orchestrator struct and TicketProcessor interface"
```

---

### Task 2: Orchestrator — ProcessTicket Implementation

**Files:**
- Modify: `internal/daemon/orchestrator.go`

**Step 1: Implement ProcessTicket with full lifecycle**

Add to `internal/daemon/orchestrator.go`:

```go
// ProcessTicket runs the full lifecycle for a single ticket:
// pickup -> plan -> reserve files -> DAG execute -> rebase -> PR -> awaiting_merge
func (o *Orchestrator) ProcessTicket(ctx context.Context, ticket models.Ticket) (err error) {
	log := o.log.With().Str("ticket_id", ticket.ID).Str("external_id", ticket.ExternalID).Logger()

	// Deferred error handler: on any error, mark failed + comment + release files
	defer func() {
		if err != nil {
			log.Error().Err(err).Msg("ticket processing failed")
			_ = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusFailed)
			_ = o.tracker.AddComment(ctx, ticket.ExternalID, fmt.Sprintf("Foreman failed: %s", err.Error()))
			_ = o.scheduler.Release(ctx, ticket.ID)
		}
	}()

	// 1. Transition to planning
	if err = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPlanning); err != nil {
		return fmt.Errorf("updating status to planning: %w", err)
	}
	_ = o.tracker.AddComment(ctx, ticket.ExternalID, "Foreman picked up this ticket")
	log.Info().Msg("ticket picked up, planning")

	// 2. Check cost budgets before starting
	dailyCost, _ := o.db.GetDailyCost(ctx, time.Now().Format("2006-01-02"))
	if budgetErr := o.costCtrl.CheckDailyBudget(dailyCost); budgetErr != nil {
		return fmt.Errorf("daily budget exceeded: %w", budgetErr)
	}
	monthlyCost, _ := o.db.GetMonthlyCost(ctx, time.Now().Format("2006-01"))
	if budgetErr := o.costCtrl.CheckMonthlyBudget(monthlyCost); budgetErr != nil {
		return fmt.Errorf("monthly budget exceeded: %w", budgetErr)
	}

	// 3. Ensure repo is ready
	workDir := o.config.WorkDir
	if err = o.git.EnsureRepo(ctx, workDir); err != nil {
		return fmt.Errorf("ensuring repo: %w", err)
	}

	// 4. Check ticket clarity (if clarification enabled)
	if o.config.EnableClarification {
		p := pipeline.NewPipeline(pipeline.PipelineConfig{
			EnableClarification: true,
		})
		clear, clarErr := p.CheckTicketClarity(&ticket)
		if clarErr != nil {
			return fmt.Errorf("checking ticket clarity: %w", clarErr)
		}
		if !clear {
			_ = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusClarificationNeeded)
			if o.config.ClarificationLabel != "" {
				_ = o.tracker.AddLabel(ctx, ticket.ExternalID, o.config.ClarificationLabel)
			}
			_ = o.tracker.AddComment(ctx, ticket.ExternalID, "Foreman needs clarification on this ticket before proceeding.")
			log.Info().Msg("ticket needs clarification")
			err = nil // Not a failure — clear the deferred handler
			return nil
		}
	}

	// 5. Create branch
	branchName := fmt.Sprintf("%s%s", o.config.BranchPrefix, ticket.ExternalID)
	if err = o.git.CreateBranch(ctx, workDir, branchName); err != nil {
		return fmt.Errorf("creating branch %s: %w", branchName, err)
	}

	// 6. Plan
	planner := pipeline.NewPlanner(o.llm, &models.LimitsConfig{
		ContextTokenBudget:     o.config.ContextTokenBudget,
		MaxTasksPerTicket:      50,
		MaxImplementationRetries: o.config.MaxImplementRetries,
		MaxSpecReviewCycles:    o.config.MaxSpecReviewCycles,
		MaxQualityReviewCycles: o.config.MaxQualityReviewCycles,
		EnableTDDVerification:  o.config.EnableTDDVerification,
		EnablePartialPR:        o.config.EnablePartialPR,
	})
	planResult, err := planner.Plan(ctx, workDir, &ticket)
	if err != nil {
		return fmt.Errorf("planning: %w", err)
	}
	if planResult.Status != "OK" {
		return fmt.Errorf("planner returned status %s: %s", planResult.Status, planResult.Message)
	}
	log.Info().Int("task_count", len(planResult.Tasks)).Msg("plan generated")

	// 7. Convert planned tasks to model tasks and persist
	tasks := make([]models.Task, len(planResult.Tasks))
	for i, pt := range planResult.Tasks {
		tasks[i] = models.Task{
			TicketID:            ticket.ID,
			Title:               pt.Title,
			Description:         pt.Description,
			AcceptanceCriteria:  pt.AcceptanceCriteria,
			TestAssertions:      pt.TestAssertions,
			FilesToRead:         pt.FilesToRead,
			FilesToModify:       pt.FilesToModify,
			EstimatedComplexity: pt.EstimatedComplexity,
			DependsOn:           pt.DependsOn,
			Sequence:            i + 1,
			Status:              models.TaskStatusPending,
		}
	}
	if err = o.db.CreateTasks(ctx, ticket.ID, tasks); err != nil {
		return fmt.Errorf("creating tasks: %w", err)
	}

	// 8. Reserve files
	allFiles := collectFilesToModify(planResult.Tasks)
	reserveErr := o.scheduler.TryReserve(ctx, ticket.ID, allFiles)
	if reserveErr != nil {
		// File conflict — put ticket back to queued for retry next cycle
		_ = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued)
		log.Info().Err(reserveErr).Msg("file conflict, re-queuing")
		err = nil // Not a failure
		return nil
	}
	defer o.scheduler.Release(ctx, ticket.ID)

	// 9. Transition to implementing
	if err = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusImplementing); err != nil {
		return fmt.Errorf("updating status to implementing: %w", err)
	}

	// 10. Re-read tasks from DB (now have IDs assigned)
	dbTasks, err := o.db.ListTasks(ctx, ticket.ID)
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}

	// 11. Execute via DAG executor
	taskRunner := pipeline.NewPipelineTaskRunner(
		o.llm, o.db, o.git, o.cmdRunner,
		pipeline.TaskRunnerConfig{
			Models:                   o.config.Models,
			WorkDir:                  workDir,
			CodebasePatterns:         formatCodebasePatterns(planResult.CodebasePatterns),
			TestCommand:              o.config.TestCommand,
			MaxImplementationRetries: o.config.MaxImplementRetries,
			MaxSpecReviewCycles:      o.config.MaxSpecReviewCycles,
			MaxQualityReviewCycles:   o.config.MaxQualityReviewCycles,
			MaxLlmCallsPerTask:       o.config.MaxLlmCallsPerTask,
			EnableTDDVerification:    o.config.EnableTDDVerification,
		},
	)

	dagAdapter := pipeline.NewDAGTaskAdapter(taskRunner, o.db, ticket.ID)
	dagTasks := buildDAGTasks(dbTasks)
	executor := NewDAGExecutor(dagAdapter, o.config.MaxParallelTasks, time.Duration(o.config.TaskTimeoutMinutes)*time.Minute)

	results := executor.Execute(ctx, dagTasks)

	// 12. Analyze results
	var failedTasks []string
	successCount := 0
	for _, r := range results {
		switch r.Status {
		case models.TaskStatusDone:
			successCount++
		case models.TaskStatusFailed:
			failedTasks = append(failedTasks, r.TaskID)
		}
	}

	totalTasks := len(dbTasks)
	allPassed := successCount == totalTasks && len(failedTasks) == 0

	if !allPassed && !o.config.EnablePartialPR {
		return fmt.Errorf("%d/%d tasks failed: %v", len(failedTasks), totalTasks, failedTasks)
	}

	if successCount == 0 {
		return fmt.Errorf("all %d tasks failed", totalTasks)
	}

	// 13. Rebase if configured
	if o.config.RebaseBeforePR {
		rebaseResult, rebaseErr := o.git.RebaseOnto(ctx, workDir, o.config.DefaultBranch)
		if rebaseErr != nil {
			return fmt.Errorf("rebase: %w", rebaseErr)
		}
		if !rebaseResult.Success {
			return fmt.Errorf("rebase conflicts: %v", rebaseResult.ConflictFiles)
		}
	}

	// 14. Push
	if o.config.AutoPush {
		if err = o.git.Push(ctx, workDir, branchName); err != nil {
			return fmt.Errorf("pushing branch: %w", err)
		}
	}

	// 15. Create PR
	isPartial := !allPassed
	taskSummaries := buildTaskSummaries(dbTasks, results)
	prBody := git.FormatPRBody(git.PRBodyInput{
		TicketExternalID: ticket.ExternalID,
		TicketTitle:      ticket.Title,
		TaskSummaries:    taskSummaries,
		IsPartial:        isPartial,
	})

	prReq := git.PrRequest{
		Title:      fmt.Sprintf("[%s] %s", ticket.ExternalID, ticket.Title),
		Body:       prBody,
		HeadBranch: branchName,
		BaseBranch: o.config.DefaultBranch,
		Reviewers:  o.config.PRReviewers,
		Draft:      o.config.PRDraft,
	}
	prResp, err := o.prCreator.CreatePR(ctx, prReq)
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	// 16. Update ticket with PR info
	_ = o.tracker.AttachPR(ctx, ticket.ExternalID, prResp.HTMLURL)

	if isPartial {
		_ = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPartial)
		_ = o.tracker.AddComment(ctx, ticket.ExternalID,
			fmt.Sprintf("Foreman created a partial PR (%d/%d tasks completed): %s", successCount, totalTasks, prResp.HTMLURL))
	} else {
		_ = o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusAwaitingMerge)
		_ = o.tracker.AddComment(ctx, ticket.ExternalID,
			fmt.Sprintf("Foreman created PR: %s", prResp.HTMLURL))
	}

	log.Info().Str("pr_url", prResp.HTMLURL).Bool("partial", isPartial).Msg("PR created")
	err = nil // Clear for deferred handler
	return nil
}

// collectFilesToModify gathers all files from planned tasks.
func collectFilesToModify(tasks []pipeline.PlannedTask) []string {
	seen := make(map[string]bool)
	var files []string
	for _, t := range tasks {
		for _, f := range t.FilesToModify {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return files
}

// formatCodebasePatterns formats CodebasePatterns into a string for the task runner.
func formatCodebasePatterns(cp pipeline.CodebasePatterns) string {
	return fmt.Sprintf("Language: %s, Framework: %s, TestRunner: %s, Style: %s",
		cp.Language, cp.Framework, cp.TestRunner, cp.StyleNotes)
}

// buildDAGTasks converts DB tasks to DAG tasks for the executor.
func buildDAGTasks(tasks []models.Task) []DAGTask {
	dagTasks := make([]DAGTask, len(tasks))
	// Build title->ID map for dependency resolution
	titleToID := make(map[string]string, len(tasks))
	for _, t := range tasks {
		titleToID[t.Title] = t.ID
	}
	for i, t := range tasks {
		deps := make([]string, 0, len(t.DependsOn))
		for _, depTitle := range t.DependsOn {
			if id, ok := titleToID[depTitle]; ok {
				deps = append(deps, id)
			}
		}
		dagTasks[i] = DAGTask{
			ID:        t.ID,
			DependsOn: deps,
		}
	}
	return dagTasks
}

// buildTaskSummaries creates PR task summary from results.
func buildTaskSummaries(tasks []models.Task, results map[string]TaskResult) []git.PRTaskSummary {
	summaries := make([]git.PRTaskSummary, len(tasks))
	for i, t := range tasks {
		status := "pending"
		if r, ok := results[t.ID]; ok {
			status = string(r.Status)
		}
		summaries[i] = git.PRTaskSummary{
			Title:  t.Title,
			Status: status,
		}
	}
	return summaries
}
```

**Step 2: Check if DAGTaskAdapter exists, or if we need to define it**

Run: `grep -n "DAGTaskAdapter\|NewDAGTaskAdapter" /Users/canh/Projects/Indies/Foreman/internal/pipeline/`
If it doesn't exist, we need to create a thin adapter. Check and adjust.

**Step 3: Verify it compiles**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./internal/daemon/...`
Expected: Success. If there are missing types (like DAGTaskAdapter), create them in Task 3.

**Step 4: Commit**

```bash
git add internal/daemon/orchestrator.go
git commit -m "feat(daemon): implement ProcessTicket lifecycle in Orchestrator"
```

---

### Task 3: DAGTaskAdapter (if missing)

**Files:**
- Create: `internal/pipeline/dag_adapter.go` (only if `DAGTaskAdapter` doesn't exist)

**Step 1: Check if DAGTaskAdapter exists**

Run: `grep -rn "DAGTaskAdapter" /Users/canh/Projects/Indies/Foreman/internal/`

If it exists, skip this task entirely. If not:

**Step 2: Create adapter**

```go
// internal/pipeline/dag_adapter.go
package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/models"
)
```

Note: This may cause a circular import (daemon imports pipeline, pipeline imports daemon). If so, define the `TaskRunner` interface in a shared location or use the existing `daemon.TaskRunner` interface — the adapter should live in `internal/daemon/` instead:

```go
// internal/daemon/dag_adapter.go
package daemon

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// PipelineRunner is the interface the DAG adapter calls for each task.
type PipelineRunner interface {
	RunTask(ctx context.Context, task *models.Task) error
}

// TaskDB is the subset of db.Database the adapter needs.
type TaskDB interface {
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
}

// DAGTaskAdapter bridges PipelineTaskRunner to the DAGExecutor's TaskRunner interface.
type DAGTaskAdapter struct {
	runner   PipelineRunner
	db       TaskDB
	ticketID string
}

// NewDAGTaskAdapter creates a new adapter.
func NewDAGTaskAdapter(runner PipelineRunner, db TaskDB, ticketID string) *DAGTaskAdapter {
	return &DAGTaskAdapter{runner: runner, db: db, ticketID: ticketID}
}

// Run implements TaskRunner.Run for the DAG executor.
func (a *DAGTaskAdapter) Run(ctx context.Context, taskID string) TaskResult {
	// Find the task from DB
	tasks, err := a.db.ListTasks(ctx, a.ticketID)
	if err != nil {
		return TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: err}
	}

	var task *models.Task
	for i := range tasks {
		if tasks[i].ID == taskID {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		return TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: fmt.Errorf("task %s not found", taskID)}
	}

	_ = a.db.UpdateTaskStatus(ctx, taskID, models.TaskStatusImplementing)

	if err := a.runner.RunTask(ctx, task); err != nil {
		_ = a.db.UpdateTaskStatus(ctx, taskID, models.TaskStatusFailed)
		return TaskResult{TaskID: taskID, Status: models.TaskStatusFailed, Error: err}
	}

	_ = a.db.UpdateTaskStatus(ctx, taskID, models.TaskStatusDone)
	return TaskResult{TaskID: taskID, Status: models.TaskStatusDone}
}
```

**Step 3: Verify it compiles**

Run: `go build ./internal/daemon/...`

**Step 4: Commit**

```bash
git add internal/daemon/dag_adapter.go
git commit -m "feat(daemon): add DAGTaskAdapter bridging pipeline to DAG executor"
```

---

### Task 4: Wire Daemon Poll Loop

**Files:**
- Modify: `internal/daemon/daemon.go`

**Step 1: Add TicketProcessor field, SetOrchestrator, WaitForDrain, and replace atomic with WaitGroup**

Replace the contents of `daemon.go`. Key changes:

1. Add `orchestrator TicketProcessor` field
2. Add `wg sync.WaitGroup` field (keep `active atomic.Int32` for Status reporting)
3. Add `SetOrchestrator(tp TicketProcessor)` method
4. Add `WaitForDrain(ctx context.Context)` method
5. Wire the poll loop body

```go
// Add to Daemon struct:
orchestrator TicketProcessor
wg           sync.WaitGroup

// Add setter:
func (d *Daemon) SetOrchestrator(tp TicketProcessor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.orchestrator = tp
}

// Add WaitForDrain:
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
```

**Step 2: Replace the poll loop stub (lines 147-156)**

```go
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
```

**Step 3: Add helper methods**

```go
// ingestFromTracker fetches ready tickets and inserts new ones into the DB.
func (d *Daemon) ingestFromTracker(ctx context.Context, database db.Database, tr tracker.IssueTracker) {
	tickets, err := tr.FetchReadyTickets(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to fetch ready tickets from tracker")
		return
	}

	for _, t := range tickets {
		existing, _ := database.GetTicketByExternalID(ctx, t.ExternalID)
		if existing != nil {
			continue // Already known
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

// processQueuedTickets spawns bounded goroutines for queued tickets.
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
```

**Step 4: Add ClarificationTimeoutHours and ClarificationLabel to DaemonConfig**

```go
type DaemonConfig struct {
	RunnerMode                 string
	ClarificationLabel         string
	PollIntervalSecs           int
	IdlePollIntervalSecs       int
	MaxParallelTickets         int
	MaxParallelTasks           int
	TaskTimeoutMinutes         int
	MergeCheckIntervalSecs     int
	ClarificationTimeoutHours  int
}
```

**Step 5: Verify it compiles**

Run: `go build ./internal/daemon/...`

**Step 6: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat(daemon): wire poll loop with tracker ingestion and bounded ticket processing"
```

---

### Task 5: Orchestrator Unit Tests

**Files:**
- Create: `internal/daemon/orchestrator_test.go`

**Step 1: Create mock interfaces and test helpers**

```go
package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/canhta/foreman/internal/runner"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations ---

type mockDB struct {
	tickets        map[string]*models.Ticket
	tasks          map[string][]models.Task
	statusUpdates  []statusUpdate
	ticketCost     float64
	dailyCost      float64
	monthlyCost    float64
	reservedFiles  map[string]string
	createTasksErr error
}

type statusUpdate struct {
	id     string
	status models.TicketStatus
}

// Implement db.Database methods needed by orchestrator...
// (Each test creates a fresh mockDB with the methods it needs)

type mockTracker struct {
	comments []trackerComment
	labels   []trackerLabel
	prURLs   []string
}

type trackerComment struct {
	externalID string
	comment    string
}

type trackerLabel struct {
	externalID string
	label      string
	added      bool
}

type mockGit struct {
	ensureRepoErr   error
	createBranchErr error
	pushErr         error
	rebaseResult    *git.RebaseResult
}

type mockPRCreator struct {
	lastReq  git.PrRequest
	response *git.PrResponse
	err      error
}

type mockLLM struct {
	responses []models.LlmResponse
	callIdx   int
	err       error
}

type mockCmdRunner struct{}
```

**Step 2: Write TestProcessTicket_HappyPath**

Test: Creates orchestrator with all mocks, processes a ticket, asserts:
- Status transitions: queued -> planning -> implementing -> awaiting_merge
- Comment posted on tracker
- PR created with correct branch name
- Files reserved and released

**Step 3: Write TestProcessTicket_ClarificationNeeded**

Test: Mock CheckTicketClarity returns false. Assert:
- Status set to clarification_needed
- Clarification label added
- No PR created

**Step 4: Write TestProcessTicket_FileConflict**

Test: Mock TryReserve returns FileConflictError. Assert:
- Status set back to queued
- No error returned (not a failure)

**Step 5: Write TestProcessTicket_PlanFailure**

Test: Mock LLM returns error during planning. Assert:
- Status set to failed
- Error comment posted
- Files released

**Step 6: Write TestProcessTicket_CostBudgetExceeded**

Test: Set daily cost above limit. Assert:
- Status set to failed
- Budget error in comment

**Step 7: Run tests**

Run: `go test ./internal/daemon/ -run TestProcessTicket -v`
Expected: All pass

**Step 8: Commit**

```bash
git add internal/daemon/orchestrator_test.go
git commit -m "test(daemon): add Orchestrator ProcessTicket unit tests"
```

---

### Task 6: Daemon Poll Loop Tests

**Files:**
- Modify: `internal/daemon/daemon_test.go` (or create if doesn't exist)

**Step 1: Write TestDaemon_PollFetchesAndQueues**

Mock tracker returns 2 tickets. Start daemon with short poll (50ms), cancel after 200ms.
Assert: Both tickets inserted into mock DB as queued.

**Step 2: Write TestDaemon_RespectsMaxParallel**

Set maxParallelTickets=1, queue 3 tickets. Assert only 1 goroutine runs at a time
(use a channel/counter in mock ProcessTicket that blocks briefly).

**Step 3: Write TestDaemon_SkipWhenPaused**

Pause daemon, run one poll cycle. Assert no tracker fetch, no processing.

**Step 4: Write TestDaemon_GracefulShutdown**

Start daemon, cancel context, call WaitForDrain. Assert it returns without hanging.

**Step 5: Write TestDaemon_DeduplicatesTickets**

Tracker returns same ticket twice across cycles. Assert only one DB insert.

**Step 6: Run tests**

Run: `go test ./internal/daemon/ -run TestDaemon -v`
Expected: All pass

**Step 7: Commit**

```bash
git add internal/daemon/daemon_test.go
git commit -m "test(daemon): add poll loop unit tests"
```

---

### Task 7: CLI Helper — loadConfigAndDB

**Files:**
- Create: `cmd/helpers.go`

**Step 1: Create helper**

```go
package cmd

import (
	"fmt"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
)

func loadConfigAndDB() (*models.Config, db.Database, error) {
	cfg, err := config.LoadFromFile("foreman.toml")
	if err != nil {
		// Fall back to defaults
		cfg, err = config.LoadDefaults()
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w — run 'foreman doctor' to validate setup", err)
		}
	}

	database, err := openDB(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w — has 'foreman start' been run at least once?", err)
	}

	return cfg, database, nil
}

func openDB(cfg *models.Config) (db.Database, error) {
	switch cfg.Database.Driver {
	case "postgres":
		return db.NewPostgresDB(cfg.Database.Postgres.URL, cfg.Database.Postgres.MaxConns)
	default:
		return db.NewSQLiteDB(cfg.Database.SQLite.Path)
	}
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`

**Step 3: Commit**

```bash
git add cmd/helpers.go
git commit -m "feat(cmd): add loadConfigAndDB helper for CLI commands"
```

---

### Task 8: Wire cmd/doctor.go

**Files:**
- Modify: `cmd/doctor.go`

**Step 1: Rewrite doctor with real checks and --quick flag**

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var quick bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Health check all configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Running health checks...")

			hasFailure := false
			check := func(name string, fn func() error) {
				fmt.Fprintf(w, "  %s... ", name)
				if err := fn(); err != nil {
					fmt.Fprintf(w, "FAIL: %s\n", err)
					hasFailure = true
				} else {
					fmt.Fprintln(w, "OK")
				}
			}

			// Always check: config + database
			cfg, err := config.LoadFromFile("foreman.toml")
			if err != nil {
				cfg, err = config.LoadDefaults()
				if err != nil {
					fmt.Fprintf(w, "  Config... FAIL: %s\n", err)
					os.Exit(1)
				}
			}
			fmt.Fprintln(w, "  Config... OK")

			check("Database", func() error {
				database, err := openDB(cfg)
				if err != nil {
					return err
				}
				return database.Close()
			})

			if quick {
				if hasFailure {
					os.Exit(1)
				}
				return nil
			}

			// Full checks
			check("LLM provider", func() error {
				provider, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
				if err != nil {
					return err
				}
				return provider.HealthCheck(ctx)
			})

			check("Issue tracker", func() error {
				tr, err := newTrackerFromConfig(cfg)
				if err != nil {
					return err
				}
				_, err = tr.FetchReadyTickets(ctx)
				return err
			})

			check("Git", func() error {
				// Validate clone URL is set
				if cfg.Git.CloneURL == "" {
					return fmt.Errorf("git.clone_url not configured")
				}
				return nil
			})

			// Config validation
			check("Config validation", func() error {
				errs := config.Validate(cfg)
				if len(errs) > 0 {
					return errs[0]
				}
				return nil
			})

			// Skills
			fmt.Fprint(w, "  Skills... ")
			skillDir := filepath.Join(".", "skills")
			if _, err := os.Stat(skillDir); os.IsNotExist(err) {
				fmt.Fprintln(w, "no skills/ directory (OK)")
			} else {
				entries, _ := os.ReadDir(skillDir)
				validCount := 0
				for _, e := range entries {
					ext := filepath.Ext(e.Name())
					if ext == ".yml" || ext == ".yaml" {
						validCount++
					}
				}
				fmt.Fprintf(w, "%d skill files found (OK)\n", validCount)
			}

			if hasFailure {
				os.Exit(1)
			}
			fmt.Fprintln(w, "\nAll checks passed.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&quick, "quick", false, "Quick check (database only, for health checks)")
	return cmd
}

// newTrackerFromConfig creates an IssueTracker from config.
// This is a local helper — the full factory lives in the start command bootstrap.
func newTrackerFromConfig(cfg *models.Config) (tracker.IssueTracker, error) {
	switch cfg.Tracker.Provider {
	case "github":
		return tracker.NewGitHubIssuesTracker(
			"https://api.github.com", cfg.LLM.Anthropic.APIKey, // TODO: use git token
			"", "", cfg.Tracker.PickupLabel,
		), nil
	default:
		return nil, fmt.Errorf("tracker provider %q not supported in doctor check", cfg.Tracker.Provider)
	}
}
```

Note: The `newTrackerFromConfig` helper needs access to the GitHub token. This should be resolved during implementation by checking how tokens are stored in config (likely `cfg.Git` or env var). Adjust accordingly.

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`

**Step 3: Commit**

```bash
git add cmd/doctor.go
git commit -m "feat(cmd): wire doctor command with real provider checks and --quick flag"
```

---

### Task 9: Wire cmd/ps.go

**Files:**
- Modify: `cmd/ps.go`

**Step 1: Rewrite with DB query and table output**

```go
package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/cobra"
)

func newPsCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			_ = cfg
			defer database.Close()

			ctx := cmd.Context()

			filter := models.TicketFilter{}
			if !showAll {
				filter.StatusIn = []models.TicketStatus{
					models.TicketStatusQueued,
					models.TicketStatusPlanning,
					models.TicketStatusPlanValidating,
					models.TicketStatusImplementing,
					models.TicketStatusReviewing,
					models.TicketStatusAwaitingMerge,
					models.TicketStatusClarificationNeeded,
					models.TicketStatusDecomposing,
				}
			}

			tickets, err := database.ListTickets(ctx, filter)
			if err != nil {
				return fmt.Errorf("listing tickets: %w", err)
			}

			if len(tickets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pipelines found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tExternal\tStatus\tDuration\tTasks")
			fmt.Fprintln(w, "--\t--------\t------\t--------\t-----")

			for _, t := range tickets {
				duration := "-"
				if t.StartedAt != nil {
					d := time.Since(*t.StartedAt)
					if t.CompletedAt != nil {
						d = t.CompletedAt.Sub(*t.StartedAt)
					}
					duration = formatDuration(d)
				}

				tasks, _ := database.ListTasks(ctx, t.ID)
				doneCount := 0
				for _, task := range tasks {
					if task.Status == models.TaskStatusDone {
						doneCount++
					}
				}
				taskStr := fmt.Sprintf("%d/%d", doneCount, len(tasks))

				short := t.ID
				if len(short) > 8 {
					short = short[:8]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					short, t.ExternalID, t.Status, duration, taskStr)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all pipelines including completed")
	return cmd
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`

**Step 3: Commit**

```bash
git add cmd/ps.go
git commit -m "feat(cmd): wire ps command to query DB and display pipeline table"
```

---

### Task 10: Wire cmd/cost.go

**Files:**
- Modify: `cmd/cost.go`

**Step 1: Rewrite with DB query and limits comparison**

```go
package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/cobra"
)

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost [today|week|month|per-ticket]",
		Short: "Show cost breakdown",
		Long:  "Show cost breakdown: today, week, month, or per-ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			ctx := cmd.Context()
			period := args[0]
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)

			switch period {
			case "today":
				cost, _ := database.GetDailyCost(ctx, time.Now().Format("2006-01-02"))
				limit := cfg.Cost.MaxCostPerDayUSD
				pct := 0.0
				if limit > 0 {
					pct = (cost / limit) * 100
				}
				fmt.Fprintln(w, "Period\tSpent\tLimit\tUsage")
				fmt.Fprintln(w, "------\t-----\t-----\t-----")
				fmt.Fprintf(w, "Today\t$%.2f\t$%.2f\t%.1f%%\n", cost, limit, pct)

			case "month":
				cost, _ := database.GetMonthlyCost(ctx, time.Now().Format("2006-01"))
				limit := cfg.Cost.MaxCostPerMonthUSD
				pct := 0.0
				if limit > 0 {
					pct = (cost / limit) * 100
				}
				fmt.Fprintln(w, "Period\tSpent\tLimit\tUsage")
				fmt.Fprintln(w, "------\t-----\t-----\t-----")
				fmt.Fprintf(w, "This month\t$%.2f\t$%.2f\t%.1f%%\n", cost, limit, pct)

			case "week":
				fmt.Fprintln(w, "Period\tSpent")
				fmt.Fprintln(w, "------\t-----")
				now := time.Now()
				total := 0.0
				for i := 6; i >= 0; i-- {
					day := now.AddDate(0, 0, -i)
					cost, _ := database.GetDailyCost(ctx, day.Format("2006-01-02"))
					total += cost
					fmt.Fprintf(w, "%s\t$%.2f\n", day.Format("Mon 01/02"), cost)
				}
				fmt.Fprintf(w, "Total\t$%.2f\n", total)

			case "per-ticket":
				tickets, _ := database.ListTickets(ctx, models.TicketFilter{})
				fmt.Fprintln(w, "Ticket\tExternal\tStatus\tCost")
				fmt.Fprintln(w, "------\t--------\t------\t----")
				for _, t := range tickets {
					cost, _ := database.GetTicketCost(ctx, t.ID)
					if cost > 0 {
						short := t.ID
						if len(short) > 8 {
							short = short[:8]
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t$%.2f\n", short, t.ExternalID, t.Status, cost)
					}
				}

			default:
				return fmt.Errorf("unknown period %q (use: today, week, month, per-ticket)", period)
			}

			return w.Flush()
		},
	}
	return cmd
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`

**Step 3: Commit**

```bash
git add cmd/cost.go
git commit -m "feat(cmd): wire cost command with DB query and budget comparison"
```

---

### Task 11: Wire cmd/start.go — Full Daemon Bootstrap

**Files:**
- Modify: `cmd/start.go`

**Step 1: Rewrite with full bootstrap**

```go
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Load config
			cfg, err := config.LoadFromFile("foreman.toml")
			if err != nil {
				cfg, err = config.LoadDefaults()
				if err != nil {
					return err
				}
			}

			// Validate
			if errs := config.Validate(cfg); len(errs) > 0 {
				for _, e := range errs {
					log.Error().Err(e).Msg("config validation error")
				}
				return errs[0]
			}

			// 2. Initialize database
			database, err := openDB(cfg)
			if err != nil {
				return err
			}
			defer database.Close()

			// 3. Initialize providers
			llmProv, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
			if err != nil {
				return err
			}

			tr, err := buildTracker(cfg)
			if err != nil {
				return err
			}

			gitProv := buildGitProvider(cfg)
			prCreator := buildPRCreator(cfg)
			prChecker := buildPRChecker(cfg)
			cmdRunner := buildCommandRunner(cfg)
			costCtrl := telemetry.NewCostController(cfg.Cost)
			scheduler := daemon.NewScheduler(database)

			// 4. Build orchestrator
			orch := daemon.NewOrchestrator(
				database, tr, gitProv, prCreator, llmProv, cmdRunner, costCtrl, scheduler,
				log.Logger,
				daemon.OrchestratorConfig{
					WorkDir:                cfg.Daemon.WorkDir,
					DefaultBranch:          cfg.Git.DefaultBranch,
					BranchPrefix:           cfg.Git.BranchPrefix,
					PRDraft:                cfg.Git.PRDraft,
					PRReviewers:            cfg.Git.PRReviewers,
					RebaseBeforePR:         cfg.Git.RebaseBeforePR,
					AutoPush:               cfg.Git.AutoPush,
					MaxParallelTasks:       cfg.Daemon.MaxParallelTasks,
					TaskTimeoutMinutes:     cfg.Daemon.TaskTimeoutMinutes,
					ClarificationLabel:     cfg.Tracker.ClarificationLabel,
					EnablePartialPR:        cfg.Limits.EnablePartialPR,
					EnableTDDVerification:  cfg.Limits.EnableTDDVerification,
					EnableClarification:    cfg.Limits.EnableClarification,
					MaxLlmCallsPerTask:    cfg.Cost.MaxLlmCallsPerTask,
					MaxImplementRetries:    cfg.Limits.MaxImplementationRetries,
					MaxSpecReviewCycles:    cfg.Limits.MaxSpecReviewCycles,
					MaxQualityReviewCycles: cfg.Limits.MaxQualityReviewCycles,
					ContextTokenBudget:     cfg.Limits.ContextTokenBudget,
					Models:                 cfg.Models,
				},
			)

			// 5. Build daemon
			d := daemon.NewDaemon(daemon.DaemonConfig{
				RunnerMode:                cfg.Runner.Mode,
				PollIntervalSecs:          cfg.Daemon.PollIntervalSecs,
				IdlePollIntervalSecs:      cfg.Daemon.IdlePollIntervalSecs,
				MaxParallelTickets:        cfg.Daemon.MaxParallelTickets,
				MaxParallelTasks:          cfg.Daemon.MaxParallelTasks,
				TaskTimeoutMinutes:        cfg.Daemon.TaskTimeoutMinutes,
				MergeCheckIntervalSecs:    cfg.Daemon.MergeCheckIntervalSecs,
				ClarificationTimeoutHours: cfg.Tracker.ClarificationTimeoutHours,
				ClarificationLabel:        cfg.Tracker.ClarificationLabel,
			})
			d.SetDB(database)
			d.SetTracker(tr)
			d.SetOrchestrator(orch)
			d.SetPRChecker(prChecker)

			hookRunner := skills.NewHookRunner(cfg.Pipeline.Hooks)
			if hookRunner != nil {
				d.SetHookRunner(hookRunner)
			}

			// 6. Signal context
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// 7. Dashboard in background
			if cfg.Dashboard.Enabled {
				reg := prometheus.NewRegistry()
				_ = telemetry.NewMetrics(reg)
				emitter := telemetry.NewEventEmitter(database)

				port := cfg.Dashboard.Port
				if port == 0 {
					port = 3333
				}
				host := cfg.Dashboard.Host
				if host == "" {
					host = "127.0.0.1"
				}

				srv := dashboard.NewServer(database, emitter, d, reg, cfg.Cost, "0.1.0", host, port)
				go func() {
					if err := srv.Start(); err != nil {
						log.Error().Err(err).Msg("dashboard server error")
					}
				}()
				go func() {
					<-ctx.Done()
					shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = srv.Shutdown(shutCtx)
				}()
			}

			// 8. Start daemon (blocks until ctx cancelled)
			log.Info().Msg("Starting Foreman daemon")
			d.Start(ctx)

			// 9. Drain active pipelines
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer drainCancel()
			d.WaitForDrain(drainCtx)
			log.Info().Msg("Foreman daemon stopped")

			return nil
		},
	}
	return cmd
}

// buildTracker creates an IssueTracker from config.
// Implement based on provider — adjust token source per provider.
func buildTracker(cfg *models.Config) (tracker.IssueTracker, error) {
	// Implementation depends on how tokens are stored.
	// This will be resolved during implementation by reading the config model.
	return nil, fmt.Errorf("TODO: implement tracker factory in start.go")
}

func buildGitProvider(cfg *models.Config) git.GitProvider {
	if cfg.Git.Backend == "gogit" {
		return git.NewGoGitProvider()
	}
	return git.NewNativeGitProvider()
}

func buildPRCreator(cfg *models.Config) git.PRCreator {
	// Parse owner/repo from clone URL
	// Return GitHubPRCreator
	return nil // TODO
}

func buildPRChecker(cfg *models.Config) git.PRChecker {
	return nil // TODO
}

func buildCommandRunner(cfg *models.Config) runner.CommandRunner {
	// Return LocalRunner or DockerRunner based on config
	return nil // TODO
}
```

Note: The `buildTracker`, `buildPRCreator`, `buildPRChecker`, and `buildCommandRunner` factory functions need to be fully implemented during this task. Check existing patterns in the codebase — there may already be factory functions. The `TODO` markers indicate where the implementer needs to wire the actual constructors using values from `cfg`.

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`

**Step 3: Commit**

```bash
git add cmd/start.go
git commit -m "feat(cmd): wire start command with full daemon bootstrap"
```

---

### Task 12: Dockerfile + docker-compose.yml Updates

**Files:**
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`

**Step 1: Update Dockerfile — add git to runtime + healthcheck**

The runtime stage should become:

```dockerfile
FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tini \
    git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/foreman /usr/local/bin/foreman

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["foreman", "doctor", "--quick"]

ENTRYPOINT ["/usr/bin/tini", "--", "foreman"]
CMD ["start"]
```

**Step 2: Update docker-compose.yml — add healthcheck**

```yaml
version: "3.9"

services:
  foreman:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: foreman
    restart: unless-stopped
    environment:
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY:-}
      OPENAI_API_KEY: ${OPENAI_API_KEY:-}
      GITHUB_TOKEN: ${GITHUB_TOKEN:-}
      FOREMAN_DASHBOARD_TOKEN: ${FOREMAN_DASHBOARD_TOKEN:-}
    volumes:
      - ./.foreman:/root/.foreman
      - ./foreman.toml:/app/foreman.toml:ro
    command: ["start"]
    ports:
      - "3333:3333"
    healthcheck:
      test: ["CMD", "foreman", "doctor", "--quick"]
      interval: 30s
      timeout: 5s
      start_period: 10s
      retries: 3
```

Note: Changed volume mount from `foreman.example.toml` to `foreman.toml` — users should create their own `foreman.toml`.

**Step 3: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "build: add git to runtime image, healthcheck, update compose"
```

---

### Task 13: Systemd Service + Install Script

**Files:**
- Create: `deploy/foreman.service`
- Create: `deploy/install-systemd.sh`

**Step 1: Create systemd service file**

```ini
[Unit]
Description=Foreman Autonomous Coding Daemon
Documentation=https://github.com/canhta/foreman
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=foreman
Group=foreman
WorkingDirectory=/var/lib/foreman
ExecStart=/usr/local/bin/foreman start
Restart=always
RestartSec=10
EnvironmentFile=-/etc/foreman/env

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/foreman
ReadWritePaths=/tmp
StateDirectory=foreman
LogsDirectory=foreman
PrivateTmp=true

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=foreman

[Install]
WantedBy=multi-user.target
```

**Step 2: Create install script**

```bash
#!/usr/bin/env bash
set -euo pipefail

# deploy/install-systemd.sh
# Installs Foreman as a systemd service on Linux.
# Usage: sudo ./deploy/install-systemd.sh [--binary /path/to/foreman]

BINARY="${1:-./foreman}"
INSTALL_DIR="/var/lib/foreman"
CONFIG_DIR="/etc/foreman"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Foreman Systemd Installer ==="

# 1. Create system user
if ! id -u foreman &>/dev/null; then
    echo "Creating foreman user..."
    useradd --system --shell /usr/sbin/nologin --home-dir "$INSTALL_DIR" foreman
fi

# 2. Install binary
echo "Installing binary to /usr/local/bin/foreman..."
cp "$BINARY" /usr/local/bin/foreman
chmod 755 /usr/local/bin/foreman

# 3. Create directories
echo "Creating directories..."
mkdir -p "$INSTALL_DIR/.foreman"
mkdir -p "$CONFIG_DIR"
chown -R foreman:foreman "$INSTALL_DIR"

# 4. Copy config template if no config exists
if [ ! -f "$INSTALL_DIR/foreman.toml" ]; then
    if [ -f "$SCRIPT_DIR/../foreman.example.toml" ]; then
        cp "$SCRIPT_DIR/../foreman.example.toml" "$INSTALL_DIR/foreman.toml"
        chown foreman:foreman "$INSTALL_DIR/foreman.toml"
        echo "Copied foreman.example.toml -> $INSTALL_DIR/foreman.toml (edit before starting)"
    fi
fi

# 5. Create env file template if not exists
if [ ! -f "$CONFIG_DIR/env" ]; then
    cat > "$CONFIG_DIR/env" <<'ENVEOF'
# Foreman environment variables
# Edit these values before starting the service.
ANTHROPIC_API_KEY=
GITHUB_TOKEN=
FOREMAN_DASHBOARD_TOKEN=
ENVEOF
    chmod 600 "$CONFIG_DIR/env"
    echo "Created $CONFIG_DIR/env (edit before starting)"
fi

# 6. Install systemd service
echo "Installing systemd service..."
cp "$SCRIPT_DIR/foreman.service" /etc/systemd/system/foreman.service
systemctl daemon-reload
systemctl enable foreman

echo ""
echo "=== Installation complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit $INSTALL_DIR/foreman.toml with your settings"
echo "  2. Edit $CONFIG_DIR/env with your API keys"
echo "  3. Run: foreman doctor"
echo "  4. Run: sudo systemctl start foreman"
echo "  5. Check: sudo journalctl -u foreman -f"
```

**Step 3: Make install script executable**

Run: `chmod +x deploy/install-systemd.sh`

**Step 4: Commit**

```bash
git add deploy/
git commit -m "deploy: add systemd service file and install script"
```

---

### Task 14: SSL Setup Script

**Files:**
- Create: `scripts/setup-ssl.sh`

**Step 1: Create the SSL setup script**

```bash
#!/usr/bin/env bash
set -euo pipefail

# scripts/setup-ssl.sh
# Sets up Nginx reverse proxy with Let's Encrypt SSL for Foreman dashboard.
#
# PREREQUISITE: Your domain's DNS A record must point to this server's IP
# before running this script. Certbot's HTTP-01 challenge will fail otherwise.
#
# Usage: sudo ./scripts/setup-ssl.sh --domain foreman.example.com --email you@email.com

DOMAIN=""
EMAIL=""
UPSTREAM="127.0.0.1:3333"

# Parse args
while [[ $# -gt 0 ]]; do
    case $1 in
        --domain) DOMAIN="$2"; shift 2 ;;
        --email)  EMAIL="$2"; shift 2 ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

if [[ -z "$DOMAIN" || -z "$EMAIL" ]]; then
    echo "Usage: $0 --domain <domain> --email <email>"
    exit 1
fi

echo "=== Foreman SSL Setup ==="
echo "Domain: $DOMAIN"
echo "Email:  $EMAIL"
echo ""

# 1. DNS pre-flight check
echo "Checking DNS..."
SERVER_IP=$(curl -4 -s ifconfig.me)
DOMAIN_IP=$(dig +short "$DOMAIN" | tail -1)
if [[ "$SERVER_IP" != "$DOMAIN_IP" ]]; then
    echo "ERROR: $DOMAIN resolves to $DOMAIN_IP but this server is $SERVER_IP"
    echo "Point your DNS A record to $SERVER_IP and wait for propagation."
    exit 1
fi
echo "DNS OK: $DOMAIN -> $SERVER_IP"

# 2. Install nginx + certbot if missing
echo "Checking dependencies..."
if ! command -v nginx &>/dev/null; then
    echo "Installing nginx..."
    apt-get update && apt-get install -y nginx
fi
if ! command -v certbot &>/dev/null; then
    echo "Installing certbot..."
    apt-get update && apt-get install -y certbot python3-certbot-nginx
fi

# 3. Write nginx config
echo "Writing nginx config..."
cat > /etc/nginx/sites-available/foreman <<NGINX
server {
    listen 80;
    server_name $DOMAIN;

    location / {
        proxy_pass http://$UPSTREAM;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }
}
NGINX

# 4. Enable site
ln -sf /etc/nginx/sites-available/foreman /etc/nginx/sites-enabled/foreman

# Remove default site if it conflicts
if [ -f /etc/nginx/sites-enabled/default ]; then
    rm /etc/nginx/sites-enabled/default
fi

echo "Testing nginx config..."
nginx -t
systemctl reload nginx

# 5. Get SSL certificate
echo "Obtaining SSL certificate..."
certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$EMAIL"

# 6. Reload nginx with SSL
systemctl reload nginx

echo ""
echo "=== SSL Setup Complete ==="
echo "Dashboard available at https://$DOMAIN"
```

**Step 2: Make executable**

Run: `chmod +x scripts/setup-ssl.sh`

**Step 3: Commit**

```bash
git add scripts/setup-ssl.sh
git commit -m "scripts: add SSL setup script with nginx reverse proxy and certbot"
```

---

### Task 15: Deployment Guide

**Files:**
- Create: `docs/deployment.md`

**Step 1: Write deployment guide**

Write `docs/deployment.md` with these sections:

1. **Prerequisites** — server specs table (1 vCPU/1GB min, 2 vCPU/2GB rec), required API keys, DNS (if using SSL)
2. **Option A: Docker Compose**
   - Clone repo, create `foreman.toml` from example, create `.env`
   - `docker compose up -d`
   - Verify: `docker compose exec foreman foreman doctor`
   - Warning: Never `docker compose down -v` (destroys DB)
3. **Option B: Systemd Native Binary**
   - Build: `CGO_ENABLED=1 go build -o foreman ./main.go`
   - Install: `sudo ./deploy/install-systemd.sh`
   - Configure: edit `/var/lib/foreman/foreman.toml` + `/etc/foreman/env`
   - Verify: `foreman doctor`
   - Start: `sudo systemctl start foreman`
4. **SSL Setup** — `sudo ./scripts/setup-ssl.sh --domain ... --email ...`
5. **Observability**
   - Logs: `docker compose logs -f` or `journalctl -u foreman -f`
   - Dashboard: `http://<ip>:3333` (with auth token)
   - Pipelines: `foreman ps --all`
   - Costs: `foreman cost today`, `foreman cost month`
6. **Updating**
   - Docker: `git pull && docker compose up --build -d` (never `down -v`)
   - Native: `git pull && go build && foreman doctor && sudo systemctl restart foreman`

**Step 2: Commit**

```bash
git add docs/deployment.md
git commit -m "docs: add production deployment guide for Docker and systemd"
```

---

### Task 16: Integration Test

**Files:**
- Create: `tests/integration/daemon_integration_test.go`

**Step 1: Write end-to-end daemon integration test**

```go
//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Uses real SQLite (in-memory) + mock tracker + mock LLM.
// Verifies: ticket ingestion -> planning -> execution -> PR creation -> status transitions.

func TestDaemon_EndToEnd(t *testing.T) {
	// 1. Setup in-memory SQLite
	database, err := db.NewSQLiteDB(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// 2. Create mock tracker that returns one ticket
	mockTracker := &mockIntegrationTracker{
		readyTickets: []tracker.Ticket{
			{
				ExternalID:  "TEST-1",
				Title:       "Add hello endpoint",
				Description: "Create a /hello endpoint that returns 'world'",
			},
		},
	}

	// 3. Create mock LLM that returns deterministic plan
	mockLLM := &mockIntegrationLLM{...}

	// 4. Create mock git + PR creator
	mockGit := &mockIntegrationGit{...}
	mockPR := &mockIntegrationPRCreator{
		response: &git.PrResponse{HTMLURL: "https://github.com/test/repo/pull/1", Number: 1},
	}

	// 5. Build orchestrator + daemon
	costCtrl := telemetry.NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 15.0,
		MaxCostPerDayUSD:    150.0,
		MaxCostPerMonthUSD:  3000.0,
	})
	scheduler := daemon.NewScheduler(database)
	orch := daemon.NewOrchestrator(database, mockTracker, mockGit, mockPR, mockLLM, nil, costCtrl, scheduler, ...)

	d := daemon.NewDaemon(daemon.DaemonConfig{
		PollIntervalSecs:   1, // Fast polling for test
		MaxParallelTickets: 1,
		MaxParallelTasks:   1,
		TaskTimeoutMinutes: 1,
	})
	d.SetDB(database)
	d.SetTracker(mockTracker)
	d.SetOrchestrator(orch)

	// 6. Start daemon
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go d.Start(ctx)

	// 7. Wait for ticket to be processed
	assert.Eventually(t, func() bool {
		ticket, _ := database.GetTicketByExternalID(ctx, "TEST-1")
		return ticket != nil && (ticket.Status == models.TicketStatusAwaitingMerge || ticket.Status == models.TicketStatusPartial)
	}, 8*time.Second, 200*time.Millisecond, "ticket should reach awaiting_merge")

	// 8. Verify PR was created
	assert.True(t, mockPR.called, "PR should have been created")

	// 9. Clean shutdown
	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	d.WaitForDrain(drainCtx)
}
```

Note: The mock implementations need to be fleshed out during implementation. The mock LLM should return a valid planner YAML response with 1-2 simple tasks and valid implementer responses.

**Step 2: Run test**

Run: `go test -tags integration ./tests/integration/ -run TestDaemon_EndToEnd -v -timeout 30s`

**Step 3: Commit**

```bash
git add tests/integration/daemon_integration_test.go
git commit -m "test(integration): add end-to-end daemon integration test"
```

---

### Task 17: Final Verification

**Step 1: Run all unit tests**

Run: `go test ./internal/daemon/... -v`
Expected: All pass

**Step 2: Run all cmd tests (if any)**

Run: `go test ./cmd/... -v`

**Step 3: Build binary**

Run: `CGO_ENABLED=1 go build -o foreman ./main.go`
Expected: Binary built successfully

**Step 4: Run linter**

Run: `golangci-lint run`
Expected: No new issues

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: All pass (integration tests skipped unless `-tags integration`)

**Step 6: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix: address lint and test issues from final verification"
```
