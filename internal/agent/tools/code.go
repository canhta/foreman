package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

func registerCode(r *Registry, cmd runner.CommandRunner) {
	r.Register(&getSymbolTool{})
	r.Register(&getErrorsTool{cmd: cmd})
}

// --- GetSymbol ---

type getSymbolTool struct{}

func (t *getSymbolTool) Name() string { return "GetSymbol" }
func (t *getSymbolTool) Description() string {
	return "Find where a symbol (function, type, class) is defined"
}
func (t *getSymbolTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"Symbol name to find"},"kind":{"type":"string","description":"Symbol kind: func, type, class, def (optional)"},"path":{"type":"string","description":"Directory to search (default: working dir)"}},"required":["symbol"]}`)
}
func (t *getSymbolTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Symbol string `json:"symbol"`
		Kind   string `json:"kind"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetSymbol: %w", err)
	}
	searchDir := workDir
	if args.Path != "" {
		if err := ValidatePath(workDir, args.Path); err != nil {
			return "", fmt.Errorf("GetSymbol: %w", err)
		}
		searchDir = AbsPath(workDir, args.Path)
	}
	// Build pattern based on kind
	var patterns []string
	if args.Kind != "" {
		patterns = []string{fmt.Sprintf(`%s %s\b`, args.Kind, regexp.QuoteMeta(args.Symbol))}
	} else {
		patterns = []string{
			fmt.Sprintf(`func %s\b`, regexp.QuoteMeta(args.Symbol)),
			fmt.Sprintf(`type %s\b`, regexp.QuoteMeta(args.Symbol)),
			fmt.Sprintf(`class %s\b`, regexp.QuoteMeta(args.Symbol)),
			fmt.Sprintf(`def %s\b`, regexp.QuoteMeta(args.Symbol)),
			fmt.Sprintf(`const %s\b`, regexp.QuoteMeta(args.Symbol)),
			fmt.Sprintf(`var %s\b`, regexp.QuoteMeta(args.Symbol)),
		}
	}
	combined := "(" + strings.Join(patterns, "|") + ")"
	re, err := regexp.Compile(combined)
	if err != nil {
		return "", fmt.Errorf("GetSymbol: invalid pattern: %w", err)
	}

	var results []string
	filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		if info != nil && info.Size() > 1<<20 {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		rel, _ := filepath.Rel(workDir, path)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, lineNum, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if len(results) == 0 {
		return fmt.Sprintf("Symbol %q not found", args.Symbol), nil
	}
	return strings.Join(results, "\n"), nil
}

// --- GetErrors ---

type getErrorsTool struct{ cmd runner.CommandRunner }

func (t *getErrorsTool) Name() string { return "GetErrors" }
func (t *getErrorsTool) Description() string {
	return "Run a lint/check tool and return structured errors"
}
func (t *getErrorsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tool":{"type":"string","description":"Tool to run e.g. golangci-lint, eslint"},"path":{"type":"string","description":"Path to lint (default: working dir)"}},"required":["tool"]}`)
}
func (t *getErrorsTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("GetErrors: command runner not available")
	}
	var args struct {
		Tool string `json:"tool"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetErrors: %w", err)
	}
	target := "."
	if args.Path != "" {
		target = args.Path
	}
	out, err := t.cmd.Run(ctx, workDir, args.Tool, []string{"run", target}, 60)
	if err != nil {
		return "", fmt.Errorf("GetErrors: %w", err)
	}
	result := runner.ParseLintOutput(out.Stdout+out.Stderr, "")
	if result.Clean {
		return "No issues found.", nil
	}
	var lines []string
	for _, issue := range result.Issues {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", issue.File, issue.Line, issue.Message))
	}
	return strings.Join(lines, "\n"), nil
}
