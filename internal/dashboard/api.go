package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/command"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/project"
	"github.com/canhta/foreman/internal/util"
	"github.com/rs/zerolog/log"
)

// DashboardDB is a subset of db.Database needed by the dashboard.
type DashboardDB interface {
	AuthValidator
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	SaveDAGState(ctx context.Context, ticketID string, state db.DAGState) error
	GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error)
	GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error)
	DeleteTicket(ctx context.Context, id string) error
	CreateChatMessage(ctx context.Context, msg *models.ChatMessage) error
	GetChatMessages(ctx context.Context, ticketID string, limit int) ([]models.ChatMessage, error)
}

// EventSubscriber is the subset of EventEmitter needed for WebSocket.
type EventSubscriber interface {
	Subscribe() chan *models.EventRecord
	Unsubscribe(ch chan *models.EventRecord)
}

// EventPublisher is an optional capability implemented by telemetry.EventEmitter.
// API handlers use this when available to add entries to the activity feed.
type EventPublisher interface {
	Emit(ctx context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string)
}

// DaemonStatusProvider is an optional interface for exposing daemon runtime state.
// Pass nil when running the dashboard without an attached daemon.
type DaemonStatusProvider interface {
	IsRunning() bool
	IsPaused() bool
}

// DaemonController allows the dashboard to control the daemon lifecycle.
type DaemonController interface {
	DaemonStatusProvider
	Pause()
	Resume()
}

// TicketRetrier re-queues a failed ticket for processing.
type TicketRetrier interface {
	RetryTicket(ctx context.Context, ticketID string) error
}

// TrackerSyncer triggers an immediate tracker poll, bypassing the normal interval.
type TrackerSyncer interface {
	TriggerSync()
}

// MCPHealthProvider exposes the health state of all registered MCP servers.
// Implement this interface to include MCP server health in dashboard responses.
type MCPHealthProvider interface {
	HealthStatus() map[string]bool
}

// ConfigProvider supplies the active configuration for the dashboard.
type ConfigProvider interface {
	GetConfig() *models.Config
}

// PromptSnapshotQuerier returns the recorded prompt template snapshots.
// Defined as a separate interface to avoid widening DashboardDB.
type PromptSnapshotQuerier interface {
	GetPromptSnapshots(ctx context.Context) ([]db.PromptSnapshot, error)
}

// API handles REST API requests for the dashboard.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type API struct {
	startedAt       time.Time
	db              DashboardDB
	emitter         EventSubscriber
	globalEmitter   EventSubscriber // fans in events from all projects
	statusProvider  DaemonStatusProvider
	controller      DaemonController
	retrier         TicketRetrier
	syncer          TrackerSyncer
	mcpHealth       MCPHealthProvider
	promptSnapshots PromptSnapshotQuerier
	configProvider  ConfigProvider
	commandRegistry *command.Registry
	channelHealth   map[string]interface{ IsConnected() bool }
	version         string
	costCfg         models.CostConfig
	projects        ProjectRegistry
}

// SetChannelHealth registers a HealthChecker for a named channel.
func (a *API) SetChannelHealth(name string, h interface{ IsConnected() bool }) {
	if a.channelHealth == nil {
		a.channelHealth = make(map[string]interface{ IsConnected() bool })
	}
	a.channelHealth[name] = h
}

// SetMCPHealthProvider wires a provider that exposes MCP server health.
func (a *API) SetMCPHealthProvider(p MCPHealthProvider) {
	a.mcpHealth = p
}

// SetDaemonController wires a DaemonController for pause/resume.
func (a *API) SetDaemonController(c DaemonController) {
	a.controller = c
	a.statusProvider = c
}

// SetTicketRetrier wires a TicketRetrier for ticket retry.
func (a *API) SetTicketRetrier(r TicketRetrier) {
	a.retrier = r
}

// SetTrackerSyncer wires a TrackerSyncer for the forced sync endpoint.
func (a *API) SetTrackerSyncer(s TrackerSyncer) {
	a.syncer = s
}

// SetPromptSnapshotQuerier wires a PromptSnapshotQuerier for the versions endpoint.
func (a *API) SetPromptSnapshotQuerier(q PromptSnapshotQuerier) {
	a.promptSnapshots = q
}

// SetConfigProvider wires a ConfigProvider for the config summary endpoint.
func (a *API) SetConfigProvider(p ConfigProvider) {
	a.configProvider = p
}

// SetCommandRegistry wires a command registry for the commands endpoints.
func (a *API) SetCommandRegistry(r *command.Registry) {
	a.commandRegistry = r
}

// SetProjectRegistry wires the project registry for multi-project API endpoints.
func (a *API) SetProjectRegistry(r ProjectRegistry) {
	a.projects = r
}

