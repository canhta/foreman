// internal/daemon/orchestrator.go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/canhta/foreman/internal/channel"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
)

// TicketProcessor processes a single ticket through the full lifecycle.
type TicketProcessor interface {
	ProcessTicket(ctx context.Context, ticket models.Ticket) error
}

// TicketPlanner abstracts the planning phase (implemented by pipeline.Planner).
type TicketPlanner interface {
	Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlanResult, error)
}

// ClarityChecker determines if a ticket has enough detail to proceed.
type ClarityChecker interface {
	CheckTicketClarity(ticket *models.Ticket) (bool, error)
}

// DAGTaskRunnerFactory creates a TaskRunner for DAG execution from orchestrator context.
type DAGTaskRunnerFactory interface {
	Create(config TaskRunnerFactoryInput) TaskRunner
}

// TaskRunnerFactoryInput holds the parameters needed to create a task runner.
type TaskRunnerFactoryInput struct {
	TicketID                 string
	Models                   models.ModelsConfig
	WorkDir                  string
	CodebasePatterns         string
	TestCommand              string
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask       int
	EnableTDDVerification    bool
}

// PlanResult mirrors pipeline.PlannerResult without creating an import cycle.
type PlanResult struct {
	Status           string
	Message          string
	CodebasePatterns CodebasePatterns
	Tasks            []PlannedTask
}

// CodebasePatterns holds detected patterns from the codebase.
type CodebasePatterns struct {
	Language   string
	Framework  string
	TestRunner string
	StyleNotes string
}

// PlannedTask represents a single task decomposed from a ticket.
type PlannedTask struct {
	Title               string
	Description         string
	AcceptanceCriteria  []string
	TestAssertions      []string
	FilesToRead         []string
	FilesToModify       []string
	EstimatedComplexity string
	DependsOn           []string
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	Models                 models.ModelsConfig
	WorkDir                string
	DefaultBranch          string
	BranchPrefix           string
	TestCommand            string
	ClarificationLabel     string
	PRReviewers            []string
	MaxParallelTasks       int
	TaskTimeoutMinutes     int
	MaxLlmCallsPerTask     int
	MaxImplementRetries    int
	MaxSpecReviewCycles    int
	MaxQualityReviewCycles int
	ContextTokenBudget     int
	PRDraft                bool
	RebaseBeforePR         bool
	AutoPush               bool
	EnablePartialPR        bool
	EnableTDDVerification  bool
	EnableClarification    bool
}

// Orchestrator coordinates the full ticket-to-PR lifecycle.
type Orchestrator struct {
	db             db.Database
	tracker        tracker.IssueTracker
	git            git.GitProvider
	prCreator      git.PRCreator
	costCtrl       *telemetry.CostController
	scheduler      *Scheduler
	planner        TicketPlanner
	clarityChecker ClarityChecker
	runnerFactory  DAGTaskRunnerFactory
	ch             channel.Channel
	emitter        *telemetry.EventEmitter
	log            zerolog.Logger
	config         OrchestratorConfig
}

// NewOrchestrator creates an Orchestrator with all required dependencies.
func NewOrchestrator(
	database db.Database,
	issueTracker tracker.IssueTracker,
	gitProv git.GitProvider,
	prCreator git.PRCreator,
	costCtrl *telemetry.CostController,
	scheduler *Scheduler,
	planner TicketPlanner,
	clarityChecker ClarityChecker,
	runnerFactory DAGTaskRunnerFactory,
	log zerolog.Logger,
	config OrchestratorConfig,
) *Orchestrator {
	return &Orchestrator{
		db:             database,
		tracker:        issueTracker,
		git:            gitProv,
		prCreator:      prCreator,
		costCtrl:       costCtrl,
		scheduler:      scheduler,
		planner:        planner,
		clarityChecker: clarityChecker,
		runnerFactory:  runnerFactory,
		log:            log.With().Str("component", "orchestrator").Logger(),
		config:         config,
	}
}

// SetChannel attaches a messaging channel for lifecycle notifications.
func (o *Orchestrator) SetChannel(ch channel.Channel) {
	o.ch = ch
}

// SetEventEmitter attaches an event emitter for dashboard event streaming.
func (o *Orchestrator) SetEventEmitter(e *telemetry.EventEmitter) {
	o.emitter = e
}

