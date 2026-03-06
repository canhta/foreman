package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Models                   models.ModelsConfig
	WorkDir                  string
	CodebasePatterns         string
	TestCommand              string
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask       int
	SearchReplaceSimilarity  float64
	EnableTDDVerification    bool
}

// TaskRunnerDB is the subset of db.Database needed by the task runner.
type TaskRunnerDB interface {
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
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
		// Clear stale feedback from previous attempt so it does not leak
		// into this iteration's retry prompt.
		feedback.Reset()

		// Revert any file changes left by the previous attempt.
		if attempt > 1 {
			if cleanErr := r.git.CleanWorkingTree(ctx, r.config.WorkDir); cleanErr != nil {
				return fmt.Errorf("clean working tree before retry: %w", cleanErr)
			}
		}

		// Check call cap before each LLM call.
		if err := CheckTaskCallCap(ctx, r.db, task.ID, r.config.MaxLlmCallsPerTask); err != nil {
			return fmt.Errorf("call cap: %w", err)
		}

		// Run implementer.
		contextFiles := r.loadContextFiles(task.FilesToRead)
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
			return &EscalationError{Question: question}
		}

		// Parse output into file changes.
		parsed, err := ParseImplementerOutput(
			result.Response.Content,
			r.config.SearchReplaceSimilarity,
			0,
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

	if !result.Approved && result.HasCritical {
		feedback.AddQualityFeedback(result.IssuesText())
		return &reviewRejectedError{reviewer: "quality"}
	}
	return nil
}

func (r *PipelineTaskRunner) loadContextFiles(paths []string) map[string]string {
	files := make(map[string]string, len(paths))
	for _, p := range paths {
		fullPath := filepath.Join(r.config.WorkDir, p)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		files[p] = string(data)
	}
	return files
}

func (r *PipelineTaskRunner) applyChanges(parsed *ParsedOutput) error {
	for _, fc := range parsed.Files {
		fullPath := filepath.Join(r.config.WorkDir, fc.Path)

		if fc.IsNew {
			dir := filepath.Dir(fullPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dir, err)
			}
			if err := os.WriteFile(fullPath, []byte(fc.Content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", fc.Path, err)
			}
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

		if err := os.WriteFile(fullPath, []byte(result), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", fc.Path, err)
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
