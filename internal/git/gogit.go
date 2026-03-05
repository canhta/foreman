// internal/git/gogit.go
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Compile-time interface check.
var _ GitProvider = (*GoGitProvider)(nil)

// GoGitProvider implements GitProvider using go-git (pure Go).
// Used as fallback when native git CLI is not available.
type GoGitProvider struct{}

func NewGoGitProvider() *GoGitProvider {
	return &GoGitProvider{}
}

func (g *GoGitProvider) EnsureRepo(ctx context.Context, workDir string) error {
	_, err := gogit.PlainOpen(workDir)
	if err == nil {
		return nil // Already a repo
	}
	_, err = gogit.PlainInit(workDir, false)
	if err != nil {
		return fmt.Errorf("go-git init: %w", err)
	}
	return nil
}

func (g *GoGitProvider) CreateBranch(ctx context.Context, workDir, branchName string) error {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return fmt.Errorf("go-git open: %w", err)
	}
	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("go-git head: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branchName), headRef.Hash())
	return repo.Storer.SetReference(ref)
}

func (g *GoGitProvider) Commit(ctx context.Context, workDir, message string) (string, error) {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return "", fmt.Errorf("go-git open: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("go-git worktree: %w", err)
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Foreman",
			Email: "foreman@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("go-git commit: %w", err)
	}
	return hash.String(), nil
}

func (g *GoGitProvider) Diff(ctx context.Context, workDir, base, head string) (string, error) {
	// go-git diff requires complex patch generation; fall back to error so callers
	// can use native git when available.
	return "", fmt.Errorf("go-git Diff not fully implemented — use native git provider")
}

func (g *GoGitProvider) DiffWorking(ctx context.Context, workDir string) (string, error) {
	return "", fmt.Errorf("go-git DiffWorking not fully implemented — use native git provider")
}

func (g *GoGitProvider) Push(ctx context.Context, workDir, branchName string) error {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return fmt.Errorf("go-git open: %w", err)
	}
	return repo.Push(&gogit.PushOptions{})
}

func (g *GoGitProvider) RebaseOnto(ctx context.Context, workDir, targetBranch string) (*RebaseResult, error) {
	// go-git does not support rebase natively.
	return nil, fmt.Errorf("go-git does not support rebase — use native git provider")
}

func (g *GoGitProvider) CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error) {
	return nil, fmt.Errorf("go-git CreatePR not implemented — requires GitHub/GitLab API client")
}

func (g *GoGitProvider) StageAll(ctx context.Context, workDir string) error {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return fmt.Errorf("go-git open: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("go-git worktree: %w", err)
	}
	return wt.AddGlob(".")
}

func (g *GoGitProvider) FileTree(ctx context.Context, workDir string) ([]FileEntry, error) {
	var entries []FileEntry
	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(workDir, path)
		entries = append(entries, FileEntry{
			Path:      rel,
			IsDir:     info.IsDir(),
			SizeBytes: info.Size(),
		})
		return nil
	})
	return entries, err
}

func (g *GoGitProvider) Log(ctx context.Context, workDir string, count int) ([]CommitEntry, error) {
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		return nil, fmt.Errorf("go-git open: %w", err)
	}
	iter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("go-git log: %w", err)
	}

	var entries []CommitEntry
	for i := 0; i < count; i++ {
		commit, err := iter.Next()
		if err != nil {
			break
		}
		entries = append(entries, CommitEntry{
			SHA:     commit.Hash.String(),
			Message: commit.Message,
			Author:  commit.Author.Name,
			Date:    commit.Author.When,
		})
	}
	return entries, nil
}

func (g *GoGitProvider) CheckFileOverlap(ctx context.Context, workDir, branchA string, filesB []string) ([]string, error) {
	return nil, fmt.Errorf("go-git CheckFileOverlap not implemented — use native git provider")
}
