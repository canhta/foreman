package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// GenerateOptions controls context generation behavior.
type GenerateOptions struct {
	MaxTokens int
	Offline   bool
}

// Generator creates AGENTS.md content from repository analysis.
type Generator struct {
	provider llm.LlmProvider
	model    string
}

// NewGenerator creates a Generator. provider may be nil for offline mode.
func NewGenerator(provider llm.LlmProvider, model string) *Generator {
	return &Generator{provider: provider, model: model}
}

const defaultMaxTokens = 32000

const systemPrompt = `You are generating an AGENTS.md for Foreman, a fully autonomous coding daemon.
This file is read by an LLM agent, not a human developer. Optimize for:
- Precise naming conventions (the agent will follow them literally)
- Exact test commands (the agent will run them verbatim)
- Explicit anti-patterns to avoid (the agent has no implicit human intuition)
- File organization rules (the agent must know where to create new files)
Omit marketing language, narrative prose, and generic best practices.

Output pure Markdown. Include these sections:
1. Project Overview (language, framework, purpose)
2. Architecture (key packages/modules, entry points)
3. Coding Conventions (naming, error handling, patterns)
4. Build & Test Commands (exact, copy-pasteable)
5. Key Dependencies
6. File Organization Rules`

// Generate creates AGENTS.md content for the repository at workDir.
func (g *Generator) Generate(ctx context.Context, workDir string, opts GenerateOptions) (string, error) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaultMaxTokens
	}

	if opts.Offline || g.provider == nil {
		return g.generateOffline(workDir)
	}

	return g.generateOnline(ctx, workDir, opts)
}

func (g *Generator) generateOffline(workDir string) (string, error) {
	info, err := AnalyzeRepo(workDir)
	if err != nil {
		return "", fmt.Errorf("analyze repo: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# AGENTS.md\n\n")
	fmt.Fprintf(&sb, "## Language\n\n%s\n\n", info.Language)

	if info.Framework != "" {
		fmt.Fprintf(&sb, "## Framework\n\n%s\n\n", info.Framework)
	}

	sb.WriteString("## Commands\n\n")
	if info.BuildCmd != "" {
		fmt.Fprintf(&sb, "- Build: `%s`\n", info.BuildCmd)
	}
	if info.TestCmd != "" {
		fmt.Fprintf(&sb, "- Test: `%s`\n", info.TestCmd)
	}
	if info.LintCmd != "" {
		fmt.Fprintf(&sb, "- Lint: `%s`\n", info.LintCmd)
	}

	if info.FileTree != "" {
		sb.WriteString("\n## File Tree\n\n```\n")
		sb.WriteString(info.FileTree)
		sb.WriteString("\n```\n")
	}

	return sb.String(), nil
}

func (g *Generator) generateOnline(ctx context.Context, workDir string, opts GenerateOptions) (string, error) {
	// Scan files for context
	files := ScanFiles(workDir, opts.MaxTokens)

	// Also get repo analysis
	info, err := AnalyzeRepo(workDir)
	if err != nil {
		return "", fmt.Errorf("analyze repo: %w", err)
	}

	// Build user prompt with repo info and file contents
	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "Repository language: %s\n", info.Language)
	if info.Framework != "" {
		fmt.Fprintf(&userPrompt, "Framework: %s\n", info.Framework)
	}
	if info.BuildCmd != "" {
		fmt.Fprintf(&userPrompt, "Build command: %s\n", info.BuildCmd)
	}
	if info.TestCmd != "" {
		fmt.Fprintf(&userPrompt, "Test command: %s\n", info.TestCmd)
	}
	if info.LintCmd != "" {
		fmt.Fprintf(&userPrompt, "Lint command: %s\n", info.LintCmd)
	}

	userPrompt.WriteString("\n## File Tree\n\n```\n")
	userPrompt.WriteString(info.FileTree)
	userPrompt.WriteString("\n```\n\n")

	userPrompt.WriteString("## File Contents\n\n")
	for _, f := range files {
		fmt.Fprintf(&userPrompt, "### %s (tier %d)\n\n```\n%s\n```\n\n", f.Path, f.Tier, f.Content)
	}

	userPrompt.WriteString("Generate the AGENTS.md content for this repository.")

	req := models.LlmRequest{
		Model:        g.model,
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt.String(),
		MaxTokens:    4096,
		Temperature:  0.3,
	}

	resp, err := g.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("llm complete: %w", err)
	}

	return resp.Content, nil
}
