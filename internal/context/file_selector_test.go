// internal/context/file_selector_test.go
package context

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()

	files := map[string]string{
		"internal/handler.go":      "package internal\n\nimport \"internal/models\"\n\nfunc Handle() {}",
		"internal/handler_test.go": "package internal\n\nfunc TestHandle() {}",
		"internal/models/user.go":  "package models\n\ntype User struct{}",
		"internal/utils/helper.go": "package utils\n\nfunc Help() {}",
		"cmd/main.go":              "package main\n\nfunc main() {}",
		"go.mod":                   "module test\ngo 1.23",
	}
	for path, content := range files {
		fullPath := filepath.Join(workDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}
	return workDir
}

func TestSelectFilesForTask_ExplicitFiles(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToRead:   []string{"internal/models/user.go"},
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	paths := filePaths(files)
	assert.Contains(t, paths, "internal/models/user.go")
	assert.Contains(t, paths, "internal/handler.go")
}

func TestSelectFilesForTask_TestSibling(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)

	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
}

func TestSelectFilesForTask_ProximityBoost(t *testing.T) {
	workDir := setupTestRepo(t)
	// handler.go is in internal/, so other files in internal/ should be proximity-boosted
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)

	// internal/models/user.go is NOT in the same directory (it's in internal/models/)
	// but internal/handler_test.go IS a test sibling (score=60) and
	// no other non-sibling file lives directly in internal/ in the test repo.
	// The key proximity assertion: a file from a DIFFERENT directory (cmd/main.go)
	// should NOT appear due to proximity scoring alone.
	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
	assert.NotContains(t, paths, "cmd/main.go")
}

func TestSelectFilesForTask_ScoreOrdering(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToRead:   []string{"internal/models/user.go"},
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Explicit files should have highest scores
	assert.True(t, files[0].Score >= 90, "First file should have score >= 90, got %f", files[0].Score)
}

func TestSelectFilesForTask_TokenBudget(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// Very small budget should limit results
	files, err := SelectFilesForTask(task, workDir, 100, nil, nil, 1.5)
	require.NoError(t, err)
	// Should include at least the explicit file
	assert.NotEmpty(t, files)
}

// mockFeedbackQuerier is a test double for the FeedbackQuerier interface.
type mockFeedbackQuerier struct {
	rows []db.ContextFeedbackRow
}

func (m *mockFeedbackQuerier) QueryContextFeedback(_ context.Context, _ []string, _ float64) ([]db.ContextFeedbackRow, error) {
	return m.rows, nil
}

// TestFileSelector_AppliesFeedbackBoost verifies that files in files_touched
// but not in files_selected receive a boost to their score.
func TestFileSelector_AppliesFeedbackBoost(t *testing.T) {
	workDir := setupTestRepo(t)

	// The task explicitly references handler.go only.
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// The mock returns a feedback row where internal/utils/helper.go was
	// touched in a prior similar task but was NOT in files_selected.
	mockFB := &mockFeedbackQuerier{
		rows: []db.ContextFeedbackRow{
			{
				FilesSelected: []string{"internal/handler.go"},
				FilesTouched:  []string{"internal/handler.go", "internal/utils/helper.go"},
			},
		},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, mockFB, 1.5)
	require.NoError(t, err)

	// internal/utils/helper.go should be boosted: base proximity score * 1.5
	var helperScore float64
	found := false
	for _, f := range files {
		if f.Path == "internal/utils/helper.go" {
			helperScore = f.Score
			found = true
			break
		}
	}
	require.True(t, found, "internal/utils/helper.go should be in results after feedback boost")

	// Without boost: proximity score = 0 (different dir). With boost it must be > 0.
	// The base score for an unrelated directory would be 0 (not proximity-matching internal/).
	// But since the file IS in a different directory, it would normally not be selected.
	// The boost should bring it into scoring.
	assert.Greater(t, helperScore, 0.0, "feedback-boosted file must have positive score")
}