// SetGlobalEmitter wires a global event emitter that fans in events from all projects.
func (a *API) SetGlobalEmitter(e EventSubscriber) {
	a.globalEmitter = e
}

// projectDB resolves the database for a project from the URL path.
// Returns an error if the project is not found or its database does not implement DashboardDB.
func (a *API) projectDB(r *http.Request) (DashboardDB, error) {
	if a.projects == nil {
		return nil, fmt.Errorf("project registry not configured")
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		return nil, fmt.Errorf("missing project ID in path")
	}
	worker, ok := a.projects.GetWorker(pid)
	if !ok {
		return nil, fmt.Errorf("project %q not found or not running", pid)
	}
	projDB, ok := worker.Database.(DashboardDB)
	if !ok {
		return nil, fmt.Errorf("project %q database does not implement DashboardDB", pid)
	}
	return projDB, nil
}

// handleListProjects handles GET /api/projects.
func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusOK, []project.IndexEntry{})
		return
	}
	entries, err := a.projects.ListProjects()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []project.IndexEntry{}
	}
	// Enrich entries with empty names from their config files.
	for i, e := range entries {
		if e.Name == "" {
			if cfg, _, err := a.projects.GetProject(e.ID); err == nil && cfg.Project.Name != "" {
				entries[i].Name = cfg.Project.Name
			}
		}
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleCreateProject handles POST /api/projects.
func (a *API) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
		return
	}
	var dto projectConfigDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg := expandProjectConfigDTO(dto)
	id, err := a.projects.CreateProject(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleDeleteProject handles DELETE /api/projects/{pid}.
func (a *API) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		http.Error(w, "project registry not configured", http.StatusServiceUnavailable)
		return
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project ID"})
		return
	}
	if err := a.projects.DeleteProject(pid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProjectTickets handles GET /api/projects/{pid}/tickets.
func (a *API) handleProjectTickets(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	filter := models.TicketFilter{}
	tickets, err := projDB.ListTickets(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tickets == nil {
		tickets = []models.Ticket{}
	}
	writeJSON(w, http.StatusOK, tickets)
}

// handleOverview handles GET /api/overview — aggregated metrics across all projects.
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	projectCount := 0
	activeTickets := 0
	openPRs := 0
	needInput := 0
	costToday := 0.0

	if a.projects != nil {
		entries, err := a.projects.ListProjects()
		if err == nil {
			projectCount = len(entries)
			date := time.Now().Format("2006-01-02")
			activeStatuses := []models.TicketStatus{
				models.TicketStatusPlanning, models.TicketStatusImplementing,
				models.TicketStatusReviewing, models.TicketStatusPlanValidating,
				models.TicketStatusQueued,
			}
			prStatuses := []models.TicketStatus{
				models.TicketStatusPRCreated, models.TicketStatusPRUpdated,
				models.TicketStatusAwaitingMerge,
			}

			for _, entry := range entries {
				worker, ok := a.projects.GetWorker(entry.ID)
				if !ok {
					continue
				}
				projDB, ok := worker.Database.(DashboardDB)
				if !ok {
					continue
				}
				if tickets, err := projDB.ListTickets(r.Context(), models.TicketFilter{StatusIn: activeStatuses}); err == nil {
					activeTickets += len(tickets)
				}
				if tickets, err := projDB.ListTickets(r.Context(), models.TicketFilter{StatusIn: prStatuses}); err == nil {
					openPRs += len(tickets)
				}
				if tickets, err := projDB.ListTickets(r.Context(), models.TicketFilter{StatusIn: []models.TicketStatus{models.TicketStatusClarificationNeeded}}); err == nil {
					needInput += len(tickets)
				}
				if c, err := projDB.GetDailyCost(r.Context(), date); err == nil {
					costToday += c
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_tickets": activeTickets,
		"open_prs":       openPRs,
		"need_input":     needInput,
		"cost_today":     costToday,
		"projects":       projectCount,
	})
}

// NewAPI creates a new API instance.
func NewAPI(db DashboardDB, emitter EventSubscriber, statusProvider DaemonStatusProvider, costCfg models.CostConfig, version string) *API {
	return &API{
		db:             db,
		emitter:        emitter,
		statusProvider: statusProvider,
		costCfg:        costCfg,
		version:        version,
		startedAt:      time.Now(),
	}
}

// configSummary is the JSON response for GET /api/config/summary.
type configSummary struct {
	LLM         configLLM         `json:"llm"`
	Tracker     configTracker     `json:"tracker"`
	Git         configGit         `json:"git"`
	AgentRunner configAgentRunner `json:"agent_runner"`
	Database    configDatabase    `json:"database"`
	MCP         configMCP         `json:"mcp"`
	Daemon      configDaemon      `json:"daemon"`
	Cost        configCost        `json:"cost"`
	RateLimit   configRateLimit   `json:"rate_limit"`
}

type configLLM struct {
	Provider string            `json:"provider"`
	Models   map[string]string `json:"models"`
	APIKey   string            `json:"api_key"`
}

type configTracker struct {
	Provider     string `json:"provider"`
	PollInterval string `json:"poll_interval"`
}

type configGit struct {
	Provider     string `json:"provider"`
	CloneURL     string `json:"clone_url"`
	BranchPrefix string `json:"branch_prefix"`
	AutoMerge    bool   `json:"auto_merge"`
}

type configAgentRunner struct {
	Provider    string `json:"provider"`
	MaxTurns    int    `json:"max_turns"`
	TokenBudget int    `json:"token_budget"`
}

type configCost struct {
	DailyBudget     float64 `json:"daily_budget"`
	MonthlyBudget   float64 `json:"monthly_budget"`
	PerTicketBudget float64 `json:"per_ticket_budget"`
	AlertThreshold  int     `json:"alert_threshold"`
}

type configDaemon struct {
	LogLevel           string `json:"log_level"`
	MaxParallelTickets int    `json:"max_parallel_tickets"`
	MaxParallelTasks   int    `json:"max_parallel_tasks"`
}

type configDatabase struct {
	Driver string `json:"driver"`
	Path   string `json:"path"`
}

type configMCP struct {
	Servers []string `json:"servers"`
}

type configRateLimit struct {
	RequestsPerMinute int `json:"requests_per_minute"`
}

// redactKey returns a redacted version of an API key for display purposes.
// Deprecated: use util.RedactKey directly.
func redactKey(key string) string {
	return util.RedactKey(key)
}

func (a *API) handleConfigSummary(w http.ResponseWriter, r *http.Request) {
	if a.configProvider == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	cfg := a.configProvider.GetConfig()
	if cfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config not available"})
		return
	}

	modelsMap := map[string]string{
		"planner":          cfg.Models.Planner,
		"implementer":      cfg.Models.Implementer,
		"spec_reviewer":    cfg.Models.SpecReviewer,
		"quality_reviewer": cfg.Models.QualityReviewer,
		"final_reviewer":   cfg.Models.FinalReviewer,
	}

	apiKey := ""
	switch cfg.LLM.DefaultProvider {
	case "anthropic":
		apiKey = redactKey(cfg.LLM.Anthropic.APIKey)
	case "openai":
		apiKey = redactKey(cfg.LLM.OpenAI.APIKey)
	case "openrouter":
		apiKey = redactKey(cfg.LLM.OpenRouter.APIKey)
	}

	dbPath := cfg.Database.SQLite.Path

	mcpServers := make([]string, 0, len(cfg.MCP.Servers))
	for _, s := range cfg.MCP.Servers {
		mcpServers = append(mcpServers, s.Name)
	}

	summary := configSummary{
		LLM: configLLM{
			Provider: cfg.LLM.DefaultProvider,
			Models:   modelsMap,
			APIKey:   apiKey,
		},
		Tracker: configTracker{
			Provider:     cfg.Tracker.Provider,
			PollInterval: fmt.Sprintf("%ds", cfg.Daemon.PollIntervalSecs),
		},
		Git: configGit{
			Provider:     cfg.Git.Provider,
			CloneURL:     cfg.Git.CloneURL,
			BranchPrefix: cfg.Git.BranchPrefix,
			AutoMerge:    cfg.Git.AutoMerge,
		},
		AgentRunner: configAgentRunner{
			Provider:    cfg.AgentRunner.Provider,
			MaxTurns:    cfg.AgentRunner.MaxTurnsDefault,
			TokenBudget: cfg.AgentRunner.TokenBudget,
		},
		Cost: configCost{
			DailyBudget:     cfg.Cost.MaxCostPerDayUSD,
			MonthlyBudget:   cfg.Cost.MaxCostPerMonthUSD,
			PerTicketBudget: cfg.Cost.MaxCostPerTicketUSD,
			AlertThreshold:  cfg.Cost.AlertThresholdPct,
		},
		Daemon: configDaemon{
			MaxParallelTickets: cfg.Daemon.MaxParallelTickets,
			MaxParallelTasks:   cfg.Daemon.MaxParallelTasks,
			LogLevel:           cfg.Daemon.LogLevel,
		},
		Database: configDatabase{
			Driver: cfg.Database.Driver,
			Path:   dbPath,
		},
		MCP: configMCP{
			Servers: mcpServers,
		},
		RateLimit: configRateLimit{
			RequestsPerMinute: cfg.RateLimit.RequestsPerMinute,
		},
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	daemonState := "stopped"
	if a.statusProvider != nil {
		if a.statusProvider.IsRunning() {
			if a.statusProvider.IsPaused() {
				daemonState = "paused"
			} else {
				daemonState = "running"
			}
		}
	}

	resp := map[string]interface{}{
		"status":       "running",
		"version":      a.version,
		"uptime":       time.Since(a.startedAt).String(),
		"daemon_state": daemonState,
	}

	if len(a.channelHealth) > 0 {
		channels := make(map[string]interface{})
		for name, h := range a.channelHealth {
			channels[name] = map[string]interface{}{
				"connected": h.IsConnected(),
			}
		}
		resp["channels"] = channels
	}

	if a.mcpHealth != nil {
		mcpStatus := a.mcpHealth.HealthStatus()
		if len(mcpStatus) > 0 {
			servers := make(map[string]interface{}, len(mcpStatus))
			for name, healthy := range mcpStatus {
				servers[name] = map[string]interface{}{
					"healthy": healthy,
				}
			}
			resp["mcp_servers"] = servers
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleCostsBudgets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_daily_usd":       a.costCfg.MaxCostPerDayUSD,
		"max_monthly_usd":     a.costCfg.MaxCostPerMonthUSD,
		"max_ticket_usd":      a.costCfg.MaxCostPerTicketUSD,
		"alert_threshold_pct": a.costCfg.AlertThresholdPct,
	})
}

func (a *API) handleRetryTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.retrier == nil {
		http.Error(w, "retry not available", http.StatusServiceUnavailable)
		return
	}
	id := extractPathParam(r.URL.Path, "/api/tickets/")
	if idx := strings.Index(id, "/"); idx != -1 {
		id = id[:idx]
	}
	if err := a.retrier.RetryTicket(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.emitEvent(r.Context(), id, "", "ticket_retried", "info", "Retry requested from dashboard", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying", "ticket_id": id})
}

func (a *API) handleDaemonPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.controller == nil {
		http.Error(w, "daemon control not available", http.StatusServiceUnavailable)
		return
	}
	a.controller.Pause()
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (a *API) handleDaemonResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.controller == nil {
		http.Error(w, "daemon control not available", http.StatusServiceUnavailable)
		return
	}
	a.controller.Resume()
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (a *API) handleDaemonSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.syncer == nil {
		http.Error(w, "sync not available", http.StatusServiceUnavailable)
		return
	}
	a.syncer.TriggerSync()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sync triggered"})
}

func (a *API) handlePromptVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.promptSnapshots == nil {
		writeJSON(w, http.StatusOK, []db.PromptSnapshot{})
		return
	}
	snapshots, err := a.promptSnapshots.GetPromptSnapshots(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if snapshots == nil {
		snapshots = []db.PromptSnapshot{}
	}
	writeJSON(w, http.StatusOK, snapshots)
}

func (a *API) handleClaudeCodeUsage(w http.ResponseWriter, _ *http.Request) {
	usage := parseClaudeCodeUsageCached()
	writeJSON(w, http.StatusOK, usage)
}

// commandListItem is the JSON representation of a command in the list response.
type commandListItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Subtask     bool   `json:"subtask"`
}

func (a *API) handleListCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.commandRegistry == nil {
		writeJSON(w, http.StatusOK, []commandListItem{})
		return
	}
	cmds := a.commandRegistry.List()
	items := make([]commandListItem, len(cmds))
	for i, cmd := range cmds {
		items[i] = commandListItem{
			Name:        cmd.Name,
			Description: cmd.Description,
			Source:      cmd.Source,
			Subtask:     cmd.Subtask,
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) handleRenderCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.commandRegistry == nil {
		http.Error(w, "command registry not available", http.StatusServiceUnavailable)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/commands/")
	if name == "" {
		http.Error(w, "missing command name", http.StatusBadRequest)
		return
	}

	var body struct {
		Args []string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	rendered, err := a.commandRegistry.Render(name, body.Args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rendered": rendered})
}

// projectConfigDTO is the flat JSON representation of a project config used by the dashboard UI.
type projectConfigDTO struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	GitCloneURL        string  `json:"git_clone_url"`
	GitDefaultBranch   string  `json:"git_default_branch"`
	GitToken           string  `json:"git_token"`
	GitProvider        string  `json:"git_provider"`
	RepoReady          bool    `json:"repo_ready"`
	TrackerProvider    string  `json:"tracker_provider"`
	TrackerToken       string  `json:"tracker_token"`
	TrackerProjectKey  string  `json:"tracker_project_key"`
	TrackerLabels      string  `json:"tracker_labels"`
	TrackerURL         string  `json:"tracker_url"`
	TrackerEmail       string  `json:"tracker_email"`
	AgentRunner        string  `json:"agent_runner"`
	ModelPlanner       string  `json:"model_planner"`
	ModelImplementer   string  `json:"model_implementer"`
	MaxParallelTickets int     `json:"max_parallel_tickets"`
	MaxTasksPerTicket  int     `json:"max_tasks_per_ticket"`
	MaxCostPerTicket   float64 `json:"max_cost_per_ticket"`
}

// flattenProjectConfig converts the nested ProjectConfig to a flat DTO for the frontend.
func flattenProjectConfig(cfg *project.ProjectConfig) projectConfigDTO {
	dto := projectConfigDTO{
		Name:               cfg.Project.Name,
		Description:        cfg.Project.Description,
		GitCloneURL:        cfg.Git.CloneURL,
		GitDefaultBranch:   cfg.Git.DefaultBranch,
		GitProvider:        cfg.Git.Provider,
		GitToken:           cfg.Git.GitHub.Token,
		TrackerProvider:    cfg.Tracker.Provider,
		TrackerLabels:      cfg.Tracker.PickupLabel,
		AgentRunner:        cfg.AgentRunner.Provider,
		ModelPlanner:       cfg.Models.Planner,
		ModelImplementer:   cfg.Models.Implementer,
		MaxParallelTickets: cfg.Limits.MaxParallelTickets,
		MaxTasksPerTicket:  cfg.Limits.MaxTasksPerTicket,
		MaxCostPerTicket:   cfg.Cost.MaxCostPerTicketUSD,
	}
	switch cfg.Tracker.Provider {
	case "github":
		dto.TrackerToken = cfg.Tracker.GitHub.Token
		dto.TrackerProjectKey = cfg.Tracker.GitHub.Owner + "/" + cfg.Tracker.GitHub.Repo
		dto.TrackerURL = cfg.Tracker.GitHub.BaseURL
	case "jira":
		dto.TrackerToken = cfg.Tracker.Jira.APIToken
		dto.TrackerProjectKey = cfg.Tracker.Jira.ProjectKey
		dto.TrackerURL = cfg.Tracker.Jira.BaseURL
		dto.TrackerEmail = cfg.Tracker.Jira.Email
	case "linear":
		dto.TrackerToken = cfg.Tracker.Linear.APIKey
		dto.TrackerProjectKey = cfg.Tracker.Linear.TeamID
		dto.TrackerURL = cfg.Tracker.Linear.BaseURL
	}
	return dto
}

// expandProjectConfigDTO converts the flat DTO back to a nested ProjectConfig.
func expandProjectConfigDTO(dto projectConfigDTO) *project.ProjectConfig {
	cfg := &project.ProjectConfig{}
	cfg.Project.Name = dto.Name
	cfg.Project.Description = dto.Description
	cfg.Git.CloneURL = dto.GitCloneURL
	cfg.Git.DefaultBranch = dto.GitDefaultBranch
	cfg.Git.Provider = dto.GitProvider
	cfg.Git.GitHub.Token = dto.GitToken
	cfg.Tracker.Provider = dto.TrackerProvider
	cfg.Tracker.PickupLabel = dto.TrackerLabels
	cfg.AgentRunner.Provider = dto.AgentRunner
	cfg.Models.Planner = dto.ModelPlanner
	cfg.Models.Implementer = dto.ModelImplementer
	cfg.Limits.MaxParallelTickets = dto.MaxParallelTickets
	cfg.Limits.MaxTasksPerTicket = dto.MaxTasksPerTicket
	cfg.Cost.MaxCostPerTicketUSD = dto.MaxCostPerTicket
	switch dto.TrackerProvider {
	case "github":
		cfg.Tracker.GitHub.Token = dto.TrackerToken
		cfg.Tracker.GitHub.BaseURL = dto.TrackerURL
		if i := strings.Index(dto.TrackerProjectKey, "/"); i > 0 {
			cfg.Tracker.GitHub.Owner = dto.TrackerProjectKey[:i]
			cfg.Tracker.GitHub.Repo = dto.TrackerProjectKey[i+1:]
		}
	case "jira":
		cfg.Tracker.Jira.APIToken = dto.TrackerToken
		cfg.Tracker.Jira.ProjectKey = dto.TrackerProjectKey
		cfg.Tracker.Jira.BaseURL = dto.TrackerURL
		cfg.Tracker.Jira.Email = dto.TrackerEmail
	case "linear":
		cfg.Tracker.Linear.APIKey = dto.TrackerToken
		cfg.Tracker.Linear.TeamID = dto.TrackerProjectKey
		cfg.Tracker.Linear.BaseURL = dto.TrackerURL
	}
	return cfg
}

// handleGetProject handles GET /api/projects/{pid} — returns project details.
func (a *API) handleGetProject(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
		return
	}
	pid := extractProjectID(r.URL.Path)
	cfg, projDir, err := a.projects.GetProject(pid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project config not found"})
		return
	}
	dto := flattenProjectConfig(cfg)
	workDir := project.ProjectWorkDir(projDir)
	dto.RepoReady = isRepoReady(workDir)
	writeJSON(w, http.StatusOK, dto)
}

// handleUpdateProject handles PUT /api/projects/{pid} — updates project config.
func (a *API) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
		return
	}
	var dto projectConfigDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	pid := extractProjectID(r.URL.Path)
	cfg := expandProjectConfigDTO(dto)
	if err := a.projects.UpdateProject(pid, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// isRepoReady returns true if the given directory contains a valid git repository
// (i.e. git rev-parse HEAD succeeds).
func isRepoReady(workDir string) bool {
	if _, err := os.Stat(workDir); err != nil {
		return false
	}
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "HEAD")
	return cmd.Run() == nil
}

// handleProjectClone handles POST /api/projects/{pid}/clone — triggers a git clone.
func (a *API) handleProjectClone(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
		return
	}
	pid := extractProjectID(r.URL.Path)
	cfg, projDir, err := a.projects.GetProject(pid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project config not found"})
		return
	}
	cloneURL := cfg.Git.CloneURL
	if cloneURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no clone_url configured"})
		return
	}
	workDir := project.ProjectWorkDir(projDir)

	gitProv := git.NewNativeGitProviderWithClone(cloneURL)
	if cfg.Git.GitHub.Token != "" {
		gitProv = gitProv.WithHTTPToken(cfg.Git.GitHub.Token)
	}

	if err := gitProv.EnsureRepo(r.Context(), workDir); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"repo_ready": true,
	})
}

