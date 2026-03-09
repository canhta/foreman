# Remove Legacy Prompt Infrastructure — Hard Cut

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Completely delete all legacy prompt infrastructure — no fallbacks, no `if registry != nil` guards, no `.md.j2` files, no `SkillInjector`, no `PromptBuilder`, no `RenderPrompt`. Registry is the only path.

**Architecture:** Every pipeline component that currently has a `nil`-guarded `registry` field is refactored so the registry is a required constructor argument. `PipelineTaskRunner.runTaskWithAgent` replaces `SkillInjector` + `PromptBuilder` with `registry.ForClaude()` + `registry.Render()`. `prompt_renderer.go`, `prompt_builder.go`, and `skill_injector.go` (plus their tests and embedded assets) are deleted entirely.

**Tech Stack:** Go, `internal/prompts` registry (already implemented), pongo2

---

### Task 1: Make `Implementer` require registry; delete fallback

**Files:**
- Modify: `internal/pipeline/implementer.go`

The goal: remove `buildImplementerSystemPrompt()`, `buildImplementerUserPrompt()`, `retryLabelAndGuidance()`, `retryHeadingAndGuidance()`, and all `if impl.registry != nil` guards. The implementer now requires a registry and always uses `registry.Render()` for both system prompt and user prompt (via the `implementer` and `implementer-retry` roles).

**Step 1: Read the current implementer role prompts to understand the variables expected**

Run: `cat /Users/canh/Projects/Indies/Foreman/prompts/roles/implementer/ROLE.md`
Run: `cat /Users/canh/Projects/Indies/Foreman/prompts/roles/implementer-retry/ROLE.md`

**Step 2: Rewrite `implementer.go`**

Replace the entire file content with:

```go
// internal/pipeline/implementer.go
package pipeline

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/prompts"
)

// Implementer generates code changes for a task via LLM using TDD.
type Implementer struct {
	llm      LLMProvider
	registry *prompts.Registry
}

// NewImplementer creates an Implementer with the given LLM provider and registry.
// Registry is required; NewImplementer panics if reg is nil.
func NewImplementer(provider LLMProvider, reg *prompts.Registry) *Implementer {
	if reg == nil {
		panic("implementer: registry must not be nil")
	}
	return &Implementer{llm: provider, registry: reg}
}

// ImplementerInput holds all parameters for a single implementer call.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type ImplementerInput struct {
	Task           *models.Task
	ContextFiles   map[string]string
	Model          string
	Feedback       string
	PromptVersion  string
	MaxTokens      int
	Attempt        int
	RetryErrorType ErrorType
}

// ImplementerResult holds the raw LLM response from the implementer.
type ImplementerResult struct {
	Response *models.LlmResponse
}

// Execute runs the implementer step and returns the LLM response.
func (impl *Implementer) Execute(ctx context.Context, input ImplementerInput) (*ImplementerResult, error) {
	roleName := "implementer"
	if input.Attempt > 1 {
		roleName = "implementer-retry"
	}

	vars := map[string]any{
		"task_title":          input.Task.Title,
		"task_description":    input.Task.Description,
		"acceptance_criteria": input.Task.AcceptanceCriteria,
		"context_files":       input.ContextFiles,
		"codebase_patterns":   "",
		"attempt":             input.Attempt,
		"retry_feedback":      input.Feedback,
		"retry_error_type":    string(input.RetryErrorType),
	}

	rendered, err := impl.registry.Render(prompts.KindRole, roleName, vars)
	if err != nil {
		return nil, fmt.Errorf("render %s prompt: %w", roleName, err)
	}

	resp, err := impl.llm.Complete(ctx, models.LlmRequest{
		Model:             input.Model,
		SystemPrompt:      rendered,
		UserPrompt:        "Implement the task.",
		PromptVersion:     input.PromptVersion,
		Stage:             "implementing",
		MaxTokens:         input.MaxTokens,
		Temperature:       0.0,
		CacheSystemPrompt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("implementer LLM call: %w", err)
	}

	return &ImplementerResult{Response: resp}, nil
}

// retryErrorTypeLabel maps an ErrorType to its short label.
// Used only for metrics/logging — not for prompt generation.
func retryErrorTypeLabel(errType ErrorType) string {
	switch errType {
	case ErrorTypeCompile:
		return "Compile Error"
	case ErrorTypeTypeError:
		return "Type Error"
	case ErrorTypeLintStyle:
		return "Lint/Style"
	case ErrorTypeTestAssertion:
		return "Test Assertion"
	case ErrorTypeTestRuntime:
		return "Test Runtime"
	case ErrorTypeSpecViolation:
		return "Spec Violation"
	case ErrorTypeQualityConcern:
		return "Quality Concern"
	default:
		return ""
	}
}
```

