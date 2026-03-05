// internal/context/repo_analyzer.go
package context

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RepoInfo holds detected information about a repository.
type RepoInfo struct {
	Language  string
	Framework string
	TestCmd   string
	LintCmd   string
	BuildCmd  string
	FileTree  string
}

// AnalyzeRepo detects the language, framework, and commands for a repository.
// Priority 1: .foreman-context.md overrides commands; Priority 2: config file detection.
// Returns an error if workDir does not exist or is not a directory.
func AnalyzeRepo(workDir string) (*RepoInfo, error) {
	if fi, err := os.Stat(workDir); err != nil {
		return nil, fmt.Errorf("AnalyzeRepo: workDir does not exist: %w", err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("AnalyzeRepo: workDir is not a directory: %s", workDir)
	}

	info := &RepoInfo{Language: "unknown"}

	// Priority 1: Read .foreman-context.md
	contextPath := filepath.Join(workDir, ".foreman-context.md")
	if content, err := os.ReadFile(contextPath); err == nil {
		parseContextFile(info, string(content))
		// Still detect language
		detectLanguage(workDir, info)
	} else {
		// Priority 2: Auto-detect from config files
		detectLanguage(workDir, info)
		detectCommands(workDir, info)
	}

	// Always generate file tree
	info.FileTree = generateFileTree(workDir)

	return info, nil
}

func parseContextFile(info *RepoInfo, content string) {
	// Extract commands from backtick-wrapped values after "Test:", "Lint:", "Build:"
	testRe := regexp.MustCompile("(?i)[-*]\\s*test:\\s*`([^`]+)`")
	lintRe := regexp.MustCompile("(?i)[-*]\\s*lint:\\s*`([^`]+)`")
	buildRe := regexp.MustCompile("(?i)[-*]\\s*build:\\s*`([^`]+)`")

	if m := testRe.FindStringSubmatch(content); len(m) > 1 {
		info.TestCmd = m[1]
	}
	if m := lintRe.FindStringSubmatch(content); len(m) > 1 {
		info.LintCmd = m[1]
	}
	if m := buildRe.FindStringSubmatch(content); len(m) > 1 {
		info.BuildCmd = m[1]
	}
}

func detectLanguage(workDir string, info *RepoInfo) {
	if fileExists(workDir, "go.mod") {
		info.Language = "go"
		return
	}
	if fileExists(workDir, "Cargo.toml") {
		info.Language = "rust"
		return
	}
	if fileExists(workDir, "package.json") {
		// Check if TypeScript by presence of tsconfig.json or "tsc" in scripts
		if fileExists(workDir, "tsconfig.json") {
			info.Language = "typescript"
			return
		}
		if content, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
			if strings.Contains(string(content), `"tsc"`) || strings.Contains(string(content), "typescript") {
				info.Language = "typescript"
				return
			}
		}
		info.Language = "javascript"
		return
	}
	if fileExists(workDir, "pyproject.toml") || fileExists(workDir, "requirements.txt") || fileExists(workDir, "setup.py") {
		info.Language = "python"
		return
	}
	if fileExists(workDir, "Gemfile") {
		info.Language = "ruby"
		return
	}
}

func detectCommands(workDir string, info *RepoInfo) {
	switch info.Language {
	case "go":
		info.TestCmd = "go test ./..."
		info.BuildCmd = "go build ./..."
		info.LintCmd = "go vet ./..."
	case "typescript", "javascript":
		info.TestCmd = "npm test"
		info.BuildCmd = "npm run build"
		if content, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
			var pkg map[string]interface{}
			if json.Unmarshal(content, &pkg) == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					if _, ok := scripts["lint"]; ok {
						info.LintCmd = "npm run lint"
					}
				}
			}
		}
	case "python":
		info.TestCmd = "pytest"
		info.LintCmd = "ruff check ."
	case "rust":
		info.TestCmd = "cargo test"
		info.BuildCmd = "cargo build"
		info.LintCmd = "cargo clippy"
	case "ruby":
		info.TestCmd = "bundle exec rspec"
		info.LintCmd = "bundle exec rubocop"
	}
}

func fileExists(workDir, name string) bool {
	_, err := os.Stat(filepath.Join(workDir, name))
	return err == nil
}

// skipDirs are directories to exclude from the file tree.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, "__pycache__": true,
	".next": true, "dist": true, "build": true, "target": true,
	".claude": true, ".idea": true, ".vscode": true,
}

func generateFileTree(workDir string) string {
	var entries []string
	filepath.Walk(workDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		if rel == "." {
			return nil
		}
		if fi.IsDir() {
			base := filepath.Base(rel)
			if skipDirs[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			entries = append(entries, rel+"/")
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	sort.Strings(entries)
	if len(entries) > 500 {
		entries = entries[:500]
		entries = append(entries, fmt.Sprintf("... (%d+ files, truncated)", 500))
	}
	return strings.Join(entries, "\n")
}