// emitEvent broadcasts a lifecycle event to the dashboard (no-op if emitter not set).
func (o *Orchestrator) emitEvent(ctx context.Context, ticketID, eventType, severity, message string) {
	if o.emitter == nil {
		return
	}
	o.emitter.Emit(ctx, ticketID, "", eventType, severity, message, nil)
}

func (o *Orchestrator) notify(ctx context.Context, ticket models.Ticket, msg string) {
	if o.ch == nil || ticket.ChannelSenderID == "" {
		return
	}
	if err := o.ch.Send(ctx, ticket.ChannelSenderID, msg); err != nil {
		o.log.Warn().Err(err).Str("ticket", ticket.ID).Msg("channel notify failed")
	}
}

// ProcessTicket implements the full ticket lifecycle:
// queued -> planning -> implementing -> PR -> awaiting_merge.
func (o *Orchestrator) ProcessTicket(ctx context.Context, ticket models.Ticket) error {
	log := o.log.With().
		Str("ticket_id", ticket.ID).
		Str("external_id", ticket.ExternalID).
		Logger()

	// Deferred error handler: on error mark failed + comment + release.
	var returnErr error
	defer func() {
		if returnErr != nil {
			log.Error().Err(returnErr).Msg("ticket processing failed")
			o.emitEvent(ctx, ticket.ID, "ticket_failed", "error", returnErr.Error())
			o.notify(ctx, ticket, fmt.Sprintf("Ticket #%s failed: %s", ticket.ID, returnErr.Error()))

			if dbErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusFailed); dbErr != nil {
				log.Error().Err(dbErr).Msg("failed to update ticket status to failed")
			}

			comment := fmt.Sprintf("Foreman encountered an error processing this ticket: %s", returnErr)
			if commentErr := o.tracker.AddComment(ctx, ticket.ExternalID, comment); commentErr != nil {
				log.Error().Err(commentErr).Msg("failed to add failure comment")
			}

			if releaseErr := o.scheduler.Release(ctx, ticket.ID); releaseErr != nil {
				log.Error().Err(releaseErr).Msg("failed to release scheduler reservation")
			}
		}
	}()

	// Status: queued -> planning
	if err := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPlanning); err != nil {
		returnErr = fmt.Errorf("update status to planning: %w", err)
		return returnErr
	}

	if err := o.tracker.AddComment(ctx, ticket.ExternalID, "Foreman picked up this ticket"); err != nil {
		log.Warn().Err(err).Msg("failed to add pickup comment")
	}
	o.notify(ctx, ticket, fmt.Sprintf("Ticket #%s picked up — planning...", ticket.ID))
	o.emitEvent(ctx, ticket.ID, "ticket_picked_up", "info", fmt.Sprintf("Ticket %q picked up — planning...", ticket.Title))

	// Check cost budgets.
	if returnErr = o.checkCostBudgets(ctx); returnErr != nil {
		return returnErr
	}

	// Ensure repo is cloned / up-to-date.
	if err := o.git.EnsureRepo(ctx, o.config.WorkDir); err != nil {
		returnErr = fmt.Errorf("ensure repo: %w", err)
		return returnErr
	}
	o.emitEvent(ctx, ticket.ID, "repo_ready", "info", "Repository ready")

	// Check ticket clarity (if enabled).
	if o.config.EnableClarification {
		clear, err := o.clarityChecker.CheckTicketClarity(&ticket)
		if err != nil {
			returnErr = fmt.Errorf("check ticket clarity: %w", err)
			return returnErr
		}
		if !clear {
			log.Info().Msg("ticket needs clarification")
			if err := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusClarificationNeeded); err != nil {
				returnErr = fmt.Errorf("update status to clarification_needed: %w", err)
				return returnErr
			}
			if o.config.ClarificationLabel != "" {
				if err := o.tracker.AddLabel(ctx, ticket.ExternalID, o.config.ClarificationLabel); err != nil {
					log.Warn().Err(err).Msg("failed to add clarification label")
				}
			}
			if err := o.tracker.AddComment(ctx, ticket.ExternalID,
				"Foreman needs more detail to proceed. Please add a clearer description or acceptance criteria."); err != nil {
				log.Warn().Err(err).Msg("failed to add clarification comment")
			}
			o.notify(ctx, ticket, fmt.Sprintf("Question about ticket #%s: needs more detail to proceed.", ticket.ID))
			// Not an error — will be retried after clarification.
			returnErr = nil
			return nil
		}
	}

	// Create feature branch.
	branchName := o.config.BranchPrefix + ticket.ExternalID
	if err := o.git.CreateBranch(ctx, o.config.WorkDir, branchName); err != nil {
		returnErr = fmt.Errorf("create branch %s: %w", branchName, err)
		return returnErr
	}

	// Plan the ticket.
	o.emitEvent(ctx, ticket.ID, "planning_started", "info", "Planning ticket...")
	planResult, err := o.planner.Plan(ctx, o.config.WorkDir, &ticket)
	if err != nil {
		returnErr = fmt.Errorf("planning: %w", err)
		return returnErr
	}

	if planResult.Status != "OK" {
		returnErr = fmt.Errorf("planner returned non-OK status: %s — %s", planResult.Status, planResult.Message)
		return returnErr
	}
	o.emitEvent(ctx, ticket.ID, "planning_done", "info", fmt.Sprintf("Plan ready: %d tasks", len(planResult.Tasks)))

	// Convert PlannedTask -> models.Task and persist.
	tasks := make([]models.Task, len(planResult.Tasks))
	for i, pt := range planResult.Tasks {
		tasks[i] = models.Task{
			Title:               pt.Title,
			Description:         pt.Description,
			FilesToModify:       pt.FilesToModify,
			FilesToRead:         pt.FilesToRead,
			TestAssertions:      pt.TestAssertions,
			AcceptanceCriteria:  pt.AcceptanceCriteria,
			DependsOn:           pt.DependsOn,
			EstimatedComplexity: pt.EstimatedComplexity,
			Status:              models.TaskStatusPending,
			Sequence:            i + 1,
		}
	}

	if createErr := o.db.CreateTasks(ctx, ticket.ID, tasks); createErr != nil {
		returnErr = fmt.Errorf("create tasks: %w", createErr)
		return returnErr
	}

	// Reserve files via scheduler.
	filesToReserve := collectFilesToModify(planResult.Tasks)
	if reserveErr := o.scheduler.TryReserve(ctx, ticket.ID, filesToReserve); reserveErr != nil {
		var conflictErr *FileConflictError
		if errors.As(reserveErr, &conflictErr) {
			log.Info().Str("conflicts", conflictErr.Error()).Msg("file conflict, requeueing ticket")
			if dbErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); dbErr != nil {
				returnErr = fmt.Errorf("requeue after conflict: %w", dbErr)
				return returnErr
			}
			// Not an error — will retry next cycle.
			returnErr = nil
			return nil
		}
		returnErr = fmt.Errorf("reserve files: %w", reserveErr)
		return returnErr
	}

	// Status: planning -> implementing
	if implErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusImplementing); implErr != nil {
		returnErr = fmt.Errorf("update status to implementing: %w", implErr)
		return returnErr
	}

	o.notify(ctx, ticket, fmt.Sprintf("Implementing %d tasks for ticket #%s...", len(tasks), ticket.ID))
	o.emitEvent(ctx, ticket.ID, "implementing_started", "info", fmt.Sprintf("Starting implementation of %d tasks", len(tasks)))

	// Reload tasks from DB to get assigned IDs.
	dbTasks, err := o.db.ListTasks(ctx, ticket.ID)
	if err != nil {
		returnErr = fmt.Errorf("list tasks: %w", err)
		return returnErr
	}

	// Build task runner via factory.
	codebasePatterns := formatCodebasePatterns(planResult.CodebasePatterns)
	dagRunner := o.runnerFactory.Create(TaskRunnerFactoryInput{
		TicketID:                 ticket.ID,
		Models:                   o.config.Models,
		WorkDir:                  o.config.WorkDir,
		CodebasePatterns:         codebasePatterns,
		TestCommand:              o.config.TestCommand,
		MaxImplementationRetries: o.config.MaxImplementRetries,
		MaxSpecReviewCycles:      o.config.MaxSpecReviewCycles,
		MaxQualityReviewCycles:   o.config.MaxQualityReviewCycles,
		MaxLlmCallsPerTask:       o.config.MaxLlmCallsPerTask,
		EnableTDDVerification:    o.config.EnableTDDVerification,
	})

	// Build DAG tasks (resolve title->ID dependencies).
	dagTasks := buildDAGTasks(dbTasks)

	// Create executor and run.
	timeout := time.Duration(o.config.TaskTimeoutMinutes) * time.Minute
	executor := NewDAGExecutor(dagRunner, o.config.MaxParallelTasks, timeout)

	log.Info().Int("task_count", len(dagTasks)).Msg("starting DAG execution")
	results := executor.Execute(ctx, dagTasks)

	// Analyze results.
	doneCount, failedCount, skippedCount := analyzeResults(results)
	totalCount := len(results)
	log.Info().
		Int("done", doneCount).
		Int("failed", failedCount).
		Int("skipped", skippedCount).
		Int("total", totalCount).
		Msg("DAG execution complete")

	isPartial := failedCount > 0 && doneCount > 0

	if doneCount == 0 {
		returnErr = fmt.Errorf("all %d tasks failed", totalCount)
		return returnErr
	}

	if failedCount > 0 && !o.config.EnablePartialPR {
		returnErr = fmt.Errorf("%d of %d tasks failed and partial PRs are disabled", failedCount, totalCount)
		return returnErr
	}

	if failedCount > 0 {
		o.emitEvent(ctx, ticket.ID, "tasks_partial", "warning", fmt.Sprintf("%d/%d tasks succeeded (partial)", doneCount, totalCount))
	} else {
		o.emitEvent(ctx, ticket.ID, "tasks_done", "success", fmt.Sprintf("All %d tasks completed", doneCount))
	}

	// Rebase if configured.
	if o.config.RebaseBeforePR {
		rebaseResult, rebaseErr := o.git.RebaseOnto(ctx, o.config.WorkDir, o.config.DefaultBranch)
		if rebaseErr != nil {
			returnErr = fmt.Errorf("rebase onto %s: %w", o.config.DefaultBranch, rebaseErr)
			return returnErr
		}
		if !rebaseResult.Success {
			returnErr = fmt.Errorf("rebase failed with conflicts in: %s",
				strings.Join(rebaseResult.ConflictFiles, ", "))
			return returnErr
		}
	}

	// Push if AutoPush.
	if o.config.AutoPush {
		if pushErr := o.git.Push(ctx, o.config.WorkDir, branchName); pushErr != nil {
			returnErr = fmt.Errorf("push branch %s: %w", branchName, pushErr)
			return returnErr
		}
	}

	// Build task summaries for PR body.
	taskSummaries := buildTaskSummaries(dbTasks, results)

	// Find first failed task info for partial PR.
	failedTask, failureReason := findFirstFailure(dbTasks, results)

	// Format PR body.
	prBody := git.FormatPRBody(git.PRBodyInput{
		TicketExternalID: ticket.ExternalID,
		TicketTitle:      ticket.Title,
		TaskSummaries:    taskSummaries,
		IsPartial:        isPartial,
		FailedTask:       failedTask,
		FailureReason:    failureReason,
	})

	// Create PR request.
	var prReq git.PrRequest
	if isPartial {
		prReq = git.NewPartialPRRequest(ticket.ExternalID, ticket.Title, branchName, o.config.DefaultBranch, o.config.PRReviewers)
	} else {
		prReq = git.NewPRRequest(ticket.ExternalID, ticket.Title, branchName, o.config.DefaultBranch, o.config.PRDraft, o.config.PRReviewers)
	}
	prReq.Body = prBody

	// Create the PR.
	prResp, err := o.prCreator.CreatePR(ctx, prReq)
	if err != nil {
		returnErr = fmt.Errorf("create PR: %w", err)
		return returnErr
	}

	// Attach PR to tracker.
	if err := o.tracker.AttachPR(ctx, ticket.ExternalID, prResp.HTMLURL); err != nil {
		log.Warn().Err(err).Msg("failed to attach PR to tracker")
	}

	// Update ticket status.
	finalStatus := models.TicketStatusAwaitingMerge
	if isPartial {
		finalStatus = models.TicketStatusPartial
	}
	if err := o.db.UpdateTicketStatus(ctx, ticket.ID, finalStatus); err != nil {
		returnErr = fmt.Errorf("update status to %s: %w", finalStatus, err)
		return returnErr
	}

	// Release file reservations. Return the error directly (not via returnErr)
	// so the deferred handler does not incorrectly mark the ticket as failed
	// when the PR has already been created successfully.
	if err := o.scheduler.Release(ctx, ticket.ID); err != nil {
		log.Error().Err(err).Msg("failed to release file reservations after PR")
		return fmt.Errorf("release reservations: %w", err)
	}

	o.notify(ctx, ticket, fmt.Sprintf("PR opened for ticket #%s: %s", ticket.ID, prResp.HTMLURL))
	o.emitEvent(ctx, ticket.ID, "pr_created", "success", fmt.Sprintf("PR created: %s", prResp.HTMLURL))

	// Comment with PR URL.
	prComment := fmt.Sprintf("Foreman created a PR: %s", prResp.HTMLURL)
	if isPartial {
		prComment = fmt.Sprintf("Foreman created a **partial** PR (%d/%d tasks succeeded): %s",
			doneCount, totalCount, prResp.HTMLURL)
	}
	if err := o.tracker.AddComment(ctx, ticket.ExternalID, prComment); err != nil {
		log.Warn().Err(err).Msg("failed to add PR comment to tracker")
	}

	log.Info().
		Str("pr_url", prResp.HTMLURL).
		Int("pr_number", prResp.Number).
		Str("status", string(finalStatus)).
		Msg("ticket processing complete")

	return nil
}

