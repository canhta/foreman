package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkContextFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := WalkContextFiles(dir, dir)
	if len(got) != 0 {
		t.Errorf("expected no files, got %v", got)
	}
}

func TestWalkContextFiles_SingleLevel(t *testing.T) {
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("context"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := WalkContextFiles(dir, dir)
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %v", got)
	}
	if got[0] != agentsPath {
		t.Errorf("expected %q, got %q", agentsPath, got[0])
	}
}

func TestWalkContextFiles_MultiLevel(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Place AGENTS.md in sub (more specific) and root.
	subAgents := filepath.Join(sub, "AGENTS.md")
	rootAgents := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(subAgents, []byte("sub context"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootAgents, []byte("root context"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := WalkContextFiles(sub, root)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %v", got)
	}
	// More specific (sub) should come first.
	if got[0] != subAgents {
		t.Errorf("expected first file to be %q, got %q", subAgents, got[0])
	}
	if got[1] != rootAgents {
		t.Errorf("expected second file to be %q, got %q", rootAgents, got[1])
	}
}

func TestWalkContextFiles_Deduplication(t *testing.T) {
	// When startDir == workDir, each file should only appear once.
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("context"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := WalkContextFiles(dir, dir)
	if len(got) != 1 {
		t.Fatalf("expected 1 file (no duplicates), got %v", got)
	}
	// Verify it's the right file.
	if got[0] != agentsPath {
		t.Errorf("expected %q, got %q", agentsPath, got[0])
	}
}
