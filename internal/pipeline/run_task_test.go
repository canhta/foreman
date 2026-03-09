// run_task_test.go tests PipelineTaskRunner.RunTask and its component methods.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers reused across subtests ---

func newTaskRunnerForTest(t *testing.T, db TaskRunnerDB, llm LLMProvider, g git.GitProvider, cmd runner.CommandRunner, cfg TaskRunnerConfig) *PipelineTaskRunner {
	t.Helper()
	if cfg.WorkDir == "" {
		cfg.WorkDir = t.TempDir()
	}
	if cfg.MaxLlmCallsPerTask == 0 {
		cfg.MaxLlmCallsPerTask = 8
	}
	if cfg.SearchReplaceSimilarity == 0 {
		cfg.SearchReplaceSimilarity = 0.8
	}
	return NewPipelineTaskRunner(llm, db, g, cmd, cfg, mustLoadTestRegistry(t))
}

func simpleTask(id, title string) *models.Task {
	return &models.Task{ID: id, Title: title}
}

func approvedLLM() *mockLLM {
	return &mockLLM{
		responses: map[string]string{
			"implementer":      buildNewFileResponse("out.go", "package main\n"),
			"spec_reviewer":    "STATUS: APPROVED\nCRITERIA:\n- [pass] done\nISSUES:\n- None",
			"quality_reviewer": "STATUS: APPROVED\nISSUES:\n- None",
		},
	}
}

// =============================================
// RunTask: happy path
// =============================================

func TestRunTask_HappyPath_NewFile(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+package main", commitSHA: "sha1"}
	cmd := &realMockCmdRunner{exitCode: 0}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 1,
		EnableTDDVerification:    false,
	})

	task := simpleTask("t1", "Add main package")
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, models.TaskStatusDone, db.statuses["t1"])
}

func TestRunTask_HappyPath_WithAcceptanceCriteria(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+feature", commitSHA: "sha2"}
	cmd := &realMockCmdRunner{exitCode: 0}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 1,
		EnableTDDVerification:    false,
	})

	task := &models.Task{
		ID:                 "t2",
		Title:              "Add feature",
		AcceptanceCriteria: []string{"feature exists", "tests pass"},
	}
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, models.TaskStatusDone, db.statuses["t2"])
}

// =============================================
// RunTask: status transitions
// =============================================

func TestRunTask_StatusTransitions(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+x", commitSHA: "sha3"}
	cmd := &realMockCmdRunner{exitCode: 0}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 0,
		EnableTDDVerification:    false,
	})

	task := simpleTask("t3", "Simple task")
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)

	// Final status must be Done.
	assert.Equal(t, models.TaskStatusDone, db.statuses["t3"])
}

func TestRunTask_UpdateStatusError_PropagatesImmediately(t *testing.T) {
	db := newMockTaskRunnerDB()
	db.updateErr = fmt.Errorf("db write failed")

	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{})

	err := r.RunTask(context.Background(), simpleTask("t4", "Failing status update"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update task status")
}

// =============================================
// RunTask: escalation
// =============================================

func TestRunTask_Escalation(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{}
	llm := &mockLLM{
		responses: map[string]string{
			"implementer": "NEEDS_CLARIFICATION: Which DB engine should I use?",
		},
	}

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 1,
		EnableTDDVerification:    false,
	})

	err := r.RunTask(context.Background(), simpleTask("t5", "Ambiguous task"))
	require.Error(t, err)

	var esc *EscalationError
	require.ErrorAs(t, err, &esc)
	assert.Contains(t, esc.Question, "DB engine")
}

// =============================================
// RunTask: retries exhausted
// =============================================

