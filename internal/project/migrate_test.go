package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFromSingleProject(t *testing.T) {
	// Simulate existing single-project setup
	baseDir := t.TempDir()

	// Create old-style foreman.db in base dir
	oldDB := filepath.Join(baseDir, "foreman.db")
	if err := os.WriteFile(oldDB, []byte("sqlite-data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create old-style foreman.toml
	oldConfig := filepath.Join(baseDir, "foreman.toml")
	configContent := `
[project]
name = "DefaultProject"

[tracker]
provider = "github"
[tracker.github]
token = "test"
owner = "myorg"
repo = "myrepo"

[git]
provider = "github"
clone_url = "git@github.com:myorg/myrepo.git"
default_branch = "main"
[git.github]
token = "test"
`
	if err := os.WriteFile(oldConfig, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	projectID, err := MigrateFromSingleProject(baseDir, oldConfig)
	if err != nil {
		t.Fatalf("MigrateFromSingleProject: %v", err)
	}

	// Verify project directory was created
	projDir := filepath.Join(baseDir, "projects", projectID)
	if _, err := os.Stat(filepath.Join(projDir, "config.toml")); err != nil {
		t.Error("project config.toml not created")
	}

	// Verify database was moved
	if _, err := os.Stat(filepath.Join(projDir, "foreman.db")); err != nil {
		t.Error("foreman.db not moved to project dir")
	}

	// Verify old DB is gone
	if _, err := os.Stat(oldDB); !os.IsNotExist(err) {
		t.Error("old foreman.db still exists")
	}

	// Verify index was created
	idx := NewIndex(filepath.Join(baseDir, "projects.json"))
	entries, _ := idx.List()
	if len(entries) != 1 {
		t.Fatalf("index has %d entries, want 1", len(entries))
	}
}

func TestNeedsMigration(t *testing.T) {
	baseDir := t.TempDir()

	// No files: no migration needed
	if NeedsMigration(baseDir) {
		t.Error("expected no migration needed in empty dir")
	}

	// foreman.db exists: migration needed
	os.WriteFile(filepath.Join(baseDir, "foreman.db"), []byte("data"), 0644)
	if !NeedsMigration(baseDir) {
		t.Error("expected migration needed when foreman.db exists")
	}

	// projects.json exists: no migration needed even if foreman.db present
	os.WriteFile(filepath.Join(baseDir, "projects.json"), []byte("{}"), 0644)
	if NeedsMigration(baseDir) {
		t.Error("expected no migration needed when projects.json exists")
	}
}
