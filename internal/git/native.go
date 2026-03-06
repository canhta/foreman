package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NativeGitProvider shells out to the native git CLI.
type NativeGitProvider struct {
	cloneURL string
}

// NewNativeGitProvider creates a native git provider.
func NewNativeGitProvider() *NativeGitProvider {
	return &NativeGitProvider{}
}

// NewNativeGitProviderWithClone creates a native git provider that can clone the repo
// into the work directory if it does not yet exist as a git repository.
func NewNativeGitProviderWithClone(cloneURL string) *NativeGitProvider {
	return &NativeGitProvider{cloneURL: cloneURL}
}

func (g *NativeGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	// If already a valid git repo, nothing to do.
	if _, err := g.run(ctx, workDir, "git", "status"); err == nil {
		return nil
	}
	// Not a git repo — clone if a URL is configured.
	if g.cloneURL != "" {
		_, err := g.run(ctx, workDir, "git", "clone", g.cloneURL, ".")
		return err
	}
	return fmt.Errorf("work directory %q is not a git repository and no clone_url is configured", workDir)
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
	// Use two-dot range: changes between base and head (not symmetric difference).
	out, err := g.run(ctx, workDir, "git", "diff", base+".."+head)
	if err != nil {
		return "", err
	}
	return out, nil
}

// DiffWorking returns the diff of the working tree against HEAD.
// Precondition: the repository must have at least one commit (HEAD must be valid).
func (g *NativeGitProvider) DiffWorking(ctx context.Context, workDir string) (string, error) {
	// Use "git diff HEAD" to include both staged and unstaged changes vs last commit.
	out, err := g.run(ctx, workDir, "git", "diff", "HEAD")
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
		// Check for conflicts using git status --porcelain (reliable across git versions).
		// Lines with 'U' in either column, or 'AA'/'DD', indicate unmerged paths.
		statusOut, _ := g.run(ctx, workDir, "git", "status", "--porcelain")
		var conflicts []string
		for _, line := range strings.Split(strings.TrimSpace(statusOut), "\n") {
			if len(line) >= 2 && (line[0] == 'U' || line[1] == 'U' ||
				(line[0] == 'A' && line[1] == 'A') ||
				(line[0] == 'D' && line[1] == 'D')) {
				conflicts = append(conflicts, strings.TrimSpace(line[3:]))
			}
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
		entry := FileEntry{Path: f}
		if info, statErr := os.Stat(filepath.Join(workDir, f)); statErr == nil {
			entry.IsDir = info.IsDir()
			entry.SizeBytes = info.Size()
		}
		entries = append(entries, entry)
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
		date, err := time.Parse(time.RFC3339, parts[3])
		if err != nil {
			return nil, fmt.Errorf("log: parse date %q: %w", parts[3], err)
		}
		entries = append(entries, CommitEntry{
			SHA:     parts[0],
			Message: parts[1],
			Author:  parts[2],
			Date:    date,
		})
	}
	return entries, nil
}

func (g *NativeGitProvider) StageAll(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, "git", "add", "-A")
	return err
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
