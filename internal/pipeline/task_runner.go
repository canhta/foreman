package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/agent"
	appcontext "github.com/canhta/foreman/internal/context"
	dbpkg "github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/telemetry"
)

// EscalationError signals that the implementer detected ambiguity mid-task
// and wants to escalate to the ticket owner for clarification.
type EscalationError struct {
	Question string
}

func (e *EscalationError) Error() string {
	return fmt.Sprintf("implementer needs clarification: %s", e.Question)
}

// TaskRunnerConfig holds configuration for the pipeline task runner.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type TaskRunnerConfig struct {
	Cache            *appcontext.ContextCache
	Models           models.ModelsConfig
	WorkDir          string
	CodebasePatterns string
	TestCommand      string
	// PromptVersions maps prompt template filenames (e.g. "planner.md.j2") to
	// their SHA256 hashes computed at startup (REQ-OBS-001). Used to populate
	// LlmRequest.PromptVersion so each call is traceable to a specific template version.
	PromptVersions map[string]string
	// HookRunner fires post_lint skill hooks after each task commit (REQ-OBS-002).
	// Optional — nil disables skill hooks in the task runner.
	HookRunner               *skills.HookRunner
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask       int
	// ContextTokenBudget is the baseline token budget for context file loading.
	// Dynamic budget scales this by task complexity (low=50%, medium=100%, high=150%).
	// 0 means unlimited.
	ContextTokenBudget      int
	SearchReplaceSimilarity float64
	EnableTDDVerification   bool
	// ContextFeedbackBoost is the score multiplier for files that appeared in
	// files_touched of prior similar tasks. Default 1.5 (REQ-CTX-003).
	ContextFeedbackBoost float64
	// IntermediateReviewInterval controls how often the cross-task consistency
	// check runs. After every N completed tasks (where N = IntermediateReviewInterval),
	// a lightweight LLM consistency check is triggered. 0 disables the check.
	IntermediateReviewInterval int
	// DiscoveryBoard is the shared board for this ticket (ARCH-S02).
	// When non-nil, patterns from the board are merged with DB patterns
	// during context-file selection so parallel tasks share discoveries.
	DiscoveryBoard *models.DiscoveryBoard
	// AgentRunner is an optional external agent runner. When non-nil, RunTask
	// delegates implementation to this runner instead of using the builtin
	// implementer → parse → apply → review loop.
	AgentRunner agent.AgentRunner
	// AgentRunnerName identifies the runner type ("claudecode", "copilot").
	// Used to decide whether to inject Claude Code skills.
	AgentRunnerName string
}

// ConsistencyReviewDB is the subset of db.Database needed by the intermediate
// cross-task consistency review (REQ-PIPE-006).
type ConsistencyReviewDB interface {
	SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error
}

// TaskRunnerDB is the subset of db.Database needed by the task runner.
type TaskRunnerDB interface {
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	SetTaskErrorType(ctx context.Context, id, errorType string) error
	SetTaskAgentRunner(ctx context.Context, id, agentRunner string) error
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)
	RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error
	WriteContextFeedback(ctx context.Context, row dbpkg.ContextFeedbackRow) error
	QueryContextFeedback(ctx context.Context, candidates []string, minJaccard float64) ([]dbpkg.ContextFeedbackRow, error)
	GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error)
}

// PipelineTaskRunner implements daemon.TaskRunner by orchestrating the full
// per-task pipeline: implement → parse → apply → TDD verify → test → spec review → quality review → commit.
type PipelineTaskRunner struct {
	llm             LLMProvider
	db              TaskRunnerDB
	git             git.GitProvider
	cmdRunner       runner.CommandRunner
	implementer     *Implementer
	specReviewer    *SpecReviewer
	qualityReviewer *QualityReviewer
	metrics         *telemetry.Metrics
	config          TaskRunnerConfig
}

// NewPipelineTaskRunner creates a task runner that wires all pipeline stages together.
func NewPipelineTaskRunner(
	llm LLMProvider,
	database TaskRunnerDB,
	gitProv git.GitProvider,
	cmdRunner runner.CommandRunner,
	config TaskRunnerConfig,
) *PipelineTaskRunner {
	return &PipelineTaskRunner{
		llm:             llm,
		db:              database,
		git:             gitProv,
		cmdRunner:       cmdRunner,
		config:          config,
		implementer:     NewImplementer(llm),
		specReviewer:    NewSpecReviewer(llm),
		qualityReviewer: NewQualityReviewer(llm),
	}
}