// TestFileSelector_NilFeedbackQuerier verifies that passing nil as the feedback querier
// works fine (backward compatibility).
func TestFileSelector_NilFeedbackQuerier(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// nil querier must not panic
	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

// TestSelectFilesForTask_ProgressPatternBonus verifies that a file whose path
// appears in a progress pattern value receives a +20 bonus above its base score.
func TestSelectFilesForTask_ProgressPatternBonus(t *testing.T) {
	workDir := setupTestRepo(t)

	// Task only explicitly references handler.go — utils/helper.go has no
	// explicit reference, test-sibling, or proximity signal.
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// Pattern whose value contains the path of a file that would otherwise
	// have no score signal.
	patterns := []models.ProgressPattern{
		{
			PatternKey:   "helper_location",
			PatternValue: "internal/utils/helper.go",
		},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5, patterns...)
	require.NoError(t, err)

	// Find the pattern-boosted file.
	var helperScore float64
	found := false
	for _, f := range files {
		if f.Path == "internal/utils/helper.go" {
			helperScore = f.Score
			found = true
			break
		}
	}
	require.True(t, found, "internal/utils/helper.go should be in results after pattern bonus")

	// The file has no base score (it's not in an adjacent directory), so the
	// score should be exactly +20 from the pattern bonus.
	assert.Equal(t, 20.0, helperScore, "pattern-matched file should have score of exactly 20")
}

// TestSelectFilesForTask_ProgressPatternBonus_AddsToBaseScore verifies that a
// file that already has a base score gets +20 added (not replaced).
func TestSelectFilesForTask_ProgressPatternBonus_AddsToBaseScore(t *testing.T) {
	workDir := setupTestRepo(t)

	// handler_test.go gets a test-sibling score of 60 from handler.go.
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// Pattern whose value contains the test sibling path.
	patterns := []models.ProgressPattern{
		{
			PatternKey:   "test_pattern",
			PatternValue: "use internal/handler_test.go for testing",
		},
	}

	files, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5, patterns...)
	require.NoError(t, err)

	var siblingScore float64
	for _, f := range files {
		if f.Path == "internal/handler_test.go" {
			siblingScore = f.Score
			break
		}
	}

	// Base score = 60 (test sibling) + 20 (pattern bonus) = 80.
	assert.Equal(t, 80.0, siblingScore, "pattern-matched file with base score 60 should have score 80")
}

// TestSelectFilesForTask_NoPatternMatch verifies that a file with no pattern
// match gets its unmodified base score.
func TestSelectFilesForTask_NoPatternMatch(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	// Pattern that doesn't match any file path.
	patterns := []models.ProgressPattern{
		{
			PatternKey:   "irrelevant",
			PatternValue: "this_pattern_does_not_match_any_file",
		},
	}

	filesWithPatterns, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5, patterns...)
	require.NoError(t, err)

	filesNoPatterns, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)

	// Scores should be identical for all files.
	scoreMap := func(files []ScoredFile) map[string]float64 {
		m := make(map[string]float64, len(files))
		for _, f := range files {
			m[f.Path] = f.Score
		}
		return m
	}

	withPatternScores := scoreMap(filesWithPatterns)
	withoutPatternScores := scoreMap(filesNoPatterns)

	for path, score := range withoutPatternScores {
		assert.Equal(t, score, withPatternScores[path],
			"file %s score should be unchanged when pattern doesn't match", path)
	}
}

// TestSelectFilesForTask_EmptyPatterns verifies that an empty patterns list
// produces identical scores to calling with no patterns.
func TestSelectFilesForTask_EmptyPatterns(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	filesEmpty, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5, []models.ProgressPattern{}...)
	require.NoError(t, err)

	filesNone, err := SelectFilesForTask(task, workDir, 80000, nil, nil, 1.5)
	require.NoError(t, err)

	assert.Equal(t, len(filesNone), len(filesEmpty), "empty patterns should produce same number of files")
	for i := range filesNone {
		assert.Equal(t, filesNone[i].Score, filesEmpty[i].Score,
			"file %s score should match with empty vs no patterns", filesNone[i].Path)
	}
}

func filePaths(files []ScoredFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}
