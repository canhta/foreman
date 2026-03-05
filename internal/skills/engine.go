// internal/skills/engine.go
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// SkillContext holds the execution context for a skill run.
type SkillContext struct {
	Ticket   interface{}            // Current ticket (set by caller)
	Diff     string                 // Current branch diff
	FileTree string                 // Repo file tree
	Models   map[string]string      // Model routing config
	Steps    map[string]*StepResult // Results of previous steps
}

// NewSkillContext creates an empty skill execution context.
func NewSkillContext() *SkillContext {
	return &SkillContext{
		Steps: make(map[string]*StepResult),
	}
}

// Engine executes YAML skill definitions.
type Engine struct {
	llm           llm.LlmProvider
	runner        runner.CommandRunner
	workDir       string
	defaultBranch string
}

// NewEngine creates a skill engine.
func NewEngine(llmProvider llm.LlmProvider, cmdRunner runner.CommandRunner, workDir, defaultBranch string) *Engine {
	return &Engine{
		llm:           llmProvider,
		runner:        cmdRunner,
		workDir:       workDir,
		defaultBranch: defaultBranch,
	}
}

// Execute runs all steps in a skill sequentially.
// If a step with AllowFailure=true fails, execution continues in "lenient" mode
// where subsequent step failures are recorded but do not stop execution.
// If a step with AllowFailure=false fails in "strict" mode, execution stops and
// an error is returned.
func (e *Engine) Execute(ctx context.Context, skill *Skill, sCtx *SkillContext) error {
	lenient := false
	for _, step := range skill.Steps {
		result, err := e.executeStep(ctx, step, sCtx)
		if err != nil {
			if step.AllowFailure || lenient {
				sCtx.Steps[step.ID] = &StepResult{Error: err.Error()}
				if step.AllowFailure {
					lenient = true
				}
				continue
			}
			return fmt.Errorf("skill '%s' step '%s' failed: %w", skill.ID, step.ID, err)
		}
		sCtx.Steps[step.ID] = result
	}
	return nil
}

func (e *Engine) executeStep(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	switch step.Type {
	case "llm_call":
		return e.executeLLMCall(ctx, step, sCtx)
	case "run_command":
		return e.executeRunCommand(ctx, step, sCtx)
	case "file_write":
		return e.executeFileWrite(step, sCtx)
	case "git_diff":
		return e.executeGitDiff(ctx)
	default:
		return nil, fmt.Errorf("unknown step type: %s", step.Type)
	}
}

func (e *Engine) executeLLMCall(ctx context.Context, step SkillStep, _ *SkillContext) (*StepResult, error) {
	prompt := step.Content
	if prompt == "" {
		prompt = fmt.Sprintf("Execute skill step: %s", step.ID)
	}

	resp, err := e.llm.Complete(ctx, models.LlmRequest{
		Model:      step.Model,
		UserPrompt: prompt,
		MaxTokens:  4096,
	})
	if err != nil {
		return nil, err
	}
	return &StepResult{Output: resp.Content}, nil
}

func (e *Engine) executeRunCommand(ctx context.Context, step SkillStep, _ *SkillContext) (*StepResult, error) {
	out, err := e.runner.Run(ctx, e.workDir, step.Command, step.Args, 120)
	if err != nil {
		return nil, err
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("command '%s' failed (exit %d): %s", step.Command, out.ExitCode, out.Stderr)
	}
	return &StepResult{Output: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode}, nil
}

func (e *Engine) executeFileWrite(step SkillStep, _ *SkillContext) (*StepResult, error) {
	path := filepath.Join(e.workDir, step.Path)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", step.Path, err)
	}

	content := step.Content

	switch step.Mode {
	case "prepend":
		existing, _ := os.ReadFile(path)
		content = content + "\n" + string(existing)
	case "append":
		existing, _ := os.ReadFile(path)
		content = string(existing) + "\n" + content
		// default: overwrite
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing file %s: %w", step.Path, err)
	}
	return &StepResult{Output: path}, nil
}

func (e *Engine) executeGitDiff(_ context.Context) (*StepResult, error) {
	return &StepResult{Output: ""}, nil
}
