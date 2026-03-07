package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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
	r.Register(&getTypeDefinitionTool{})
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
	_ = filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
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

// --- GetTypeDefinition ---

// maxTypeSearchFiles caps how many files we walk to avoid runaway searches.
const maxTypeSearchFiles = 1000

type getTypeDefinitionTool struct{}

func (t *getTypeDefinitionTool) Name() string { return "get_type_definition" }
func (t *getTypeDefinitionTool) Description() string {
	return "Returns the full type definition for a named type, interface, struct, or type alias in the codebase."
}
func (t *getTypeDefinitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "symbol":{"type":"string","description":"The type name to look up (e.g. 'Database', 'AgentRequest', 'ErrorType')"},
  "file":{"type":"string","description":"Optional: path to a file in the same package as the symbol, used to determine search scope"}
},
"required":["symbol"]}`)
}

func (t *getTypeDefinitionTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Symbol string `json:"symbol"`
		File   string `json:"file"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("get_type_definition: %w", err)
	}
	if args.Symbol == "" {
		return "", fmt.Errorf("get_type_definition: symbol is required")
	}

	// Validate optional file hint
	if args.File != "" {
		if err := ValidatePath(workDir, args.File); err != nil {
			return "", fmt.Errorf("get_type_definition: %w", err)
		}
	}

	// Determine the hint file's absolute path (empty means no hint).
	hintFile := ""
	if args.File != "" {
		hintFile = AbsPath(workDir, args.File)
	}

	// Detect language from hint file extension or search all files.
	ext := ""
	if hintFile != "" {
		ext = strings.ToLower(filepath.Ext(hintFile))
	}

	switch ext {
	case ".go", "": // Go (or unknown — try Go first since it's a Go project)
		result, err := findGoTypeDefinition(workDir, hintFile, args.Symbol)
		if err != nil {
			return "", fmt.Errorf("get_type_definition: %w", err)
		}
		if result != "" {
			return result, nil
		}
		// If hint was a .go file or empty and nothing found, fall through only
		// for non-.go hint extensions.
		if ext == ".go" || ext == "" {
			return fmt.Sprintf("Type %q not found in Go files under %s", args.Symbol, workDir), nil
		}
		fallthrough
	default:
		result, err := findTypeDefinitionRegex(workDir, hintFile, args.Symbol)
		if err != nil {
			return "", fmt.Errorf("get_type_definition: %w", err)
		}
		if result != "" {
			return result, nil
		}
		return fmt.Sprintf("Type %q not found", args.Symbol), nil
	}
}

// findGoTypeDefinition uses AST parsing to locate a type declaration in Go source files.
// It first searches the directory containing hintFile (same package), then walks workDir.
func findGoTypeDefinition(workDir, hintFile, symbol string) (string, error) {
	// Collect directories to search: package dir first, then full workDir walk.
	var searchDirs []string
	if hintFile != "" {
		pkgDir := filepath.Dir(hintFile)
		searchDirs = append(searchDirs, pkgDir)
	}
	// Always include workDir as fallback (deduplication handled below).
	searchDirs = append(searchDirs, workDir)

	seen := make(map[string]bool)
	fileCount := 0

	for _, dir := range searchDirs {
		result, err := walkGoFiles(dir, symbol, seen, &fileCount)
		if err != nil {
			return "", err
		}
		if result != "" {
			return result, nil
		}
		if fileCount >= maxTypeSearchFiles {
			break
		}
	}
	return "", nil
}

// walkGoFiles walks a directory tree parsing .go files for a type declaration.
func walkGoFiles(root, symbol string, seen map[string]bool, count *int) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if found != "" || *count >= maxTypeSearchFiles {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".go" {
			return nil
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		(*count)++

		result, parseErr := parseGoFileForType(path, symbol)
		if parseErr != nil {
			return nil // skip unparseable files silently
		}
		if result != "" {
			found = result
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	return found, nil
}

// parseGoFileForType parses a single .go file looking for a type declaration named symbol.
// Returns the formatted declaration (with doc comment) if found, empty string otherwise.
func parseGoFileForType(filePath, symbol string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return "", err
	}

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != symbol {
				continue
			}
			// Found it — format the declaration.
			// Build a minimal file fragment to format with go/format.
			var buf bytes.Buffer
			// Include doc comment from the GenDecl (if only one spec) or the TypeSpec.
			var doc *ast.CommentGroup
			if len(genDecl.Specs) == 1 {
				doc = genDecl.Doc
			}
			if typeSpec.Comment != nil {
				doc = typeSpec.Comment
			}
			if typeSpec.Doc != nil {
				doc = typeSpec.Doc
			}
			if doc != nil {
				for _, c := range doc.List {
					buf.WriteString(c.Text)
					buf.WriteByte('\n')
				}
			}
			// Create a minimal synthetic file to format just the type decl.
			snippet := &ast.File{
				Name:  ast.NewIdent("p"),
				Decls: []ast.Decl{genDecl},
			}
			var fmtBuf bytes.Buffer
			if fmtErr := format.Node(&fmtBuf, fset, snippet); fmtErr == nil {
				// Strip the "package p\n" header added by format.Node.
				formatted := fmtBuf.String()
				if idx := strings.Index(formatted, "\ntype "); idx >= 0 {
					formatted = strings.TrimSpace(formatted[idx:])
				} else if idx := strings.Index(formatted, "type "); idx >= 0 {
					formatted = strings.TrimSpace(formatted[idx:])
				}
				if doc != nil {
					// Prepend the doc comment text.
					return buf.String() + formatted, nil
				}
				return formatted, nil
			}
			// Fallback: raw source extraction.
			return extractRawSource(filePath, fset.Position(genDecl.Pos()).Line, fset.Position(genDecl.End()).Line)
		}
	}
	return "", nil
}

