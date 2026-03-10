# Phase 1b: Multi-Project Backend — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor Foreman from single-project to multi-project architecture with per-project SQLite databases, config isolation, and independent orchestrators.

**Architecture:** Introduce a `ProjectManager` that discovers projects from `~/.foreman/projects/` directories, each containing its own `config.toml` and `foreman.db`. Each project gets a `ProjectWorker` — a goroutine group owning its own orchestrator, tracker, git provider, and database. The daemon becomes a supervisor of project workers. The API layer gains project-scoped endpoints. A `GlobalCostController` aggregates costs across projects.

**Tech Stack:** Go, SQLite, Viper (config), existing daemon/orchestrator patterns

**Spec:** `docs/superpowers/specs/2026-03-10-multi-project-refactor-design.md`

**Prerequisite:** Phase 1a (PostgreSQL removal) must be completed first.

---

## Chunk 1: Project Config Model & Directory Structure

### Task 1: Define Project Config Struct

**Files:**
- Create: `internal/project/config.go`
- Modify: `internal/models/config.go`

- [ ] **Step 1: Write test for project config loading**

Create `internal/project/config_test.go`:

```go
package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
[project]
name = "TestProject"
description = "A test project"

[tracker]
provider = "github"
pickup_label = "foreman-ready"

[tracker.github]
token = "test-token"
owner = "myorg"
repo = "myrepo"

[git]
provider = "github"
clone_url = "git@github.com:myorg/myrepo.git"
default_branch = "main"

[git.github]
token = "test-token"

[models]
planner = "anthropic:claude-sonnet-4-6"

[cost]
max_cost_per_ticket_usd = 10.0

[limits]
max_parallel_tickets = 2
max_parallel_tasks = 3
max_tasks_per_ticket = 15

[agent_runner]
provider = "builtin"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProjectConfig(configPath)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if cfg.Project.Name != "TestProject" {
		t.Errorf("name = %q, want TestProject", cfg.Project.Name)
	}
	if cfg.Tracker.Provider != "github" {
		t.Errorf("tracker.provider = %q, want github", cfg.Tracker.Provider)
	}
	if cfg.Limits.MaxParallelTickets != 2 {
		t.Errorf("limits.max_parallel_tickets = %d, want 2", cfg.Limits.MaxParallelTickets)
	}
	if cfg.Cost.MaxCostPerTicketUSD != 10.0 {
		t.Errorf("cost.max_cost_per_ticket_usd = %f, want 10.0", cfg.Cost.MaxCostPerTicketUSD)
	}
}

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestLoadProjectConfig -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Create project package with config types and loader**

Create `internal/project/config.go`:

```go
package project

import (
	"fmt"
	"os"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/viper"
)

// ProjectMeta holds project identity fields.
type ProjectMeta struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
}