// handleProjectTicketDetail handles GET /api/projects/{pid}/tickets/{id}.
func (a *API) handleProjectTicketDetail(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	ticket, err := projDB.GetTicket(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ticket == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

// handleProjectTasks handles GET /api/projects/{pid}/tickets/{id}/tasks.
func (a *API) handleProjectTasks(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	tasks, err := projDB.ListTasks(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

// handleProjectLlmCalls handles GET /api/projects/{pid}/tickets/{id}/llm-calls.
func (a *API) handleProjectLlmCalls(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	calls, err := projDB.ListLlmCalls(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if calls == nil {
		calls = []models.LlmCallRecord{}
	}
	writeJSON(w, http.StatusOK, calls)
}

// handleProjectEvents handles GET /api/projects/{pid}/tickets/{id}/events.
func (a *API) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	events, err := projDB.GetEvents(r.Context(), id, 100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if events == nil {
		events = []models.EventRecord{}
	}
	writeJSON(w, http.StatusOK, events)
}

// handleProjectDailyCost handles GET /api/projects/{pid}/cost/daily/{date}.
func (a *API) handleProjectDailyCost(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Extract date from /api/projects/{pid}/cost/daily/{date}
	date := extractProjectCostParam(r.URL.Path, "daily")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	cost, err := projDB.GetDailyCost(r.Context(), date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"date": date, "cost_usd": cost})
}

// handleProjectMonthlyCost handles GET /api/projects/{pid}/cost/monthly/{yearMonth}.
func (a *API) handleProjectMonthlyCost(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Extract yearMonth from /api/projects/{pid}/cost/monthly/{yearMonth}
	yearMonth := extractProjectCostParam(r.URL.Path, "monthly")
	if yearMonth == "" {
		yearMonth = time.Now().Format("2006-01")
	}
	cost, err := projDB.GetMonthlyCost(r.Context(), yearMonth)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"month": yearMonth, "cost_usd": cost})
}

// handleProjectSync handles POST /api/projects/{pid}/sync.
func (a *API) handleProjectSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.projects == nil {
		http.Error(w, "project registry not configured", http.StatusServiceUnavailable)
		return
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project ID"})
		return
	}
	_, ok := a.projects.GetWorker(pid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found or not running"})
		return
	}
	// Sync is a best-effort trigger; the worker's tracker poll handles the actual sync.
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sync triggered", "project_id": pid})
}

// handleProjectPause handles POST /api/projects/{pid}/pause.
func (a *API) handleProjectPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.projects == nil {
		http.Error(w, "project registry not configured", http.StatusServiceUnavailable)
		return
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project ID"})
		return
	}
	worker, ok := a.projects.GetWorker(pid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found or not running"})
		return
	}
	worker.Pause()
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused", "project_id": pid})
}