func TestRunTask_AllRetriesExhausted(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{}
	llm := &mockLLM{
		responses: map[string]string{
			"implementer": "this is not valid implementer output at all",
		},
	}

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 2,
		EnableTDDVerification:    false,
	})

	err := r.RunTask(context.Background(), simpleTask("t6", "Broken task"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after")

	// DB status should be set to Failed.
	assert.Equal(t, models.TaskStatusFailed, db.statuses["t6"])
}

// =============================================
// RunTask: call cap
// =============================================

func TestRunTask_CallCapExceeded(t *testing.T) {
	db := newMockTaskRunnerDB()
	// Pre-seed call count to just at limit so next increment exceeds it.
	db.callCounts["t7"] = 8 // MaxLlmCallsPerTask = 8; next call would be count=9 > 8

	g := &realMockGitProvider{}
	cmd := &realMockCmdRunner{}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 3,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
	})

	err := r.RunTask(context.Background(), simpleTask("t7", "Cap-limited task"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call cap")
}

// =============================================
// RunTask: test failures trigger retry
// =============================================

func TestRunTask_TestFailure_TriggersRetry(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+x", commitSHA: "sha4"}

	callCount := 0
	// First call: test fails. Second call: test passes.
	cmd := &callCountingRunner{
		responses: func(n int) *runner.CommandOutput {
			if n == 1 {
				return &runner.CommandOutput{Stdout: "FAIL\n--- FAIL: TestFoo", ExitCode: 1}
			}
			return &runner.CommandOutput{Stdout: "ok", ExitCode: 0}
		},
		callCount: &callCount,
	}
	llm := approvedLLM()

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 2,
		MaxLlmCallsPerTask:       8,
		TestCommand:              "go test ./...",
		EnableTDDVerification:    false,
	})

	task := simpleTask("t8", "Test retry task")
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, models.TaskStatusDone, db.statuses["t8"])
	assert.Equal(t, 2, callCount, "expected exactly 2 test runs (fail then pass)")
}

// =============================================
// RunTask: spec review rejection triggers retry
// =============================================

func TestRunTask_SpecReviewRejection_TriggersRetry(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+spec", commitSHA: "sha5"}
	cmd := &realMockCmdRunner{exitCode: 0}

	specCallCount := 0
	llm := &callCountingLLM{
		specResponder: func(n int) string {
			specCallCount++
			if n == 1 {
				return "STATUS: REJECTED\nCRITERIA:\n- [fail] missing validation\nISSUES:\n- Missing input validation"
			}
			return "STATUS: APPROVED\nCRITERIA:\n- [pass] all done\nISSUES:\n- None"
		},
		implResponse:    buildNewFileResponse("x.go", "package main\n"),
		qualityResponse: "STATUS: APPROVED\nISSUES:\n- None",
	}

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 3,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
	})

	task := &models.Task{
		ID:                 "t9",
		Title:              "Spec rejection task",
		AcceptanceCriteria: []string{"validation exists"},
	}
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, models.TaskStatusDone, db.statuses["t9"])
	assert.Equal(t, 2, specCallCount, "spec reviewer should be called twice (reject then approve)")
}

// =============================================
// RunTask: resetWorkingTree (via git checkout) called between retries, not CleanWorkingTree
// =============================================