// SetMetrics attaches a Metrics instance for instrumentation.
func (r *PipelineTaskRunner) SetMetrics(m *telemetry.Metrics) {
	r.metrics = m
}

// WithRegistry attaches a prompt registry to all pipeline components that support it.
// Passing nil is a no-op. Returns the runner for chaining.
func (r *PipelineTaskRunner) WithRegistry(reg *prompts.Registry) *PipelineTaskRunner {
	if reg == nil {
		return r
	}
	r.implementer.WithRegistry(reg)
	r.specReviewer.WithRegistry(reg)
	r.qualityReviewer.WithRegistry(reg)
	return r
}

// CloneWithWorkDir returns a shallow copy of the runner with a different WorkDir.
// The cache is reset since it is path-specific.
func (r *PipelineTaskRunner) CloneWithWorkDir(workDir string) *PipelineTaskRunner {
	cloned := *r
	cfg := r.config
	cfg.WorkDir = workDir
	cfg.Cache = nil
	cloned.config = cfg
	return &cloned
}

// runPostLintHook fires post_lint skill hooks after a task commit (REQ-OBS-002).
// Failures are logged as warnings and do not abort the task.
func (r *PipelineTaskRunner) runPostLintHook(ctx context.Context, task *models.Task) {
	if r.config.HookRunner == nil {
		return
	}
	tc := telemetry.TraceFromContext(ctx)
	sCtx := &skills.SkillContext{
		PipelineCtx: &telemetry.PipelineContext{
			TraceID:  tc.TraceID,
			TicketID: task.TicketID,
			TaskID:   task.ID,
			Stage:    "post_lint",
		},
		Models: map[string]string{
			"Planner":         r.config.Models.Planner,
			"Implementer":     r.config.Models.Implementer,
			"SpecReviewer":    r.config.Models.SpecReviewer,
			"QualityReviewer": r.config.Models.QualityReviewer,
			"FinalReviewer":   r.config.Models.FinalReviewer,
			"Clarifier":       r.config.Models.Clarifier,
		},
	}
	for _, hr := range r.config.HookRunner.RunHook(ctx, "post_lint", sCtx) {
		if hr.Error != nil {
			log.Warn().Err(hr.Error).
				Str("task_id", task.ID).
				Str("skill", hr.SkillID).
				Msg("post_lint hook failed (non-fatal)")
		}
	}
}

