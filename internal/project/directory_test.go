package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateProjectDir(t *testing.T) {
	baseDir := t.TempDir()
	projectID := "test-project-id"

	dir, err := CreateProjectDir(baseDir, projectID)
	if err != nil {
		t.Fatalf("CreateProjectDir: %v", err)
	}

	expected := filepath.Join(baseDir, projectID)
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}

	// Check subdirectories exist
	for _, sub := range []string{"work", "ssh"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("subdir %q not created: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("%q is not a directory", sub)
		}
	}
}

func TestDeleteProjectDir(t *testing.T) {
	baseDir := t.TempDir()
	projectID := "to-delete"
	dir, _ := CreateProjectDir(baseDir, projectID)

	// Write a file to verify recursive deletion
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("test"), 0644)

	if err := DeleteProjectDir(dir); err != nil {
		t.Fatalf("DeleteProjectDir: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("directory still exists after deletion")
	}
}
