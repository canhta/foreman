package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command failed: %s %v\noutput: %s", name, args, out)
}

func TestNativeGitProvider_CreateBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	err := git.CreateBranch(context.Background(), dir, "feature/test")
	require.NoError(t, err)

	// Verify we're on the new branch
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "feature/test", trimNewline(string(out)))
}

func TestNativeGitProvider_Commit(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main"), 0o644))
	run(t, dir, "git", "add", ".")

	sha, err := git.Commit(context.Background(), dir, "test commit")
	require.NoError(t, err)
	assert.NotEmpty(t, sha)
	assert.Len(t, sha, 40) // Full SHA
}

func TestNativeGitProvider_DiffWorking(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Updated"), 0o644))

	diff, err := git.DiffWorking(context.Background(), dir)
	require.NoError(t, err)
	assert.Contains(t, diff, "Updated")
}

func TestNativeGitProvider_FileTree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/app.go"), []byte("package src"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add src")

	entries, err := git.FileTree(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	assert.Contains(t, paths, "README.md")
	assert.Contains(t, paths, "src/app.go")
}

func TestNativeGitProvider_Log(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	commits, err := git.Log(context.Background(), dir, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, commits)
	assert.Equal(t, "initial", commits[0].Message)
}

func trimNewline(s string) string {
	return strings.TrimRight(s, "\r\n")
}
