package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanFiles_Tier1AlwaysIncluded(t *testing.T) {
	dir := t.TempDir()

	// Create tier-1 files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello"), 0o644))
	// Create a non-tier file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "random.txt"), []byte("not important"), 0o644))

	files := ScanFiles(dir, 100000)

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}
	assert.True(t, paths["go.mod"], "go.mod should be included as tier 1")
	assert.True(t, paths["README.md"], "README.md should be included as tier 1")

	// Verify tier assignments
	for _, f := range files {
		if f.Path == "go.mod" || f.Path == "README.md" {
			assert.Equal(t, 1, f.Tier, "tier-1 files should have Tier=1")
		}
	}
}

func TestScanFiles_RespectsTokenBudget(t *testing.T) {
	dir := t.TempDir()

	// Small tier-1 file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example"), 0o644))

	// Large file that should be excluded under tight budget
	bigContent := strings.Repeat("x", 200*1024) // 200KB
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg", "big.go"), []byte(bigContent), 0o644))

	// Very tight budget: only enough for go.mod (~4 tokens)
	files := scanFilesWithEstimator(dir, 50, func(content string) int {
		return (len(content) + 3) / 4
	})

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	assert.True(t, paths["go.mod"], "small tier-1 file should fit")
	assert.False(t, paths[filepath.Join("pkg", "big.go")], "big file should be excluded by token budget")
}

func TestScanFiles_Tier2CIConfigs(t *testing.T) {
	dir := t.TempDir()

	// Create a CI config
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644))
	// Create Dockerfile
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM golang"), 0o644))

	files := ScanFiles(dir, 100000)

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}
	assert.True(t, paths[filepath.Join(".github", "workflows", "ci.yml")])
	assert.True(t, paths["Dockerfile"])
}

func TestScanFiles_Tier3RepresentativeFiles(t *testing.T) {
	dir := t.TempDir()

	// Create top-level dirs with source files
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "other.go"), []byte("package main"), 0o644))
	// Test files should be excluded from tier 3
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "main_test.go"), []byte("package main"), 0o644))

	files := ScanFiles(dir, 100000)

	// Should have exactly one representative file from cmd/
	tier3Count := 0
	for _, f := range files {
		if f.Tier == 3 && strings.HasPrefix(f.Path, "cmd/") {
			tier3Count++
		}
	}
	assert.Equal(t, 1, tier3Count, "should include exactly one representative file per top-level dir")
}
