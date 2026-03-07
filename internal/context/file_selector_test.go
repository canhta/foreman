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

func filePaths(files []ScoredFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}