// handleProjectResume handles POST /api/projects/{pid}/resume.
func (a *API) handleProjectResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.projects == nil {
		http.Error(w, "project registry not configured", http.StatusServiceUnavailable)
		return
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project ID"})
		return
	}
	worker, ok := a.projects.GetWorker(pid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found or not running"})
		return
	}
	worker.Resume()
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "project_id": pid})
}

// handleProjectHealth handles GET /api/projects/{pid}/health.
func (a *API) handleProjectHealth(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
		return
	}
	pid := extractProjectID(r.URL.Path)
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project ID"})
		return
	}
	worker, ok := a.projects.GetWorker(pid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found or not running"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id": pid,
		"status":     string(worker.Status()),
	})
}

// handleProjectTicketSummaries handles GET /api/projects/{pid}/ticket-summaries.
func (a *API) handleProjectTicketSummaries(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	summaries, err := projDB.GetTicketSummaries(r.Context(), models.TicketFilter{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if summaries == nil {
		summaries = []models.TicketSummary{}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// handleProjectCostsToday handles GET /api/projects/{pid}/costs/today.
func (a *API) handleProjectCostsToday(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	date := time.Now().Format("2006-01-02")
	cost, err := projDB.GetDailyCost(r.Context(), date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"date": date, "cost_usd": cost})
}

// handleProjectCostsMonth handles GET /api/projects/{pid}/costs/month.
func (a *API) handleProjectCostsMonth(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	yearMonth := time.Now().Format("2006-01")
	cost, err := projDB.GetMonthlyCost(r.Context(), yearMonth)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"month": yearMonth, "cost_usd": cost})
}

// handleProjectCostsWeek handles GET /api/projects/{pid}/costs/week.
func (a *API) handleProjectCostsWeek(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	var costs []map[string]interface{}
	for i := 6; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		cost, err := projDB.GetDailyCost(r.Context(), date)
		entry := map[string]interface{}{"date": date, "cost_usd": cost}
		if err != nil {
			entry["error"] = "unavailable"
		}
		costs = append(costs, entry)
	}
	writeJSON(w, http.StatusOK, costs)
}

