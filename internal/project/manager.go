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

// UpdateProject merges the provided config into the project's config.toml
// and updates the name in the project index.
func (m *Manager) UpdateProject(id string, cfg *ProjectConfig) error {
	projDir := filepath.Join(m.ProjectsDir(), id)

	// Load existing config to preserve fields not in the DTO.
	existing, err := LoadProjectConfig(ProjectConfigPath(projDir))
	if err != nil {
		return fmt.Errorf("load existing config: %w", err)
	}

	// Apply provided fields (non-zero values override existing).
	if cfg.Project.Name != "" {
		existing.Project.Name = cfg.Project.Name
	}
	if cfg.Project.Description != "" {
		existing.Project.Description = cfg.Project.Description
	}
	if cfg.Git.CloneURL != "" {
		existing.Git.CloneURL = cfg.Git.CloneURL
	}
	if cfg.Git.DefaultBranch != "" {
		existing.Git.DefaultBranch = cfg.Git.DefaultBranch
	}
	if cfg.Git.Provider != "" {
		existing.Git.Provider = cfg.Git.Provider
	}
	if cfg.Git.GitHub.Token != "" {
		existing.Git.GitHub.Token = cfg.Git.GitHub.Token
	}
	if cfg.Tracker.Provider != "" {
		existing.Tracker.Provider = cfg.Tracker.Provider
	}
	if cfg.Tracker.PickupLabel != "" {
		existing.Tracker.PickupLabel = cfg.Tracker.PickupLabel
	}
	if cfg.Tracker.GitHub.Token != "" {
		existing.Tracker.GitHub.Token = cfg.Tracker.GitHub.Token
		existing.Tracker.GitHub.Owner = cfg.Tracker.GitHub.Owner
		existing.Tracker.GitHub.Repo = cfg.Tracker.GitHub.Repo
		existing.Tracker.GitHub.BaseURL = cfg.Tracker.GitHub.BaseURL
	}
	if cfg.Tracker.Jira.APIToken != "" {
		existing.Tracker.Jira.APIToken = cfg.Tracker.Jira.APIToken
		existing.Tracker.Jira.ProjectKey = cfg.Tracker.Jira.ProjectKey
		existing.Tracker.Jira.BaseURL = cfg.Tracker.Jira.BaseURL
	}
	if cfg.Tracker.Linear.APIKey != "" {
		existing.Tracker.Linear.APIKey = cfg.Tracker.Linear.APIKey
		existing.Tracker.Linear.TeamID = cfg.Tracker.Linear.TeamID
		existing.Tracker.Linear.BaseURL = cfg.Tracker.Linear.BaseURL
	}
	if cfg.AgentRunner.Provider != "" {
		existing.AgentRunner.Provider = cfg.AgentRunner.Provider
	}
	if cfg.Models.Planner != "" {
		existing.Models.Planner = cfg.Models.Planner
	}
	if cfg.Models.Implementer != "" {
		existing.Models.Implementer = cfg.Models.Implementer
	}
	if cfg.Limits.MaxParallelTickets > 0 {
		existing.Limits.MaxParallelTickets = cfg.Limits.MaxParallelTickets
	}
	if cfg.Limits.MaxTasksPerTicket > 0 {
		existing.Limits.MaxTasksPerTicket = cfg.Limits.MaxTasksPerTicket
	}
	if cfg.Cost.MaxCostPerTicketUSD > 0 {
		existing.Cost.MaxCostPerTicketUSD = cfg.Cost.MaxCostPerTicketUSD
	}

	if err := WriteProjectConfig(projDir, existing); err != nil {
		return fmt.Errorf("write project config: %w", err)
	}

	// Keep the index name in sync.
	if existing.Project.Name != "" {
		_ = m.index.UpdateName(id, existing.Project.Name)
	}

	return nil
}

// RegisterWorker adds a worker to the registry.
func (m *Manager) RegisterWorker(id string, w *Worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[id] = w
}