// checkCostBudgets verifies daily and monthly cost limits.
func (o *Orchestrator) checkCostBudgets(ctx context.Context) error {
	now := time.Now()

	dailyCost, err := o.db.GetDailyCost(ctx, now.Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("get daily cost: %w", err)
	}
	if budgetErr := o.costCtrl.CheckDailyBudget(dailyCost); budgetErr != nil {
		return fmt.Errorf("daily budget check: %w", budgetErr)
	}

	monthlyCost, err := o.db.GetMonthlyCost(ctx, now.Format("2006-01"))
	if err != nil {
		return fmt.Errorf("get monthly cost: %w", err)
	}
	if err := o.costCtrl.CheckMonthlyBudget(monthlyCost); err != nil {
		return fmt.Errorf("monthly budget check: %w", err)
	}

	return nil
}

// collectFilesToModify gathers all unique files from planned tasks.
func collectFilesToModify(tasks []PlannedTask) []string {
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

// formatCodebasePatterns converts CodebasePatterns to a summary string.
func formatCodebasePatterns(cp CodebasePatterns) string {
	var parts []string
	if cp.Language != "" {
		parts = append(parts, "Language: "+cp.Language)
	}
	if cp.Framework != "" {
		parts = append(parts, "Framework: "+cp.Framework)
	}
	if cp.TestRunner != "" {
		parts = append(parts, "TestRunner: "+cp.TestRunner)
	}
	if cp.StyleNotes != "" {
		parts = append(parts, "Style: "+cp.StyleNotes)
	}
	return strings.Join(parts, "; ")
}

// buildDAGTasks maps task title-based dependencies to ID-based DAGTask entries.
func buildDAGTasks(tasks []models.Task) []DAGTask {
	// Build title -> ID lookup.
	titleToID := make(map[string]string, len(tasks))
	for _, t := range tasks {
		titleToID[t.Title] = t.ID
	}

	dagTasks := make([]DAGTask, 0, len(tasks))
	for _, t := range tasks {
		var deps []string
		for _, depTitle := range t.DependsOn {
			if depID, ok := titleToID[depTitle]; ok {
				deps = append(deps, depID)
			}
		}
		dagTasks = append(dagTasks, DAGTask{
			ID:        t.ID,
			DependsOn: deps,
		})
	}
	return dagTasks
}

// buildTaskSummaries creates PR task summaries from DB tasks and execution results.
func buildTaskSummaries(tasks []models.Task, results map[string]TaskResult) []git.PRTaskSummary {
	summaries := make([]git.PRTaskSummary, 0, len(tasks))
	for _, t := range tasks {
		status := "pending"
		if res, ok := results[t.ID]; ok {
			status = string(res.Status)
		}
		summaries = append(summaries, git.PRTaskSummary{
			Title:  t.Title,
			Status: status,
		})
	}
	return summaries
}

// analyzeResults counts task outcomes.
func analyzeResults(results map[string]TaskResult) (done, failed, skipped int) {
	for _, r := range results {
		switch r.Status {
		case models.TaskStatusDone:
			done++
		case models.TaskStatusFailed:
			failed++
		case models.TaskStatusSkipped:
			skipped++
		default:
			failed++
		}
	}
	return
}

// findFirstFailure returns the title and error of the first failed task.
func findFirstFailure(tasks []models.Task, results map[string]TaskResult) (title, reason string) {
	for _, t := range tasks {
		if res, ok := results[t.ID]; ok && res.Status == models.TaskStatusFailed {
			errMsg := "unknown error"
			if res.Error != nil {
				errMsg = res.Error.Error()
			}
			return t.Title, errMsg
		}
	}
	return "", ""
}

// Compile-time check.
var _ TicketProcessor = (*Orchestrator)(nil)