func TestRunTask_ResetWorkingTree_CalledBetweenRetries_NotCleanWorkingTree(t *testing.T) {
	db := newMockTaskRunnerDB()
	cleanCount := 0
	// CleanWorkingTree must NOT be called — we track it to assert it stays at 0.
	g := &realMockGitProvider{diffOutput: "+x", commitSHA: "sha-clean", cleanCalled: &cleanCount}

	var checkoutArgs [][]string
	baseCmd := &realMockCmdRunner{exitCode: 0}
	trackCmd := &trackingCmdRunner{inner: baseCmd, checkoutArgs: &checkoutArgs}

	// First attempt: invalid output (triggers retry). Second attempt: valid.
	llm2 := &implCountingLLM{
		implResponses: func(n int) string {
			if n == 1 {
				return "this is not valid implementer output"
			}
			return buildNewFileResponse("out.go", "package main\n")
		},
		specResponse:    "STATUS: APPROVED\nCRITERIA:\n- [pass] ok\nISSUES:\n- None",
		qualityResponse: "STATUS: APPROVED\nISSUES:\n- None",
	}

	r := newTaskRunnerForTest(t, db, llm2, g, trackCmd, TaskRunnerConfig{
		MaxImplementationRetries: 2,
		EnableTDDVerification:    false,
	})

	task := &models.Task{
		ID:            "t-clean",
		Title:         "Clean test",
		FilesToModify: []string{"handler.go"},
	}
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)

	// CleanWorkingTree (nuke-all) must NOT be called — only per-file checkout is allowed.
	assert.Equal(t, 0, cleanCount, "CleanWorkingTree must not be called; use resetWorkingTree instead")

	// At least one git checkout -- call must have happened (for the retry).
	hasCheckout := false
	for _, args := range checkoutArgs {
		if len(args) >= 2 && args[0] == "git" && args[1] == "checkout" {
			hasCheckout = true
		}
	}
	assert.True(t, hasCheckout, "resetWorkingTree should have invoked git checkout between retries")
}

// =============================================
// RunTask: resetWorkingTree only touches FilesToModify files (BUG-C02)
// =============================================

func TestRunTask_ResetWorkingTree_OnlyTouchesFilesToModify(t *testing.T) {
	workDir := t.TempDir()

	// Create two files in the work directory.
	fileA := "internal/handler.go"
	fileB := "internal/util.go"
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, fileA), []byte("package handler\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, fileB), []byte("package util\n"), 0o644))

	// Track which git checkout -- calls are made.
	var checkoutArgs [][]string

	baseCmd := &realMockCmdRunner{exitCode: 0}
	trackCmd := &trackingCmdRunner{
		inner:        baseCmd,
		checkoutArgs: &checkoutArgs,
	}

	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+x", commitSHA: "sha-reset"}

	// First attempt: implementer gives bad output (triggers retry).
	// Second attempt: implementer succeeds.
	callN := 0
	llm3 := &implCountingLLM{
		implResponses: func(n int) string {
			callN = n
			if n == 1 {
				return "not valid implementer output at all"
			}
			return buildNewFileResponse("out.go", "package main\n")
		},
		specResponse:    "STATUS: APPROVED\nCRITERIA:\n- [pass] ok\nISSUES:\n- None",
		qualityResponse: "STATUS: APPROVED\nISSUES:\n- None",
	}

	r := newTaskRunnerForTest(t, db, llm3, g, trackCmd, TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 2,
		EnableTDDVerification:    false,
	})

	task := &models.Task{
		ID:            "t-reset",
		Title:         "Reset test",
		FilesToModify: []string{fileA}, // only fileA is in scope
	}
	err := r.RunTask(context.Background(), task)
	require.NoError(t, err)
	_ = callN

	// After retry, git checkout -- should have been called with fileA only,
	// not fileB and not ".".
	checkedOut := false
	for _, args := range checkoutArgs {
		// args is the argv slice for git checkout
		if len(args) >= 3 && args[0] == "git" && args[1] == "checkout" {
			// Should contain "--" and the specific file.
			argsStr := strings.Join(args, " ")
			assert.NotContains(t, argsStr, fileB, "resetWorkingTree must not touch fileB which is not in FilesToModify")
			assert.NotContains(t, argsStr, "checkout -- .", "resetWorkingTree must not run git checkout -- . (all files)")
			if strings.Contains(argsStr, fileA) {
				checkedOut = true
			}
		}
	}
	assert.True(t, checkedOut, "resetWorkingTree should have run git checkout -- for fileA")
}

// =============================================
// resetWorkingTree: selective git error handling
// =============================================

