package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Manager discovers, creates, and manages project lifecycle.
type Manager struct {
	baseDir   string         // e.g., ~/.foreman
	globalCfg *models.Config // global config
	index     *Index
	workers   map[string]*Worker
	mu        sync.RWMutex
	log       zerolog.Logger
}

// NewManager creates a ProjectManager.
func NewManager(baseDir string, globalCfg *models.Config) *Manager {
	return &Manager{
		baseDir:   baseDir,
		globalCfg: globalCfg,
		index:     NewIndex(filepath.Join(baseDir, "projects.json")),
		workers:   make(map[string]*Worker),
		log:       log.With().Str("component", "project-manager").Logger(),
	}
}

// ProjectsDir returns the base directory for all projects.
func (m *Manager) ProjectsDir() string {
	return filepath.Join(m.baseDir, "projects")
}

// DiscoverProjects scans the projects directory and loads configs.
func (m *Manager) DiscoverProjects() ([]IndexEntry, error) {
	entries, err := m.index.List()
	if err != nil {
		return nil, fmt.Errorf("read project index: %w", err)
	}

	// Validate each entry has a directory and config
	var valid []IndexEntry
	for _, entry := range entries {
		projDir := filepath.Join(m.ProjectsDir(), entry.ID)
		configPath := ProjectConfigPath(projDir)
		if _, err := os.Stat(configPath); err != nil {
			m.log.Warn().Str("project", entry.ID).Err(err).Msg("project directory missing, skipping")
			continue
		}
		valid = append(valid, entry)
	}

	return valid, nil
}

// CreateProject creates a new project with the given config.
// Returns the project ID.
func (m *Manager) CreateProject(cfg *ProjectConfig) (string, error) {
	id := uuid.New().String()

	projDir, err := CreateProjectDir(m.ProjectsDir(), id)
	if err != nil {
		return "", fmt.Errorf("create project dir: %w", err)
	}

	if err := WriteProjectConfig(projDir, cfg); err != nil {
		// Cleanup on failure
		os.RemoveAll(projDir)
		return "", fmt.Errorf("write project config: %w", err)
	}

	entry := IndexEntry{
		ID:        id,
		Name:      cfg.Project.Name,
		CreatedAt: time.Now().UTC(),
		Active:    true,
	}

	if err := m.index.Add(entry); err != nil {
		os.RemoveAll(projDir)
		return "", fmt.Errorf("update index: %w", err)
	}

	return id, nil
}

// DeleteProject stops the worker and removes the project directory.
func (m *Manager) DeleteProject(id string) error {
	m.mu.Lock()
	if w, ok := m.workers[id]; ok {
		w.Stop()
		delete(m.workers, id)
	}
	m.mu.Unlock()

	projDir := filepath.Join(m.ProjectsDir(), id)
	if err := DeleteProjectDir(projDir); err != nil {
		return fmt.Errorf("delete project dir: %w", err)
	}

	if err := m.index.Remove(id); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	return nil
}

// GetProject returns a project's config and directory info.
func (m *Manager) GetProject(id string) (*ProjectConfig, string, error) {
	projDir := filepath.Join(m.ProjectsDir(), id)
	configPath := ProjectConfigPath(projDir)

	cfg, err := LoadProjectConfig(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("load project config: %w", err)
	}

	return cfg, projDir, nil
}

// GetWorker returns a running worker by project ID.
func (m *Manager) GetWorker(id string) (*Worker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workers[id]
	return w, ok
}

// ListProjects returns all project entries from the index.
func (m *Manager) ListProjects() ([]IndexEntry, error) {
	return m.index.List()
}

// RegisterWorker adds a worker to the registry.
func (m *Manager) RegisterWorker(id string, w *Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[id] = w
}
