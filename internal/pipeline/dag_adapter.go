package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/envloader"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/snapshot"
)

// DAGTaskAdapter adapts PipelineTaskRunner to the daemon.TaskRunner interface,
// bridging task ID lookups with the full pipeline execution.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type DAGTaskAdapter struct {
	db                TaskRunnerDB
	llm               LLMProvider
	cdb               ConsistencyReviewDB
	git               git.GitProvider
	runner            *PipelineTaskRunner
	envFiles          map[string]string
	config            TaskRunnerConfig
	ticketID          string
	ticketBranch      string
	lastReviewedSHA   string
	completedCount    atomic.Int64
	lastReviewedSHAMu sync.Mutex
}

// NewDAGTaskAdapter creates an adapter that connects PipelineTaskRunner to DAGExecutor.
func NewDAGTaskAdapter(runner *PipelineTaskRunner, db TaskRunnerDB, ticketID string) *DAGTaskAdapter {
	return &DAGTaskAdapter{runner: runner, db: db, ticketID: ticketID}
}

// NewDAGTaskAdapterWithConsistency creates an adapter with intermediate consistency
// review support (REQ-PIPE-006). Pass interval=0 to disable the check.
// ticketBranch is the git branch for the ticket (e.g. "foremanSU-SU-738"); when
// non-empty, each task runs in its own git worktree branched from this branch.
func NewDAGTaskAdapterWithConsistency(
	runner *PipelineTaskRunner,
	db TaskRunnerDB,
	ticketID string,
	llm LLMProvider,
	cdb ConsistencyReviewDB,
	gitProv git.GitProvider,
	config TaskRunnerConfig,
	ticketBranch string,
	envFiles map[string]string,
) *DAGTaskAdapter {
	return &DAGTaskAdapter{
		runner:       runner,
		db:           db,
		ticketID:     ticketID,
		ticketBranch: ticketBranch,
		llm:          llm,
		cdb:          cdb,
		git:          gitProv,
		config:       config,
		envFiles:     envFiles,
	}
}

