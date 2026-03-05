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

const defaultMaxTokens = 120000

const systemPrompt = `You are an expert at generating agent-optimized AGENTS.md files for software repositories.
Your output will be read by AI coding agents (not humans primarily), so optimize for:
- Precise conventions: exact file patterns, naming rules, import ordering
- Exact commands: build, test, lint, format — copy-pasteable
- Anti-patterns: things an agent should NEVER do in this codebase
- Architecture: key directories, module boundaries, data flow
- Testing: how to write and run tests, test file naming conventions

Output only the AGENTS.md content in markdown. Be concise but complete.
Do NOT include generic advice — only project-specific information derived from the provided files.`

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
	sb.WriteString(fmt.Sprintf("## Language\n\n%s\n\n", info.Language))

	if info.Framework != "" {
		sb.WriteString(fmt.Sprintf("## Framework\n\n%s\n\n", info.Framework))
	}

	sb.WriteString("## Commands\n\n")
	if info.BuildCmd != "" {
		sb.WriteString(fmt.Sprintf("- Build: `%s`\n", info.BuildCmd))
	}
	if info.TestCmd != "" {
		sb.WriteString(fmt.Sprintf("- Test: `%s`\n", info.TestCmd))
	}
	if info.LintCmd != "" {
		sb.WriteString(fmt.Sprintf("- Lint: `%s`\n", info.LintCmd))
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
	userPrompt.WriteString(fmt.Sprintf("Repository language: %s\n", info.Language))
	if info.Framework != "" {
		userPrompt.WriteString(fmt.Sprintf("Framework: %s\n", info.Framework))
	}
	if info.BuildCmd != "" {
		userPrompt.WriteString(fmt.Sprintf("Build command: %s\n", info.BuildCmd))
	}
	if info.TestCmd != "" {
		userPrompt.WriteString(fmt.Sprintf("Test command: %s\n", info.TestCmd))
	}
	if info.LintCmd != "" {
		userPrompt.WriteString(fmt.Sprintf("Lint command: %s\n", info.LintCmd))
	}

	userPrompt.WriteString("\n## File Tree\n\n```\n")
	userPrompt.WriteString(info.FileTree)
	userPrompt.WriteString("\n```\n\n")

	userPrompt.WriteString("## File Contents\n\n")
	for _, f := range files {
		userPrompt.WriteString(fmt.Sprintf("### %s (tier %d)\n\n```\n%s\n```\n\n", f.Path, f.Tier, f.Content))
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
