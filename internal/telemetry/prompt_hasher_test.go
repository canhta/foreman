package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestHashPromptTemplates_BasicFiles(t *testing.T) {
	dir := t.TempDir()

	// Write two *.md.j2 files and one non-matching file.
	files := map[string]string{
		"implementer.md.j2": "Hello {{ name }}",
		"planner.md.j2":     "Plan: {{ task }}",
		"notes.txt":         "should be ignored",
		"template.j2":       "also ignored (not .md.j2)",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := HashPromptTemplates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect exactly two entries.
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(result), result)
	}

	for _, key := range []string{"implementer.md.j2", "planner.md.j2"} {
		content := files[key]
		sum := sha256.Sum256([]byte(content))
		want := hex.EncodeToString(sum[:])
		got, ok := result[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("key %q: got sha256 %q, want %q", key, got, want)
		}
	}

	// Non-.md.j2 files must not appear.
	for _, key := range []string{"notes.txt", "template.j2"} {
		if _, ok := result[key]; ok {
			t.Errorf("unexpected key %q in result", key)
		}
	}
}

func TestHashPromptTemplates_NestedSubdirectory(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "retry")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "retry prompt {{ attempt }}"
	if err := os.WriteFile(filepath.Join(subdir, "compile.md.j2"), []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := HashPromptTemplates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	key := "retry/compile.md.j2"
	got, ok := result[key]
	if !ok {
		t.Fatalf("expected key %q, got %v", key, result)
	}

	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("sha256 mismatch for %q: got %q, want %q", key, got, want)
	}
}

func TestHashPromptTemplates_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	result, err := HashPromptTemplates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestHashPromptTemplates_NonExistentDir(t *testing.T) {
	result, err := HashPromptTemplates("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("expected no error for missing dir, got: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for missing dir, got %v", result)
	}
}

func TestHashPromptTemplates_DirIsFile(t *testing.T) {
	f, err := os.CreateTemp("", "not-a-dir-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	_ = f.Close()
	defer os.Remove(f.Name())

	_, err = HashPromptTemplates(f.Name())
	if err == nil {
		t.Error("expected error when path is a file, not a directory")
	}
}