// RunTask executes the full per-task pipeline. Returns nil on success,
// *EscalationError if clarification is needed, or a regular error on failure.
func (r *PipelineTaskRunner) RunTask(ctx context.Context, task *models.Task) error {
	if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusImplementing); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	// External runner path — delegate entire implementation to AgentRunner.
	if r.config.AgentRunner != nil {
		runnerName := r.config.AgentRunnerName
		if runnerName == "" {
			runnerName = "agent"
		}
		if err := r.db.SetTaskAgentRunner(ctx, task.ID, runnerName); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID).Msg("failed to stamp agent runner on task")
		}
		return r.runTaskWithAgent(ctx, task)
	}

	// Builtin runner — stamp the runner name.
	if err := r.db.SetTaskAgentRunner(ctx, task.ID, "builtin"); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID).Msg("failed to stamp builtin runner on task")
	}

	feedback := NewFeedbackAccumulator()

	// currentRetryErrorType holds the error type for the current retry.
	// Zero value on attempt 1 is intentional — buildImplementerUserPrompt
	// renders no retry section when Attempt == 1, regardless of error type.
	var currentRetryErrorType ErrorType

	for attempt := 1; attempt <= r.config.MaxImplementationRetries+1; attempt++ {
		// Collapse prior feedback into a summary so the implementer retains
		// context from previous attempts without raw entries growing unboundedly.
		// On the very first attempt this is a no-op (accumulator is empty).
		feedback.ResetKeepingSummary()

		// Revert only the files this task is supposed to modify, preserving
		// committed changes from prior tasks in the pipeline.
		if attempt > 1 {
			// Classify the error before retry for targeted feedback and metrics.
			feedbackText := feedback.Render()
			errType := ClassifyRetryError(feedbackText)
			currentRetryErrorType = errType
			if dbErr := r.db.SetTaskErrorType(ctx, task.ID, string(errType)); dbErr != nil {
				log.Warn().Err(dbErr).Str("task_id", task.ID).Str("error_type", string(errType)).
					Msg("failed to record task error type")
			}

			// Record retry triggered metric.
			if r.metrics != nil {
				r.metrics.RetryTriggeredTotal.WithLabelValues("implement", string(errType)).Inc()
			}

			if resetErr := r.resetWorkingTree(ctx, task.FilesToModify); resetErr != nil {
				return fmt.Errorf("reset working tree before retry: %w", resetErr)
			}
		}

		// Check call cap before each LLM call.
		if err := CheckTaskCallCap(ctx, r.db, task.ID, r.config.MaxLlmCallsPerTask); err != nil {
			return fmt.Errorf("call cap: %w", err)
		}

		// Run implementer.
		// Compute dynamic context budget scaled by task complexity (REQ-CTX-002).
		ctxBudget := appcontext.DynamicContextBudget(r.config.ContextTokenBudget, task.EstimatedComplexity, 0)
		log.Debug().
			Str("complexity", task.EstimatedComplexity).
			Int("base_budget", r.config.ContextTokenBudget).
			Int("dynamic_budget", ctxBudget).
			Str("task_id", task.ID).
			Msg("context_assembly: dynamic budget computed")
		contextFiles := r.selectContextFiles(ctx, task, ctxBudget)
		contextFilePaths := make([]string, 0, len(contextFiles))
		for p := range contextFiles {
			contextFilePaths = append(contextFilePaths, p)
		}
		result, err := r.implementer.Execute(ctx, ImplementerInput{
			Task:           task,
			ContextFiles:   contextFiles,
			Model:          r.config.Models.Implementer,
			Feedback:       feedback.Render(),
			PromptVersion:  r.promptVersion("implementer"),
			MaxTokens:      8192,
			Attempt:        attempt,
			RetryErrorType: currentRetryErrorType,
		})
		if err != nil {
			return fmt.Errorf("implementer (attempt %d): %w", attempt, err)
		}

		// Check for mid-implementation escalation.
		if question := detectEscalation(result.Response.Content); question != "" {
			_ = r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusEscalated)
			return &EscalationError{Question: question}
		}

		// Parse output into file changes.
		parsed, err := ParseImplementerOutput(
			result.Response.Content,
			r.config.SearchReplaceSimilarity,
		)
		if err != nil {
			feedback.AddLintError(fmt.Sprintf("Failed to parse output: %s", err))
			continue
		}

		// Apply file changes.
		if applyErr := r.applyChanges(parsed); applyErr != nil {
			feedback.AddLintError(fmt.Sprintf("Failed to apply changes: %s", applyErr))
			continue
		}

		// TDD verification.
		if r.config.EnableTDDVerification {
			if statusErr := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusTDDVerifying); statusErr != nil {
				return fmt.Errorf("update task status: %w", statusErr)
			}
			tddResult := r.runTDDVerification(ctx, parsed)
			if !tddResult.Valid {
				feedback.AddTDDFeedback(fmt.Sprintf("Phase: %s, Reason: %s", tddResult.Phase, tddResult.Reason))
				continue
			}
		}

		// Run tests.
		if statusErr := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusTesting); statusErr != nil {
			return fmt.Errorf("update task status: %w", statusErr)
		}
		testOutput, testPassed := r.runTests(ctx)
		if !testPassed {
			feedback.AddTestError(testOutput)
			continue
		}

		// Get diff for reviewers.
		diff, err := r.git.DiffWorking(ctx, r.config.WorkDir)
		if err != nil {
			return fmt.Errorf("git diff: %w", err)
		}

		// Spec review.
		if len(task.AcceptanceCriteria) > 0 {
			specErr := r.runSpecReview(ctx, task, diff, testOutput, feedback)
			if specErr != nil {
				if _, ok := specErr.(*reviewRejectedError); ok {
					continue
				}
				return specErr
			}
		}

		// Quality review.
		qualityErr := r.runQualityReview(ctx, task.ID, diff, feedback)
		if qualityErr != nil {
			if _, ok := qualityErr.(*reviewRejectedError); ok {
				continue
			}
			return qualityErr
		}

		// All checks passed — stage and commit.
		if stageErr := r.git.StageAll(ctx, r.config.WorkDir); stageErr != nil {
			return fmt.Errorf("git stage: %w", stageErr)
		}
		commitMsg := fmt.Sprintf("feat: %s", task.Title)
		_, err = r.git.Commit(ctx, r.config.WorkDir, commitMsg)
		if err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		// Invalidate the context cache after commit so the next task sees fresh HEAD.
		if r.config.Cache != nil {
			r.config.Cache.Invalidate()
		}

		// Fire post_lint skill hooks after successful commit (REQ-OBS-002).
		r.runPostLintHook(ctx, task)

		// Write context feedback row so future tasks can learn from this one.
		r.writeContextFeedback(ctx, task, contextFilePaths, parsed)

		if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
			return fmt.Errorf("update task status: %w", err)
		}
		return nil
	}

	// All retries exhausted.
	_ = r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed)
	feedbackText := feedback.Render()
	if r.metrics != nil {
		errType := string(ClassifyRetryError(feedbackText))
		r.metrics.TaskFailuresTotal.WithLabelValues(errType, "builtin").Inc()
	}
	// Write context feedback on failure too, so the system can learn from missed files.
	r.writeContextFeedback(ctx, task, nil, nil)
	return fmt.Errorf("task %q failed after %d attempts: %s", task.Title, r.config.MaxImplementationRetries+1, feedbackText)
}

