// internal/skills/engine.go
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// SkillContext holds the execution context for a skill run.
type SkillContext struct {
	Ticket   interface{}
	Models   map[string]string
	Steps    map[string]*StepResult
	Diff     string
	FileTree string
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
	agentRunner   agent.AgentRunner
	git           git.GitProvider
	skillsByID    map[string]*Skill
	workDir       string
	defaultBranch string
}

// SetAgentRunner configures the agent runner for agentsdk step types.
func (e *Engine) SetAgentRunner(ar agent.AgentRunner) {
	e.agentRunner = ar
}

// SetGitProvider configures the git provider for git_diff step types.
func (e *Engine) SetGitProvider(g git.GitProvider) {
	e.git = g
}

// RegisterSkills indexes skills by ID for subskill resolution.
func (e *Engine) RegisterSkills(skills []*Skill) {
	e.skillsByID = make(map[string]*Skill, len(skills))
	for _, s := range skills {
		e.skillsByID[s.ID] = s
	}
}

// NewEngine creates a skill engine.
func NewEngine(llmProvider llm.LlmProvider, cmdRunner runner.CommandRunner, workDir, defaultBranch string) *Engine {
	return &Engine{
		llm:           llmProvider,
		runner:        cmdRunner,
		workDir:       workDir,
		defaultBranch: defaultBranch,
		skillsByID:    make(map[string]*Skill),
	}
}

// Execute runs all steps in a skill sequentially.
// If a step has AllowFailure=true and fails, the error is stored in sCtx.Steps
// and execution continues to the next step.
// If a step has AllowFailure=false and fails, execution stops immediately and
// an error is returned.
func (e *Engine) Execute(ctx context.Context, skill *Skill, sCtx *SkillContext) error {
	for _, step := range skill.Steps {
		result, err := e.executeStep(ctx, step, sCtx)
		if err != nil {
			if step.AllowFailure {
				sCtx.Steps[step.ID] = &StepResult{Error: err.Error()}
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
	case "agentsdk":
		return e.executeAgentSDK(ctx, step)
	case "subskill":
		return e.executeSubSkill(ctx, step, sCtx)
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
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading existing file %s: %w", step.Path, err)
		}
		content = content + "\n" + string(existing)
	case "append":
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading existing file %s: %w", step.Path, err)
		}
		content = string(existing) + "\n" + content
		// default: overwrite
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing file %s: %w", step.Path, err)
	}
	return &StepResult{Output: path}, nil
}

func (e *Engine) executeGitDiff(ctx context.Context) (*StepResult, error) {
	if e.git == nil {
		return nil, fmt.Errorf("git_diff step requires a GitProvider — call engine.SetGitProvider()")
	}
	diff, err := e.git.DiffWorking(ctx, e.workDir)
	if err != nil {
		return nil, fmt.Errorf("git_diff: %w", err)
	}
	return &StepResult{Output: diff}, nil
}

func (e *Engine) executeAgentSDK(ctx context.Context, step SkillStep) (*StepResult, error) {
	if e.agentRunner == nil {
		return nil, fmt.Errorf("agentsdk step '%s': no agent runner configured", step.ID)
	}

	req := agent.AgentRequest{
		Prompt:        step.Content,
		WorkDir:       e.workDir,
		AllowedTools:  step.AllowedTools,
		MaxTurns:      step.MaxTurns,
		TimeoutSecs:   step.TimeoutSecs,
		FallbackModel: step.FallbackModel,
	}

	// Marshal OutputSchema map → json.RawMessage
	if step.OutputSchema != nil {
		b, err := json.Marshal(step.OutputSchema)
		if err == nil {
			req.OutputSchema = b
		}
	}

	// Pre-assemble AGENTS.md into SystemPrompt for all runners
	if fc := loadForemanContextFromDir(e.workDir); fc != "" {
		req.SystemPrompt = fc
	}

	result, err := e.agentRunner.Run(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agentsdk step '%s': %w", step.ID, err)
	}

	// Validate output_format
	switch step.OutputFormat {
	case "json":
		if !json.Valid([]byte(result.Output)) {
			return nil, fmt.Errorf("agentsdk step '%s': output_format=json but output is not valid JSON", step.ID)
		}
	case "diff":
		if !strings.Contains(result.Output, "--- ") && !strings.Contains(result.Output, "+++ ") {
			return nil, fmt.Errorf("agentsdk step '%s': output_format=diff but output is not a unified diff", step.ID)
		}
	case "checklist":
		_, failed := parseChecklist(result.Output)
		return &StepResult{Output: result.Output, ExitCode: failed}, nil
	}

	return &StepResult{Output: result.Output}, nil
}

func (e *Engine) executeSubSkill(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	if step.SkillRef == "" {
		return nil, fmt.Errorf("subskill step '%s': missing skill_ref", step.ID)
	}
	sub, ok := e.skillsByID[step.SkillRef]
	if !ok {
		return nil, fmt.Errorf("subskill step '%s': skill %q not found", step.ID, step.SkillRef)
	}
	subCtx := &SkillContext{
		Ticket:   sCtx.Ticket,
		Diff:     sCtx.Diff,
		FileTree: sCtx.FileTree,
		Models:   sCtx.Models,
		Steps:    make(map[string]*StepResult),
	}
	// Inject input vars as step results so templates can reference them
	for k, v := range step.Input {
		subCtx.Steps[k] = &StepResult{Output: v}
	}
	if err := e.Execute(ctx, sub, subCtx); err != nil {
		return nil, fmt.Errorf("subskill '%s': %w", step.SkillRef, err)
	}
	// Return output of last step in sub-skill
	var lastOutput string
	for _, s := range sub.Steps {
		if r, ok := subCtx.Steps[s.ID]; ok && r != nil {
			lastOutput = r.Output
		}
	}
	return &StepResult{Output: lastOutput}, nil
}

// loadForemanContextFromDir reads project context from workDir.
// AGENTS.md is the standard cross-tool convention; .foreman/context.md is for Foreman-specific cached content.
func loadForemanContextFromDir(workDir string) string {
	candidates := []string{
		filepath.Join(workDir, "AGENTS.md"),
		filepath.Join(workDir, ".foreman", "context.md"),
	}
	for _, path := range candidates {
		if content, err := os.ReadFile(path); err == nil {
			return string(content)
		}
	}
	return ""
}

// parseChecklist counts passed (- [x]) and failed (- [ ]) checklist items.
func parseChecklist(output string) (passed, failed int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "- [X]") {
			passed++
		} else if strings.HasPrefix(line, "- [ ]") {
			failed++
		}
	}
	return
}
