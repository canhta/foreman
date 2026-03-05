// internal/context/file_selector_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

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

	files, err := SelectFilesForTask(task, workDir, 80000)
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

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)

	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
}

func TestSelectFilesForTask_ProximityBoost(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
	require.NoError(t, err)

	// Files in the same directory should be included
	paths := filePaths(files)
	assert.Contains(t, paths, "internal/handler_test.go")
}

func TestSelectFilesForTask_ScoreOrdering(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		FilesToRead:   []string{"internal/models/user.go"},
		FilesToModify: []string{"internal/handler.go"},
	}

	files, err := SelectFilesForTask(task, workDir, 80000)
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
	files, err := SelectFilesForTask(task, workDir, 100)
	require.NoError(t, err)
	// Should include at least the explicit file
	assert.NotEmpty(t, files)
}

func filePaths(files []ScoredFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}