**Step 3: Run: `go build ./internal/pipeline/`**

Expected: compile errors pointing to callers of the old `NewImplementer(llm)` signature and references to `buildImplementerSystemPrompt`, `retryLabelAndGuidance`, etc. Record every error location — they will be fixed in subsequent tasks.

**Step 4: Run: `go test ./internal/pipeline/ -run TestImplementer 2>&1 | head -40`**

Expected: compile errors (not test failures). Note them.

---

### Task 2: Fix `task_runner.go` — remove SkillInjector, PromptBuilder, WithRegistry

**Files:**
- Modify: `internal/pipeline/task_runner.go`

The task runner's `NewPipelineTaskRunner` must now take a `*prompts.Registry`. All `NewImplementer`, `NewSpecReviewer`, `NewQualityReviewer` calls must pass the registry. `WithRegistry()` method is deleted. `runTaskWithAgent()` replaces `SkillInjector`+`PromptBuilder` with `registry.ForClaude()` + `registry.Render(KindRole, "implementer", vars)`.

**Step 1: Read the current task_runner.go top section**

Run: `head -170 /Users/canh/Projects/Indies/Foreman/internal/pipeline/task_runner.go`

**Step 2: Find the `runTaskWithAgent` function boundaries**

Run: `grep -n "func.*runTaskWithAgent\|^func " /Users/canh/Projects/Indies/Foreman/internal/pipeline/task_runner.go`

**Step 3: Edit `task_runner.go`**

Changes needed (edit each independently):

**3a. Add `registry` field to `PipelineTaskRunner`:**
```go
// PipelineTaskRunner struct — add registry field
type PipelineTaskRunner struct {
	llm             LLMProvider
	db              TaskRunnerDB
	git             git.GitProvider
	cmdRunner       runner.CommandRunner
	implementer     *Implementer
	specReviewer    *SpecReviewer
	qualityReviewer *QualityReviewer
	metrics         *telemetry.Metrics
	snap            *snapshot.Snapshot
	config          TaskRunnerConfig
	registry        *prompts.Registry  // ADD THIS
}
```

**3b. Update `NewPipelineTaskRunner` signature and body:**
```go
func NewPipelineTaskRunner(
	llm LLMProvider,
	database TaskRunnerDB,
	gitProv git.GitProvider,
	cmdRunner runner.CommandRunner,
	config TaskRunnerConfig,
	reg *prompts.Registry,
) *PipelineTaskRunner {
	return &PipelineTaskRunner{
		llm:             llm,
		db:              database,
		git:             gitProv,
		cmdRunner:       cmdRunner,
		config:          config,
		registry:        reg,
		implementer:     NewImplementer(llm, reg),
		specReviewer:    NewSpecReviewer(llm, reg),
		qualityReviewer: NewQualityReviewer(llm, reg),
	}
}
```

**3c. Delete the `WithRegistry` method entirely** (the one at line ~141).

**3d. Rewrite `runTaskWithAgent` — replace SkillInjector + PromptBuilder sections:**

Find this block (around line 421-447):
```go
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
```

Replace with:
```go
feedback := NewFeedbackAccumulator()

// Write .claude/ structure if running under Claude Code runner.
if r.config.AgentRunnerName == "claudecode" {
    vars := map[string]any{
        "test_command": r.config.TestCommand,
    }
    if err := r.registry.ForClaude(r.config.WorkDir, vars); err != nil {
        log.Warn().Err(err).Msg("registry.ForClaude failed, proceeding without .claude/ structure")
    }
}

for attempt := 1; attempt <= r.config.MaxImplementationRetries+1; attempt++ {
    if attempt > 1 {
        feedback.ResetKeepingSummary()
    }

    // Build prompt for this attempt via registry.
    roleName := "implementer"
    if attempt > 1 {
        roleName = "implementer-retry"
    }
    prompt, err := r.registry.Render(prompts.KindRole, roleName, map[string]any{
        "task_title":          task.Title,
        "task_description":    task.Description,
        "acceptance_criteria": task.AcceptanceCriteria,
        "context_files":       map[string]string{},
        "codebase_patterns":   r.config.CodebasePatterns,
        "test_command":        r.config.TestCommand,
        "attempt":             attempt,
        "retry_feedback":      feedback.Render(),
    })
    if err != nil {
        return fmt.Errorf("render agent prompt (attempt %d): %w", attempt, err)
    }
```

