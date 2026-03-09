package git

import (
	"context"
	"time"
)

// GitProvider abstracts git operations.
type GitProvider interface {
	EnsureRepo(ctx context.Context, workDir string) error
	CreateBranch(ctx context.Context, workDir, branchName string) error
	Commit(ctx context.Context, workDir, message string) (sha string, err error)
	Diff(ctx context.Context, workDir, base, head string) (string, error)
	DiffWorking(ctx context.Context, workDir string) (string, error)
	Push(ctx context.Context, workDir, branchName string) error
	RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error)
	FileTree(ctx context.Context, workDir string) ([]FileEntry, error)
	Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error)
	StageAll(ctx context.Context, workDir string) error
	CleanWorkingTree(ctx context.Context, workDir string) error
	Checkout(ctx context.Context, workDir, branch string) error
	Pull(ctx context.Context, workDir string) error
	// Worktree operations for parallel task isolation.
	AddWorktree(ctx context.Context, repoDir, worktreeDir, newBranch, startPoint string) error
	RemoveWorktree(ctx context.Context, repoDir, worktreeDir string) error
	MergeNoFF(ctx context.Context, workDir, branch string) error
	DeleteBranch(ctx context.Context, workDir, branch string) error
}

// RebaseResult holds rebase outcome.
type RebaseResult struct {
	ConflictDiff  string
	ConflictFiles []string
	Success       bool
}

// FileEntry represents a file in the repo tree.
type FileEntry struct {
	Path      string
	IsDir     bool
	SizeBytes int64
}

// CommitEntry represents a git commit.
type CommitEntry struct {
	Date    time.Time
	SHA     string
	Message string
	Author  string
}
