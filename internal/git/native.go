package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// NativeGitProvider shells out to the native git CLI.
type NativeGitProvider struct {
	cloneURL   string
	sshKeyPath string // injected as GIT_SSH_COMMAND for SSH clone URLs
	httpToken  string // injected via inline credential helper for HTTPS clone URLs
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

// WithSSHKey returns a copy of the provider configured to use the given private
// key for all git operations via GIT_SSH_COMMAND. The key path must be absolute.
func (g *NativeGitProvider) WithSSHKey(privKeyPath string) *NativeGitProvider {
	return &NativeGitProvider{cloneURL: g.cloneURL, sshKeyPath: privKeyPath, httpToken: g.httpToken}
}

// WithHTTPToken returns a copy of the provider configured to authenticate HTTPS
// git operations using a Personal Access Token. The token is passed via an
// inline credential helper so it never appears in process args or git config.
func (g *NativeGitProvider) WithHTTPToken(token string) *NativeGitProvider {
	return &NativeGitProvider{cloneURL: g.cloneURL, sshKeyPath: g.sshKeyPath, httpToken: token}
}

func (g *NativeGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	// A usable repo needs at least one commit (HEAD must resolve).
	// git status succeeds even on an empty/broken init, so we use rev-parse.
	if _, err := g.run(ctx, workDir, "git", "rev-parse", "HEAD"); err == nil {
		log.Info().Str("work_dir", workDir).Msg("git repo already exists and is ready")
		return nil
	}
	// No valid HEAD — clone if a URL is configured.
	if g.cloneURL == "" {
		return fmt.Errorf("work directory %q has no commits and no clone_url is configured", workDir)
	}
	// If a broken .git dir exists (e.g. empty init), remove it so clone can proceed.
	gitDir := filepath.Join(workDir, ".git")
	if _, statErr := os.Stat(gitDir); statErr == nil {
		log.Warn().Str("work_dir", workDir).Msg("removing invalid .git dir before cloning")
		if err := os.RemoveAll(gitDir); err != nil {
			return fmt.Errorf("remove broken .git dir: %w", err)
		}
	}
	log.Info().Str("work_dir", workDir).Str("clone_url", g.cloneURL).Msg("cloning repository into work dir")
	if _, err := g.run(ctx, workDir, "git", "clone", g.cloneURL, "."); err != nil {
		return fmt.Errorf("clone %s: %w", g.cloneURL, err)
	}
	log.Info().Str("work_dir", workDir).Msg("repository cloned successfully")
	return nil
}

func (g *NativeGitProvider) CreateBranch(ctx context.Context, workDir, branchName string) error {
	// Try to create the branch.
	if _, err := g.run(ctx, workDir, "git", "checkout", "-b", branchName); err == nil {
		return nil
	}
	// Branch may already exist locally — only fall back to checkout if it does.
	if _, err := g.run(ctx, workDir, "git", "rev-parse", "--verify", "refs/heads/"+branchName); err == nil {
		_, err = g.run(ctx, workDir, "git", "checkout", branchName)
		return err
	}
	// Branch doesn't exist; retry creation to surface the real error.
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

func (g *NativeGitProvider) CleanWorkingTree(ctx context.Context, workDir string) error {
	if _, err := g.run(ctx, workDir, "git", "checkout", "--", "."); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	if _, err := g.run(ctx, workDir, "git", "clean", "-fd"); err != nil {
		return fmt.Errorf("git clean: %w", err)
	}
	return nil
}

// Checkout switches the working tree to an existing branch.
func (g *NativeGitProvider) Checkout(ctx context.Context, workDir, branch string) error {
	_, err := g.run(ctx, workDir, "git", "checkout", branch)
	return err
}

// Pull fast-forwards the current branch from its upstream.
func (g *NativeGitProvider) Pull(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, "git", "pull")
	return err
}

// AddWorktree creates a new git worktree with a new branch rooted at startPoint.
func (g *NativeGitProvider) AddWorktree(ctx context.Context, repoDir, worktreeDir, newBranch, startPoint string) error {
	_, err := g.run(ctx, repoDir, "git", "worktree", "add", "-b", newBranch, worktreeDir, startPoint)
	return err
}

// RemoveWorktree removes a git worktree directory and prunes the worktree metadata.
func (g *NativeGitProvider) RemoveWorktree(ctx context.Context, repoDir, worktreeDir string) error {
	_, err := g.run(ctx, repoDir, "git", "worktree", "remove", "--force", worktreeDir)
	return err
}

// MergeNoFF merges the given branch into the current branch without fast-forward.
func (g *NativeGitProvider) MergeNoFF(ctx context.Context, workDir, branch string) error {
	_, err := g.run(ctx, workDir, "git", "merge", "--no-ff", "--no-edit", branch)
	return err
}

// DeleteBranch force-deletes the given local branch.
func (g *NativeGitProvider) DeleteBranch(ctx context.Context, workDir, branch string) error {
	_, err := g.run(ctx, workDir, "git", "branch", "-D", branch)
	return err
}

func (g *NativeGitProvider) run(ctx context.Context, workDir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	env := os.Environ()
	if g.sshKeyPath != "" {
		sshCmd := fmt.Sprintf(
			"ssh -i %s -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o IdentitiesOnly=yes",
			g.sshKeyPath,
		)
		env = append(env, "GIT_SSH_COMMAND="+sshCmd)
	}
	if g.httpToken != "" {
		// Inject token via an inline credential helper — the token never
		// appears in process args, git config, or the clone URL.
		// This is the same pattern used by GitHub Actions and most CI systems.
		helper := fmt.Sprintf("!f() { echo username=x-access-token; echo password=%s; }; f", g.httpToken)
		cmd.Args = append([]string{cmd.Args[0], "-c", "credential.helper=" + helper}, cmd.Args[1:]...)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\noutput: %s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// Ensure NativeGitProvider implements GitProvider.
var _ GitProvider = (*NativeGitProvider)(nil)