**Step 4: Remove the `import` of `"github.com/canhta/foreman/internal/prompts"` if now handled differently, and add it if missing. Also add import for `prompts` package.**

**Step 5: Run: `go build ./internal/pipeline/`**

Expected: errors shift to callers of `NewPipelineTaskRunner` (now requires `reg` arg). Record.

**Step 6: Run: `go test ./internal/pipeline/ -run TestTaskRunner 2>&1 | head -40`**

---

### Task 3: Fix `spec_reviewer.go`, `quality_reviewer.go`, `final_reviewer.go` — require registry

**Files:**
- Modify: `internal/pipeline/spec_reviewer.go`
- Modify: `internal/pipeline/quality_reviewer.go`
- Modify: `internal/pipeline/final_reviewer.go`

Each: remove `WithRegistry()`, remove `if r.registry != nil` guard, make registry a required constructor arg.

**Step 1: Rewrite `spec_reviewer.go`**

Change constructor:
```go
func NewSpecReviewer(provider llm.LlmProvider, reg *prompts.Registry) *SpecReviewer {
	if reg == nil {
		panic("spec_reviewer: registry must not be nil")
	}
	return &SpecReviewer{llm: provider, registry: reg}
}
```

Remove `WithRegistry()` method entirely.

In `Review()`, replace:
```go
if r.registry != nil {
    system, err = r.registry.Render(prompts.KindRole, "spec-reviewer", map[string]any{...})
} else {
    system, err = RenderPrompt("spec_reviewer", PromptContext{...})
}
```
with:
```go
system, err = r.registry.Render(prompts.KindRole, "spec-reviewer", map[string]any{
    "task_title":          input.TaskTitle,
    "acceptance_criteria": input.AcceptanceCriteria,
    "diff":                input.Diff,
})
```

**Step 2: Rewrite `quality_reviewer.go`**

Same pattern — constructor requires `reg`, remove `WithRegistry()`, remove nil guard:
```go
func NewQualityReviewer(provider llm.LlmProvider, reg *prompts.Registry) *QualityReviewer {
	if reg == nil {
		panic("quality_reviewer: registry must not be nil")
	}
	return &QualityReviewer{llm: provider, registry: reg}
}
```

Remove `WithRegistry()`. In `Review()`:
```go
system, err = r.registry.Render(prompts.KindRole, "quality-reviewer", map[string]any{
    "diff":              input.Diff,
    "codebase_patterns": input.CodebasePatterns,
})
```

**Step 3: Rewrite `final_reviewer.go`**

`CompletedTask` type is defined in `prompt_renderer.go` (which will be deleted). Move `CompletedTask` into `final_reviewer.go` — it's only used there. Remove `WithRegistry()`, require registry in constructor.

```go
// CompletedTask represents a completed task summary for the final review prompt.
type CompletedTask struct {
	Title  string
	Status string
}
```

Constructor:
```go
func NewFinalReviewer(provider llm.LlmProvider, reg *prompts.Registry) *FinalReviewer {
	if reg == nil {
		panic("final_reviewer: registry must not be nil")
	}
	return &FinalReviewer{llm: provider, registry: reg}
}
```

Remove `WithRegistry()`. In `Review()`:
```go
system, err = r.registry.Render(prompts.KindRole, "final-reviewer", map[string]any{
    "ticket_title":       input.TicketTitle,
    "ticket_description": input.TicketDescription,
    "full_diff":          input.FullDiff,
    "completed_tasks":    completedTasks,
})
if err != nil {
    return nil, fmt.Errorf("render final_reviewer prompt: %w", err)
}
```

**Step 4: Run: `go build ./internal/pipeline/`**

Expected: errors from `NewPipelineTaskRunner` (already updated to pass `reg`) and test files that call the old zero-arg constructors. Record test file errors.

