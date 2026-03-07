package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
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
type TaskRunnerConfig struct {
	Cache                    *appcontext.ContextCache
	Models                   models.ModelsConfig
	WorkDir                  string
	CodebasePatterns         string
	TestCommand              string
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
}

// TaskRunnerDB is the subset of db.Database needed by the task runner.
type TaskRunnerDB interface {
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	SetTaskErrorType(ctx context.Context, id, errorType string) error
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)
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

// RunTask executes the full per-task pipeline. Returns nil on success,
// *EscalationError if clarification is needed, or a regular error on failure.
func (r *PipelineTaskRunner) RunTask(ctx context.Context, task *models.Task) error {
	if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusImplementing); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	feedback := NewFeedbackAccumulator()

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
			if dbErr := r.db.SetTaskErrorType(ctx, task.ID, string(errType)); dbErr != nil {
				log.Warn().Err(dbErr).Str("task_id", task.ID).Str("error_type", string(errType)).
					Msg("failed to record task error type")
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
		contextFiles := r.loadContextFiles(task.FilesToRead, ctxBudget)
		result, err := r.implementer.Execute(ctx, ImplementerInput{
			Task:         task,
			ContextFiles: contextFiles,
			Model:        r.config.Models.Implementer,
			Feedback:     feedback.Render(),
			MaxTokens:    4096,
			Attempt:      attempt,
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

		if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
			return fmt.Errorf("update task status: %w", err)
		}
		return nil
	}

	// All retries exhausted.
	_ = r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed)
	return fmt.Errorf("task %q failed after %d attempts", task.Title, r.config.MaxImplementationRetries+1)
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
	})
	if err != nil {
		return fmt.Errorf("spec review: %w", err)
	}

	if !result.Approved {
		feedback.AddSpecFeedback(result.IssuesText())
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
	})
	if err != nil {
		return fmt.Errorf("quality review: %w", err)
	}

	if !result.Approved {
		feedback.AddQualityFeedback(result.IssuesText())
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
