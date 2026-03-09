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
	cmd := exec.CommandContext(context.Background(), name, args...)
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
	cmd := exec.CommandContext(context.Background(), "git", "branch", "--show-current")
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

	// SizeBytes must be populated for tracked files (regression guard for Issue 4).
	for _, e := range entries {
		if e.Path == "README.md" {
			assert.Greater(t, e.SizeBytes, int64(0), "README.md SizeBytes should be > 0")
			break
		}
	}
}

func TestNativeGitProvider_Diff(t *testing.T) {
	dir := initTestRepo(t)
	g := NewNativeGitProvider()
	ctx := context.Background()

	// Capture SHA of the initial commit.
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	sha1 := strings.TrimSpace(string(out))

	// Make a second commit with a changed file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Updated content"), 0o644))
	run(t, dir, "git", "add", ".")
	sha2, err := g.Commit(ctx, dir, "update readme")
	require.NoError(t, err)

	diff, err := g.Diff(ctx, dir, sha1, sha2)
	require.NoError(t, err)
	assert.Contains(t, diff, "Updated content")
}

func TestNativeGitProvider_RebaseOnto(t *testing.T) {
	dir := initTestRepo(t)
	g := NewNativeGitProvider()
	ctx := context.Background()

	// Detect the current default branch name (could be "main" or "master").
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	branchOut, err := cmd.Output()
	require.NoError(t, err)
	mainBranch := strings.TrimSpace(string(branchOut))

	// Create a feature branch from the initial commit and modify the same line.
	run(t, dir, "git", "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Feature change"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "feature change")

	// Go back to the default branch and make a conflicting change to the same line.
	run(t, dir, "git", "checkout", mainBranch)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Main change"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "main change")

	// Switch back to feature and rebase onto main — this should conflict.
	run(t, dir, "git", "checkout", "feature")

	// Ensure we abort any mid-rebase state on exit so TempDir cleanup succeeds.
	defer func() {
		abortCmd := exec.CommandContext(context.Background(), "git", "rebase", "--abort")
		abortCmd.Dir = dir
		_ = abortCmd.Run() // ignore error if no rebase in progress
	}()

	result, err := g.RebaseOnto(ctx, dir, mainBranch)
	require.NoError(t, err, "RebaseOnto should not return a Go error on conflict")
	assert.False(t, result.Success, "rebase should report failure due to conflict")
	assert.NotEmpty(t, result.ConflictFiles, "conflict files should be reported")
}

func TestNativeGitProvider_EnsureRepo(t *testing.T) {
	g := NewNativeGitProvider()
	ctx := context.Background()

	t.Run("valid git repo", func(t *testing.T) {
		dir := initTestRepo(t)
		err := g.EnsureRepo(ctx, dir)
		require.NoError(t, err)
	})

	t.Run("non-git directory without clone_url returns error", func(t *testing.T) {
		dir := t.TempDir() // plain directory, not a git repo
		err := g.EnsureRepo(ctx, dir)
		require.Error(t, err, "EnsureRepo should fail on a non-git directory when no clone_url is set")
	})

	t.Run("non-existent directory is created and cloned", func(t *testing.T) {
		// Use an existing repo as the clone source so no network is required.
		src := initTestRepo(t)
		dest := filepath.Join(t.TempDir(), "work")
		gWithClone := NewNativeGitProviderWithClone(src)
		require.NoError(t, gWithClone.EnsureRepo(ctx, dest))
		// Verify the clone landed correctly.
		_, err := os.Stat(filepath.Join(dest, ".git"))
		require.NoError(t, err, "expected .git directory after clone")
	})
}

func TestNativeGitProvider_Log(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	commits, err := git.Log(context.Background(), dir, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, commits)
	assert.Equal(t, "initial", commits[0].Message)
}

func TestNativeGitProvider_StageAll(t *testing.T) {
	dir := initTestRepo(t)
	git := NewNativeGitProvider()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main"), 0o644))
	require.NoError(t, git.StageAll(context.Background(), dir))

	// Verify the file is staged
	cmd := exec.CommandContext(context.Background(), "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), "new.go")
}

func trimNewline(s string) string {
	return strings.TrimRight(s, "\r\n")
}

func TestNativeGitProvider_ResetWorktree(t *testing.T) {
	// Create a temp git repo
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", repoDir)
	require.NoError(t, cmd.Run())
	// Configure git user for commits
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "Test").Run()
	// Create initial commit
	testFile := filepath.Join(repoDir, "main.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))
	exec.Command("git", "-C", repoDir, "add", ".").Run()
	exec.Command("git", "-C", repoDir, "commit", "-m", "init").Run()

	g := NewNativeGitProvider()
	// Dirty the repo
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// dirty"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "untracked.go"), []byte("x"), 0o644))

	err := g.ResetWorktree(context.Background(), repoDir, "HEAD")
	require.NoError(t, err)

	// main.go should be restored to original content
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "package main", string(data))

	// untracked.go should be removed
	_, err = os.Stat(filepath.Join(repoDir, "untracked.go"))
	assert.True(t, os.IsNotExist(err))
}

func TestNativeGitProvider_CleanWorktree(t *testing.T) {
	repoDir := t.TempDir()
	run(t, repoDir, "git", "init")
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")
	testFile := filepath.Join(repoDir, "main.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0o644))
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")

	g := NewNativeGitProvider()

	// Add an untracked file
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "untracked.go"), []byte("x"), 0o644))

	err := g.CleanWorktree(context.Background(), repoDir)
	require.NoError(t, err)

	// untracked.go should be removed
	_, err = os.Stat(filepath.Join(repoDir, "untracked.go"))
	assert.True(t, os.IsNotExist(err))

	// main.go should still be present (tracked file, not touched by clean)
	_, err = os.Stat(testFile)
	require.NoError(t, err)
}