---

### Task 4: Delete `prompt_renderer.go`, `prompt_builder.go`, `skill_injector.go` and their tests

**Files:**
- Delete: `internal/pipeline/prompt_renderer.go`
- Delete: `internal/pipeline/prompt_builder_test.go`
- Delete: `internal/pipeline/skill_injector.go`
- Delete: `internal/pipeline/skill_injector_test.go`
- Delete: `internal/pipeline/assets/` (entire directory, including embedded claude assets)

**Step 1: Delete the files**

```bash
rm /Users/canh/Projects/Indies/Foreman/internal/pipeline/prompt_renderer.go
rm /Users/canh/Projects/Indies/Foreman/internal/pipeline/prompt_builder.go
rm /Users/canh/Projects/Indies/Foreman/internal/pipeline/prompt_builder_test.go
rm /Users/canh/Projects/Indies/Foreman/internal/pipeline/skill_injector.go
rm /Users/canh/Projects/Indies/Foreman/internal/pipeline/skill_injector_test.go
rm -rf /Users/canh/Projects/Indies/Foreman/internal/pipeline/assets/
```

**Step 2: Run: `go build ./internal/pipeline/`**

Expected: compile errors for:
- `PromptContext` type gone (used in test files and any remaining callers)
- `CompletedTask` type — now only in `final_reviewer.go` ✓
- `RenderPrompt` function gone
- `SkillInjectorConfig`, `NewSkillInjector`, `PromptBuilderConfig`, `NewPromptBuilder` gone

List every error location.

**Step 3: Run: `go vet ./internal/pipeline/ 2>&1 | head -40`**

---

### Task 5: Fix pipeline test files that referenced legacy types

**Files:**
- Modify: any `*_test.go` in `internal/pipeline/` that referenced `PromptContext`, `RenderPrompt`, `PromptBuilderConfig`, `NewPromptBuilder`, `SkillInjector`, `CompletedTask`, `NewFinalReviewer(llm)`, `NewSpecReviewer(llm)`, `NewQualityReviewer(llm)`, `NewImplementer(llm)`, `NewPipelineTaskRunner(...5 args)`.

**Step 1: Find all affected test files**

Run:
```bash
grep -rln "PromptContext\|RenderPrompt\|PromptBuilderConfig\|NewPromptBuilder\|SkillInjector\|NewFinalReviewer\|NewSpecReviewer\|NewQualityReviewer\|NewImplementer\|NewPipelineTaskRunner" \
  /Users/canh/Projects/Indies/Foreman/internal/pipeline/ --include="*_test.go"
```

**Step 2: For each affected test file, apply the appropriate fix:**

- Tests calling `NewImplementer(mockLLM)` → `NewImplementer(mockLLM, mustLoadTestRegistry(t))`
- Tests calling `NewSpecReviewer(mockLLM)` → `NewSpecReviewer(mockLLM, mustLoadTestRegistry(t))`
- Tests calling `NewQualityReviewer(mockLLM)` → `NewQualityReviewer(mockLLM, mustLoadTestRegistry(t))`
- Tests calling `NewFinalReviewer(mockLLM)` → `NewFinalReviewer(mockLLM, mustLoadTestRegistry(t))`
- Tests calling `NewPipelineTaskRunner(llm, db, git, cmd, cfg)` → `NewPipelineTaskRunner(llm, db, git, cmd, cfg, mustLoadTestRegistry(t))`
- Tests referencing `PromptContext{...}` — these tests were testing `RenderPrompt` which is deleted. **Delete these test functions** (they belong to `prompt_renderer.go` which no longer exists).
- Tests referencing `PromptBuilderConfig`/`NewPromptBuilder` — these belong to `prompt_builder.go` which is deleted. Their test file is already deleted in Task 4.

**Step 3: Run: `go build ./internal/pipeline/`**

Expected: PASS (zero errors).

**Step 4: Run: `go test ./internal/pipeline/ -v -count=1 2>&1 | tail -30`**

Expected: PASS.

---

### Task 6: Fix callers of `NewPipelineTaskRunner` in `cmd/` and `daemon/`

**Files:**
- Modify: `cmd/start.go` (calls `pipeline.NewPipelineTaskRunner` via `taskRunnerFactory`)