func TestResetWorkingTree_SuppressesNotTrackedError(t *testing.T) {
	// A "did not match any file(s) known to git" stderr with non-zero exit
	// should be silently ignored (new file, never committed).
	cmd := &stderrCmdRunner{
		stderr:   "error: pathspec 'new_file.go' did not match any file(s) known to git",
		exitCode: 128,
	}
	r := &PipelineTaskRunner{
		cmdRunner: cmd,
		config:    TaskRunnerConfig{WorkDir: t.TempDir()},
	}
	err := r.resetWorkingTree(context.Background(), []string{"new_file.go"})
	assert.NoError(t, err, "untracked-file error should be suppressed")
}

func TestResetWorkingTree_ReturnsOtherGitErrors(t *testing.T) {
	// A non-zero exit with a different stderr should be returned as an error.
	cmd := &stderrCmdRunner{
		stderr:   "fatal: not a git repository",
		exitCode: 128,
	}
	r := &PipelineTaskRunner{
		cmdRunner: cmd,
		config:    TaskRunnerConfig{WorkDir: t.TempDir()},
	}
	err := r.resetWorkingTree(context.Background(), []string{"some_file.go"})
	require.Error(t, err, "non-pathspec git errors should propagate")
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestResetWorkingTree_ReturnsRunError(t *testing.T) {
	// Run() returning a non-nil error (e.g. git binary missing) must propagate.
	cmd := &realMockCmdRunner{runErr: fmt.Errorf("exec: git not found")}
	r := &PipelineTaskRunner{
		cmdRunner: cmd,
		config:    TaskRunnerConfig{WorkDir: t.TempDir()},
	}
	err := r.resetWorkingTree(context.Background(), []string{"handler.go"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git not found")
}

func TestResetWorkingTree_SuccessIsTransparent(t *testing.T) {
	// Exit code 0 — no error.
	cmd := &realMockCmdRunner{exitCode: 0}
	r := &PipelineTaskRunner{
		cmdRunner: cmd,
		config:    TaskRunnerConfig{WorkDir: t.TempDir()},
	}
	err := r.resetWorkingTree(context.Background(), []string{"main.go", "util.go"})
	assert.NoError(t, err)
}

// stderrCmdRunner returns a fixed stderr/exit-code response (simulates git failures).
type stderrCmdRunner struct {
	stderr   string
	exitCode int
}

func (m *stderrCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stderr: m.stderr, ExitCode: m.exitCode}, nil
}

func (m *stderrCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

// trackingCmdRunner wraps another CommandRunner and records git checkout calls.
type trackingCmdRunner struct {
	inner        runner.CommandRunner
	checkoutArgs *[][]string
}

func (m *trackingCmdRunner) Run(ctx context.Context, workDir, cmd string, args []string, timeout int) (*runner.CommandOutput, error) {
	if cmd == "git" && len(args) >= 2 && args[0] == "checkout" {
		full := append([]string{"git"}, args...)
		*m.checkoutArgs = append(*m.checkoutArgs, full)
	}
	return m.inner.Run(ctx, workDir, cmd, args, timeout)
}

func (m *trackingCmdRunner) CommandExists(ctx context.Context, name string) bool {
	return m.inner.CommandExists(ctx, name)
}

// implCountingLLM tracks per-role call counts with separate impl call counter.
type implCountingLLM struct {
	implResponses   func(n int) string
	specResponse    string
	qualityResponse string
	implCallN       int
}

func (m *implCountingLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	var content string
	switch {
	case contains(req.SystemPrompt, "reviewing code changes for spec compliance") ||
		contains(req.SystemPrompt, "verify that the implementation satisfies"):
		content = m.specResponse
	case contains(req.SystemPrompt, "reviewing code changes for quality") ||
		contains(req.SystemPrompt, "review code quality"):
		content = m.qualityResponse
	default:
		m.implCallN++
		content = m.implResponses(m.implCallN)
	}
	return &models.LlmResponse{
		Content:    content,
		StopReason: models.StopReasonEndTurn,
		Model:      "test-model",
	}, nil
}

func (m *implCountingLLM) ProviderName() string                { return "mock" }
func (m *implCountingLLM) HealthCheck(_ context.Context) error { return nil }

// =============================================
// RunTask: quality review with only MINOR issues now rejects
// =============================================

func TestRunTask_QualityReview_MinorOnly_Rejects(t *testing.T) {
	db := newMockTaskRunnerDB()
	g := &realMockGitProvider{diffOutput: "+quality", commitSHA: "sha6"}
	cmd := &realMockCmdRunner{exitCode: 0}
	llm := &mockLLM{
		responses: map[string]string{
			"implementer": buildNewFileResponse("y.go", "package main\n"),
			// CHANGES_REQUESTED with only MINOR issues should still reject.
			"quality_reviewer": "STATUS: CHANGES_REQUESTED\nISSUES:\n- [MINOR] naming: rename foo to bar",
		},
	}

	r := newTaskRunnerForTest(t, db, llm, g, cmd, TaskRunnerConfig{
		MaxImplementationRetries: 1,
		MaxLlmCallsPerTask:       8,
		EnableTDDVerification:    false,
	})

	err := r.RunTask(context.Background(), simpleTask("t10", "Minor quality issues"))
	require.Error(t, err, "CHANGES_REQUESTED should cause rejection regardless of severity")
	assert.Equal(t, models.TaskStatusFailed, db.statuses["t10"])
}

// =============================================
// loadContextFiles
// =============================================

func TestLoadContextFiles_ReadsExistingFiles(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "util.go"), []byte("package main\n\nfunc helper() {}\n"), 0o644))

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir}}
	files := r.loadContextFiles([]string{"main.go", "util.go"}, 0)

	assert.Len(t, files, 2)
	assert.Equal(t, "package main\n", files["main.go"])
	assert.Contains(t, files["util.go"], "helper")
}

