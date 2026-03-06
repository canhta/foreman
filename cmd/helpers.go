package cmd

import (
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
)

// loadConfigAndDB loads the configuration and opens the database.
// Used by CLI commands that need read access to foreman state.
func loadConfigAndDB() (*models.Config, db.Database, error) {
	cfg, err := config.LoadFromFile("foreman.toml")
	if err != nil {
		cfg, err = config.LoadDefaults()
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w — run 'foreman doctor' to validate setup", err)
		}
	}

	database, err := openDB(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w — has 'foreman start' been run at least once?", err)
	}

	return cfg, database, nil
}

func openDB(cfg *models.Config) (db.Database, error) {
	switch cfg.Database.Driver {
	case "postgres":
		return db.NewPostgresDB(cfg.Database.Postgres.URL, cfg.Database.Postgres.MaxConnections)
	default:
		return db.NewSQLiteDB(cfg.Database.SQLite.Path)
	}
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