**Step 1: Find all callers**

Run:
```bash
grep -rn "NewPipelineTaskRunner\|pipeline\.NewPipelineTaskRunner" \
  /Users/canh/Projects/Indies/Foreman/ --include="*.go" | grep -v "_test.go"
```

**Step 2: Update `cmd/start.go` — `taskRunnerFactory`**

The factory already holds `f.registry`. Update `Create()`:

Find:
```go
tr := pipeline.NewPipelineTaskRunner(f.llm, f.db, f.gitProv, f.cmdRunner, cfg)
if f.registry != nil {
    tr.WithRegistry(f.registry)
}
```

Replace with:
```go
tr := pipeline.NewPipelineTaskRunner(f.llm, f.db, f.gitProv, f.cmdRunner, cfg, f.registry)
```

Note: `f.registry` may now be `nil` if `prompts.Load()` fails. Since registry is required (panics on nil), also update the graceful-loading logic to make `prompts.Load()` a **hard failure** rather than a warning:

Find in `cmd/start.go` (around line 219):
```go
var promptRegistry *prompts.Registry
if reg, regErr := prompts.Load(promptsDir); regErr != nil {
    log.Warn().Err(regErr).Str("prompts_dir", promptsDir).Msg("could not load prompt registry; pipeline components will use legacy prompts")
} else {
    promptRegistry = reg
    log.Info().Str("prompts_dir", promptsDir).Msg("prompt registry loaded")
}
```

Replace with:
```go
promptRegistry, err := prompts.Load(promptsDir)
if err != nil {
    return fmt.Errorf("load prompt registry from %s: %w", promptsDir, err)
}
log.Info().Str("prompts_dir", promptsDir).Msg("prompt registry loaded")
```

**Step 3: Run: `go build ./cmd/ ./...`**

Expected: PASS or remaining errors in daemon/test files only.

---

### Task 7: Fix `FinalReviewer` in daemon — update constructor call

**Files:**
- Modify: any file in `internal/daemon/` or `cmd/` that calls `pipeline.NewFinalReviewer`

**Step 1: Find all callers**

Run:
```bash
grep -rn "NewFinalReviewer\|pipeline\.NewFinalReviewer" \
  /Users/canh/Projects/Indies/Foreman/ --include="*.go" | grep -v "_test.go"
```

**Step 2: Update each caller to pass the registry**

Typical pattern — change:
```go
pipeline.NewFinalReviewer(llmProv)
```
to:
```go
pipeline.NewFinalReviewer(llmProv, promptRegistry)
```

**Step 3: Run: `go build ./...`**

Expected: PASS.

---

### Task 8: Full build + test verification

**Step 1: Build**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: exit 0, zero errors.

**Step 2: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1 2>&1`
Expected: all packages PASS, zero failures.

**Step 3: Run `make build`**

Run: `cd /Users/canh/Projects/Indies/Foreman && make build`
Expected: exit 0.

**Step 4: Verify binary starts**

Run: `cd /Users/canh/Projects/Indies/Foreman && ./foreman --help`
Expected: help text printed, no startup errors.

**Step 5: Confirm deleted files are gone**

Run:
```bash
ls /Users/canh/Projects/Indies/Foreman/internal/pipeline/prompt_renderer.go 2>&1
ls /Users/canh/Projects/Indies/Foreman/internal/pipeline/prompt_builder.go 2>&1
ls /Users/canh/Projects/Indies/Foreman/internal/pipeline/skill_injector.go 2>&1
ls /Users/canh/Projects/Indies/Foreman/internal/pipeline/assets/ 2>&1
```
Expected: all `No such file or directory`.

**Step 6: Confirm no legacy references remain**

Run:
```bash
grep -rn "RenderPrompt\|PromptContext\|buildImplementerSystemPrompt\|buildImplementerUserPrompt\|retryLabelAndGuidance\|SkillInjector\|NewSkillInjector\|PromptBuilder\|NewPromptBuilder\|claudeAssets\|go:embed assets" \
  /Users/canh/Projects/Indies/Foreman/internal/ --include="*.go"
```
Expected: zero matches.

**Step 7: Commit**

```bash
cd /Users/canh/Projects/Indies/Foreman
git add -A
git commit -m "refactor: hard-delete legacy prompt infrastructure, registry is sole prompt source"
```
