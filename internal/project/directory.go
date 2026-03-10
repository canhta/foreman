package project

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// CreateProjectDir creates the project directory structure.
// Returns the project root directory path.
func CreateProjectDir(baseDir, projectID string) (string, error) {
	projectDir := filepath.Join(baseDir, projectID)

	subdirs := []string{"work", "ssh"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0755); err != nil {
			return "", fmt.Errorf("create %s: %w", sub, err)
		}
	}

	return projectDir, nil
}

// DeleteProjectDir removes a project directory and all contents.
func DeleteProjectDir(projectDir string) error {
	return os.RemoveAll(projectDir)
}

// WriteProjectConfig writes a ProjectConfig to a TOML file in the project directory.
func WriteProjectConfig(projectDir string, cfg *ProjectConfig) error {
	path := filepath.Join(projectDir, "config.toml")
	return writeTomlFile(path, cfg)
}

// ProjectDBPath returns the SQLite database path for a project.
func ProjectDBPath(projectDir string) string {
	return filepath.Join(projectDir, "foreman.db")
}

// ProjectConfigPath returns the config file path for a project.
func ProjectConfigPath(projectDir string) string {
	return filepath.Join(projectDir, "config.toml")
}

// ProjectWorkDir returns the work directory for git worktrees.
func ProjectWorkDir(projectDir string) string {
	return filepath.Join(projectDir, "work")
}

func writeTomlFile(path string, v interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(v)
}