// handleProjectGlobalEvents handles GET /api/projects/{pid}/events.
func (a *API) handleProjectGlobalEvents(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, nerr := strconv.Atoi(v); nerr == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, nerr := strconv.Atoi(v); nerr == nil && n >= 0 {
			offset = n
		}
	}
	events, err := projDB.GetGlobalEvents(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if events == nil {
		events = []models.EventRecord{}
	}
	writeJSON(w, http.StatusOK, events)
}

// handleProjectDeleteTicket handles DELETE /api/projects/{pid}/tickets/{id}.
func (a *API) handleProjectDeleteTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	if err := projDB.DeleteTicket(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "ticket_id": id})
}

// handleProjectDashboard handles GET /api/projects/{pid}/dashboard.
func (a *API) handleProjectDashboard(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	pid := extractProjectID(r.URL.Path)

	// Aggregate summary stats from project DB.
	active := []models.TicketStatus{
		models.TicketStatusPlanning, models.TicketStatusImplementing,
		models.TicketStatusReviewing, models.TicketStatusPlanValidating,
	}
	activeTickets, err := projDB.ListTickets(r.Context(), models.TicketFilter{StatusIn: active})
	if err != nil {
		log.Warn().Err(err).Str("project_id", pid).Msg("dashboard: failed to list active tickets")
		activeTickets = nil
	}

	date := time.Now().Format("2006-01-02")
	costToday, err := projDB.GetDailyCost(r.Context(), date)
	if err != nil {
		log.Warn().Err(err).Str("project_id", pid).Msg("dashboard: failed to get daily cost")
		costToday = 0
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id":     pid,
		"active_tickets": len(activeTickets),
		"cost_today":     costToday,
	})
}

