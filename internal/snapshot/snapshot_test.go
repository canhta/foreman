package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackAndPatch(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()

	s := New(workDir, dataDir)

	// Create initial file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main"), 0o644))

	// Track initial state
	hash, err := s.Track()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Modify file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n\nfunc main() {}"), 0o644))

	// Add new file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "util.go"), []byte("package main"), 0o644))

	// Get patch — should show both files changed
	patch, err := s.Patch(hash)
	require.NoError(t, err)
	assert.Contains(t, patch.Files, filepath.Join(workDir, "main.go"))
	assert.Contains(t, patch.Files, filepath.Join(workDir, "util.go"))
}

func TestDiff(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()
	s := New(workDir, dataDir)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.go"), []byte("hello"), 0o644))
	hash, err := s.Track()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.go"), []byte("world"), 0o644))

	diff, err := s.Diff(hash)
	require.NoError(t, err)
	assert.Contains(t, diff, "hello")
	assert.Contains(t, diff, "world")
}

func TestRestore(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()
	s := New(workDir, dataDir)

	filePath := filepath.Join(workDir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("original"), 0o644))

	hash, err := s.Track()
	require.NoError(t, err)

	// Modify
	require.NoError(t, os.WriteFile(filePath, []byte("modified"), 0o644))

	// Restore
	err = s.Restore(hash)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}