// runTaskWithAgent delegates the full implementation of a task to the configured
// AgentRunner. The agent handles its own TDD loop and file operations.
// Foreman only: builds prompt, calls agent, verifies diff, stage+commit, update status.
func (r *PipelineTaskRunner) runTaskWithAgent(ctx context.Context, task *models.Task) error {
	pb := NewPromptBuilder(r.llm)
	feedback := NewFeedbackAccumulator()

	// Inject Claude Code skills if applicable.
	if r.config.AgentRunnerName == "claudecode" {
		injector := NewSkillInjector(SkillInjectorConfig{
			TestCommand: r.config.TestCommand,
			Language:    r.config.CodebasePatterns,
		})
		if err := injector.Inject(r.config.WorkDir); err != nil {
			log.Warn().Err(err).Msg("skill injection failed, proceeding without skills")
		}
		defer injector.Cleanup(r.config.WorkDir)
	}

	for attempt := 1; attempt <= r.config.MaxImplementationRetries+1; attempt++ {
		if attempt > 1 {
			feedback.ResetKeepingSummary()
		}

		// Build prompt for this attempt.
		prompt := pb.Build(task, nil, PromptBuilderConfig{
			TestCommand:      r.config.TestCommand,
			CodebasePatterns: r.config.CodebasePatterns,
			RetryFeedback:    feedback.Render(),
			Attempt:          attempt,
		})

		// Delegate to agent.
		result, err := r.config.AgentRunner.Run(ctx, agent.AgentRequest{
			Prompt:  prompt,
			WorkDir: r.config.WorkDir,
		})
		if err != nil {
			log.Warn().Err(err).
				Str("task_id", task.ID).
				Int("attempt", attempt).
				Msg("agent runner error; will retry if attempts remain")
			feedback.AddLintError(fmt.Sprintf("agent error: %s", err))
			continue
		}

		log.Info().
			Str("task_id", task.ID).
			Int("attempt", attempt).
			Str("runner", r.config.AgentRunnerName).
			Float64("cost_usd", result.Usage.CostUSD).
			Int("input_tokens", result.Usage.InputTokens).
			Int("output_tokens", result.Usage.OutputTokens).
			Msg("agent runner completed task")

		// Write a synthetic LlmCallRecord so the dashboard can show cost/tokens
		// for external runners (claudecode, copilot) alongside builtin calls.
		tc := telemetry.TraceFromContext(ctx)
		// Use the actual model reported by the agent runner at runtime.
		// Fall back to config only if the runner didn't report a model.
		syntheticModel := result.Usage.Model
		if syntheticModel == "" {
			syntheticModel = r.config.Models.Implementer
		}
		syntheticCall := &models.LlmCallRecord{
			ID:           fmt.Sprintf("agent-%d", time.Now().UnixNano()),
			TicketID:     tc.TicketID,
			TaskID:       task.ID,
			Role:         "implementing",
			Stage:        "implementing",
			Provider:     r.config.AgentRunnerName,
			Model:        syntheticModel,
			AgentRunner:  r.config.AgentRunnerName,
			TokensInput:  result.Usage.InputTokens,
			TokensOutput: result.Usage.OutputTokens,
			CostUSD:      result.Usage.CostUSD,
			DurationMs:   int64(result.Usage.DurationMs),
			Attempt:      attempt,
			Status:       "success",
			CreatedAt:    time.Now(),
		}
		if writeErr := r.db.RecordLlmCall(ctx, syntheticCall); writeErr != nil {
			log.Warn().Err(writeErr).Str("task_id", task.ID).Msg("failed to record agent llm call")
		}

		// Verify the agent produced a diff.
		diff, diffErr := r.git.DiffWorking(ctx, r.config.WorkDir)
		if diffErr != nil {
			return fmt.Errorf("git diff after agent: %w", diffErr)
		}
		if strings.TrimSpace(diff) == "" {
			log.Warn().
				Str("task_id", task.ID).
				Int("attempt", attempt).
				Msg("agent produced empty diff; will retry if attempts remain")
			feedback.AddLintError("agent produced no file changes (empty diff)")
			continue
		}

		// Stage all changes and commit.
		if stageErr := r.git.StageAll(ctx, r.config.WorkDir); stageErr != nil {
			return fmt.Errorf("git stage after agent: %w", stageErr)
		}
		commitMsg := fmt.Sprintf("feat: %s", task.Title)
		_, commitErr := r.git.Commit(ctx, r.config.WorkDir, commitMsg)
		if commitErr != nil {
			// If the agent already committed, the working tree may be clean.
			// Verify by checking the diff again; if clean this is fine.
			cleanDiff, _ := r.git.DiffWorking(ctx, r.config.WorkDir)
			if strings.TrimSpace(cleanDiff) != "" {
				return fmt.Errorf("git commit after agent: %w", commitErr)
			}
			log.Info().
				Str("task_id", task.ID).
				Msg("commit skipped: agent already committed the changes")
		}

		// Invalidate context cache so the next task sees fresh HEAD.
		if r.config.Cache != nil {
			r.config.Cache.Invalidate()
		}

		// Non-blocking spec review.
		if r.specReviewer != nil && len(task.AcceptanceCriteria) > 0 {
			specFeedback := NewFeedbackAccumulator()
			specErr := r.runSpecReview(ctx, task, diff, "", specFeedback)
			if specErr != nil {
				log.Warn().Err(specErr).
					Str("task_id", task.ID).
					Msg("spec review rejected agent output (non-blocking)")
			}
		}

		// Non-blocking quality review.
		if r.qualityReviewer != nil {
			qualFeedback := NewFeedbackAccumulator()
			qualErr := r.runQualityReview(ctx, task.ID, diff, qualFeedback)
			if qualErr != nil {
				log.Warn().Err(qualErr).
					Str("task_id", task.ID).
					Msg("quality review rejected agent output (non-blocking)")
			}
		}

		// Fire post_lint skill hooks after successful commit.
		r.runPostLintHook(ctx, task)

		// Write context feedback for future context selection learning.
		r.writeContextFeedback(ctx, task, nil, nil)

		if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
			return fmt.Errorf("update task status done: %w", err)
		}
		return nil
	}

	// All retries exhausted.
	_ = r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed)
	if r.metrics != nil {
		r.metrics.TaskFailuresTotal.WithLabelValues("agent_empty_diff", r.config.AgentRunnerName).Inc()
	}
	return fmt.Errorf("task %q failed after %d agent attempts: %s",
		task.Title, r.config.MaxImplementationRetries+1, feedback.Render())
}

