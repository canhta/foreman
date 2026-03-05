// internal/pipeline/prompt_renderer.go
package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/flosch/pongo2/v6"
)

// CompletedTask represents a task that has been completed, used in final review prompts.
type CompletedTask struct {
	Title  string
	Status string
}

// PromptContext holds all variables available to prompt templates.
type PromptContext struct {
	ContextFiles          map[string]string
	ProjectContext        string
	QualityReviewFeedback string
	FileTree              string
	TicketTitle           string
	FullDiff              string
	TaskTitle             string
	TaskDescription       string
	TicketDescription     string
	TestFailure           string
	TDDFailure            string
	Diff                  string
	CodebasePatterns      string
	SpecReviewFeedback    string
	AcceptanceCriteria    []string
	CompletedTasks        []CompletedTask
	MaxAttempts           int
	Attempt               int
	MaxTasks              int
}

// promptsDir is the directory containing prompt templates.
// Default is "prompts/" relative to the project root (go.mod location).
// It can be overridden in tests.
var promptsDir = ""

var (
	resolvedPromptsDir string
	promptsDirOnce     sync.Once
)

// resolvePromptsDir returns the prompts directory path.
// If promptsDir is explicitly set (non-empty), it is used directly.
// Otherwise, it walks up from cwd to find the project root (go.mod).
func resolvePromptsDir() (string, error) {
	// Walk up from cwd to find go.mod (project root)
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		gomod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gomod); err == nil {
			return filepath.Join(dir, "prompts"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (go.mod) from %s", dir)
		}
		dir = parent
	}
}

// getPromptsDir returns the resolved prompts directory, using sync.Once to cache
// the result of the filesystem walk (unless promptsDir is explicitly set).
func getPromptsDir() (string, error) {
	if promptsDir != "" {
		return promptsDir, nil
	}
	var resolveErr error
	promptsDirOnce.Do(func() {
		resolvedPromptsDir, resolveErr = resolvePromptsDir()
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return resolvedPromptsDir, nil
}

// RenderPrompt renders a named prompt template with the given context.
// templateName should be the filename without the ".md.j2" extension.
func RenderPrompt(templateName string, ctx PromptContext) (string, error) {
	dir, err := getPromptsDir()
	if err != nil {
		return "", fmt.Errorf("resolve prompts dir: %w", err)
	}

	path := filepath.Join(dir, templateName+".md.j2")
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("prompt template not found: %s", path)
	}

	// Use a TemplateSet with the prompts directory as base so that
	// {% include %} directives can resolve sibling templates.
	loader, err := pongo2.NewLocalFileSystemLoader(dir)
	if err != nil {
		return "", fmt.Errorf("create template loader: %w", err)
	}
	tplSet := pongo2.NewSet("foreman", loader)

	tpl, err := tplSet.FromFile(path)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", templateName, err)
	}

	pongoCtx := pongo2.Context{
		"ticket_title":            ctx.TicketTitle,
		"ticket_description":      ctx.TicketDescription,
		"acceptance_criteria":     ctx.AcceptanceCriteria,
		"file_tree":               ctx.FileTree,
		"project_context":         ctx.ProjectContext,
		"full_diff":               ctx.FullDiff,
		"task_title":              ctx.TaskTitle,
		"task_description":        ctx.TaskDescription,
		"context_files":           ctx.ContextFiles,
		"codebase_patterns":       ctx.CodebasePatterns,
		"diff":                    ctx.Diff,
		"attempt":                 ctx.Attempt,
		"max_attempts":            ctx.MaxAttempts,
		"spec_review_feedback":    ctx.SpecReviewFeedback,
		"quality_review_feedback": ctx.QualityReviewFeedback,
		"tdd_failure":             ctx.TDDFailure,
		"test_failure":            ctx.TestFailure,
		"completed_tasks":         ctx.CompletedTasks,
		"max_tasks":               ctx.MaxTasks,
	}

	result, err := tpl.Execute(pongoCtx)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", templateName, err)
	}

	return result, nil
}
