package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
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
	ticketID          string
	lastReviewedSHA   string
	config            TaskRunnerConfig
	completedCount    atomic.Int64
	lastReviewedSHAMu sync.Mutex
}

// NewDAGTaskAdapter creates an adapter that connects PipelineTaskRunner to DAGExecutor.
func NewDAGTaskAdapter(runner *PipelineTaskRunner, db TaskRunnerDB, ticketID string) *DAGTaskAdapter {
	return &DAGTaskAdapter{runner: runner, db: db, ticketID: ticketID}
}

// NewDAGTaskAdapterWithConsistency creates an adapter with intermediate consistency
// review support (REQ-PIPE-006). Pass interval=0 to disable the check.
func NewDAGTaskAdapterWithConsistency(
	runner *PipelineTaskRunner,
	db TaskRunnerDB,
	ticketID string,
	llm LLMProvider,
	cdb ConsistencyReviewDB,
	gitProv git.GitProvider,
	config TaskRunnerConfig,
) *DAGTaskAdapter {
	return &DAGTaskAdapter{
		runner:   runner,
		db:       db,
		ticketID: ticketID,
		llm:      llm,
		cdb:      cdb,
		git:      gitProv,
		config:   config,
	}
}

// Run implements daemon.TaskRunner. It looks up the task by ID from the DB
// task list and delegates to PipelineTaskRunner.RunTask.
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

	err = a.runner.RunTask(ctx, task)
	if err != nil {
		var escalation *EscalationError
		if errors.As(err, &escalation) {
			// BUG-M01: Escalation is not a task failure — it's a signal to pause the ticket.
			// Return TaskStatusEscalated so the DB status set in RunTask is consistent
			// with the result reported to the orchestrator. Dependents are still skipped
			// because the DAG executor treats any non-Done status as a failure for propagation.
			return daemon.TaskResult{
				TaskID: taskID,
				Status: models.TaskStatusEscalated,
				Error:  err,
			}
		}
		return daemon.TaskResult{
			TaskID: taskID,
			Status: models.TaskStatusFailed,
			Error:  err,
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
		TaskID: taskID,
		Status: models.TaskStatusDone,
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