// reviewRejectedError is an internal sentinel for review rejection (triggers retry).
type reviewRejectedError struct {
	reviewer string
}

func (e *reviewRejectedError) Error() string {
	return fmt.Sprintf("%s review rejected", e.reviewer)
}

func (r *PipelineTaskRunner) runSpecReview(
	ctx context.Context,
	task *models.Task,
	diff, testOutput string,
	feedback *FeedbackAccumulator,
) error {
	if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusSpecReview); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	if err := CheckTaskCallCap(ctx, r.db, task.ID, r.config.MaxLlmCallsPerTask); err != nil {
		return fmt.Errorf("call cap: %w", err)
	}

	result, err := r.specReviewer.Review(ctx, SpecReviewInput{
		TaskTitle:          task.Title,
		Diff:               diff,
		TestOutput:         testOutput,
		AcceptanceCriteria: task.AcceptanceCriteria,
		PromptVersion:      r.promptVersion("spec_reviewer"),
	})
	if err != nil {
		return fmt.Errorf("spec review: %w", err)
	}

	if !result.Approved {
		issuesText := result.IssuesText()
		if issuesText == "" {
			issuesText = "Spec review rejected: no specific issues were parsed from the reviewer output"
		}
		feedback.AddSpecFeedback(issuesText)
		return &reviewRejectedError{reviewer: "spec"}
	}
	return nil
}