// Run implements daemon.TaskRunner. It looks up the task by ID from the DB
// task list and delegates to PipelineTaskRunner.RunTask.
//
// When a ticketBranch is configured, each task runs in a dedicated git worktree
// so parallel tasks don't share a working directory. On success the result
// carries WorktreeDir and WorktreeBranch so the orchestrator can merge and
// clean up after all tasks finish.
func (a *DAGTaskAdapter) Run(ctx context.Context, taskID string) daemon.TaskResult {
	// Find the task in the DB. We need the ticket ID to list tasks,
	// but the DAG executor only gives us the task ID. Use the task directly
	// from the pre-loaded task map if available, or query by convention.
	task, err := a.findTask(ctx, taskID)
	if err != nil {
		return daemon.TaskResult{
			TaskID: taskID,
			Status: models.TaskStatusFailed,
			Error:  err,
		}
	}

	// Attempt to create a per-task git worktree for isolation.
	var worktreeDir, worktreeBranch string
	runnerToUse := a.runner

	if a.ticketBranch != "" && a.git != nil && a.config.WorkDir != "" {
		shortID := taskID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		worktreeDir = filepath.Join(filepath.Dir(a.config.WorkDir), "worktrees", taskID)
		worktreeBranch = a.ticketBranch + "-wt-" + shortID

		if wtErr := a.git.AddWorktree(ctx, a.config.WorkDir, worktreeDir, worktreeBranch, a.ticketBranch); wtErr != nil {
			log.Warn().Err(wtErr).Str("task_id", taskID).Msg("worktree creation failed, running in main workdir")
			// Fall back to the main workdir — clear the tracking vars.
			worktreeDir = ""
			worktreeBranch = ""
		} else {
			cloned := a.runner.CloneWithWorkDir(worktreeDir)
			// Create a per-worktree snapshot store so the task runner can roll
			// back partial changes when all implementation retries are exhausted.
			snapshotDataDir := filepath.Join(worktreeDir, ".foreman-snapshots")
			if mkErr := os.MkdirAll(snapshotDataDir, 0o755); mkErr != nil {
				log.Warn().Err(mkErr).Str("task_id", taskID).Msg("failed to create snapshot data dir, running without snapshot")
			} else {
				cloned.WithSnapshot(snapshot.New(worktreeDir, snapshotDataDir))
			}
			runnerToUse = cloned
			// Reload env vars from disk and copy files into the worktree.
			if len(a.envFiles) > 0 {
				if err = envloader.Load(a.envFiles); err != nil {
					log.Warn().Err(err).Str("task_id", taskID).Msg("env reload failed for worktree")
				}
				if err = envloader.CopyInto(a.envFiles, worktreeDir); err != nil {
					log.Warn().Err(err).Str("task_id", taskID).Msg("env file copy into worktree failed")
				}
			}
			// Run the optional start command (e.g. "npm install") in the new worktree.
			// Failure is non-fatal — log a warning and continue task execution.
			if startCmd := a.config.WorktreeStartCommand; startCmd != "" {
				startExec := exec.CommandContext(ctx, "sh", "-c", startCmd)
				startExec.Dir = worktreeDir
				if out, cmdErr := startExec.CombinedOutput(); cmdErr != nil {
					log.Warn().
						Err(cmdErr).
						Str("task_id", taskID).
						Str("start_command", startCmd).
						Str("output", string(out)).
						Msg("worktree start command failed (non-fatal)")
				} else {
					log.Info().
						Str("task_id", taskID).
						Str("start_command", startCmd).
						Msg("worktree start command completed")
				}
			}
		}
	}

	err = runnerToUse.RunTask(ctx, task)
	if err != nil {
		var escalation *EscalationError
		if errors.As(err, &escalation) {
			// BUG-M01: Escalation is not a task failure — it's a signal to pause the ticket.
			// Return TaskStatusEscalated so the DB status set in RunTask is consistent
			// with the result reported to the orchestrator. Dependents are still skipped
			// because the DAG executor treats any non-Done status as a failure for propagation.
			return daemon.TaskResult{
				TaskID:         taskID,
				Status:         models.TaskStatusEscalated,
				Error:          err,
				WorktreeDir:    worktreeDir,
				WorktreeBranch: worktreeBranch,
			}
		}
		return daemon.TaskResult{
			TaskID:         taskID,
			Status:         models.TaskStatusFailed,
			Error:          err,
			WorktreeDir:    worktreeDir,
			WorktreeBranch: worktreeBranch,
		}
	}

	// REQ-PIPE-006: atomically increment and trigger consistency review when
	// the returned new value is a multiple of the configured interval.
	interval := int64(a.config.IntermediateReviewInterval)
	if interval > 0 {
		newCount := a.completedCount.Add(1)
		if newCount%interval == 0 {
			a.runIntermediateConsistencyReview(ctx)
		}
	}

	return daemon.TaskResult{
		TaskID:         taskID,
		Status:         models.TaskStatusDone,
		WorktreeDir:    worktreeDir,
		WorktreeBranch: worktreeBranch,
	}
}

// consistencyViolation is the JSON shape returned by the consistency review LLM.
type consistencyViolation struct {
	Pattern    string `json:"pattern"`
	File       string `json:"file"`
	Suggestion string `json:"suggestion"`
	Line       int    `json:"line"`
}