// ProjectLimits holds per-project execution limits.
// Fields previously under [daemon] that are project-specific.
type ProjectLimits struct {
	MaxParallelTickets           int     `mapstructure:"max_parallel_tickets"`
	MaxParallelTasks             int     `mapstructure:"max_parallel_tasks"`
	TaskTimeoutMinutes           int     `mapstructure:"task_timeout_minutes"`
	MaxTasksPerTicket            int     `mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries     int     `mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles          int     `mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles       int     `mapstructure:"max_quality_review_cycles"`
	MaxLlmCallsPerTask           int     `mapstructure:"max_llm_calls_per_task"`
	MaxTaskDurationSecs          int     `mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs         int     `mapstructure:"max_total_duration_secs"`
	ContextTokenBudget           int     `mapstructure:"context_token_budget"`
	EnablePartialPR              bool    `mapstructure:"enable_partial_pr"`
	EnableClarification          bool    `mapstructure:"enable_clarification"`
	EnableTDDVerification        bool    `mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity      float64 `mapstructure:"search_replace_similarity"`
	SearchReplaceMinContextLines int     `mapstructure:"search_replace_min_context_lines"`
	PlanConfidenceThreshold      float64 `mapstructure:"plan_confidence_threshold"`
	IntermediateReviewInterval   int     `mapstructure:"intermediate_review_interval"`
	ConflictResolutionTokenBudget int    `mapstructure:"conflict_resolution_token_budget"`
}

// ProjectConfig holds all per-project configuration.
type ProjectConfig struct {
	Project     ProjectMeta            `mapstructure:"project"`
	Tracker     models.TrackerConfig   `mapstructure:"tracker"`
	Git         models.GitConfig       `mapstructure:"git"`
	Models      models.ModelsConfig    `mapstructure:"models"`
	Cost        models.CostConfig      `mapstructure:"cost"`
	Limits      ProjectLimits          `mapstructure:"limits"`
	AgentRunner models.AgentRunnerConfig `mapstructure:"agent_runner"`
	Skills      models.SkillsConfig    `mapstructure:"skills"`
	Decompose   models.DecomposeConfig `mapstructure:"decompose"`
	Context     models.ContextConfig   `mapstructure:"context"`
	Runner      models.RunnerConfig    `mapstructure:"runner"`
	EnvFiles    map[string]string      `mapstructure:"env_files"`
	LLM         models.LLMConfig       `mapstructure:"llm"` // optional per-project API key overrides
}

// LoadProjectConfig loads a project config from a TOML file.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")

	// Set defaults matching the current global defaults
	v.SetDefault("limits.max_parallel_tickets", 3)
	v.SetDefault("limits.max_parallel_tasks", 3)
	v.SetDefault("limits.task_timeout_minutes", 15)
	v.SetDefault("limits.max_tasks_per_ticket", 20)
	v.SetDefault("limits.max_implementation_retries", 2)
	v.SetDefault("limits.max_spec_review_cycles", 2)
	v.SetDefault("limits.max_quality_review_cycles", 1)
	v.SetDefault("limits.max_llm_calls_per_task", 8)
	v.SetDefault("limits.max_task_duration_secs", 600)
	v.SetDefault("limits.max_total_duration_secs", 7200)
	v.SetDefault("limits.context_token_budget", 80000)
	v.SetDefault("limits.enable_partial_pr", true)
	v.SetDefault("limits.enable_clarification", true)
	v.SetDefault("limits.enable_tdd_verification", true)
	v.SetDefault("limits.search_replace_similarity", 0.92)
	v.SetDefault("limits.search_replace_min_context_lines", 3)
	v.SetDefault("limits.plan_confidence_threshold", 0.60)
	v.SetDefault("limits.intermediate_review_interval", 3)
	v.SetDefault("limits.conflict_resolution_token_budget", 40000)
	v.SetDefault("agent_runner.provider", "builtin")
	v.SetDefault("runner.mode", "local")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read project config %s: %w", path, err)
	}

	var cfg ProjectConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal project config: %w", err)
	}

	expandProjectEnvVars(&cfg)
	return &cfg, nil
}

func expandProjectEnvVars(cfg *ProjectConfig) {
	cfg.Tracker.Jira.APIToken = expandEnv(cfg.Tracker.Jira.APIToken)
	cfg.Tracker.GitHub.Token = expandEnv(cfg.Tracker.GitHub.Token)
	cfg.Tracker.Linear.APIKey = expandEnv(cfg.Tracker.Linear.APIKey)
	cfg.Git.GitHub.Token = expandEnv(cfg.Git.GitHub.Token)
	cfg.Git.GitLab.Token = expandEnv(cfg.Git.GitLab.Token)
	cfg.LLM.Anthropic.APIKey = expandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.OpenAI.APIKey = expandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.OpenRouter.APIKey = expandEnv(cfg.LLM.OpenRouter.APIKey)
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/... -run TestLoadProjectConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/config.go internal/project/config_test.go
git commit -m "feat: add project config model and loader"
```

---

### Task 2: Project Index (projects.json)

**Files:**
- Create: `internal/project/index.go`
- Create: `internal/project/index_test.go`

- [ ] **Step 1: Write test for project index**

Create `internal/project/index_test.go`:

```go
package project

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProjectIndex_AddAndList(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "projects.json"))

	entry := IndexEntry{
		ID:        "test-uuid",
		Name:      "TestProject",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Active:    true,
	}

	if err := idx.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := idx.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(entries))
	}
	if entries[0].ID != "test-uuid" {
		t.Errorf("ID = %q, want test-uuid", entries[0].ID)
	}
	if entries[0].Name != "TestProject" {
		t.Errorf("Name = %q, want TestProject", entries[0].Name)
	}
}

func TestProjectIndex_Remove(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "projects.json"))

	_ = idx.Add(IndexEntry{ID: "a", Name: "A", Active: true})
	_ = idx.Add(IndexEntry{ID: "b", Name: "B", Active: true})

	if err := idx.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, _ := idx.List()
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries after remove, want 1", len(entries))
	}
	if entries[0].ID != "b" {
		t.Errorf("remaining entry ID = %q, want b", entries[0].ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestProjectIndex -v`
Expected: FAIL — NewIndex not defined

- [ ] **Step 3: Implement project index**

Create `internal/project/index.go`:

```go
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// IndexEntry represents a project in the index.
type IndexEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

type indexFile struct {
	Projects []IndexEntry `json:"projects"`
}

// Index manages the projects.json file.
// All writes are serialized through a mutex.
type Index struct {
	path string
	mu   sync.Mutex
}

// NewIndex creates an Index for the given file path.
func NewIndex(path string) *Index {
	return &Index{path: path}
}

// List returns all project entries.
func (idx *Index) List() ([]IndexEntry, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.load()
}

// Add adds a project entry to the index.
func (idx *Index) Add(entry IndexEntry) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := idx.load()
	if err != nil {
		return err
	}

	entries = append(entries, entry)
	return idx.save(entries)
}

// Remove removes a project entry by ID.
func (idx *Index) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := idx.load()
	if err != nil {
		return err
	}

	filtered := make([]IndexEntry, 0, len(entries))
	for _, e := range entries {
		if e.ID != id {
			filtered = append(filtered, e)
		}
	}
	return idx.save(filtered)
}

func (idx *Index) load() ([]IndexEntry, error) {
	data, err := os.ReadFile(idx.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}

	var f indexFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return f.Projects, nil
}

func (idx *Index) save(entries []IndexEntry) error {
	data, err := json.MarshalIndent(indexFile{Projects: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	return os.WriteFile(idx.path, data, 0644)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestProjectIndex -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/index.go internal/project/index_test.go
git commit -m "feat: add project index (projects.json) management"
```

---

### Task 3: Project Directory Lifecycle

**Files:**
- Create: `internal/project/directory.go`
- Create: `internal/project/directory_test.go`

- [ ] **Step 1: Write test for project directory creation**

Create `internal/project/directory_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestCreateProjectDir -v`
Expected: FAIL

- [ ] **Step 3: Implement directory lifecycle**

Create `internal/project/directory.go`:

```go
package project

import (
	"fmt"
	"os"
	"path/filepath"
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
```

Note: `writeTomlFile` is a helper that serializes a struct to TOML. Use `github.com/pelletier/go-toml/v2` (already available via Viper dependency) or a simple template-based writer. The implementation should write the config sections in a readable format. For the initial version, use `go-toml/v2`:

```go
func writeTomlFile(path string, v interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(v)
}
```

Add import: `toml "github.com/pelletier/go-toml/v2"`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run "TestCreateProjectDir|TestDeleteProjectDir" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/directory.go internal/project/directory_test.go
git commit -m "feat: add project directory lifecycle management"
```

---

### Task 4: Config Merge (Global + Project with Fallback)

**Files:**
- Create: `internal/project/merge.go`
- Create: `internal/project/merge_test.go`

- [ ] **Step 1: Write test for config merging**

Create `internal/project/merge_test.go`:

```go
package project

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestMergeConfigs_ProjectOverridesGlobal(t *testing.T) {
	global := &models.Config{}
	global.LLM.Anthropic.APIKey = "global-key"
	global.LLM.DefaultProvider = "anthropic"
	global.Cost.MaxCostPerDayUSD = 150.0
	global.Cost.MaxCostPerMonthUSD = 3000.0

	proj := &ProjectConfig{}
	proj.Project.Name = "Test"
	proj.Cost.MaxCostPerTicketUSD = 10.0
	proj.Limits.MaxParallelTickets = 2

	merged := MergeConfigs(global, proj, "/tmp/project")

	// Project values should be set
	if merged.Cost.MaxCostPerTicketUSD != 10.0 {
		t.Errorf("ticket cost = %f, want 10.0", merged.Cost.MaxCostPerTicketUSD)
	}

	// Global LLM key should be used (project didn't set one)
	if merged.LLM.Anthropic.APIKey != "global-key" {
		t.Errorf("api key = %q, want global-key", merged.LLM.Anthropic.APIKey)
	}

	// Global daily/monthly limits should carry through
	if merged.Cost.MaxCostPerDayUSD != 150.0 {
		t.Errorf("daily cost = %f, want 150.0", merged.Cost.MaxCostPerDayUSD)
	}
}

func TestMergeConfigs_ProjectOverridesAPIKey(t *testing.T) {
	global := &models.Config{}
	global.LLM.Anthropic.APIKey = "global-key"

	proj := &ProjectConfig{}
	proj.LLM.Anthropic.APIKey = "project-key"

	merged := MergeConfigs(global, proj, "/tmp/project")

	if merged.LLM.Anthropic.APIKey != "project-key" {
		t.Errorf("api key = %q, want project-key", merged.LLM.Anthropic.APIKey)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestMergeConfigs -v`
Expected: FAIL

- [ ] **Step 3: Implement config merge**

Create `internal/project/merge.go`:

```go
package project

import (
	"github.com/canhta/foreman/internal/models"
)

// MergeConfigs produces a models.Config by applying project-level overrides
// on top of global defaults. The projectDir is used to set work directory
// and database path.
func MergeConfigs(global *models.Config, proj *ProjectConfig, projectDir string) *models.Config {
	merged := *global // shallow copy

	// Project identity (informational)
	// Not stored in models.Config — handled separately by ProjectWorker

	// Tracker — always from project
	merged.Tracker = proj.Tracker

	// Git — always from project
	merged.Git = proj.Git
	merged.Git.Worktree.StartCommand = proj.Git.Worktree.StartCommand

	// Models — from project
	merged.Models = proj.Models

	// Agent runner — from project
	merged.AgentRunner = proj.AgentRunner

	// Skills agent runner — from project if set
	if proj.Skills.AgentRunner.Provider != "" {
		merged.Skills = proj.Skills
	}

	// Decompose — from project
	merged.Decompose = proj.Decompose

	// Context — from project
	merged.Context = proj.Context

	// Runner — from project if set, else global
	if proj.Runner.Mode != "" {
		merged.Runner = proj.Runner
	}

	// Cost — merge: per-ticket from project, daily/monthly stay global
	if proj.Cost.MaxCostPerTicketUSD > 0 {
		merged.Cost.MaxCostPerTicketUSD = proj.Cost.MaxCostPerTicketUSD
	}

	// Limits — from project, mapped to daemon config fields
	merged.Daemon.MaxParallelTickets = proj.Limits.MaxParallelTickets
	merged.Daemon.MaxParallelTasks = proj.Limits.MaxParallelTasks
	merged.Daemon.TaskTimeoutMinutes = proj.Limits.TaskTimeoutMinutes
	merged.Limits.MaxTasksPerTicket = proj.Limits.MaxTasksPerTicket
	merged.Limits.MaxImplementationRetries = proj.Limits.MaxImplementationRetries
	merged.Limits.MaxSpecReviewCycles = proj.Limits.MaxSpecReviewCycles
	merged.Limits.MaxQualityReviewCycles = proj.Limits.MaxQualityReviewCycles
	merged.Limits.MaxLlmCallsPerTask = proj.Limits.MaxLlmCallsPerTask
	merged.Limits.MaxTaskDurationSecs = proj.Limits.MaxTaskDurationSecs
	merged.Limits.MaxTotalDurationSecs = proj.Limits.MaxTotalDurationSecs
	merged.Limits.ContextTokenBudget = proj.Limits.ContextTokenBudget
	merged.Limits.EnablePartialPR = proj.Limits.EnablePartialPR
	merged.Limits.EnableClarification = proj.Limits.EnableClarification
	merged.Limits.EnableTDDVerification = proj.Limits.EnableTDDVerification
	merged.Limits.SearchReplaceSimilarity = proj.Limits.SearchReplaceSimilarity
	merged.Limits.SearchReplaceMinContextLines = proj.Limits.SearchReplaceMinContextLines
	merged.Limits.PlanConfidenceThreshold = proj.Limits.PlanConfidenceThreshold
	merged.Limits.IntermediateReviewInterval = proj.Limits.IntermediateReviewInterval
	merged.Limits.ConflictResolutionTokenBudget = proj.Limits.ConflictResolutionTokenBudget

	// LLM API keys — project overrides if set, else global
	if proj.LLM.Anthropic.APIKey != "" {
		merged.LLM.Anthropic.APIKey = proj.LLM.Anthropic.APIKey
	}
	if proj.LLM.OpenAI.APIKey != "" {
		merged.LLM.OpenAI.APIKey = proj.LLM.OpenAI.APIKey
	}
	if proj.LLM.OpenRouter.APIKey != "" {
		merged.LLM.OpenRouter.APIKey = proj.LLM.OpenRouter.APIKey
	}

	// Work directory — always project-specific
	merged.Daemon.WorkDir = ProjectWorkDir(projectDir)

	// Database — always project-specific
	merged.Database.SQLite.Path = ProjectDBPath(projectDir)

	// Env files — from project
	if len(proj.EnvFiles) > 0 {
		merged.Daemon.EnvFiles = proj.EnvFiles
	}

	return &merged
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestMergeConfigs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/merge.go internal/project/merge_test.go
git commit -m "feat: add global+project config merge with fallback"
```

---

## Chunk 2: ProjectManager & ProjectWorker

### Task 5: ProjectWorker — Per-Project Goroutine Group

**Files:**
- Create: `internal/project/worker.go`
- Create: `internal/project/worker_test.go`

- [ ] **Step 1: Write test for worker lifecycle**

Create `internal/project/worker_test.go`:

```go
package project

import (
	"context"
	"testing"
	"time"
)

func TestProjectWorker_StartStop(t *testing.T) {
	w := &Worker{
		ID:     "test-worker",
		Name:   "TestProject",
		status: StatusStopped,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start sets status to running
	go w.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	if w.Status() != StatusRunning {
		t.Errorf("status = %q, want %q", w.Status(), StatusRunning)
	}

	// Cancel stops the worker
	cancel()
	time.Sleep(50 * time.Millisecond)

	if w.Status() != StatusStopped {
		t.Errorf("status after cancel = %q, want %q", w.Status(), StatusStopped)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestProjectWorker -v`
Expected: FAIL

- [ ] **Step 3: Implement ProjectWorker**

Create `internal/project/worker.go`:

```go
package project

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

// WorkerStatus represents the state of a ProjectWorker.
type WorkerStatus string

const (
	StatusStopped WorkerStatus = "stopped"
	StatusRunning WorkerStatus = "running"
	StatusPaused  WorkerStatus = "paused"
	StatusError   WorkerStatus = "error"
)

// Worker runs an independent goroutine group for a single project.
// It owns its own database, orchestrator, tracker, and git provider.
type Worker struct {
	ID        string
	Name      string
	Dir       string           // project directory path
	Config    *models.Config   // merged config (global + project)
	ProjConfig *ProjectConfig  // raw project config
	Database  db.Database

	cancel context.CancelFunc
	status WorkerStatus
	paused atomic.Bool
	mu     sync.RWMutex
	log    zerolog.Logger
	err    error

	// Dependencies set after construction (mirrors daemon pattern)
	// These will be set by ProjectManager during setup
	setupFn func(ctx context.Context, w *Worker) error
}

// Status returns the current worker status.
func (w *Worker) Status() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) setStatus(s WorkerStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = s
}

// Error returns the last error if status is StatusError.
func (w *Worker) Error() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.err
}

// Start begins the worker's event loop. Blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	w.setStatus(StatusRunning)
	defer w.setStatus(StatusStopped)

	w.log.Info().Str("project", w.Name).Msg("project worker started")

	<-ctx.Done()

	w.log.Info().Str("project", w.Name).Msg("project worker stopped")
}

// Pause pauses the worker's polling loop.
func (w *Worker) Pause() {
	w.paused.Store(true)
	w.setStatus(StatusPaused)
}

// Resume resumes the worker's polling loop.
func (w *Worker) Resume() {
	w.paused.Store(false)
	w.setStatus(StatusRunning)
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}
```

Note: The `Start` method is a skeleton. The full daemon loop (poll tracker, orchestrate tickets, check merges) will be wired in Task 7 when we refactor the daemon. For now this establishes the lifecycle contract.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestProjectWorker -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/worker.go internal/project/worker_test.go
git commit -m "feat: add ProjectWorker with lifecycle management"
```

---

### Task 6: ProjectManager — Discovery and Registry

**Files:**
- Create: `internal/project/manager.go`
- Create: `internal/project/manager_test.go`

- [ ] **Step 1: Write test for project discovery**

Create `internal/project/manager_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/... -run TestManager -v`
Expected: FAIL

- [ ] **Step 3: Implement ProjectManager**

Create `internal/project/manager.go`:

```go
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
	baseDir    string         // e.g., ~/.foreman
	globalCfg  *models.Config // global config
	index      *Index
	workers    map[string]*Worker
	mu         sync.RWMutex
	log        zerolog.Logger
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestManager -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat: add ProjectManager for project discovery and lifecycle"
```

---

## Chunk 3: Daemon Refactoring

### Task 7: Refactor cmd/start.go to Use ProjectManager

**Files:**
- Modify: `cmd/start.go`
- Modify: `internal/daemon/daemon.go`

This is the central integration task. The current `cmd/start.go` creates a single orchestrator. It needs to:

1. Create a `ProjectManager`
2. Discover all projects
3. For each project, create a `ProjectWorker` that sets up its own database, tracker, git provider, orchestrator, and daemon loop
4. Start all workers
5. Keep the global dashboard running with aggregated data

- [ ] **Step 1: Add ProjectManager field to the start command**

In `cmd/start.go`, after loading global config (around line 177), add project manager initialization:

```go
// After: cfg, err := config.LoadFromFile("foreman.toml")
// Add:
homeDir, _ := os.UserHomeDir()
foremanDir := filepath.Join(homeDir, ".foreman")
projManager := project.NewManager(foremanDir, cfg)
```

- [ ] **Step 2: Discover projects and create workers**

Replace the single-orchestrator creation block (lines ~230-363) with a project loop. The existing setup logic (create tracker, git provider, orchestrator) becomes a `setupProjectWorker` function that runs per project:

```go
func setupProjectWorker(ctx context.Context, cfg *models.Config, projDir string, projCfg *project.ProjectConfig) (*daemon.Daemon, db.Database, error) {
	mergedCfg := project.MergeConfigs(cfg, projCfg, projDir)

	// Open per-project database
	database, err := db.NewSQLiteDB(project.ProjectDBPath(projDir))
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}

	// Create tracker, git provider, cost controller, orchestrator...
	// (Same logic as current single-project setup, but using mergedCfg)
	// This is a refactor of the existing code, not new logic

	return d, database, nil
}
```

The exact implementation extracts the existing setup code (lines 223-363 in current start.go) into this function, parameterized by the merged config.

- [ ] **Step 3: Start all project workers**

```go
entries, _ := projManager.DiscoverProjects()
for _, entry := range entries {
	projCfg, projDir, err := projManager.GetProject(entry.ID)
	if err != nil {
		log.Error().Err(err).Str("project", entry.Name).Msg("skip project")
		continue
	}

	d, database, err := setupProjectWorker(ctx, cfg, projDir, projCfg)
	if err != nil {
		log.Error().Err(err).Str("project", entry.Name).Msg("skip project")
		continue
	}

	worker := &project.Worker{
		ID:       entry.ID,
		Name:     entry.Name,
		Dir:      projDir,
		Config:   project.MergeConfigs(cfg, projCfg, projDir),
		Database: database,
	}
	projManager.RegisterWorker(entry.ID, worker)

	go func(w *project.Worker, daemon *daemon.Daemon) {
		if err := daemon.Start(ctx); err != nil {
			w.SetError(err)
		}
	}(worker, d)
}
```

- [ ] **Step 4: Update dashboard to accept ProjectManager**

The dashboard server needs access to all project databases for the global overview. Add a `ProjectRegistry` interface:

```go
type ProjectRegistry interface {
	ListProjects() ([]project.IndexEntry, error)
	GetWorker(id string) (*project.Worker, bool)
}
```

Pass this to the dashboard server so it can query per-project databases.

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: SUCCESS (with possible warnings about unused code from the old single-project path)

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -short -count=1`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add cmd/start.go internal/daemon/daemon.go
git commit -m "refactor: use ProjectManager to start per-project daemon workers"
```

---

### Task 8: Global Cost Controller

**Files:**
- Create: `internal/project/cost.go`
- Create: `internal/project/cost_test.go`

- [ ] **Step 1: Write test for global cost aggregation**

Create `internal/project/cost_test.go`:

```go
package project

import (
	"testing"
)

func TestGlobalCostController_ReportAndCheck(t *testing.T) {
	ctrl := NewGlobalCostController(100.0, 2000.0)

	// Report costs from two projects
	ctrl.ReportCost("proj-1", 5.0)
	ctrl.ReportCost("proj-2", 3.0)

	total := ctrl.TotalToday()
	if total != 8.0 {
		t.Errorf("total = %f, want 8.0", total)
	}

	// Under budget
	if err := ctrl.CheckDailyBudget(); err != nil {
		t.Errorf("unexpected budget exceeded: %v", err)
	}

	// Push over budget
	ctrl.ReportCost("proj-1", 95.0)
	if err := ctrl.CheckDailyBudget(); err == nil {
		t.Error("expected budget exceeded error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestGlobalCostController -v`
Expected: FAIL

- [ ] **Step 3: Implement global cost controller**

Create `internal/project/cost.go`:

```go
package project

import (
	"fmt"
	"sync"
)

// GlobalCostController aggregates costs across all projects.
// Workers push costs to this controller via ReportCost.
type GlobalCostController struct {
	maxDailyUSD   float64
	maxMonthlyUSD float64

	dailyCosts   map[string]float64 // project ID → cost today
	monthlyCosts map[string]float64 // project ID → cost this month
	mu           sync.RWMutex
}

func NewGlobalCostController(maxDailyUSD, maxMonthlyUSD float64) *GlobalCostController {
	return &GlobalCostController{
		maxDailyUSD:   maxDailyUSD,
		maxMonthlyUSD: maxMonthlyUSD,
		dailyCosts:    make(map[string]float64),
		monthlyCosts:  make(map[string]float64),
	}
}

// ReportCost adds a cost amount for a project (called by ProjectWorker after each LLM call).
func (g *GlobalCostController) ReportCost(projectID string, amount float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts[projectID] += amount
	g.monthlyCosts[projectID] += amount
}

// TotalToday returns total cost across all projects today.
func (g *GlobalCostController) TotalToday() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var total float64
	for _, c := range g.dailyCosts {
		total += c
	}
	return total
}

// TotalThisMonth returns total cost across all projects this month.
func (g *GlobalCostController) TotalThisMonth() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var total float64
	for _, c := range g.monthlyCosts {
		total += c
	}
	return total
}

// CheckDailyBudget returns an error if the daily budget is exceeded.
func (g *GlobalCostController) CheckDailyBudget() error {
	total := g.TotalToday()
	if total > g.maxDailyUSD {
		return fmt.Errorf("daily cost budget exceeded: $%.2f / $%.2f", total, g.maxDailyUSD)
	}
	return nil
}

// CheckMonthlyBudget returns an error if the monthly budget is exceeded.
func (g *GlobalCostController) CheckMonthlyBudget() error {
	total := g.TotalThisMonth()
	if total > g.maxMonthlyUSD {
		return fmt.Errorf("monthly cost budget exceeded: $%.2f / $%.2f", total, g.maxMonthlyUSD)
	}
	return nil
}

// ResetDaily clears daily costs (called at midnight by scheduler).
func (g *GlobalCostController) ResetDaily() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts = make(map[string]float64)
}

// SeedFromDB initializes costs from project databases on startup.
func (g *GlobalCostController) SeedFromDB(projectID string, dailyCost, monthlyCost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts[projectID] = dailyCost
	g.monthlyCosts[projectID] = monthlyCost
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestGlobalCostController -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/cost.go internal/project/cost_test.go
git commit -m "feat: add GlobalCostController for cross-project cost aggregation"
```

---

## Chunk 4: API Layer Refactoring

### Task 9: Add Project-Scoped API Routes

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`

- [ ] **Step 1: Add ProjectRegistry interface to dashboard**

In `internal/dashboard/server.go`, add a new interface and setter:

```go
// ProjectRegistry provides access to project workers.
type ProjectRegistry interface {
	ListProjects() ([]project.IndexEntry, error)
	GetWorker(id string) (*project.Worker, bool)
	GetProject(id string) (*project.ProjectConfig, string, error)
	CreateProject(cfg *project.ProjectConfig) (string, error)
	DeleteProject(id string) error
}
```

Add field `projects ProjectRegistry` to the `Server` struct and a setter `SetProjectRegistry(r)`.

- [ ] **Step 2: Add global project endpoints**

In `internal/dashboard/api.go`, add handlers:

```go
// GET /api/projects — list all projects
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	entries, err := s.projects.ListProjects()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(entries)
}

// POST /api/projects — create a new project
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var cfg project.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, err := s.projects.CreateProject(&cfg)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// DELETE /api/projects/:pid — delete a project
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if err := s.projects.DeleteProject(pid); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
```

- [ ] **Step 3: Add project-scoped ticket endpoints**

Wrap existing ticket handlers to resolve project context first:

```go
// projectDB resolves the database for a project from the URL.
func (s *Server) projectDB(r *http.Request) (DashboardDB, error) {
	pid := r.PathValue("pid")
	worker, ok := s.projects.GetWorker(pid)
	if !ok {
		return nil, fmt.Errorf("project %q not found", pid)
	}
	return worker.Database.(DashboardDB), nil
}

// GET /api/projects/:pid/tickets
func (s *Server) handleProjectTickets(w http.ResponseWriter, r *http.Request) {
	db, err := s.projectDB(r)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	// Reuse existing ticket listing logic with project-specific DB
	s.listTicketsFromDB(w, r, db)
}
```

- [ ] **Step 4: Register new routes**

In the route setup (server.go), add:

```go
mux.HandleFunc("GET /api/projects", s.handleListProjects)
mux.HandleFunc("POST /api/projects", s.handleCreateProject)
mux.HandleFunc("DELETE /api/projects/{pid}", s.handleDeleteProject)
mux.HandleFunc("GET /api/projects/{pid}/tickets", s.handleProjectTickets)
mux.HandleFunc("GET /api/projects/{pid}/tickets/{id}", s.handleProjectTicketDetail)
mux.HandleFunc("GET /api/projects/{pid}/tickets/{id}/tasks", s.handleProjectTasks)
mux.HandleFunc("GET /api/projects/{pid}/tickets/{id}/llm-calls", s.handleProjectLlmCalls)
mux.HandleFunc("GET /api/projects/{pid}/tickets/{id}/events", s.handleProjectEvents)
mux.HandleFunc("GET /api/projects/{pid}/cost/daily/{date}", s.handleProjectDailyCost)
mux.HandleFunc("GET /api/projects/{pid}/cost/monthly/{yearMonth}", s.handleProjectMonthlyCost)
mux.HandleFunc("POST /api/projects/{pid}/sync", s.handleProjectSync)
mux.HandleFunc("POST /api/projects/{pid}/pause", s.handleProjectPause)
mux.HandleFunc("POST /api/projects/{pid}/resume", s.handleProjectResume)
mux.HandleFunc("GET /api/projects/{pid}/health", s.handleProjectHealth)
mux.HandleFunc("GET /api/projects/{pid}/dashboard", s.handleProjectDashboard)
```

- [ ] **Step 5: Add global overview endpoint**

```go
// GET /api/overview — aggregated metrics across all projects
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.projects.ListProjects()

	var totalActive, totalPRs, totalNeedInput int
	var totalCostToday float64

	for _, entry := range entries {
		worker, ok := s.projects.GetWorker(entry.ID)
		if !ok {
			continue
		}
		db := worker.Database.(DashboardDB)
		// Query each project DB for summary stats
		// (active tickets, open PRs, cost today, tickets needing input)
		// Aggregate into totals
		_ = db // placeholder — implement per-project queries
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_tickets": totalActive,
		"open_prs":       totalPRs,
		"need_input":     totalNeedInput,
		"cost_today":     totalCostToday,
		"projects":       len(entries),
	})
}
```

- [ ] **Step 6: Keep backward-compatible old endpoints (temporary)**

During transition, keep the old `/api/tickets` etc. endpoints working. They can use the first (or only) project's DB. This ensures the existing frontend doesn't break while Phase 2 is in progress.

- [ ] **Step 7: Verify compilation**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 8: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go
git commit -m "feat: add project-scoped API endpoints and global overview"
```

---

### Task 10: WebSocket Event Multiplexing

**Files:**
- Modify: `internal/telemetry/events.go`
- Modify: `internal/dashboard/server.go`

- [ ] **Step 1: Add project ID to event records**

Ensure `models.EventRecord` has a `ProjectID` field. If not already present, add it to the struct in `internal/models/models.go`.

- [ ] **Step 2: Create event multiplexer**

Add a `GlobalEventEmitter` that aggregates events from per-project emitters:

```go
// GlobalEventEmitter fans in events from multiple project emitters.
type GlobalEventEmitter struct {
	subscribers map[chan *models.EventRecord]struct{}
	mu          sync.RWMutex
}

func (g *GlobalEventEmitter) Forward(event *models.EventRecord) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for ch := range g.subscribers {
		select {
		case ch <- event:
		default: // drop if subscriber is slow
		}
	}
}
```

- [ ] **Step 3: Add project-scoped WebSocket route**

```go
mux.HandleFunc("/ws/projects/{pid}", s.handleProjectWebSocket)
mux.HandleFunc("/ws/global", s.handleGlobalWebSocket)
```

- [ ] **Step 4: Verify compilation and run tests**

Run: `go build ./... && go test ./internal/telemetry/... -v -count=1`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/events.go internal/dashboard/server.go internal/models/models.go
git commit -m "feat: add project-scoped WebSocket event multiplexing"
```

---

## Chunk 5: Chat Messages & CLI Commands

### Task 11: Add chat_messages Table to Schema

**Files:**
- Modify: `internal/db/schema.go`
- Modify: `internal/db/sqlite.go`
- Modify: `internal/db/db.go`

- [ ] **Step 1: Add chat_messages table to schema**

In `internal/db/schema.go`, add:

```sql
CREATE TABLE IF NOT EXISTS chat_messages (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL,
    sender TEXT NOT NULL,
    message_type TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_ticket ON chat_messages(ticket_id, created_at);
```

- [ ] **Step 2: Add chat methods to Database interface**

In `internal/db/db.go`, add to the `Database` interface:

```go
// Chat messages
CreateChatMessage(ctx context.Context, msg *models.ChatMessage) error
GetChatMessages(ctx context.Context, ticketID string, limit int) ([]models.ChatMessage, error)
```

- [ ] **Step 3: Add ChatMessage model**

In `internal/models/models.go`, add:

```go
type ChatMessage struct {
	ID          string    `json:"id" db:"id"`
	TicketID    string    `json:"ticket_id" db:"ticket_id"`
	Sender      string    `json:"sender" db:"sender"`           // agent, user, system
	MessageType string    `json:"message_type" db:"message_type"` // clarification, action_request, info, error, reply
	Content     string    `json:"content" db:"content"`
	Metadata    string    `json:"metadata,omitempty" db:"metadata"` // JSON
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
```

- [ ] **Step 4: Implement in SQLite**

In `internal/db/sqlite.go`, add the two methods:

```go
func (s *SQLiteDB) CreateChatMessage(ctx context.Context, msg *models.ChatMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_messages (id, ticket_id, sender, message_type, content, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.TicketID, msg.Sender, msg.MessageType, msg.Content, msg.Metadata)
	return err
}

func (s *SQLiteDB) GetChatMessages(ctx context.Context, ticketID string, limit int) ([]models.ChatMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, sender, message_type, content, metadata, created_at FROM chat_messages WHERE ticket_id = ? ORDER BY created_at ASC LIMIT ?`,
		ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.ChatMessage
	for rows.Next() {
		var m models.ChatMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.Sender, &m.MessageType, &m.Content, &m.Metadata, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/db/... -v -short -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/db/schema.go internal/db/sqlite.go internal/db/db.go internal/models/models.go
git commit -m "feat: add chat_messages table and database methods"
```

---

### Task 12: CLI Project Commands

**Files:**
- Create: `cmd/project.go`

- [ ] **Step 1: Create project subcommand**

Create `cmd/project.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/canhta/foreman/internal/project"
	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	cmd.AddCommand(newProjectListCmd())
	cmd.AddCommand(newProjectCreateCmd())
	cmd.AddCommand(newProjectDeleteCmd())
	return cmd
}

func foremanDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".foreman")
}

func newProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)
			entries, err := mgr.ListProjects()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("No projects configured. Use 'foreman project create' to add one.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tACTIVE\tCREATED")
			for _, e := range entries {
				fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", e.ID[:8], e.Name, e.Active, e.CreatedAt.Format("2006-01-02"))
			}
			return w.Flush()
		},
	}
}

func newProjectCreateCmd() *cobra.Command {
	var name, cloneURL, trackerProvider, defaultBranch string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new project",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)

			projCfg := &project.ProjectConfig{}
			projCfg.Project.Name = name
			projCfg.Git.CloneURL = cloneURL
			projCfg.Git.DefaultBranch = defaultBranch
			projCfg.Tracker.Provider = trackerProvider
			projCfg.AgentRunner.Provider = "builtin"

			id, err := mgr.CreateProject(projCfg)
			if err != nil {
				return err
			}

			fmt.Printf("Project %q created with ID %s\n", name, id[:8])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Project name (required)")
	cmd.Flags().StringVar(&cloneURL, "clone-url", "", "Git clone URL")
	cmd.Flags().StringVar(&trackerProvider, "tracker", "github", "Tracker provider (github, jira, linear, local_file)")
	cmd.Flags().StringVar(&defaultBranch, "branch", "main", "Default branch")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func newProjectDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [project-id]",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)

			// TODO: Add confirmation prompt
			if err := mgr.DeleteProject(args[0]); err != nil {
				return err
			}

			fmt.Println("Project deleted.")
			return nil
		},
	}
}
```

- [ ] **Step 2: Register project command in root**

In `cmd/root.go` (or wherever subcommands are registered), add:

```go
rootCmd.AddCommand(newProjectCmd())
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add cmd/project.go cmd/root.go
git commit -m "feat: add CLI project management commands (create, list, delete)"
```

---

## Chunk 6: Migration & Final Verification

### Task 13: Auto-Migration from Single-Project Setup

**Files:**
- Create: `internal/project/migrate.go`
- Create: `internal/project/migrate_test.go`

- [ ] **Step 1: Write test for migration**

Create `internal/project/migrate_test.go`:

```go
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
	os.WriteFile(oldDB, []byte("sqlite-data"), 0644)

	// Create old-style foreman.toml
	oldConfig := filepath.Join(baseDir, "foreman.toml")
	configContent := `
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
	os.WriteFile(oldConfig, []byte(configContent), 0644)

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestMigrateFromSingleProject -v`
Expected: FAIL

- [ ] **Step 3: Implement migration**

Create `internal/project/migrate.go`:

```go
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NeedsMigration checks if the base directory has a single-project setup.
func NeedsMigration(baseDir string) bool {
	// If projects.json already exists, no migration needed
	if _, err := os.Stat(filepath.Join(baseDir, "projects.json")); err == nil {
		return false
	}
	// If foreman.db exists at the base level, migration is needed
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

	// Load old config to extract project-specific fields
	projCfg, err := LoadProjectConfig(oldConfigPath)
	if err != nil {
		// If config can't be loaded as project config, create a minimal one
		projCfg = &ProjectConfig{}
		projCfg.Project.Name = "Default Project"
	}
	if projCfg.Project.Name == "" {
		projCfg.Project.Name = "Default Project"
	}

	// Write project config
	if err := WriteProjectConfig(projDir, projCfg); err != nil {
		return "", fmt.Errorf("write project config: %w", err)
	}

	// Move database
	oldDB := filepath.Join(baseDir, "foreman.db")
	newDB := ProjectDBPath(projDir)
	if _, err := os.Stat(oldDB); err == nil {
		if err := os.Rename(oldDB, newDB); err != nil {
			return "", fmt.Errorf("move database: %w", err)
		}
		log.Info().Str("from", oldDB).Str("to", newDB).Msg("moved database")
	}

	// Move WAL and SHM files if they exist
	for _, suffix := range []string{"-wal", "-shm"} {
		src := oldDB + suffix
		if _, err := os.Stat(src); err == nil {
			os.Rename(src, newDB+suffix)
		}
	}

	// Move work directory if it exists
	oldWork := filepath.Join(baseDir, "work")
	if info, err := os.Stat(oldWork); err == nil && info.IsDir() {
		newWork := ProjectWorkDir(projDir)
		os.RemoveAll(newWork) // remove empty dir created by CreateProjectDir
		if err := os.Rename(oldWork, newWork); err != nil {
			log.Warn().Err(err).Msg("could not move work directory, will re-clone")
		}
	}

	// Create project index
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/... -run TestMigrateFromSingleProject -v`
Expected: PASS

- [ ] **Step 5: Integrate migration into startup**

In `cmd/start.go`, before project discovery, add:

```go
if project.NeedsMigration(foremanDir) {
	configPath := "foreman.toml"
	if _, err := project.MigrateFromSingleProject(foremanDir, configPath); err != nil {
		log.Fatal().Err(err).Msg("migration failed")
	}
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/project/migrate.go internal/project/migrate_test.go cmd/start.go
git commit -m "feat: auto-migrate single-project setup to multi-project structure"
```

---

### Task 14: Chat API Endpoints

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`

- [ ] **Step 1: Add chat endpoints**

```go
// GET /api/projects/:pid/tickets/:id/chat
func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	db, err := s.projectDB(r)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	ticketID := r.PathValue("id")
	messages, err := db.GetChatMessages(r.Context(), ticketID, 100)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(messages)
}

// POST /api/projects/:pid/tickets/:id/chat
func (s *Server) handlePostChat(w http.ResponseWriter, r *http.Request) {
	db, err := s.projectDB(r)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	msg := &models.ChatMessage{
		ID:          uuid.New().String(),
		TicketID:    r.PathValue("id"),
		Sender:      "user",
		MessageType: "reply",
		Content:     req.Content,
	}

	if err := db.CreateChatMessage(r.Context(), msg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(201)
	json.NewEncoder(w).Encode(msg)
}
```

- [ ] **Step 2: Register routes**

```go
mux.HandleFunc("GET /api/projects/{pid}/tickets/{id}/chat", s.handleGetChat)
mux.HandleFunc("POST /api/projects/{pid}/tickets/{id}/chat", s.handlePostChat)
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go
git commit -m "feat: add chat message API endpoints"
```

---

### Task 15: Final Verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 2: Full test suite**

Run: `go test ./... -short -count=1`
Expected: All tests pass

- [ ] **Step 3: Lint**

Run: `golangci-lint run`
Expected: No critical issues

- [ ] **Step 4: Manual smoke test**

Run: `go run . project create --name "Test" --clone-url "git@github.com:test/test.git"`
Verify: Project directory created under `~/.foreman/projects/<uuid>/`
Verify: `projects.json` updated
Verify: `config.toml` written with correct content

Run: `go run . project list`
Verify: Shows the created project

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "fix: address issues from final verification"
```
