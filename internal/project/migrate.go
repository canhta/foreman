package project

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NeedsMigration checks if the base directory has a single-project setup
// that should be migrated to the multi-project structure.
func NeedsMigration(baseDir string) bool {
	// If projects.json already exists, no migration needed.
	if _, err := os.Stat(filepath.Join(baseDir, "projects.json")); err == nil {
		return false
	}
	// If foreman.db exists at the base level, migration is needed.
	_, err := os.Stat(filepath.Join(baseDir, "foreman.db"))
	return err == nil
}

// MigrateFromSingleProject converts an existing single-project setup
// into the multi-project directory structure.
// Returns the new project ID.
func MigrateFromSingleProject(baseDir, oldConfigPath string) (string, error) {
	log.Info().Msg("migrating from single-project to multi-project structure")

	projectID := uuid.New().String()
	projectsDir := filepath.Join(baseDir, "projects")

	// Create project directory
	projDir, err := CreateProjectDir(projectsDir, projectID)
	if err != nil {
		return "", fmt.Errorf("create project dir: %w", err)
	}

	// Load old config to extract project-specific fields.
	projCfg, err := LoadProjectConfig(oldConfigPath)
	if err != nil {
		// If config can't be loaded as project config, create a minimal one.
		projCfg = &ProjectConfig{}
		projCfg.Project.Name = "Default Project"
	}
	if projCfg.Project.Name == "" {
		projCfg.Project.Name = "Default Project"
	}

	// Write project config.
	if err := WriteProjectConfig(projDir, projCfg); err != nil {
		return "", fmt.Errorf("write project config: %w", err)
	}

	// Move database.
	oldDB := filepath.Join(baseDir, "foreman.db")
	newDB := ProjectDBPath(projDir)
	if _, err := os.Stat(oldDB); err == nil {
		if err := os.Rename(oldDB, newDB); err != nil {
			return "", fmt.Errorf("move database: %w", err)
		}
		log.Info().Str("from", oldDB).Str("to", newDB).Msg("moved database")
	}

	// Move WAL and SHM files if they exist.
	for _, suffix := range []string{"-wal", "-shm"} {
		src := oldDB + suffix
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, newDB+suffix)
		}
	}

	// Move work directory if it exists.
	oldWork := filepath.Join(baseDir, "work")
	if info, err := os.Stat(oldWork); err == nil && info.IsDir() {
		newWork := ProjectWorkDir(projDir)
		os.RemoveAll(newWork) // remove empty dir created by CreateProjectDir
		if err := os.Rename(oldWork, newWork); err != nil {
			log.Warn().Err(err).Msg("could not move work directory, will re-clone")
		}
	}

	// Create project index.
	idx := NewIndex(filepath.Join(baseDir, "projects.json"))
	if err := idx.Add(IndexEntry{
		ID:        projectID,
		Name:      projCfg.Project.Name,
		CreatedAt: time.Now().UTC(),
		Active:    true,
	}); err != nil {
		return "", fmt.Errorf("create index: %w", err)
	}

	log.Info().
		Str("project_id", projectID).
		Str("name", projCfg.Project.Name).
		Msg("migration complete")

	return projectID, nil
}
