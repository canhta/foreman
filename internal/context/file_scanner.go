package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScannedFile represents a file discovered during scanning with its tier and token cost.
type ScannedFile struct {
	Path    string
	Content string
	Tier    int
	Tokens  int
}

// tier1Files are always included if they exist.
var tier1Files = map[string]bool{
	"go.mod":           true,
	"package.json":     true,
	"Cargo.toml":       true,
	"pyproject.toml":   true,
	"README.md":        true,
	"CONTRIBUTING.md":  true,
	"AGENTS.md":        true,
	".golangci.yml":    true,
	"Gemfile":          true,
	"requirements.txt": true,
	"setup.py":         true,
}

// tier2ExactFiles are included up to the tier-2 cap.
var tier2ExactFiles = map[string]bool{
	"Dockerfile":        true,
	"docker-compose.yml": true,
	"Makefile":          true,
}

// tier2MainEntries are main entry point filenames.
var tier2MainEntries = map[string]bool{
	"main.go":     true,
	"main.ts":     true,
	"main.py":     true,
	"index.ts":    true,
	"index.js":    true,
	"src/main.ts": true,
	"src/main.go": true,
	"src/main.rs": true,
}

const (
	tier2Cap = 10
	tier3Cap = 20
)

// ScanFiles scans workDir and returns files organized by tier, respecting maxTokens budget.
// Tier 1 (always): manifest and config files.
// Tier 2 (up to 10): CI configs, Dockerfiles, Makefiles, main entry points.
// Tier 3 (up to 20): One representative non-test source file per top-level directory.
// Lower tiers are dropped first when the token budget is exceeded.
func ScanFiles(workDir string, maxTokens int) []ScannedFile {
	var tier1, tier2, tier3 []ScannedFile

	// Collect tier-1 files
	for name := range tier1Files {
		path := filepath.Join(workDir, name)
		if content, err := os.ReadFile(path); err == nil {
			tokens := EstimateTokens(string(content))
			tier1 = append(tier1, ScannedFile{
				Path:    name,
				Content: string(content),
				Tier:    1,
				Tokens:  tokens,
			})
		}
	}

	// Collect tier-2: CI configs
	ciDir := filepath.Join(workDir, ".github", "workflows")
	if entries, err := os.ReadDir(ciDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
				continue
			}
			relPath := filepath.Join(".github", "workflows", e.Name())
			if content, err := os.ReadFile(filepath.Join(workDir, relPath)); err == nil {
				tokens := EstimateTokens(string(content))
				tier2 = append(tier2, ScannedFile{
					Path:    relPath,
					Content: string(content),
					Tier:    2,
					Tokens:  tokens,
				})
			}
		}
	}

	// Collect tier-2: exact files
	for name := range tier2ExactFiles {
		path := filepath.Join(workDir, name)
		if content, err := os.ReadFile(path); err == nil {
			tokens := EstimateTokens(string(content))
			tier2 = append(tier2, ScannedFile{
				Path:    name,
				Content: string(content),
				Tier:    2,
				Tokens:  tokens,
			})
		}
	}

	// Collect tier-2: main entry points
	for name := range tier2MainEntries {
		path := filepath.Join(workDir, name)
		if content, err := os.ReadFile(path); err == nil {
			tokens := EstimateTokens(string(content))
			tier2 = append(tier2, ScannedFile{
				Path:    name,
				Content: string(content),
				Tier:    2,
				Tokens:  tokens,
			})
		}
	}

	// Cap tier 2
	sort.Slice(tier2, func(i, j int) bool { return tier2[i].Path < tier2[j].Path })
	if len(tier2) > tier2Cap {
		tier2 = tier2[:tier2Cap]
	}

	// Collect tier-3: one representative non-test source file per top-level directory
	sourceExts := map[string]bool{
		".go": true, ".ts": true, ".js": true, ".py": true,
		".rs": true, ".rb": true, ".java": true, ".c": true,
		".cpp": true, ".cs": true, ".swift": true, ".kt": true,
	}

	topDirs, _ := os.ReadDir(workDir)
	seenDirs := 0
	for _, d := range topDirs {
		if !d.IsDir() || skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		if seenDirs >= tier3Cap {
			break
		}
		// Find one representative source file (non-test)
		representative := findRepresentativeFile(filepath.Join(workDir, d.Name()), d.Name(), sourceExts)
		if representative != nil {
			tier3 = append(tier3, *representative)
			seenDirs++
		}
	}

	// Apply token budget: drop tier 3 first, then tier 2
	budget := NewTokenBudget(maxTokens)
	var result []ScannedFile

	// Add tier 1 (always, but skip if single file exceeds budget)
	for _, f := range tier1 {
		if budget.CanFit(f.Tokens) {
			budget.Add(f.Tokens)
			result = append(result, f)
		}
	}

	// Add tier 2
	for _, f := range tier2 {
		if budget.CanFit(f.Tokens) {
			budget.Add(f.Tokens)
			result = append(result, f)
		}
	}

	// Add tier 3
	for _, f := range tier3 {
		if budget.CanFit(f.Tokens) {
			budget.Add(f.Tokens)
			result = append(result, f)
		}
	}

	return result
}

// findRepresentativeFile finds the first non-test source file in dir.
func findRepresentativeFile(dir, relPrefix string, exts map[string]bool) *ScannedFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if !exts[ext] {
			continue
		}
		// Skip test files
		if isTestFile(name) {
			continue
		}
		relPath := filepath.Join(relPrefix, name)
		fullPath := filepath.Join(dir, name)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		tokens := EstimateTokens(string(content))
		return &ScannedFile{
			Path:    relPath,
			Content: string(content),
			Tier:    3,
			Tokens:  tokens,
		}
	}
	return nil
}

// isTestFile returns true if the filename looks like a test file.
func isTestFile(name string) bool {
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	if strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.js") {
		return true
	}
	if strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".spec.js") {
		return true
	}
	if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py") {
		return true
	}
	return false
}