func TestLoadContextFiles_SkipsMissingFiles(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exists.go"), []byte("package x\n"), 0o644))

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir}}
	files := r.loadContextFiles([]string{"exists.go", "does_not_exist.go"}, 0)

	// Missing files now produce a [FILE NOT FOUND: ...] placeholder entry (BUG-M14 fix).
	assert.Len(t, files, 2)
	assert.Contains(t, files, "exists.go")
	assert.Contains(t, files["does_not_exist.go"], "[FILE NOT FOUND:")
}

func TestLoadContextFiles_EmptyPaths(t *testing.T) {
	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: t.TempDir()}}
	files := r.loadContextFiles(nil, 0)
	assert.Empty(t, files)
}

// =============================================
// applyChanges
// =============================================

func TestApplyChanges_CreatesNewFile(t *testing.T) {
	workDir := t.TempDir()
	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir, SearchReplaceSimilarity: 0.8}}

	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "new_pkg/new.go", IsNew: true, Content: "package new_pkg\n"},
		},
	}

	require.NoError(t, r.applyChanges(parsed))

	data, err := os.ReadFile(filepath.Join(workDir, "new_pkg", "new.go"))
	require.NoError(t, err)
	assert.Equal(t, "package new_pkg\n", string(data))
}

func TestApplyChanges_PatchesExistingFile(t *testing.T) {
	workDir := t.TempDir()
	original := "package main\n\nfunc Add(a, b int) int {\n\treturn 0\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "math.go"), []byte(original), 0o644))

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir, SearchReplaceSimilarity: 0.8}}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{
				Path:  "math.go",
				IsNew: false,
				Patches: []SearchReplace{
					{Search: "return 0", Replace: "return a + b"},
				},
			},
		},
	}

	require.NoError(t, r.applyChanges(parsed))

	data, err := os.ReadFile(filepath.Join(workDir, "math.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "return a + b")
}

func TestApplyChanges_MissingFileReturnsError(t *testing.T) {
	workDir := t.TempDir()
	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir, SearchReplaceSimilarity: 0.8}}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "nonexistent.go", IsNew: false, Patches: []SearchReplace{{Search: "x", Replace: "y"}}},
		},
	}
	err := r.applyChanges(parsed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent.go")
}

