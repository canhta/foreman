// internal/git/gogit_test.go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoGitProvider_EnsureRepo(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	os.MkdirAll(workDir, 0o755)

	provider := NewGoGitProvider()
	err := provider.EnsureRepo(context.Background(), workDir)
	// go-git EnsureRepo on an empty dir should init
	if err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	// Verify .git exists
	if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
		t.Error("expected .git directory")
	}
}

func TestGoGitProvider_EnsureRepo_Idempotent(t *testing.T) {
	dir := t.TempDir()
	provider := NewGoGitProvider()

	// First call inits
	if err := provider.EnsureRepo(context.Background(), dir); err != nil {
		t.Fatalf("first EnsureRepo failed: %v", err)
	}
	// Second call should succeed without error (already a repo)
	if err := provider.EnsureRepo(context.Background(), dir); err != nil {
		t.Fatalf("second EnsureRepo failed: %v", err)
	}
}

func TestGoGitProvider_FileTree(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	exec.CommandContext(ctx, "git", "init", dir).Run()
	// Configure git identity for commit
	exec.CommandContext(ctx, "git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.CommandContext(ctx, "git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	exec.CommandContext(ctx, "git", "-C", dir, "add", ".").Run()
	exec.CommandContext(ctx, "git", "-C", dir, "commit", "-m", "init").Run()

	provider := NewGoGitProvider()
	files, err := provider.FileTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("FileTree failed: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected at least one file")
	}
}

func TestGoGitProvider_StageAll(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	exec.CommandContext(ctx, "git", "init", dir).Run()
	exec.CommandContext(ctx, "git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.CommandContext(ctx, "git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)

	provider := NewGoGitProvider()
	err := provider.StageAll(context.Background(), dir)
	if err != nil {
		t.Fatalf("StageAll failed: %v", err)
	}
}
