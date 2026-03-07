// internal/context/file_selector.go
package context

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// FeedbackQuerier is the interface for querying context feedback history.
// It is satisfied by db.Database.
type FeedbackQuerier interface {
	QueryContextFeedback(ctx context.Context, candidates []string, minJaccard float64) ([]db.ContextFeedbackRow, error)
}

// ScoredFile is a file ranked by relevance to a task.
type ScoredFile struct {
	Path      string
	Reason    string
	Score     float64
	SizeBytes int64
}

// SelectFilesForTask returns the most relevant files within the token budget.
// cache is optional (nil = no caching for the source file walk).
// fq is an optional FeedbackQuerier; when non-nil, files that appeared in
// files_touched of prior similar tasks receive a boost of feedbackBoost.
// If feedbackBoost <= 0, 1.5 is used as the default.
// patterns is an optional list of progress patterns (variadic); each file
// whose path appears as a substring of any pattern key or value receives a
// bonus of +20 to its score (ARCH-M03).
func SelectFilesForTask(task *models.Task, workDir string, tokenBudget int, cache *ContextCache, fq FeedbackQuerier, feedbackBoost float64, patterns ...models.ProgressPattern) ([]ScoredFile, error) {
	if feedbackBoost <= 0 {
		feedbackBoost = 1.5
	}

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
	allFiles := GetOrListSourceFiles(cache, workDir)
	for _, f := range allFiles {
		if inAnyDirectory(f, taskDirs) {
			addCandidate(candidates, workDir, f, 30, "proximity")
		}
	}

	// Signal 4: Context feedback boost (REQ-CTX-003)
	// Query prior feedback rows with Jaccard similarity >= 0.3 against current candidates.
	// Boost files that appeared in files_touched but not already in candidates.
	if fq != nil {
		candidatePaths := make([]string, 0, len(candidates))
		for p := range candidates {
			candidatePaths = append(candidatePaths, p)
		}
		feedbackRows, err := fq.QueryContextFeedback(context.Background(), candidatePaths, 0.3)
		if err != nil {
			log.Warn().Err(err).Msg("context_feedback: failed to query feedback rows, skipping boost")
		} else {
			// Build set of already-selected file paths for fast lookup
			selectedSet := make(map[string]struct{}, len(candidatePaths))
			for _, p := range candidatePaths {
				selectedSet[p] = struct{}{}
			}

			// Collect files that were touched but not selected — these get boosted
			boostedFiles := map[string]struct{}{}
			for _, row := range feedbackRows {
				for _, touched := range row.FilesTouched {
					if _, inSelected := selectedSet[touched]; !inSelected {
						boostedFiles[touched] = struct{}{}
					}
				}
			}

			// Apply boost: add to candidates if they exist on disk; use base score * feedbackBoost
			// Base score for feedback-boosted files is 30 (same as proximity), then multiplied.
			baseFeedbackScore := 30.0
			boostedScore := baseFeedbackScore * feedbackBoost
			for f := range boostedFiles {
				// New file from feedback — add with boosted score if it exists on disk.
				// (boostedFiles contains only files NOT already in candidates)
				addCandidate(candidates, workDir, f, boostedScore, "feedback_boost")
			}
		}
	}

	// Signal 5: Progress pattern bonus (ARCH-M03)
	// For each file (already in candidates or from a full walk), check if any
	// progress pattern key or value contains the file's path as a substring.
	// Matching files receive a +20 score bonus. New files (not yet candidates)
	// are added with a base score of 20.
	if len(patterns) > 0 {
		patternFiles := GetOrListSourceFiles(cache, workDir)
		for _, f := range patternFiles {
			for _, p := range patterns {
				if strings.Contains(p.PatternKey, f) || strings.Contains(p.PatternValue, f) {
					if existing, ok := candidates[f]; ok {
						existing.Score += 20
					} else {
						addCandidate(candidates, workDir, f, 20, "pattern_bonus")
					}
					break // one match is enough per file
				}
			}
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
	if walkErr := filepath.Walk(workDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			// Return the error to propagate it back to the Walk caller.
			return err
		}
		if fi.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		files = append(files, rel)
		return nil
	}); walkErr != nil {
		log.Warn().Err(walkErr).Str("workDir", workDir).Msg("listSourceFiles: walk incomplete")
	}
	return files
}

func estimateFileTokens(sizeBytes int64) int {
	// Rough estimate: 1 token ≈ 4 bytes
	return int(sizeBytes / 4)
}