func TestApplyChanges_Atomic_SecondFileFails_FirstFileUnchanged(t *testing.T) {
	workDir := t.TempDir()
	originalA := "line1\nline2\nline3\n"
	originalB := "foo\nbar\nbaz\n"
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "a.go"), []byte(originalA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "b.go"), []byte(originalB), 0o644))

	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir, SearchReplaceSimilarity: 0.8}}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{
				Path:    "a.go",
				IsNew:   false,
				Patches: []SearchReplace{{Search: "line1", Replace: "REPLACED1"}}, // valid
			},
			{
				Path:    "b.go",
				IsNew:   false,
				Patches: []SearchReplace{{Search: "THIS_DOES_NOT_EXIST", Replace: "x"}}, // fails
			},
		},
	}

	err := r.applyChanges(parsed)
	require.Error(t, err, "second file patch should fail")

	// File a.go must be unchanged — writes should be all-or-nothing.
	data, readErr := os.ReadFile(filepath.Join(workDir, "a.go"))
	require.NoError(t, readErr)
	assert.Equal(t, originalA, string(data), "a.go should be unchanged when b.go patch fails")
}

func TestApplyChanges_CreatesSubdirectory(t *testing.T) {
	workDir := t.TempDir()
	r := &PipelineTaskRunner{config: TaskRunnerConfig{WorkDir: workDir}}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "a/b/c/deep.go", IsNew: true, Content: "package c\n"},
		},
	}
	require.NoError(t, r.applyChanges(parsed))

	_, err := os.Stat(filepath.Join(workDir, "a", "b", "c", "deep.go"))
	assert.NoError(t, err)
}

// =============================================
// runTests
// =============================================

func TestRunTests_NoCommand_ReturnsPass(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{TestCommand: ""},
		cmdRunner: &realMockCmdRunner{exitCode: 0},
	}
	output, passed := r.runTests(context.Background())
	assert.True(t, passed)
	assert.Equal(t, "", output)
}

func TestRunTests_ExitCodeZero_Passes(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{WorkDir: t.TempDir(), TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{stdout: "ok  example.com\n", exitCode: 0},
	}
	output, passed := r.runTests(context.Background())
	assert.True(t, passed)
	assert.Contains(t, output, "ok")
}

func TestRunTests_ExitCodeNonZero_Fails(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{WorkDir: t.TempDir(), TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{stdout: "FAIL", stderr: "test failed", exitCode: 1},
	}
	output, passed := r.runTests(context.Background())
	assert.False(t, passed)
	assert.Contains(t, output, "FAIL")
}

func TestRunTests_RunError_Fails(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{WorkDir: t.TempDir(), TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{runErr: fmt.Errorf("binary not found")},
	}
	output, passed := r.runTests(context.Background())
	assert.False(t, passed)
	assert.Contains(t, output, "binary not found")
}

// =============================================
// runTDDVerification
// =============================================

func TestRunTDDVerification_NoTestFiles_Invalid(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{EnableTDDVerification: true, TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{exitCode: 0},
	}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "main.go", IsNew: true, Content: "package main"},
		},
	}
	result := r.runTDDVerification(context.Background(), parsed)
	assert.False(t, result.Valid)
	assert.Equal(t, "red", result.Phase)
	assert.Contains(t, result.Reason, "no test files")
}

func TestRunTDDVerification_NoTestCommand_Valid(t *testing.T) {
	r := &PipelineTaskRunner{
		config: TaskRunnerConfig{EnableTDDVerification: true, TestCommand: ""},
	}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "main_test.go", IsNew: true, Content: "package main"},
		},
	}
	result := r.runTDDVerification(context.Background(), parsed)
	assert.True(t, result.Valid)
	assert.Equal(t, "green", result.Phase)
}

