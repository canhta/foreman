package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/git"
)

func registerGit(r *Registry, g git.GitProvider) {
	r.Register(&getDiffTool{git: g})
	r.Register(&getCommitLogTool{git: g})
	r.Register(&treeSummaryTool{git: g})
}

// --- GetDiff ---

type getDiffTool struct{ git git.GitProvider }

func (t *getDiffTool) Name() string { return "GetDiff" }
func (t *getDiffTool) Description() string {
	return "Get git diff — working tree by default, or between two refs"
}
func (t *getDiffTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"base":{"type":"string","description":"Base ref (e.g. HEAD~1). If omitted, diffs the working tree."},"head":{"type":"string","description":"Head ref (e.g. HEAD). Required if base is set."},"path":{"type":"string","description":"Restrict diff to this path"}}}`)
}
func (t *getDiffTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("GetDiff: git provider not available")
	}
	var args struct {
		Base string `json:"base"`
		Head string `json:"head"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetDiff: %w", err)
	}
	var diff string
	var err error
	if args.Base != "" {
		head := args.Head
		if head == "" {
			head = "HEAD"
		}
		diff, err = t.git.Diff(ctx, workDir, args.Base, head)
	} else {
		diff, err = t.git.DiffWorking(ctx, workDir)
	}
	if err != nil {
		return "", fmt.Errorf("GetDiff: %w", err)
	}
	if diff == "" {
		return "No changes.", nil
	}
	return diff, nil
}

// --- GetCommitLog ---

type getCommitLogTool struct{ git git.GitProvider }

func (t *getCommitLogTool) Name() string        { return "GetCommitLog" }
func (t *getCommitLogTool) Description() string { return "Get recent git commit history" }
func (t *getCommitLogTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","description":"Number of commits to return (default 10)"},"path":{"type":"string","description":"Filter commits touching this path"}}}`)
}
func (t *getCommitLogTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("GetCommitLog: git provider not available")
	}
	var args struct {
		Count int    `json:"count"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetCommitLog: %w", err)
	}
	count := args.Count
	if count <= 0 {
		count = 10
	}
	entries, err := t.git.Log(ctx, workDir, count)
	if err != nil {
		return "", fmt.Errorf("GetCommitLog: %w", err)
	}
	if len(entries) == 0 {
		return "No commits found.", nil
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s  %s  %s  %s",
			e.SHA[:min(7, len(e.SHA))],
			e.Date.Format(time.DateOnly),
			e.Author,
			e.Message,
		))
	}
	return strings.Join(lines, "\n"), nil
}

// --- TreeSummary ---

type treeSummaryTool struct{ git git.GitProvider }

func (t *treeSummaryTool) Name() string        { return "TreeSummary" }
func (t *treeSummaryTool) Description() string { return "Show the repository file tree as a summary" }
func (t *treeSummaryTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"max_depth":{"type":"integer","description":"Maximum depth to show (default 3)"},"focus":{"type":"string","description":"Show only files under this directory"}}}`)
}
func (t *treeSummaryTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("TreeSummary: git provider not available")
	}
	var args struct {
		MaxDepth int    `json:"max_depth"`
		Focus    string `json:"focus"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("TreeSummary: %w", err)
	}
	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	entries, err := t.git.FileTree(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("TreeSummary: %w", err)
	}
	var lines []string
	for _, e := range entries {
		if args.Focus != "" && !strings.HasPrefix(e.Path, args.Focus) {
			continue
		}
		depth := strings.Count(e.Path, "/")
		if depth >= maxDepth {
			continue
		}
		indent := strings.Repeat("  ", depth)
		name := e.Path
		if strings.Contains(e.Path, "/") {
			name = e.Path[strings.LastIndex(e.Path, "/")+1:]
		}
		if e.IsDir {
			lines = append(lines, fmt.Sprintf("%s%s/", indent, name))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s", indent, name))
		}
	}
	if len(lines) == 0 {
		return "Empty tree.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