// extractRawSource reads lines [startLine, endLine] (1-indexed, inclusive) from a file.
func extractRawSource(filePath string, startLine, endLine int) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum >= startLine && lineNum <= endLine {
			lines = append(lines, scanner.Text())
		}
		if lineNum > endLine {
			break
		}
	}
	return strings.Join(lines, "\n"), nil
}

// findTypeDefinitionRegex uses regex heuristics for non-Go languages.
// It searches hintFile's directory first, then walks workDir.
func findTypeDefinitionRegex(workDir, hintFile, symbol string) (string, error) {
	// Patterns keyed by file extension — each maps to a block-start regex.
	// We look for the definition start and capture until the closing brace/end.
	escapedSymbol := regexp.QuoteMeta(symbol)

	// Common patterns across languages.
	startPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^(export\s+)?(interface|type)\s+` + escapedSymbol + `\b`),                               // TS/JS
		regexp.MustCompile(`(?m)^(export\s+)?(class|struct|enum)\s+` + escapedSymbol + `\b`),                            // many langs
		regexp.MustCompile(`(?m)^(pub\s+)?(struct|enum|trait|type)\s+` + escapedSymbol + `\b`),                          // Rust
		regexp.MustCompile(`(?m)^class\s+` + escapedSymbol + `\b`),                                                      // Python
		regexp.MustCompile(`(?m)^(public\s+|private\s+|protected\s+)?(class|interface|enum)\s+` + escapedSymbol + `\b`), // Java/C#
	}

	var searchPaths []string
	if hintFile != "" {
		searchPaths = append(searchPaths, filepath.Dir(hintFile))
	}
	searchPaths = append(searchPaths, workDir)

	seen := make(map[string]bool)
	fileCount := 0

	for _, dir := range searchPaths {
		result, err := walkNonGoFiles(dir, symbol, startPatterns, seen, &fileCount)
		if err != nil {
			return "", err
		}
		if result != "" {
			return result, nil
		}
		if fileCount >= maxTypeSearchFiles {
			break
		}
	}
	return "", nil
}

func walkNonGoFiles(root, _ string, startPatterns []*regexp.Regexp, seen map[string]bool, count *int) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if found != "" || *count >= maxTypeSearchFiles {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		// Skip Go files (handled by Go AST path) and binary-like extensions.
		skipExts := map[string]bool{".go": true, ".bin": true, ".exe": true, ".so": true, ".a": true}
		if skipExts[ext] {
			return nil
		}
		if seen[path] {
			return nil
		}
		info, _ := d.Info()
		if info != nil && info.Size() > 1<<20 { // skip files > 1 MB
			return nil
		}
		seen[path] = true
		(*count)++

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)

		for _, re := range startPatterns {
			loc := re.FindStringIndex(content)
			if loc == nil {
				continue
			}
			// Extract from match start until matching closing brace.
			block := extractBlock(content, loc[0])
			if block != "" {
				found = block
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	return found, nil
}

// extractBlock extracts a brace-delimited block starting at offset in content.
// Falls back to returning lines until a blank line if no braces found.
func extractBlock(content string, offset int) string {
	// Find the opening brace.
	braceIdx := strings.Index(content[offset:], "{")
	if braceIdx < 0 {
		// No brace — return the declaration line.
		end := strings.Index(content[offset:], "\n")
		if end < 0 {
			return strings.TrimSpace(content[offset:])
		}
		return strings.TrimSpace(content[offset : offset+end])
	}
	braceIdx += offset

	depth := 0
	for i := braceIdx; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(content[offset : i+1])
			}
		}
	}
	// Unbalanced — return from offset to end of file (truncated).
	end := offset + 2000
	if end > len(content) {
		end = len(content)
	}
	return strings.TrimSpace(content[offset:end])
}