func TestRunTDDVerification_AssertionFailure_ValidRED(t *testing.T) {
	r := &PipelineTaskRunner{
		config: TaskRunnerConfig{TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{
			stdout:   "--- FAIL: TestAdd (expected 5 got 0)",
			exitCode: 1,
		},
	}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "math_test.go", IsNew: true, Content: "package math"},
		},
	}
	result := r.runTDDVerification(context.Background(), parsed)
	assert.True(t, result.Valid)
	assert.Equal(t, "red", result.Phase)
}

func TestRunTDDVerification_CompileError_InvalidRED(t *testing.T) {
	r := &PipelineTaskRunner{
		config: TaskRunnerConfig{TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{
			stderr:   "build failed: syntax error",
			exitCode: 1,
		},
	}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "foo_test.go", IsNew: true, Content: "package foo"},
		},
	}
	result := r.runTDDVerification(context.Background(), parsed)
	assert.False(t, result.Valid)
	assert.Equal(t, "red", result.Phase)
	assert.Contains(t, result.Reason, "compile")
}

func TestRunTDDVerification_TestsPass_GreenPhase(t *testing.T) {
	r := &PipelineTaskRunner{
		config:    TaskRunnerConfig{TestCommand: "go test ./..."},
		cmdRunner: &realMockCmdRunner{stdout: "ok  example.com\n", exitCode: 0},
	}
	parsed := &ParsedOutput{
		Files: []FileChange{
			{Path: "bar_test.go", IsNew: true, Content: "package bar"},
		},
	}
	result := r.runTDDVerification(context.Background(), parsed)
	assert.True(t, result.Valid)
	assert.Equal(t, "green", result.Phase)
}

// =============================================
// runSpecReview
// =============================================

func TestRunSpecReview_Approved(t *testing.T) {
	db := newMockTaskRunnerDB()
	llm := &mockLLM{
		responses: map[string]string{
			"spec_reviewer": "STATUS: APPROVED\nCRITERIA:\n- [pass] all done\nISSUES:\n- None",
		},
	}
	r := &PipelineTaskRunner{
		db:           db,
		specReviewer: NewSpecReviewer(llm, mustLoadTestRegistry(t)),
		config: TaskRunnerConfig{
			MaxLlmCallsPerTask: 8,
		},
	}
	task := &models.Task{ID: "sr1", Title: "Spec task", AcceptanceCriteria: []string{"do X"}}
	feedback := NewFeedbackAccumulator()

	err := r.runSpecReview(context.Background(), task, "+diff", "ok", feedback)
	assert.NoError(t, err)
	assert.False(t, feedback.HasFeedback())
}

func TestRunSpecReview_Rejected_ReturnsSentinel(t *testing.T) {
	db := newMockTaskRunnerDB()
	llm := &mockLLM{
		responses: map[string]string{
			"spec_reviewer": "STATUS: REJECTED\nCRITERIA:\n- [fail] missing X\nISSUES:\n- Need to add X",
		},
	}
	r := &PipelineTaskRunner{
		db:           db,
		specReviewer: NewSpecReviewer(llm, mustLoadTestRegistry(t)),
		config:       TaskRunnerConfig{MaxLlmCallsPerTask: 8},
	}
	task := &models.Task{ID: "sr2", Title: "Failing spec", AcceptanceCriteria: []string{"do X"}}
	feedback := NewFeedbackAccumulator()

	err := r.runSpecReview(context.Background(), task, "+diff", "FAIL", feedback)
	require.Error(t, err)

	_, ok := err.(*reviewRejectedError)
	assert.True(t, ok, "expected *reviewRejectedError")
	assert.True(t, feedback.HasFeedback())
}

// =============================================
// runQualityReview
// =============================================

