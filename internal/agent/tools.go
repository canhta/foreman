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

// forbiddenPatterns are file paths the Edit/Write tools refuse to touch.
var forbiddenPatterns = []string{".env", ".key", ".pem", ".p12", ".pfx", "id_rsa", "id_ed25519", ".secret"}

// builtinTools maps ALL tool names to their Go implementations.
// The builtin runner controls which tools are exposed to the LLM via AllowedTools.
var builtinTools = map[string]ToolExecutor{
	"Read":  toolRead,
	"Glob":  toolGlob,
	"Grep":  toolGrep,
	"Edit":  toolEdit,
	"Write": toolWrite,
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

func toolEdit(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Edit: invalid input: %w", err)
	}
	if err := validateWritePath(workDir, args.Path); err != nil {
		return "", err
	}
	abs := filepath.Join(workDir, filepath.Clean(args.Path))
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	if !strings.Contains(string(content), args.OldString) {
		return "", fmt.Errorf("Edit: old_string not found in %s", args.Path)
	}
	updated := strings.Replace(string(content), args.OldString, args.NewString, 1)
	if err := os.WriteFile(abs, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	return "OK", nil
}

func toolWrite(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Write: invalid input: %w", err)
	}
	if err := validateWritePath(workDir, args.Path); err != nil {
		return "", err
	}
	abs := filepath.Join(workDir, filepath.Clean(args.Path))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	return "OK", nil
}

// validateWritePath enforces that the path is within workDir and not a forbidden file.
func validateWritePath(workDir, path string) error {
	abs := filepath.Join(workDir, filepath.Clean(path))
	clean := filepath.Clean(workDir) + string(filepath.Separator)
	if !strings.HasPrefix(abs, clean) && abs != filepath.Clean(workDir) {
		return fmt.Errorf("path %q is outside the working directory", path)
	}
	base := strings.ToLower(filepath.Base(path))
	for _, pat := range forbiddenPatterns {
		if strings.Contains(base, pat) || strings.HasSuffix(base, pat) {
			return fmt.Errorf("writing to %q is not allowed (matches forbidden pattern)", path)
		}
	}
	return nil
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
		"Edit": {
			Name:        "Edit",
			Description: "Replace a string in a file (first occurrence only)",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path to the file"},"old_string":{"type":"string","description":"The string to replace"},"new_string":{"type":"string","description":"The replacement string"}},"required":["path","old_string","new_string"]}`),
		},
		"Write": {
			Name:        "Write",
			Description: "Write content to a file (creates or overwrites)",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path to the file"},"content":{"type":"string","description":"Content to write"}},"required":["path","content"]}`),
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