// handleProjectGetChat handles GET /api/projects/{pid}/tickets/{id}/chat.
func (a *API) handleProjectGetChat(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, nerr := strconv.Atoi(v); nerr == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	msgs, err := projDB.GetChatMessages(r.Context(), id, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []models.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// handleProjectPostChat handles POST /api/projects/{pid}/tickets/{id}/chat.
func (a *API) handleProjectPostChat(w http.ResponseWriter, r *http.Request) {
	projDB, err := a.projectDB(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	id := extractTicketID(r.URL.Path)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ticket ID"})
		return
	}
	var body struct {
		Content     string `json:"content"`
		MessageType string `json:"message_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if body.MessageType == "" {
		body.MessageType = "reply"
	}
	msg := &models.ChatMessage{
		TicketID:    id,
		Sender:      "user",
		MessageType: body.MessageType,
		Content:     body.Content,
	}
	if err := projDB.CreateChatMessage(r.Context(), msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

// extractProjectID extracts the project ID from a /api/projects/{pid}/... URL path.
func extractProjectID(path string) string {
	rest := strings.TrimPrefix(path, "/api/projects/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// extractTicketID extracts the ticket ID from a /api/projects/{pid}/tickets/{id}/... URL path.
func extractTicketID(path string) string {
	// Strip /api/projects/{pid}/tickets/ prefix.
	rest := strings.TrimPrefix(path, "/api/projects/")
	// rest = {pid}/tickets/{id}/...
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return ""
	}
	rest = rest[slash+1:] // tickets/{id}/...
	rest = strings.TrimPrefix(rest, "tickets/")
	if rest == "" {
		return ""
	}
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// extractProjectCostParam extracts the trailing parameter from cost sub-paths of the form
// /api/projects/{pid}/cost/{subType}/{param} (e.g. daily/2026-01-01 or monthly/2026-01).
func extractProjectCostParam(path, subType string) string {
	// Strip /api/projects/{pid}/cost/{subType}/
	prefix := "/api/projects/"
	rest := strings.TrimPrefix(path, prefix)
	// rest = {pid}/cost/{subType}/{param}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return ""
	}
	rest = rest[slash+1:] // cost/{subType}/{param}
	rest = strings.TrimPrefix(rest, "cost/"+subType+"/")
	if rest == "" || strings.Contains(rest, "/") {
		// Contains "/" means there's further nesting — unexpected.
		if idx := strings.Index(rest, "/"); idx >= 0 {
			return rest[:idx]
		}
	}
	return rest
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) emitEvent(ctx context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string) {
	if a.emitter == nil {
		return
	}
	publisher, ok := a.emitter.(EventPublisher)
	if !ok {
		return
	}
	publisher.Emit(ctx, ticketID, taskID, eventType, severity, message, metadata)
}

func extractPathParam(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return rest
}

// handleTestConnection handles POST /api/projects/test-connection.
// It tests git or tracker credentials without requiring an existing project.
func (a *API) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string `json:"type"`
		// git fields
		CloneURL string `json:"clone_url"`
		Token    string `json:"token"`
		// tracker fields
		Provider   string `json:"provider"`
		Email      string `json:"email"`
		ProjectKey string `json:"project_key"`
		URL        string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	switch req.Type {
	case "git":
		if req.CloneURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "clone_url is required"})
			return
		}
		if err := testGitConnection(ctx, req.CloneURL, req.Token); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	case "tracker":
		if err := testTrackerConnection(ctx, req.Provider, req.Email, req.Token, req.URL, req.ProjectKey); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be 'git' or 'tracker'"})
	}
}

// testGitConnection runs git ls-remote to verify the clone URL is reachable.
func testGitConnection(ctx context.Context, cloneURL, token string) error {
	// For HTTPS URLs with a token, embed the credentials directly in the URL.
	// This is the most reliable approach — credential helpers can be fragile.
	targetURL := cloneURL
	if token != "" && strings.HasPrefix(cloneURL, "https://") {
		u, err := url.Parse(cloneURL)
		if err == nil {
			u.User = url.UserPassword("x-access-token", token)
			targetURL = u.String()
		}
	}
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", targetURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		// Strip embedded credentials from any error message before returning.
		if token != "" {
			msg = strings.ReplaceAll(msg, token, "***")
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// testTrackerConnection performs a lightweight API call to verify tracker credentials.
func testTrackerConnection(ctx context.Context, provider, email, token, baseURL, projectKey string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	switch provider {
	case "github":
		// Verify token against the GitHub API.
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("github: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("github: invalid token")
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("github: unexpected status %d", resp.StatusCode)
		}
		return nil

	case "linear":
		// Verify token via Linear GraphQL viewer query.
		body := strings.NewReader(`{"query":"{ viewer { id } }"}`)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.linear.app/graphql", body)
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("linear: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("linear: invalid token")
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("linear: unexpected status %d", resp.StatusCode)
		}
		return nil

	case "jira":
		if baseURL == "" {
			return fmt.Errorf("jira: url is required")
		}
		jiraBase := strings.TrimRight(baseURL, "/")
		if email != "" {
			// Jira Cloud: Basic auth with email:apiToken.
			jiraURL := jiraBase + "/rest/api/3/myself"
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, jiraURL, nil)
			req.SetBasicAuth(email, token)
			req.Header.Set("Accept", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("jira: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return fmt.Errorf("jira: invalid email or API token")
			}
			if resp.StatusCode >= 300 {
				return fmt.Errorf("jira: unexpected status %d", resp.StatusCode)
			}
			return nil
		}
		// No email provided — verify the URL is a reachable Jira instance via the public serverInfo endpoint.
		infoURL := jiraBase + "/rest/api/3/serverInfo"
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("jira: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("jira: URL does not appear to be a valid Jira instance (status %d)", resp.StatusCode)
		}
		return nil

	default:
		return fmt.Errorf("unsupported tracker provider: %q", provider)
	}
}
