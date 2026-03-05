// internal/context/file_selector.go
package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// ScoredFile is a file ranked by relevance to a task.
type ScoredFile struct {
	Path      string
	Score     float64
	Reason    string
	SizeBytes int64
}

// SelectFilesForTask returns the most relevant files within the token budget.
func SelectFilesForTask(task *models.Task, workDir string, tokenBudget int) ([]ScoredFile, error) {
	candidates := map[string]*ScoredFile{}

	// Signal 1: Explicit planner references (highest priority)
	for _, path := range task.FilesToRead {
		addCandidate(candidates, workDir, path, 100, "planner:read")
	}
	for _, path := range task.FilesToModify {
		cleanPath := strings.TrimSuffix(path, " (new)")
		addCandidate(candidates, workDir, cleanPath, 100, "planner:modify")
	}

	// Signal 2: Test file siblings
	for _, path := range task.FilesToModify {
		cleanPath := strings.TrimSuffix(path, " (new)")
		sibling := findTestSibling(workDir, cleanPath)
		if sibling != "" {
			addCandidate(candidates, workDir, sibling, 60, "test_sibling")
		}
	}

	// Signal 3: Directory proximity
	taskDirs := extractDirectories(task.FilesToModify)
	allFiles := listSourceFiles(workDir)
	for _, f := range allFiles {
		if _, exists := candidates[f]; exists {
			continue
		}
		if inAnyDirectory(f, taskDirs) {
			addCandidate(candidates, workDir, f, 30, "proximity")
		}
	}

	// Convert to slice and sort by score descending
	result := make([]ScoredFile, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, *c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	// Apply token budget cutoff
	selected := make([]ScoredFile, 0, len(result))
	tokensUsed := 0
	for _, c := range result {
		fileTokens := estimateFileTokens(c.SizeBytes)
		if tokensUsed+fileTokens > tokenBudget && len(selected) > 0 {
			continue
		}
		tokensUsed += fileTokens
		selected = append(selected, c)
	}

	return selected, nil
}

func addCandidate(candidates map[string]*ScoredFile, workDir, path string, score float64, reason string) {
	if existing, ok := candidates[path]; ok {
		if score > existing.Score {
			existing.Score = score
			existing.Reason = reason
		}
		return
	}
	fullPath := filepath.Join(workDir, path)
	fi, err := os.Stat(fullPath)
	if err != nil {
		return // File doesn't exist, skip
	}
	candidates[path] = &ScoredFile{
		Path:      path,
		Score:     score,
		Reason:    reason,
		SizeBytes: fi.Size(),
	}
}

func findTestSibling(workDir, path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	// Go: foo.go → foo_test.go
	if ext == ".go" {
		candidate := base + "_test.go"
		if _, err := os.Stat(filepath.Join(workDir, candidate)); err == nil {
			return candidate
		}
	}
	// JS/TS: foo.ts → foo.test.ts / foo.spec.ts
	for _, testExt := range []string{".test", ".spec"} {
		candidate := base + testExt + ext
		if _, err := os.Stat(filepath.Join(workDir, candidate)); err == nil {
			return candidate
		}
	}
	return ""
}

func extractDirectories(files []string) []string {
	dirs := map[string]bool{}
	for _, f := range files {
		cleanPath := strings.TrimSuffix(f, " (new)")
		dir := filepath.Dir(cleanPath)
		if dir != "." {
			dirs[dir] = true
		}
	}
	result := make([]string, 0, len(dirs))
	for d := range dirs {
		result = append(result, d)
	}
	return result
}

func inAnyDirectory(file string, dirs []string) bool {
	fileDir := filepath.Dir(file)
	for _, d := range dirs {
		if fileDir == d {
			return true
		}
	}
	return false
}

func listSourceFiles(workDir string) []string {
	var files []string
	filepath.Walk(workDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		files = append(files, rel)
		return nil
	})
	return files
}

func estimateFileTokens(sizeBytes int64) int {
	// Rough estimate: 1 token ≈ 4 bytes
	return int(sizeBytes / 4)
}