func (r *PipelineTaskRunner) runQualityReview(
	ctx context.Context,
	taskID string,
	diff string,
	feedback *FeedbackAccumulator,
) error {
	if err := CheckTaskCallCap(ctx, r.db, taskID, r.config.MaxLlmCallsPerTask); err != nil {
		return fmt.Errorf("call cap: %w", err)
	}

	result, err := r.qualityReviewer.Review(ctx, QualityReviewInput{
		Diff:             diff,
		CodebasePatterns: r.config.CodebasePatterns,
		PromptVersion:    r.promptVersion("quality_reviewer"),
	})
	if err != nil {
		return fmt.Errorf("quality review: %w", err)
	}

	if !result.Approved {
		issuesText := result.IssuesText()
		if issuesText == "" {
			issuesText = "Quality review rejected: no specific issues were parsed from the reviewer output"
		}
		feedback.AddQualityFeedback(issuesText)
		return &reviewRejectedError{reviewer: "quality"}
	}
	return nil
}

// resetWorkingTree reverts only the files listed in filesToModify to their
// last-committed state using `git checkout -- <file>`. This is intentionally
// narrower than CleanWorkingTree (which nukes the entire tree) so that
// committed changes from prior tasks in the same pipeline run are preserved.
//
// Errors indicating the file is not tracked by git (new files that were never
// committed) are suppressed. All other errors — which indicate a genuine
// problem such as a corrupt repo or misconfigured WorkDir — are returned so
// that the retry does not proceed with a dirty working tree.
func (r *PipelineTaskRunner) resetWorkingTree(ctx context.Context, filesToModify []string) error {
	for _, f := range filesToModify {
		fullPath := filepath.Join(r.config.WorkDir, f)
		args := []string{"checkout", "--", fullPath}
		out, err := r.cmdRunner.Run(ctx, r.config.WorkDir, "git", args, 30)
		if err != nil {
			// Run() only returns a non-nil error when the process itself could
			// not be started (e.g. git binary missing). That is always fatal.
			return fmt.Errorf("git checkout -- %s: %w", f, err)
		}
		if out != nil && out.ExitCode != 0 {
			// Suppress "pathspec did not match any file(s) known to git" — this
			// is expected for new files that have never been committed.
			if strings.Contains(out.Stderr, "did not match any file") {
				continue
			}
			return fmt.Errorf("git checkout -- %s: exit %d: %s", f, out.ExitCode, strings.TrimSpace(out.Stderr))
		}
	}
	return nil
}

