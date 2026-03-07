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
	"strconv"
	"strings"
)

func registerFS(r *Registry) {
	r.Register(&readTool{})
	r.Register(&readRangeTool{})
	r.Register(&writeTool{})
	r.Register(&editTool{})
	r.Register(&multiEditTool{})
	r.Register(&applyPatchTool{})
	r.Register(&listDirTool{})
	r.Register(&globTool{})
	r.Register(&grepTool{})
}

// --- Read ---

type readTool struct{}

func (t *readTool) Name() string { return "Read" }
func (t *readTool) Description() string {
	return "Read file contents, optionally restricting to a line range"
}
func (t *readTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer","description":"1-based start line (inclusive)"},"end_line":{"type":"integer","description":"1-based end line (inclusive)"}},"required":["path"]}`)
}
func (t *readTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	content, err := os.ReadFile(AbsPath(workDir, args.Path))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if args.StartLine == 0 && args.EndLine == 0 {
		return string(content), nil
	}
	lines := strings.Split(string(content), "\n")
	start := args.StartLine - 1
	if start < 0 {
		start = 0
	}
	end := args.EndLine
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n"), nil
}

// --- ReadRange ---

type readRangeTool struct{}

func (t *readRangeTool) Name() string { return "ReadRange" }
func (t *readRangeTool) Description() string {
	return "Read a specific line range from a file (start_line and end_line required, both 1-based inclusive)"
}
func (t *readRangeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"start_line":{"type":"integer","description":"1-based start line (inclusive, must be >= 1)"},"end_line":{"type":"integer","description":"1-based end line (inclusive, must be >= start_line)"}},"required":["path","start_line","end_line"]}`)
}
func (t *readRangeTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("ReadRange: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("ReadRange: %w", err)
	}
	if args.StartLine < 1 {
		return "", fmt.Errorf("ReadRange: start_line must be >= 1, got %d", args.StartLine)
	}
	if args.EndLine < args.StartLine {
		return "", fmt.Errorf("ReadRange: end_line (%d) must be >= start_line (%d)", args.EndLine, args.StartLine)
	}
	content, err := os.ReadFile(AbsPath(workDir, args.Path))
	if err != nil {
		return "", fmt.Errorf("ReadRange: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	start := args.StartLine - 1
	end := args.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n"), nil
}

// --- Write ---

type writeTool struct{}

func (t *writeTool) Name() string        { return "Write" }
func (t *writeTool) Description() string { return "Write content to a file (creates or overwrites)" }
func (t *writeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (t *writeTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := CheckSecrets(args.Path, args.Content); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return "OK", nil
}

// --- Edit ---

type editTool struct{}

func (t *editTool) Name() string { return "Edit" }
func (t *editTool) Description() string {
	return "Replace first occurrence of old_string with new_string in a file"
}
func (t *editTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`)
}
func (t *editTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	if err := CheckSecrets(args.Path, args.NewString); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	if !strings.Contains(string(content), args.OldString) {
		return "", fmt.Errorf("edit: old_string not found in %s", args.Path)
	}
	updated := strings.Replace(string(content), args.OldString, args.NewString, 1)
	if err := os.WriteFile(abs, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	return "OK", nil
}

// --- MultiEdit ---

type multiEditTool struct{}

func (t *multiEditTool) Name() string        { return "MultiEdit" }
func (t *multiEditTool) Description() string { return "Apply multiple edits to a file atomically" }
func (t *multiEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["old_string","new_string"]}}},"required":["path","edits"]}`)
}
func (t *multiEditTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	result := string(content)
	for i, edit := range args.Edits {
		if err := CheckSecrets(args.Path, edit.NewString); err != nil {
			return "", fmt.Errorf("MultiEdit edit %d: %w", i, err)
		}
		if !strings.Contains(result, edit.OldString) {
			return "", fmt.Errorf("MultiEdit edit %d: old_string not found", i)
		}
		result = strings.Replace(result, edit.OldString, edit.NewString, 1)
	}
	if err := os.WriteFile(abs, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	return fmt.Sprintf("OK (%d edits applied)", len(args.Edits)), nil
}

// --- ApplyPatch ---

// patchHunk represents a single hunk from a unified diff.
type patchHunk struct {
	lines     []patchLine
	origStart int // 1-based start line in original
	origCount int
	newStart  int // 1-based start line in new file
	newCount  int
}

type patchLine struct {
	text string
	op   byte // ' ' context, '+' insert, '-' delete
}

// parseUnifiedDiff parses a unified diff patch and returns a slice of hunks.
// It accepts the standard format produced by `git diff` or `diff -u`.
func parseUnifiedDiff(patch string) ([]patchHunk, error) {
	hunkHeader := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	var hunks []patchHunk
	var current *patchHunk
	sawHunk := false

	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			// file header lines — skip
			current = nil
			continue
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			if current != nil {
				hunks = append(hunks, *current)
			}
			h := patchHunk{}
			h.origStart, _ = strconv.Atoi(m[1])
			if m[2] == "" {
				h.origCount = 1
			} else {
				h.origCount, _ = strconv.Atoi(m[2])
			}
			h.newStart, _ = strconv.Atoi(m[3])
			if m[4] == "" {
				h.newCount = 1
			} else {
				h.newCount, _ = strconv.Atoi(m[4])
			}
			current = &h
			sawHunk = true
			continue
		}
		if current == nil {
			continue
		}
		if len(line) == 0 {
			// blank line within hunk = context line with empty content
			current.lines = append(current.lines, patchLine{op: ' ', text: ""})
			continue
		}
		op := line[0]
		if op != '+' && op != '-' && op != ' ' {
			// unknown prefix — treat as end of hunk
			hunks = append(hunks, *current)
			current = nil
			continue
		}
		current.lines = append(current.lines, patchLine{op: op, text: line[1:]})
	}
	if current != nil {
		hunks = append(hunks, *current)
	}
	if !sawHunk {
		return nil, fmt.Errorf("no hunk headers found in patch")
	}
	return hunks, nil
}

// applyHunks applies parsed hunks to the file lines, returning the result or
// a specific rejection reason.
//
// Note: the "\ No newline at end of file" marker in unified diffs is not
// currently tracked; trailing-newline semantics are preserved from the original
// file via the trailing "" sentinel produced by strings.Split on newline-terminated
// content. If a patch changes the trailing-newline status, that change is silently
// ignored and the original file's trailing-newline behaviour is retained.
func applyHunks(fileLines []string, hunks []patchHunk) ([]string, error) {
	result := make([]string, len(fileLines))
	copy(result, fileLines)

	// Apply hunks in reverse order so line number offsets don't shift earlier hunks.
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		// origStart is 1-based; convert to 0-based index.
		pos := h.origStart - 1
		if h.origCount == 0 {
			// Pure insertion: insert new lines after origStart (1-based).
			// origStart==0 means prepend; otherwise insert after that line.
			pos = h.origStart // 0-based index of the line to insert before = origStart

			// For newline-terminated files, strings.Split produces a trailing ""
			// sentinel at index len(fileLines)-1. When origStart equals the number
			// of real lines (i.e. pos == len(result)-1 and result[pos-1] == ""),
			// inserting at pos would place the new lines before the sentinel,
			// correctly. But if pos == len(result) and result[len(result)-1] == "",
			// the slice append puts new lines between the last real line and the
			// sentinel, which is also correct. The problem occurs when pos equals
			// len(result) and the trailing sentinel is included in the prefix:
			//   result[:pos] includes the trailing "" → joining produces an extra blank line.
			// Fix: when pos points past all real lines and the last element is the
			// trailing "" sentinel, insert before the sentinel instead of after it.
			if pos > 0 && pos == len(result) && result[pos-1] == "" {
				pos = pos - 1 // insert before trailing "" sentinel
			}
		}
		if pos < 0 {
			pos = 0
		}

		// Verify context lines match.
		origIdx := pos
		for _, pl := range h.lines {
			if pl.op == '+' {
				continue
			}
			if origIdx >= len(result) {
				return nil, fmt.Errorf("ApplyPatch: hunk @@ -%d,%d +%d,%d @@ context mismatch: file has only %d lines, expected context at line %d",
					h.origStart, h.origCount, h.newStart, h.newCount, len(result), origIdx+1)
			}
			if result[origIdx] != pl.text {
				return nil, fmt.Errorf("ApplyPatch: hunk @@ -%d,%d +%d,%d @@ context mismatch at line %d: expected %q, got %q",
					h.origStart, h.origCount, h.newStart, h.newCount, origIdx+1, pl.text, result[origIdx])
			}
			origIdx++
		}

		// Build replacement lines.
		var newLines []string
		for _, pl := range h.lines {
			if pl.op == ' ' || pl.op == '+' {
				newLines = append(newLines, pl.text)
			}
			// '-' lines are removed (not included)
		}

		// Replace the original span with new lines.
		end := pos + h.origCount
		if end > len(result) {
			end = len(result)
		}
		result = append(result[:pos], append(newLines, result[end:]...)...)
	}
	return result, nil
}

type applyPatchTool struct{}

func (t *applyPatchTool) Name() string { return "ApplyPatch" }
func (t *applyPatchTool) Description() string {
	return "Apply a unified diff format patch to a file. The patch must be in standard unified diff format (as produced by git diff or diff -u)."
}
func (t *applyPatchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative file path to patch"},"patch":{"type":"string","description":"Unified diff patch string (--- / +++ / @@ headers + +/-/space lines)"}},"required":["path","patch"]}`)
}
func (t *applyPatchTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("ApplyPatch: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("ApplyPatch: %w", err)
	}

	hunks, err := parseUnifiedDiff(args.Patch)
	if err != nil {
		return "", fmt.Errorf("ApplyPatch: parse error: %w", err)
	}

	abs := AbsPath(workDir, args.Path)
	contentBytes, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("ApplyPatch: %w", err)
	}

	// Split into lines, preserving trailing newline awareness.
	content := string(contentBytes)
	lines := strings.Split(content, "\n")
	// If the file ends with a newline, Split produces a trailing empty string — keep it.

	patched, err := applyHunks(lines, hunks)
	if err != nil {
		return "", err
	}

	if err := CheckSecrets(args.Path, strings.Join(patched, "\n")); err != nil {
		return "", fmt.Errorf("ApplyPatch: %w", err)
	}

	result := strings.Join(patched, "\n")
	if err := os.WriteFile(abs, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("ApplyPatch: %w", err)
	}
	return fmt.Sprintf("OK (%d hunk(s) applied)", len(hunks)), nil
}

