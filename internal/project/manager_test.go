package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestManager_DiscoverProjects(t *testing.T) {
	baseDir := t.TempDir()
	projectsDir := filepath.Join(baseDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a project directory with config
	projDir := filepath.Join(projectsDir, "proj-1")
	os.MkdirAll(filepath.Join(projDir, "work"), 0755)
	os.MkdirAll(filepath.Join(projDir, "ssh"), 0755)

	configContent := `
[project]
name = "DiscoveredProject"

[tracker]
provider = "github"
[tracker.github]
owner = "test"
repo = "test"

[git]
provider = "github"
clone_url = "git@github.com:test/test.git"
default_branch = "main"
[git.github]
token = "test"

[agent_runner]
provider = "builtin"
`
	os.WriteFile(filepath.Join(projDir, "config.toml"), []byte(configContent), 0644)

	// Create index
	idx := NewIndex(filepath.Join(baseDir, "projects.json"))
	idx.Add(IndexEntry{ID: "proj-1", Name: "DiscoveredProject", Active: true})

	mgr := NewManager(baseDir, &models.Config{})
	projects, err := mgr.DiscoverProjects()
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("discovered %d projects, want 1", len(projects))
	}
	if projects[0].Name != "DiscoveredProject" {
		t.Errorf("name = %q, want DiscoveredProject", projects[0].Name)
	}
}

func TestManager_CreateProject(t *testing.T) {
	baseDir := t.TempDir()
	os.MkdirAll(filepath.Join(baseDir, "projects"), 0755)

	mgr := NewManager(baseDir, &models.Config{})

	cfg := &ProjectConfig{}
	cfg.Project.Name = "NewProject"
	cfg.Tracker.Provider = "github"
	cfg.Git.Provider = "github"
	cfg.Git.CloneURL = "git@github.com:test/new.git"
	cfg.Git.DefaultBranch = "main"
	cfg.AgentRunner.Provider = "builtin"

	id, err := mgr.CreateProject(cfg)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if id == "" {
		t.Error("returned empty ID")
	}

	// Verify directory was created
	projDir := filepath.Join(baseDir, "projects", id)
	if _, err := os.Stat(projDir); err != nil {
		t.Errorf("project dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projDir, "config.toml")); err != nil {
		t.Errorf("config.toml not created: %v", err)
	}

	// Verify index was updated
	entries, _ := mgr.index.List()
	if len(entries) != 1 {
		t.Fatalf("index has %d entries, want 1", len(entries))
	}
}

func TestManager_DeleteProject(t *testing.T) {
	baseDir := t.TempDir()
	os.MkdirAll(filepath.Join(baseDir, "projects"), 0755)

	mgr := NewManager(baseDir, &models.Config{})

	cfg := &ProjectConfig{}
	cfg.Project.Name = "ToDelete"
	cfg.AgentRunner.Provider = "builtin"

	id, _ := mgr.CreateProject(cfg)

	if err := mgr.DeleteProject(id); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	projDir := filepath.Join(baseDir, "projects", id)
	if _, err := os.Stat(projDir); !os.IsNotExist(err) {
		t.Error("project dir still exists after deletion")
	}

	entries, _ := mgr.index.List()
	if len(entries) != 0 {
		t.Errorf("index has %d entries after deletion, want 0", len(entries))
	}
}