// selectContextFiles selects relevant files for the task using scored selection
// with feedback boosting (REQ-CTX-003) and progress pattern bonus (ARCH-M03).
// Falls back to loadContextFiles if SelectFilesForTask fails.
// The result is a map of relative path → file content.
func (r *PipelineTaskRunner) selectContextFiles(ctx context.Context, task *models.Task, budget int) map[string]string {
	boost := r.config.ContextFeedbackBoost
	if boost <= 0 {
		boost = 1.5
	}

	// Load progress patterns for this ticket to apply bonus weight (ARCH-M03).
	patterns, err := r.db.GetProgressPatterns(ctx, task.TicketID, nil)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID).Msg("context_assembly: failed to load progress patterns, skipping bonus")
		patterns = nil
	}

	// Publish DB-loaded patterns to the shared DiscoveryBoard (ARCH-S02) so
	// parallel tasks can read them immediately without their own DB round-trip.
	if r.config.DiscoveryBoard != nil {
		for _, p := range patterns {
			r.config.DiscoveryBoard.AddPattern(p.PatternKey, p.PatternValue)
		}
	}

	// Merge in any discoveries from the shared DiscoveryBoard (ARCH-S02).
	if r.config.DiscoveryBoard != nil {
		boardPatterns := r.config.DiscoveryBoard.Patterns(task.TicketID)
		patterns = append(patterns, boardPatterns...)
	}

	scored, err := appcontext.GetOrSelectFiles(r.config.Cache, task, r.config.WorkDir, budget, r.db, boost, patterns...)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID).Msg("context_assembly: GetOrSelectFiles failed, falling back to explicit files")
		return r.loadContextFiles(task.FilesToRead, budget)
	}

	files := make(map[string]string, len(scored))
	tokensUsed := 0
	realFilesAdded := 0
	for _, sf := range scored {
		fullPath := filepath.Join(r.config.WorkDir, sf.Path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			log.Warn().Str("path", sf.Path).Err(err).Msg("context file not readable after selection")
			files[sf.Path] = fmt.Sprintf("[FILE NOT FOUND: %q was referenced but could not be read: %s]", sf.Path, err)
			continue
		}
		content := string(data)
		if budget > 0 && realFilesAdded > 0 {
			est := appcontext.EstimateTokens(content)
			if tokensUsed+est > budget {
				continue
			}
			tokensUsed += est
		} else if budget > 0 {
			tokensUsed += appcontext.EstimateTokens(content)
		}
		files[sf.Path] = content
		realFilesAdded++
	}

	// Emit cache hit ratio metric (REQ-TELE-001).
	// This gauge reflects the lifetime ratio at the point of this call; it is
	// only updated when selectContextFiles runs (zero-task plans or early
	// failures will not update the gauge).
	if r.config.Cache != nil && r.metrics != nil {
		r.metrics.ContextCacheHitRatio.Set(r.config.Cache.HitRatio())
	}

	return files
}

// loadContextFiles reads all paths. If budget > 0, stops adding files once the
// accumulated estimated token count would exceed budget (always adds at least one
// real file regardless of size). Missing files produce a [FILE NOT FOUND: ...]
// placeholder (BUG-M14 fix) but do NOT count as "the first real file" for the
// budget exemption.
func (r *PipelineTaskRunner) loadContextFiles(paths []string, budget int) map[string]string {
	files := make(map[string]string, len(paths))
	tokensUsed := 0
	realFilesAdded := 0
	for _, p := range paths {
		fullPath := filepath.Join(r.config.WorkDir, p)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			// Log the missing file and inject a note into context so the
			// implementer knows the file was referenced but not found.
			log.Warn().Str("path", p).Err(err).Msg("context file referenced by planner not found on disk")
			files[p] = fmt.Sprintf("[FILE NOT FOUND: %q was referenced but could not be read: %s]", p, err)
			continue
		}
		content := string(data)
		if budget > 0 && realFilesAdded > 0 {
			// Only enforce the budget after at least one real file has been added.
			// This guarantees the implementer always gets at least one real file.
			est := appcontext.EstimateTokens(content)
			if tokensUsed+est > budget {
				break
			}
			tokensUsed += est
		} else if budget > 0 {
			// First real file: count its tokens so subsequent files are checked
			// against the correct accumulated usage.
			tokensUsed += appcontext.EstimateTokens(content)
		}
		files[p] = content
		realFilesAdded++
	}
	return files
}

func (r *PipelineTaskRunner) applyChanges(parsed *ParsedOutput) error {
	type pendingWrite struct {
		path    string
		mkdirs  string
		content []byte
		perm    os.FileMode
	}
	var writes []pendingWrite

	// Phase 1: Compute all changes in memory. If ANY patch fails, return
	// error without writing anything to disk.
	for _, fc := range parsed.Files {
		fullPath := filepath.Join(r.config.WorkDir, fc.Path)

		if fc.IsNew {
			writes = append(writes, pendingWrite{
				path: fullPath, content: []byte(fc.Content), perm: 0o644,
				mkdirs: filepath.Dir(fullPath),
			})
			continue
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", fc.Path, err)
		}
		result := string(content)
		for i, sr := range fc.Patches {
			applied, err := ApplySearchReplace(result, &sr, r.config.SearchReplaceSimilarity)
			if err != nil {
				return fmt.Errorf("apply patch %d to %s: %w", i+1, fc.Path, err)
			}
			result = applied
		}
		writes = append(writes, pendingWrite{path: fullPath, content: []byte(result), perm: 0o644})
	}

	// Phase 2: All patches validated — write to disk.
	for _, w := range writes {
		if w.mkdirs != "" {
			if err := os.MkdirAll(w.mkdirs, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", w.mkdirs, err)
			}
		}
		if err := os.WriteFile(w.path, w.content, w.perm); err != nil {
			return fmt.Errorf("write %s: %w", w.path, err)
		}
	}
	return nil
}

