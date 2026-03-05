package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// ToolExecutor executes a tool within a working directory and returns the result.
type ToolExecutor func(workDir string, input json.RawMessage) (string, error)

// builtinTools maps tool names to their Go implementations.
// The builtin runner is read-only by default — no Edit/Write tools.
var builtinTools = map[string]ToolExecutor{
	"Read": toolRead,
	"Glob": toolGlob,
	"Grep": toolGrep,
}

func toolRead(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Read: invalid input: %w", err)
	}
	abs := filepath.Join(workDir, filepath.Clean(args.Path))
	if !strings.HasPrefix(abs, filepath.Clean(workDir)+string(filepath.Separator)) && abs != filepath.Clean(workDir) {
		return "", fmt.Errorf("Read: path %q outside working directory", args.Path)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("Read: %w", err)
	}
	return string(content), nil
}

func toolGlob(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Glob: invalid input: %w", err)
	}
	matches, err := filepath.Glob(filepath.Join(workDir, args.Pattern))
	if err != nil {
		return "", fmt.Errorf("Glob: %w", err)
	}
	var rel []string
	for _, m := range matches {
		r, _ := filepath.Rel(workDir, m)
		rel = append(rel, r)
	}
	return strings.Join(rel, "\n"), nil
}

func toolGrep(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Grep: invalid input: %w", err)
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("Grep: invalid pattern: %w", err)
	}

	searchDir := filepath.Join(workDir, filepath.Clean(args.Path))
	if !strings.HasPrefix(searchDir, filepath.Clean(workDir)) {
		return "", fmt.Errorf("Grep: path %q outside working directory", args.Path)
	}

	var results []string
	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Size() > 1<<20 { // skip files > 1MB
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		relPath, _ := filepath.Rel(workDir, path)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
		return nil
	})

	return strings.Join(results, "\n"), nil
}

// BuiltinToolDefs returns models.ToolDef definitions for the named built-in tools.
func BuiltinToolDefs(toolNames []string) []models.ToolDef {
	schemas := map[string]models.ToolDef{
		"Read": {
			Name:        "Read",
			Description: "Read a file's contents",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path to the file"}},"required":["path"]}`),
		},
		"Glob": {
			Name:        "Glob",
			Description: "Find files matching a glob pattern",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (e.g. **/*.go)"}},"required":["pattern"]}`),
		},
		"Grep": {
			Name:        "Grep",
			Description: "Search file contents with a regex pattern",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"},"path":{"type":"string","description":"Relative directory or file path to search in"}},"required":["pattern","path"]}`),
		},
	}

	var defs []models.ToolDef
	for _, name := range toolNames {
		if def, ok := schemas[name]; ok {
			defs = append(defs, def)
		}
	}
	return defs
}
