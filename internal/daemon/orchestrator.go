// internal/daemon/orchestrator.go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/channel"
	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
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
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type TaskRunnerFactoryInput struct {
	ContextCache     *appcontext.ContextCache
	Models           models.ModelsConfig
	TicketID         string
	WorkDir          string
	CodebasePatterns string
	TestCommand      string
	// BranchName is the git branch checked out in WorkDir for this ticket
	// (e.g. "foremanSU-SU-738"). When non-empty, each task runs in its own
	// git worktree branched from this branch so parallel tasks are isolated.
	BranchName string
	// PromptVersions maps prompt template filenames (e.g. "planner.md.j2") to
	// their SHA256 hashes for LlmRequest.PromptVersion (REQ-OBS-001).
	PromptVersions map[string]string
	// HookRunner fires post_lint skill hooks after each task commit (REQ-OBS-002).
	// Optional — nil disables skill hooks in the task runner.
	HookRunner               *skills.HookRunner
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask       int
	ContextTokenBudget       int
	// ContextFeedbackBoost is the score multiplier for files that appeared in
	// files_touched of prior similar tasks. Default 1.5 (REQ-CTX-003).
	ContextFeedbackBoost  float64
	EnableTDDVerification bool
	// IntermediateReviewInterval controls how often the cross-task consistency
	// check runs (REQ-PIPE-006). 0 disables it.
	IntermediateReviewInterval int
	// DiscoveryBoard is the shared board for this ticket. Parallel tasks write
	// discovered patterns and file-relevance scores to it so subsequent tasks
	// (and parallel tasks on their next LLM turn) can benefit (ARCH-S02).
	// Optional — nil disables discovery sharing.
	DiscoveryBoard *models.DiscoveryBoard
	// AgentRunner is the optional external agent runner for task implementation.
	AgentRunner agent.AgentRunner
	// AgentRunnerName identifies the runner ("claudecode", "copilot", "").
	AgentRunnerName string
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
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type OrchestratorConfig struct {
	Models             models.ModelsConfig
	WorkDir            string
	DefaultBranch      string
	BranchPrefix       string
	TestCommand        string
	ClarificationLabel string
	PRReviewers        []string
	// PromptVersions maps prompt template filenames (e.g. "planner.md.j2") to
	// their SHA256 hashes, computed at startup (REQ-OBS-001). Passed to the
	// task runner so LlmRequest.PromptVersion is populated for each LLM call.
	PromptVersions         map[string]string
	MaxParallelTasks       int
	TaskTimeoutMinutes     int
	DAGTimeoutMinutes      int
	MaxLlmCallsPerTask     int
	MaxImplementRetries    int
	MaxSpecReviewCycles    int
	MaxQualityReviewCycles int
	ContextTokenBudget     int
	// ContextFeedbackBoost is the score multiplier for files that appeared in
	// files_touched of prior similar tasks. Default 1.5 (REQ-CTX-003).
	ContextFeedbackBoost  float64
	PRDraft               bool
	RebaseBeforePR        bool
	AutoPush              bool
	EnablePartialPR       bool
	EnableTDDVerification bool
	EnableClarification   bool
	// IntermediateReviewInterval controls how often the cross-task consistency
	// check runs. After every N completed tasks a lightweight LLM check fires.
	// 0 disables the check (REQ-PIPE-006).
	IntermediateReviewInterval int
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
	hookRunner     *skills.HookRunner
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

// SetHookRunner attaches a hook runner so the orchestrator can fire pre_pr, post_pr,
// and post_lint skill hooks (REQ-OBS-002). Optional — nil disables skill hooks.
func (o *Orchestrator) SetHookRunner(hr *skills.HookRunner) {
	o.hookRunner = hr
}

// emitEvent broadcasts a lifecycle event to the dashboard (no-op if emitter not set).
func (o *Orchestrator) emitEvent(ctx context.Context, ticketID, eventType, severity, message string) {
	if o.emitter == nil {
		return
	}
	o.emitter.Emit(ctx, ticketID, "", eventType, severity, message, nil)
}

// runSkillHook fires all skills registered for hookName for the given ticket (REQ-OBS-002).
// Failures are logged as warnings and do not abort the calling workflow.
func (o *Orchestrator) runSkillHook(ctx context.Context, ticket models.Ticket, hookName string) {
	if o.hookRunner == nil {
		return
	}
	tc := telemetry.TraceFromContext(ctx)
	sCtx := &skills.SkillContext{
		Ticket: ticket,
		PipelineCtx: &telemetry.PipelineContext{
			TraceID:  tc.TraceID,
			TicketID: ticket.ID,
			Stage:    hookName,
		},
		Models: map[string]string{
			"Planner":         o.config.Models.Planner,
			"Implementer":     o.config.Models.Implementer,
			"SpecReviewer":    o.config.Models.SpecReviewer,
			"QualityReviewer": o.config.Models.QualityReviewer,
			"FinalReviewer":   o.config.Models.FinalReviewer,
			"Clarifier":       o.config.Models.Clarifier,
		},
	}
	// Wire EventEmitter only when non-nil to avoid non-nil interface with nil pointer.
	if o.emitter != nil {
		sCtx.EventEmitter = o.emitter
	}
	// Wire HandoffDB and ProgressDB from the orchestrator's database (REQ-OBS-002).
	// Guard against non-nil interface holding a nil pointer.
	if o.db != nil {
		sCtx.HandoffDB = o.db
		sCtx.ProgressDB = o.db
	}
	for _, hr := range o.hookRunner.RunHook(ctx, hookName, sCtx) {
		if hr.Error != nil {
			o.log.Warn().Err(hr.Error).
				Str("ticket", ticket.ID).
				Str("skill", hr.SkillID).
				Str("hook", hookName).
				Msg("skill hook failed (non-fatal)")
		}
	}
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

	// Inject request-level trace for end-to-end observability (ARCH-O01).
	ctx, traceCtx := telemetry.StartTrace(ctx, ticket.ID)
	log = log.With().Str("trace_id", traceCtx.TraceID).Logger()

	// Create a per-ticket context cache. It is injected into ctx so the planner
	// can reuse the file tree across repeated AssemblePlannerContext calls, and
	// is passed to the task runner factory so each task also benefits from the
	// cache. The cache is invalidated automatically by the task runner after
	// every git commit.
	ticketCache := appcontext.NewContextCache()
	ctx = appcontext.WithCache(ctx, ticketCache)

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

	// Detect smart retry: existing done tasks mean we skip planning and reuse the branch.
	priorTasks, priorErr := o.db.ListTasks(ctx, ticket.ID)
	isRetry := priorErr == nil && hasDoneTasks(priorTasks)

	var dbTasks []models.Task
	var codebasePatterns string
	branchName := o.config.BranchPrefix + ticket.ExternalID

	if isRetry {
		log.Info().Msg("smart retry detected: skipping planning, resuming from existing tasks")
		dbTasks = priorTasks
		// CreateBranch handles existing branches via git checkout fallback.
		if err := o.git.CreateBranch(ctx, o.config.WorkDir, branchName); err != nil {
			returnErr = fmt.Errorf("checkout branch %s: %w", branchName, err)
			return returnErr
		}
		filesToReserve := collectTaskFilesToModify(dbTasks)
		if reserveErr := o.scheduler.TryReserve(ctx, ticket.ID, filesToReserve); reserveErr != nil {
			var conflictErr *FileConflictError
			if errors.As(reserveErr, &conflictErr) {
				log.Info().Str("conflicts", conflictErr.Error()).Msg("file conflict on retry, requeueing ticket")
				if dbErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); dbErr != nil {
					returnErr = fmt.Errorf("requeue after conflict: %w", dbErr)
					return returnErr
				}
				returnErr = nil
				return nil
			}
			returnErr = fmt.Errorf("reserve files: %w", reserveErr)
			return returnErr
		}
	} else {
		// Normal path: clarity check → branch → plan → tasks → reserve.

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

		// Reset workdir to a clean, up-to-date state on the default branch
		// before creating the ticket branch.
		if err := o.git.CleanWorkingTree(ctx, o.config.WorkDir); err != nil {
			log.Warn().Err(err).Msg("clean working tree failed, continuing")
		}
		if err := o.git.Checkout(ctx, o.config.WorkDir, o.config.DefaultBranch); err != nil {
			returnErr = fmt.Errorf("checkout default branch %s: %w", o.config.DefaultBranch, err)
			return returnErr
		}
		if err := o.git.Pull(ctx, o.config.WorkDir); err != nil {
			log.Warn().Err(err).Msg("git pull failed, continuing with current HEAD")
		}

		// Create feature branch from fresh HEAD.
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

		codebasePatterns = formatCodebasePatterns(planResult.CodebasePatterns)

		// Reload tasks from DB to get assigned IDs.
		var reloadErr error
		dbTasks, reloadErr = o.db.ListTasks(ctx, ticket.ID)
		if reloadErr != nil {
			returnErr = fmt.Errorf("list tasks: %w", reloadErr)
			return returnErr
		}
	}

	// Status: planning -> implementing
	if implErr := o.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusImplementing); implErr != nil {
		returnErr = fmt.Errorf("update status to implementing: %w", implErr)
		return returnErr
	}

	o.notify(ctx, ticket, fmt.Sprintf("Implementing %d tasks for ticket #%s...", len(dbTasks), ticket.ID))
	o.emitEvent(ctx, ticket.ID, "implementing_started", "info", fmt.Sprintf("Starting implementation of %d tasks", len(dbTasks)))

	// Create a shared DiscoveryBoard for this ticket invocation (ARCH-S02).
	// All parallel tasks share the same board so discoveries propagate across them.
	discoveryBoard := models.NewDiscoveryBoard()

	// Build task runner via factory.
	dagRunner := o.runnerFactory.Create(TaskRunnerFactoryInput{
		TicketID:                   ticket.ID,
		Models:                     o.config.Models,
		WorkDir:                    o.config.WorkDir,
		BranchName:                 branchName,
		CodebasePatterns:           codebasePatterns,
		TestCommand:                o.config.TestCommand,
		MaxImplementationRetries:   o.config.MaxImplementRetries,
		MaxSpecReviewCycles:        o.config.MaxSpecReviewCycles,
		MaxQualityReviewCycles:     o.config.MaxQualityReviewCycles,
		MaxLlmCallsPerTask:         o.config.MaxLlmCallsPerTask,
		ContextTokenBudget:         o.config.ContextTokenBudget,
		ContextFeedbackBoost:       o.config.ContextFeedbackBoost,
		EnableTDDVerification:      o.config.EnableTDDVerification,
		IntermediateReviewInterval: o.config.IntermediateReviewInterval,
		ContextCache:               ticketCache,
		PromptVersions:             o.config.PromptVersions,
		HookRunner:                 o.hookRunner,
		DiscoveryBoard:             discoveryBoard,
	})

	// Build DAG tasks (resolve title->ID dependencies).
	dagTasks := buildDAGTasks(dbTasks)

	// Load prior DAG state so crash recovery can skip already-completed tasks (ARCH-F03).
	dagState, dagStateErr := o.db.GetDAGState(ctx, ticket.ID)
	if dagStateErr != nil {
		log.Warn().Err(dagStateErr).Str("ticket_id", ticket.ID).Msg("failed to load DAG state, running all tasks")
		dagState = nil // explicit: don't use partial state on error
	}
	tasksToRun := TasksForDAGRecovery(dagTasks, dagState)

	// Create executor, wire DAG store for persistent state snapshots, and run.
	timeout := time.Duration(o.config.TaskTimeoutMinutes) * time.Minute
	executor := NewDAGExecutor(dagRunner, o.config.MaxParallelTasks, timeout).
		WithDAGStore(o.db, ticket.ID)

	log.Info().Int("task_count", len(tasksToRun)).Msg("starting DAG execution")
	execCtx := ctx
	if o.config.DAGTimeoutMinutes > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(o.config.DAGTimeoutMinutes)*time.Minute)
		defer cancel()
	}
	results := executor.Execute(execCtx, tasksToRun)

	// Merge successful task worktrees into the ticket branch and clean up.
	// Tasks that fell back to the main workdir have empty WorktreeBranch, so they are skipped.
	for _, result := range results {
		if result.WorktreeBranch == "" {
			continue
		}
		if result.Status == models.TaskStatusDone {
			if mergeErr := o.git.MergeNoFF(ctx, o.config.WorkDir, result.WorktreeBranch); mergeErr != nil {
				log.Error().Err(mergeErr).Str("branch", result.WorktreeBranch).Msg("failed to merge task branch")
			}
		}
		if result.WorktreeDir != "" {
			if rmErr := o.git.RemoveWorktree(ctx, o.config.WorkDir, result.WorktreeDir); rmErr != nil {
				log.Warn().Err(rmErr).Str("dir", result.WorktreeDir).Msg("failed to remove worktree")
			}
		}
		if delErr := o.git.DeleteBranch(ctx, o.config.WorkDir, result.WorktreeBranch); delErr != nil {
			log.Warn().Err(delErr).Str("branch", result.WorktreeBranch).Msg("failed to delete task branch")
		}
	}

	// Recovery case: if all tasks were already completed before this run
	// (tasksToRun was empty because every task ID appeared in dagState.CompletedTasks),
	// DAGExecutor.Execute returns an empty map. Synthesize "done" results for those
	// previously-completed tasks so the doneCount==0 gate and the PR task summaries
	// both see the correct state (ARCH-F03).
	if dagState != nil && len(dagState.CompletedTasks) > 0 {
		for _, id := range dagState.CompletedTasks {
			if _, alreadyInResults := results[id]; !alreadyInResults {
				results[id] = TaskResult{TaskID: id, Status: models.TaskStatusDone}
			}
		}
	}

	// Analyze results.
	doneCount, failedCount, skippedCount := analyzeResults(results)
	totalCount := len(results)
	log.Info().
		Int("done", doneCount).
		Int("failed", failedCount).
		Int("skipped", skippedCount).
		Int("total", totalCount).
		Msg("DAG execution complete")

	// Log individual task failures to surface the root cause.
	for _, t := range dbTasks {
		if res, ok := results[t.ID]; ok && res.Status == models.TaskStatusFailed && res.Error != nil {
			log.Error().
				Str("task_id", t.ID).
				Str("task_title", t.Title).
				Err(res.Error).
				Msg("task failed")
		}
	}

	isPartial := failedCount > 0 && doneCount > 0

	// DAG execution reached a terminal state: remove the persisted DAG state to
	// prevent unbounded table growth and avoid stale state being replayed on next
	// run. Called here (before early returns) so all exit paths clean up (ARCH-F03).
	// Failure is non-fatal — warn and continue.
	if delErr := o.db.DeleteDAGState(ctx, ticket.ID); delErr != nil {
		log.Warn().Err(delErr).Str("ticket_id", ticket.ID).Msg("failed to delete DAG state after completion; row will persist until next run")
	}

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

	// Fire pre_pr skill hooks before PR creation (REQ-OBS-002).
	o.runSkillHook(ctx, ticket, "pre_pr")

	// Create the PR.
	prResp, err := o.prCreator.CreatePR(ctx, prReq)
	if err != nil {
		returnErr = fmt.Errorf("create PR: %w", err)
		return returnErr
	}

	// Fire post_pr skill hooks after successful PR creation (REQ-OBS-002).
	o.runSkillHook(ctx, ticket, "post_pr")

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

	// Store the local branch HEAD SHA at PR creation time. This may briefly differ from the
	// GitHub API head.sha if CI pushes a merge-ref on top; the first MergeChecker poll
	// (storedSHA == "" branch) will initialize from the live API SHA, eliminating false positives.
	// We read HEAD from the git log; if this fails it's non-fatal — MergeChecker will
	// treat a missing SHA as "first-seen" and initialize it on the next poll.
	if commits, logErr := o.git.Log(ctx, o.config.WorkDir, 1); logErr == nil && len(commits) > 0 {
		if shaErr := o.db.SetTicketPRHeadSHA(ctx, ticket.ID, commits[0].SHA); shaErr != nil {
			log.Warn().Err(shaErr).Str("ticket_id", ticket.ID).Msg("failed to store PR HEAD SHA (non-fatal)")
		}
	}

	// Release file reservations. Do NOT return the error — the PR was already
	// created successfully and the ticket status has been updated. A release
	// failure is only a cosmetic leak; CleanupOrphanReservations will reclaim
	// any stale entries on the next daemon cycle.
	if err := o.scheduler.Release(ctx, ticket.ID); err != nil {
		log.Warn().Err(err).Str("ticket_id", ticket.ID).
			Msg("failed to release file reservations after PR (non-fatal, orphan cleanup will reclaim)")
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
		case models.TaskStatusFailed, models.TaskStatusEscalated:
			// BUG-M01: count escalated tasks as failed for PR/partial-PR logic.
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

// hasDoneTasks reports whether any task in the slice has status Done.
func hasDoneTasks(tasks []models.Task) bool {
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone {
			return true
		}
	}
	return false
}

// collectTaskFilesToModify collects unique FilesToModify from existing DB tasks.
// Used in the retry path where no PlanResult is available.
func collectTaskFilesToModify(tasks []models.Task) []string {
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

// Compile-time check.
var _ TicketProcessor = (*Orchestrator)(nil)