func (r *PipelineTaskRunner) runTests(ctx context.Context) (output string, passed bool) {
	if r.config.TestCommand == "" {
		return "", true
	}

	parts := strings.Fields(r.config.TestCommand)
	cmd := parts[0]
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	result, err := r.cmdRunner.Run(ctx, r.config.WorkDir, cmd, args, 120)
	if err != nil {
		return fmt.Sprintf("test command error: %s", err), false
	}

	combined := result.Stdout + "\n" + result.Stderr
	return combined, result.ExitCode == 0
}

func (r *PipelineTaskRunner) runTDDVerification(ctx context.Context, parsed *ParsedOutput) TDDResult {
	// Check that test files were generated.
	hasTests := false
	for _, fc := range parsed.Files {
		if IsTestFile(fc.Path) {
			hasTests = true
			break
		}
	}
	if !hasTests {
		return TDDResult{Valid: false, Phase: "red", Reason: "no test files in output"}
	}

	// Run tests — should fail (RED phase) before implementation.
	if r.config.TestCommand == "" {
		return TDDResult{Valid: true, Phase: "green"}
	}

	output, passed := r.runTests(ctx)
	if passed {
		return TDDResult{Valid: true, Phase: "green"}
	}

	failType := ClassifyTestFailure(output, "")
	switch failType {
	case FailureAssertion:
		return TDDResult{Valid: true, Phase: "red", Reason: "assertion failure (valid RED)"}
	case FailureCompile, FailureImport:
		return TDDResult{Valid: false, Phase: "red", Reason: fmt.Sprintf("invalid RED: %s failure", failType)}
	default:
		return TDDResult{Valid: true, Phase: "red", Reason: "test failure (accepted)"}
	}
}

// detectEscalation checks if the implementer output contains a NEEDS_CLARIFICATION marker.
func detectEscalation(output string) string {
	markers := []string{"NEEDS_CLARIFICATION:", "CLARIFICATION_NEEDED:"}
	for _, marker := range markers {
		if idx := strings.Index(output, marker); idx != -1 {
			question := strings.TrimSpace(output[idx+len(marker):])
			// Take first line only.
			if nl := strings.Index(question, "\n"); nl != -1 {
				question = question[:nl]
			}
			if question != "" {
				return question
			}
		}
	}
	return ""
}

// writeContextFeedback records which files were selected vs touched for REQ-CTX-003.
// contextFilePaths contains the actual keys of the contextFiles map passed to the LLM.
// parsed may be nil (for failure cases where no output was parsed).
func (r *PipelineTaskRunner) writeContextFeedback(ctx context.Context, task *models.Task, contextFilePaths []string, parsed *ParsedOutput) {
	filesSelected := contextFilePaths

	var filesTouched []string
	if parsed != nil {
		for _, fc := range parsed.Files {
			filesTouched = append(filesTouched, fc.Path)
		}
	}

	ticketID := task.TicketID
	row := dbpkg.ContextFeedbackRow{
		TicketID:      ticketID,
		TaskID:        task.ID,
		FilesSelected: filesSelected,
		FilesTouched:  filesTouched,
	}
	if err := r.db.WriteContextFeedback(ctx, row); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID).Msg("context_feedback: failed to write feedback row")
	}
}

// promptVersion looks up the SHA256 hash for a named template (e.g. "implementer")
// from the PromptVersions map. Returns empty string if not found (REQ-OBS-001).
func (r *PipelineTaskRunner) promptVersion(templateName string) string {
	if r.config.PromptVersions == nil {
		return ""
	}
	return r.config.PromptVersions[templateName+".md.j2"]
}