// --- ListDir ---

type listDirTool struct{}

func (t *listDirTool) Name() string        { return "ListDir" }
func (t *listDirTool) Description() string { return "List directory contents with file metadata" }
func (t *listDirTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative directory path"},"recursive":{"type":"boolean","description":"List recursively"}},"required":["path"]}`)
}
func (t *listDirTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("ListDir: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("ListDir: %w", err)
	}
	absDir := AbsPath(workDir, args.Path)
	var lines []string
	err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == absDir {
			return nil
		}
		rel, _ := filepath.Rel(absDir, path)
		if !args.Recursive && strings.Contains(rel, string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, _ := d.Info()
		kind := "file"
		size := int64(0)
		if d.IsDir() {
			kind = "dir"
		} else if info != nil {
			size = info.Size()
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%d", rel, kind, size))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("ListDir: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

// --- Glob ---

type globTool struct{}

func (t *globTool) Name() string        { return "Glob" }
func (t *globTool) Description() string { return "Find files matching a glob pattern (supports **)" }
func (t *globTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern, supports ** for recursive matching"},"base":{"type":"string","description":"Base directory (default: working dir)"}},"required":["pattern"]}`)
}
func (t *globTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Base    string `json:"base"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}
	baseDir := workDir
	if args.Base != "" {
		if err := ValidatePath(workDir, args.Base); err != nil {
			return "", fmt.Errorf("glob: %w", err)
		}
		baseDir = AbsPath(workDir, args.Base)
	}
	matches, err := globMatch(baseDir, args.Pattern)
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}
	var rels []string
	for _, m := range matches {
		rel, _ := filepath.Rel(workDir, m)
		rels = append(rels, rel)
	}
	return strings.Join(rels, "\n"), nil
}

// globMatch handles ** via WalkDir since filepath.Glob doesn't support **.
func globMatch(baseDir, pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		// Simple pattern — use filepath.Glob relative to baseDir
		matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
		if err != nil {
			return nil, err
		}
		return matches, nil
	}
	// Double-star pattern: walk and match each file
	re, err := globPatternToRegexp(pattern)
	if err != nil {
		return nil, err
	}
	var matches []string
	_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(baseDir, path)
		if re.MatchString(rel) {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, nil
}

// globPatternToRegexp converts a glob pattern (including **) to a regexp.
func globPatternToRegexp(pattern string) (*regexp.Regexp, error) {
	// Escape regexp special chars, then replace glob wildcards
	var sb strings.Builder
	sb.WriteString("^")
	i := 0
	for i < len(pattern) {
		if pattern[i] == '*' && i+1 < len(pattern) && pattern[i+1] == '*' {
			sb.WriteString(".*")
			i += 2
			if i < len(pattern) && pattern[i] == '/' {
				i++ // skip trailing slash after **
			}
		} else if pattern[i] == '*' {
			sb.WriteString("[^/]*")
			i++
		} else if pattern[i] == '?' {
			sb.WriteString("[^/]")
			i++
		} else {
			sb.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// --- Grep ---

type grepTool struct{}

func (t *grepTool) Name() string        { return "Grep" }
func (t *grepTool) Description() string { return "Search file contents for a pattern (regexp)" }
func (t *grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regexp pattern to search for"},"path":{"type":"string","description":"Directory or file to search"},"file_pattern":{"type":"string","description":"Glob pattern to filter files (e.g. *.go)"},"case_sensitive":{"type":"boolean","description":"Case-sensitive search (default true)"}},"required":["pattern","path"]}`)
}
func (t *grepTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		CaseSensitive *bool  `json:"case_sensitive"`
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		FilePattern   string `json:"file_pattern"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}

	caseSensitive := true
	if args.CaseSensitive != nil {
		caseSensitive = *args.CaseSensitive
	}
	rePattern := args.Pattern
	if !caseSensitive {
		rePattern = "(?i)" + rePattern
	}
	re, err := regexp.Compile(rePattern)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern: %w", err)
	}

	absPath := AbsPath(workDir, args.Path)
	const maxMatches = 200
	var results []string

	_ = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(results) >= maxMatches {
			return nil
		}
		// Filter by file pattern
		if args.FilePattern != "" {
			matched, _ := filepath.Match(args.FilePattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		rel, _ := filepath.Rel(workDir, path)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() && len(results) < maxMatches {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, lineNum, strings.TrimSpace(line)))
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "No matches found.", nil
	}
	suffix := ""
	if len(results) >= maxMatches {
		suffix = fmt.Sprintf("\n(capped at %d matches)", maxMatches)
	}
	return strings.Join(results, "\n") + suffix, nil
}