// runIntermediateConsistencyReview fires a lightweight LLM check after every
// IntermediateReviewInterval completed tasks. It never blocks on errors.
// The check is a no-op unless completedCount is a non-zero multiple of interval.
func (a *DAGTaskAdapter) runIntermediateConsistencyReview(ctx context.Context) {
	interval := a.config.IntermediateReviewInterval
	if interval <= 0 {
		return
	}
	count := a.completedCount.Load()
	if count == 0 || count%int64(interval) != 0 {
		return
	}
	if a.llm == nil || a.cdb == nil || a.git == nil {
		return
	}

	// Determine the diff base: use the SHA from the last review if available,
	// otherwise fall back to HEAD~<interval> for the first review.
	a.lastReviewedSHAMu.Lock()
	fromSHA := a.lastReviewedSHA
	a.lastReviewedSHAMu.Unlock()

	var diff string
	var err error
	if fromSHA == "" {
		diff, err = a.git.Diff(ctx, a.config.WorkDir, fmt.Sprintf("HEAD~%d", interval), "HEAD")
	} else {
		diff, err = a.git.Diff(ctx, a.config.WorkDir, fromSHA, "HEAD")
	}
	if err != nil {
		log.Warn().Err(err).Str("ticket", a.ticketID).Msg("consistency review: git diff failed, skipping")
		return
	}

	// Snapshot the current HEAD SHA so the next review diffs only the new tasks.
	headSHA := a.headSHA(ctx)

	prompt := buildConsistencyPrompt(diff, interval)
	resp, err := a.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: "You are a code consistency reviewer. Respond only with a JSON array of violations.",
		UserPrompt:   prompt,
		Stage:        "consistency_review",
		MaxTokens:    2048,
	})
	if err != nil {
		log.Warn().Err(err).Str("ticket", a.ticketID).Msg("consistency review: LLM call failed, skipping")
		return
	}

	violations, err := parseConsistencyViolations(resp.Content)
	if err != nil {
		log.Warn().Err(err).Str("ticket", a.ticketID).Msg("consistency review: failed to parse LLM response, skipping")
		return
	}

	for _, v := range violations {
		p := &models.ProgressPattern{
			TicketID:   a.ticketID,
			PatternKey: v.Pattern,
			PatternValue: fmt.Sprintf("file=%s line=%d suggestion=%s",
				v.File, v.Line, v.Suggestion),
		}
		if saveErr := a.cdb.SaveProgressPattern(ctx, p); saveErr != nil {
			log.Warn().Err(saveErr).Str("ticket", a.ticketID).Str("pattern", v.Pattern).
				Msg("consistency review: failed to save progress pattern")
		}
	}

	// Advance the SHA cursor only after a successful review.
	if headSHA != "" {
		a.lastReviewedSHAMu.Lock()
		a.lastReviewedSHA = headSHA
		a.lastReviewedSHAMu.Unlock()
	}

	log.Info().
		Str("ticket", a.ticketID).
		Int("violations", len(violations)).
		Int64("completed_tasks", a.completedCount.Load()).
		Msg("intermediate consistency review complete")
}

// headSHA returns the current HEAD commit SHA. Returns "" on error (non-fatal).
func (a *DAGTaskAdapter) headSHA(ctx context.Context) string {
	entries, err := a.git.Log(ctx, a.config.WorkDir, 1)
	if err != nil || len(entries) == 0 {
		return ""
	}
	return entries[0].SHA
}

// buildConsistencyPrompt constructs the LLM prompt for the consistency check.
// interval is the number of tasks covered by this diff window.
func buildConsistencyPrompt(diff string, interval int) string {
	return fmt.Sprintf(`You are reviewing changes from the last %d completed tasks.

Check ONLY for these cross-task consistency issues:
1. Naming conventions (variables, functions, types)
2. Error handling patterns (wrapping, propagation style)
3. Import grouping and ordering

Return a JSON array of violations (empty array if none):
[{"pattern": "<category>", "file": "<filename>", "line": <number>, "suggestion": "<fix>"}]

Diff:
%s`, interval, diff)
}

// parseConsistencyViolations extracts JSON violations from an LLM response.
// It tolerates markdown code fences and surrounding text.
func parseConsistencyViolations(content string) ([]consistencyViolation, error) {
	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "```"); idx != -1 {
		end := strings.LastIndex(content, "```")
		if end > idx {
			inner := content[idx+3 : end]
			// Strip optional language label on first line.
			if nl := strings.Index(inner, "\n"); nl != -1 {
				inner = inner[nl+1:]
			}
			content = strings.TrimSpace(inner)
		}
	}

	// Find the JSON array bounds.
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no JSON array found in LLM response")
	}
	content = content[start : end+1]

	var violations []consistencyViolation
	if err := json.Unmarshal([]byte(content), &violations); err != nil {
		return nil, fmt.Errorf("unmarshal violations: %w", err)
	}
	return violations, nil
}

func (a *DAGTaskAdapter) findTask(ctx context.Context, taskID string) (*models.Task, error) {
	tasks, err := a.db.ListTasks(ctx, a.ticketID)
	if err != nil {
		return nil, fmt.Errorf("listing tasks for ticket %s: %w", a.ticketID, err)
	}
	for i := range tasks {
		if tasks[i].ID == taskID {
			return &tasks[i], nil
		}
	}
	return nil, fmt.Errorf("task %s not found in ticket %s", taskID, a.ticketID)
}

// Compile-time check.
var _ daemon.TaskRunner = (*DAGTaskAdapter)(nil)
