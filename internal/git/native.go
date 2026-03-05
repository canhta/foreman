package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NativeGitProvider shells out to the native git CLI.
type NativeGitProvider struct{}

// NewNativeGitProvider creates a native git provider.
func NewNativeGitProvider() *NativeGitProvider {
	return &NativeGitProvider{}
}

func (g *NativeGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, "git", "status")
	return err
}

func (g *NativeGitProvider) CreateBranch(ctx context.Context, workDir, branchName string) error {
	_, err := g.run(ctx, workDir, "git", "checkout", "-b", branchName)
	return err
}

func (g *NativeGitProvider) Commit(ctx context.Context, workDir, message string) (string, error) {
	_, err := g.run(ctx, workDir, "git", "commit", "-m", message)
	if err != nil {
		return "", err
	}
	out, err := g.run(ctx, workDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *NativeGitProvider) Diff(ctx context.Context, workDir, base, head string) (string, error) {
	out, err := g.run(ctx, workDir, "git", "diff", base+"..."+head)
	if err != nil {
		return "", err
	}
	return out, nil
}

func (g *NativeGitProvider) DiffWorking(ctx context.Context, workDir string) (string, error) {
	out, err := g.run(ctx, workDir, "git", "diff")
	if err != nil {
		return "", err
	}
	return out, nil
}

func (g *NativeGitProvider) Push(ctx context.Context, workDir, branchName string) error {
	_, err := g.run(ctx, workDir, "git", "push", "-u", "origin", branchName)
	return err
}

func (g *NativeGitProvider) RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error) {
	_, err := g.run(ctx, workDir, "git", "rebase", targetBranch)
	if err != nil {
		// Check for conflicts
		out, _ := g.run(ctx, workDir, "git", "diff", "--name-only", "--diff-filter=U")
		conflicts := strings.Split(strings.TrimSpace(out), "\n")
		if len(conflicts) == 1 && conflicts[0] == "" {
			conflicts = nil
		}
		diffOut, _ := g.run(ctx, workDir, "git", "diff")
		return &RebaseResult{
			Success:       false,
			ConflictFiles: conflicts,
			ConflictDiff:  diffOut,
		}, nil
	}
	return &RebaseResult{Success: true}, nil
}

func (g *NativeGitProvider) FileTree(ctx context.Context, workDir string) ([]FileEntry, error) {
	out, err := g.run(ctx, workDir, "git", "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	files := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		entries = append(entries, FileEntry{Path: f})
	}
	return entries, nil
}

func (g *NativeGitProvider) Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error) {
	out, err := g.run(ctx, workDir, "git", "log",
		fmt.Sprintf("-n%d", count),
		"--format=%H|%s|%an|%aI",
	)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	entries := make([]CommitEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, parts[3])
		entries = append(entries, CommitEntry{
			SHA:     parts[0],
			Message: parts[1],
			Author:  parts[2],
			Date:    date,
		})
	}
	return entries, nil
}

func (g *NativeGitProvider) run(ctx context.Context, workDir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\noutput: %s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// Ensure NativeGitProvider implements GitProvider.
var _ GitProvider = (*NativeGitProvider)(nil)
