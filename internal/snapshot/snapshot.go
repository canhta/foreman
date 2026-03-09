package snapshot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Patch describes the files changed since a tracked state.
type Patch struct {
	Hash  string
	Files []string
}

// Snapshot manages file-state tracking using a separate git repo.
// This enables undo/rollback within pipeline stages without polluting
// the user's git history.
type Snapshot struct {
	workDir string
	gitDir  string
}

// New creates a Snapshot tracker. dataDir is where the separate git repo lives.
func New(workDir, dataDir string) *Snapshot {
	gitDir := filepath.Join(dataDir, "snapshot")
	return &Snapshot{workDir: workDir, gitDir: gitDir}
}

// Track snapshots the current working tree state and returns a tree hash.
func (s *Snapshot) Track() (string, error) {
	if err := s.ensureInit(); err != nil {
		return "", err
	}
	if err := s.add(); err != nil {
		return "", err
	}
	out, err := s.git("write-tree")
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Patch returns the list of files changed since the given hash.
func (s *Snapshot) Patch(hash string) (*Patch, error) {
	if err := s.add(); err != nil {
		return nil, err
	}
	out, err := s.git("diff", "--name-only", hash, "--", ".")
	if err != nil {
		return &Patch{Hash: hash}, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, filepath.Join(s.workDir, line))
		}
	}
	return &Patch{Hash: hash, Files: files}, nil
}

// Diff returns the unified diff between current state and the given hash.
func (s *Snapshot) Diff(hash string) (string, error) {
	if err := s.add(); err != nil {
		return "", err
	}
	out, err := s.git("diff", "--no-ext-diff", hash, "--", ".")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Restore reverts the working tree to the given snapshot hash.
func (s *Snapshot) Restore(hash string) error {
	if _, err := s.git("read-tree", hash); err != nil {
		return fmt.Errorf("read-tree: %w", err)
	}
	if _, err := s.git("checkout-index", "-a", "-f"); err != nil {
		return fmt.Errorf("checkout-index: %w", err)
	}
	return nil
}

func (s *Snapshot) ensureInit() error {
	cmd := exec.Command("git", "init", "--bare", s.gitDir)
	cmd.Dir = s.workDir
	return cmd.Run()
}

func (s *Snapshot) add() error {
	_, err := s.git("add", ".")
	return err
}

func (s *Snapshot) git(args ...string) (string, error) {
	fullArgs := append([]string{
		"--git-dir", s.gitDir,
		"--work-tree", s.workDir,
	}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, string(out))
	}
	return string(out), nil
}