func TestRunQualityReview_Approved(t *testing.T) {
	db := newMockTaskRunnerDB()
	llm := &mockLLM{
		responses: map[string]string{
			"quality_reviewer": "STATUS: APPROVED\nISSUES:\n- None",
		},
	}
	r := &PipelineTaskRunner{
		db:              db,
		qualityReviewer: NewQualityReviewer(llm, mustLoadTestRegistry(t)),
		config:          TaskRunnerConfig{MaxLlmCallsPerTask: 8},
	}
	feedback := NewFeedbackAccumulator()

	err := r.runQualityReview(context.Background(), "qr-approved", "+diff", feedback)
	assert.NoError(t, err)
}

func TestRunQualityReview_CriticalIssue_ReturnsSentinel(t *testing.T) {
	db := newMockTaskRunnerDB()
	llm := &mockLLM{
		responses: map[string]string{
			"quality_reviewer": "STATUS: CHANGES_REQUESTED\nISSUES:\n- [CRITICAL] SQL injection in handler",
		},
	}
	r := &PipelineTaskRunner{
		db:              db,
		qualityReviewer: NewQualityReviewer(llm, mustLoadTestRegistry(t)),
		config:          TaskRunnerConfig{MaxLlmCallsPerTask: 8},
	}
	feedback := NewFeedbackAccumulator()

	err := r.runQualityReview(context.Background(), "qr-critical", "+bad", feedback)
	require.Error(t, err)

	_, ok := err.(*reviewRejectedError)
	assert.True(t, ok, "expected *reviewRejectedError")
	assert.True(t, feedback.HasFeedback())
}

// =============================================
// RunTask: quality review with IMPORTANT issues (no CRITICAL) should reject
// =============================================

func TestRunQualityReview_ImportantOnly_ReturnsRejection(t *testing.T) {
	db := newMockTaskRunnerDB()
	llm := &mockLLM{
		responses: map[string]string{
			// CHANGES_REQUESTED with only IMPORTANT issues, no CRITICAL.
			"quality_reviewer": "STATUS: CHANGES_REQUESTED\nISSUES:\n- [IMPORTANT] missing error handling in handler",
		},
	}
	r := &PipelineTaskRunner{
		db:              db,
		qualityReviewer: NewQualityReviewer(llm, mustLoadTestRegistry(t)),
		config:          TaskRunnerConfig{MaxLlmCallsPerTask: 8},
	}
	feedback := NewFeedbackAccumulator()

	err := r.runQualityReview(context.Background(), "qr-important", "+diff", feedback)
	require.Error(t, err, "CHANGES_REQUESTED with IMPORTANT issues should be rejected")

	_, ok := err.(*reviewRejectedError)
	assert.True(t, ok, "expected *reviewRejectedError")
	assert.True(t, feedback.HasFeedback(), "feedback should contain quality issues")
}

// =============================================
// auxiliary mock types for this file
// =============================================

// callCountingRunner lets tests inspect how many times tests were run.
type callCountingRunner struct {
	responses func(n int) *runner.CommandOutput
	callCount *int
}

func (m *callCountingRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	*m.callCount++
	return m.responses(*m.callCount), nil
}

func (m *callCountingRunner) CommandExists(_ context.Context, _ string) bool { return true }

// callCountingLLM tracks per-role call counts to enable staged responses.
type callCountingLLM struct {
	specResponder   func(n int) string
	implResponse    string
	qualityResponse string
	specCallN       int
}

func (m *callCountingLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	var content string
	switch {
	case contains(req.SystemPrompt, "reviewing code changes for spec compliance") ||
		contains(req.SystemPrompt, "verify that the implementation satisfies"):
		m.specCallN++
		content = m.specResponder(m.specCallN)
	case contains(req.SystemPrompt, "reviewing code changes for quality") ||
		contains(req.SystemPrompt, "review code quality"):
		content = m.qualityResponse
	default:
		content = m.implResponse
	}
	return &models.LlmResponse{
		Content:    content,
		StopReason: models.StopReasonEndTurn,
		Model:      "test-model",
	}, nil
}

func (m *callCountingLLM) ProviderName() string                { return "mock" }
func (m *callCountingLLM) HealthCheck(_ context.Context) error { return nil }
