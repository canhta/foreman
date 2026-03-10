package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
)

// staticConfigProvider wraps a *models.Config to satisfy dashboard.ConfigProvider.
type staticConfigProvider struct{ cfg *models.Config }

func (s *staticConfigProvider) GetConfig() *models.Config { return s.cfg }

func pidFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".foreman", "foreman.pid")
}

// loadConfigAndDB loads the global configuration and opens the daemon database.
// Used by CLI commands that need read access to foreman state.
func loadConfigAndDB() (*models.Config, db.Database, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("home dir: %w", err)
	}
	globalCfgPath := filepath.Join(home, ".foreman", "config.toml")

	cfg, err := config.LoadFromFile(globalCfgPath)
	if err != nil {
		cfg, err = config.LoadDefaults()
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w — run 'foreman init' to create ~/.foreman/config.toml", err)
		}
	}

	database, err := openDB(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w — has 'foreman start' been run at least once?", err)
	}

	return cfg, database, nil
}

func openDB(cfg *models.Config) (db.Database, error) {
	return db.NewSQLiteDB(cfg.Database.SQLite.Path)
}

// sshHostFromURL returns the SSH hostname from a git clone URL.
// e.g. "git@github.com:org/repo.git" → "git@github.com"
//
//	"https://github.com/org/repo.git" → "github.com"
func sshHostFromURL(cloneURL string) string {
	// SCP-style SSH: git@host:path
	if strings.HasPrefix(cloneURL, "git@") {
		if idx := strings.Index(cloneURL, ":"); idx > 0 {
			return cloneURL[:idx] // e.g. git@github.com
		}
	}
	// HTTPS: extract host only (ssh won't use HTTPS but probe the host)
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(cloneURL, prefix) {
			rest := cloneURL[len(prefix):]
			if idx := strings.Index(rest, "/"); idx > 0 {
				return rest[:idx]
			}
			return rest
		}
	}
	return ""
}

// parseOwnerRepo extracts owner and repo from a GitHub clone URL.
// Supports https://github.com/owner/repo.git and git@github.com:owner/repo.git
func parseOwnerRepo(cloneURL string) (owner, repo string) {
	// Try HTTPS format: https://github.com/owner/repo.git
	if idx := strings.Index(cloneURL, "github.com/"); idx >= 0 {
		path := cloneURL[idx+len("github.com/"):]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	// Try SSH format: git@github.com:owner/repo.git
	if idx := strings.Index(cloneURL, "github.com:"); idx >= 0 {
		path := cloneURL[idx+len("github.com:"):]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}
