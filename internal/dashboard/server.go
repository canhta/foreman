package dashboard

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/command"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/project"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

//go:embed dist
var webFS embed.FS

// Server is the HTTP server for the Foreman dashboard.
type Server struct {
	api    *API
	db     DashboardDB
	reg    *prometheus.Registry
	server *http.Server
}

// smartRetrier performs a partial retry: it preserves already-done tasks by
// seeding dag_state with their IDs, resets failed/skipped tasks to pending,
// and re-queues the ticket so the daemon resumes from the failure point (ARCH-F05).
type smartRetrier struct{ db DashboardDB }

func (r *smartRetrier) RetryTicket(ctx context.Context, ticketID string) error {
	tasks, err := r.db.ListTasks(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	var doneIDs []string
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone {
			doneIDs = append(doneIDs, t.ID)
		}
	}

	if len(doneIDs) > 0 {
		state := db.DAGState{TicketID: ticketID, CompletedTasks: doneIDs}
		if err := r.db.SaveDAGState(ctx, ticketID, state); err != nil {
			return fmt.Errorf("save dag state: %w", err)
		}
	}

	for _, t := range tasks {
		if t.Status == models.TaskStatusFailed || t.Status == models.TaskStatusSkipped {
			if err := r.db.UpdateTaskStatus(ctx, t.ID, models.TaskStatusPending); err != nil {
				return fmt.Errorf("reset task %s: %w", t.ID, err)
			}
		}
	}

	return r.db.UpdateTicketStatus(ctx, ticketID, models.TicketStatusQueued)
}

// maxRequestBodyBytes is the maximum allowed request body size (1 MiB).
// Applied to every POST/PUT request to prevent memory exhaustion attacks.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// limitRequestBody wraps a handler and enforces the 1 MiB request body limit
// on POST and PUT requests. The limit is enforced — callers that read r.Body
// will get an error if the client sends more than maxRequestBodyBytes.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// NewServer creates a new dashboard Server and registers all HTTP routes.
func NewServer(db DashboardDB, emitter EventSubscriber, statusProvider DaemonStatusProvider, reg *prometheus.Registry, costCfg models.CostConfig, version, host string, port int) *Server {
	api := NewAPI(db, emitter, statusProvider, costCfg, version)

	// Wire ticket retrier using DB — retry preserves done tasks and re-queues for daemon pickup.
	api.SetTicketRetrier(&smartRetrier{db: db})

	// Wire builtin command registry.
	cmdRegistry := command.NewRegistry()
	cmdRegistry.Register(command.Command{
		Name:        "review",
		Description: "Review changes in a diff or file",
		Template:    "Review the following changes:\n$ARGUMENTS",
		Source:      "builtin",
	})
	cmdRegistry.Register(command.Command{
		Name:        "explain",
		Description: "Explain code or a concept",
		Template:    "Explain the following:\n$ARGUMENTS",
		Source:      "builtin",
	})
	cmdRegistry.Register(command.Command{
		Name:        "fix",
		Description: "Fix a bug described in the arguments",
		Template:    "Fix the following issue:\n$ARGUMENTS",
		Source:      "builtin",
		Subtask:     true,
	})
	api.SetCommandRegistry(cmdRegistry)

	mux := http.NewServeMux()

	// Auth-protected API routes
	auth := authMiddleware(db)

	mux.Handle("/api/status", auth(http.HandlerFunc(api.handleStatus)))
	mux.Handle("/api/tickets", auth(http.HandlerFunc(api.handleListTickets)))
	mux.Handle("/api/tickets/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/tasks"):
			api.handleGetTasks(w, r)
		case strings.HasSuffix(path, "/events"):
			api.handleGetEvents(w, r)
		case strings.HasSuffix(path, "/llm-calls"):
			api.handleGetLlmCalls(w, r)
		case strings.HasSuffix(path, "/retry"):
			api.handleRetryTicket(w, r)
		case strings.HasSuffix(path, "/reply"):
			api.handleReplyToTicket(w, r)
		case strings.HasSuffix(path, "/chat"):
			if r.Method == http.MethodGet {
				api.handleGetChat(w, r)
			} else if r.Method == http.MethodPost {
				api.handlePostChat(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			if r.Method == http.MethodDelete {
				api.handleDeleteTicket(w, r)
			} else {
				api.handleGetTicket(w, r)
			}
		}
	})))
	mux.Handle("/api/costs/today", auth(http.HandlerFunc(api.handleCostsToday)))
	mux.Handle("/api/pipeline/active", auth(http.HandlerFunc(api.handleActivePipelines)))
	mux.Handle("/api/costs/week", auth(http.HandlerFunc(api.handleCostsWeek)))
	mux.Handle("/api/costs/month", auth(http.HandlerFunc(api.handleCostsMonth)))
	mux.Handle("/api/costs/budgets", auth(http.HandlerFunc(api.handleCostsBudgets)))
	mux.Handle("/api/daemon/pause", auth(http.HandlerFunc(api.handleDaemonPause)))
	mux.Handle("/api/daemon/resume", auth(http.HandlerFunc(api.handleDaemonResume)))
	mux.Handle("/api/daemon/sync", auth(http.HandlerFunc(api.handleDaemonSync)))
	mux.Handle("/api/stats/team", auth(http.HandlerFunc(api.handleTeamStats)))
	mux.Handle("/api/stats/recent-prs", auth(http.HandlerFunc(api.handleRecentPRs)))
	mux.Handle("/api/ticket-summaries", auth(http.HandlerFunc(api.handleTicketSummaries)))
	mux.Handle("/api/events", auth(http.HandlerFunc(api.handleGlobalEvents)))
	mux.Handle("/api/config/summary", auth(http.HandlerFunc(api.handleConfigSummary)))
	mux.Handle("/api/usage/activity", auth(http.HandlerFunc(api.handleActivityBreakdown)))
	mux.Handle("/api/usage/claude-code", auth(http.HandlerFunc(api.handleClaudeCodeUsage)))
	mux.Handle("/api/tasks/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/context") {
			api.handleTaskContext(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/retry") {
			api.handleRetryTask(w, r)
			return
		}
		http.NotFound(w, r)
	})))
	mux.Handle("/api/prompts/versions", auth(http.HandlerFunc(api.handlePromptVersions)))
	mux.Handle("/api/commands", auth(http.HandlerFunc(api.handleListCommands)))
	mux.Handle("/api/commands/", auth(http.HandlerFunc(api.handleRenderCommand)))

	// Multi-project API routes
	mux.Handle("/api/projects", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleListProjects(w, r)
		case http.MethodPost:
			api.handleCreateProject(w, r)
		default:
			http.NotFound(w, r)
		}
	})))
	mux.Handle("/api/projects/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Determine if this is a project-only path (no sub-resource) or a nested one.
		// Strip /api/projects/ prefix and check remaining segments.
		rest := strings.TrimPrefix(path, "/api/projects/")
		parts := strings.SplitN(rest, "/", 3)
		// parts[0] = pid, parts[1] = sub-resource (optional), parts[2] = id or further (optional)

		switch {
		// POST /api/projects/test-connection
		case parts[0] == "test-connection" && r.Method == http.MethodPost:
			api.handleTestConnection(w, r)
		// DELETE /api/projects/{pid}
		case r.Method == http.MethodDelete && len(parts) == 1:
			api.handleDeleteProject(w, r)
		// GET /api/projects/{pid}
		case r.Method == http.MethodGet && len(parts) == 1:
			api.handleGetProject(w, r)
		// PUT /api/projects/{pid}
		case r.Method == http.MethodPut && len(parts) == 1:
			api.handleUpdateProject(w, r)
		// GET /api/projects/{pid}/tickets
		case len(parts) >= 2 && parts[1] == "tickets" && len(parts) == 2:
			api.handleProjectTickets(w, r)
		// /api/projects/{pid}/tickets/{id}/...
		case len(parts) >= 2 && parts[1] == "tickets" && len(parts) == 3:
			ticketRest := parts[2] // "{id}" or "{id}/tasks" etc.
			ticketParts := strings.SplitN(ticketRest, "/", 2)
			if len(ticketParts) == 1 {
				// GET or DELETE /api/projects/{pid}/tickets/{id}
				if r.Method == http.MethodDelete {
					api.handleProjectDeleteTicket(w, r)
				} else {
					api.handleProjectTicketDetail(w, r)
				}
			} else {
				switch ticketParts[1] {
				case "tasks":
					api.handleProjectTasks(w, r)
				case "llm-calls":
					api.handleProjectLlmCalls(w, r)
				case "events":
					api.handleProjectEvents(w, r)
				case "chat":
					if r.Method == http.MethodGet {
						api.handleProjectGetChat(w, r)
					} else if r.Method == http.MethodPost {
						api.handleProjectPostChat(w, r)
					} else {
						http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					}
				case "retry":
					api.handleRetryTicket(w, r)
				default:
					http.NotFound(w, r)
				}
			}
		// GET /api/projects/{pid}/ticket-summaries
		case len(parts) == 2 && parts[1] == "ticket-summaries":
			api.handleProjectTicketSummaries(w, r)
		// GET /api/projects/{pid}/events
		case len(parts) == 2 && parts[1] == "events":
			api.handleProjectGlobalEvents(w, r)
		// GET /api/projects/{pid}/costs/today|month|week (plural)
		case len(parts) >= 2 && parts[1] == "costs":
			sub := ""
			if len(parts) == 3 {
				sub = parts[2]
			}
			switch sub {
			case "today":
				api.handleProjectCostsToday(w, r)
			case "month":
				api.handleProjectCostsMonth(w, r)
			case "week":
				api.handleProjectCostsWeek(w, r)
			default:
				http.NotFound(w, r)
			}
		// GET /api/projects/{pid}/cost/daily/{date} (singular, legacy)
		case len(parts) >= 2 && parts[1] == "cost":
		case len(parts) >= 2 && parts[1] == "cost":
			costRest := ""
			if len(parts) == 3 {
				costRest = parts[2]
			}
			costParts := strings.SplitN(costRest, "/", 2)
			switch {
			case costRest == "" || costParts[0] == "breakdown":
				// GET /api/projects/{pid}/cost/breakdown — return today + month summary
				projDB, err := api.projectDB(r)
				if err != nil {
					writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
				date := time.Now().Format("2006-01-02")
				yearMonth := time.Now().Format("2006-01")
				daily, _ := projDB.GetDailyCost(r.Context(), date)
				monthly, _ := projDB.GetMonthlyCost(r.Context(), yearMonth)
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"daily_usd":   daily,
					"monthly_usd": monthly,
					"date":        date,
					"month":       yearMonth,
				})
			case costParts[0] == "daily" && len(costParts) == 2:
				api.handleProjectDailyCost(w, r)
			case costParts[0] == "monthly" && len(costParts) == 2:
				api.handleProjectMonthlyCost(w, r)
			default:
				http.NotFound(w, r)
			}
		// POST /api/projects/{pid}/sync
		case len(parts) == 2 && parts[1] == "sync" && r.Method == http.MethodPost:
			api.handleProjectSync(w, r)
		// POST /api/projects/{pid}/pause
		case len(parts) == 2 && parts[1] == "pause" && r.Method == http.MethodPost:
			api.handleProjectPause(w, r)
		// POST /api/projects/{pid}/resume
		case len(parts) == 2 && parts[1] == "resume" && r.Method == http.MethodPost:
			api.handleProjectResume(w, r)
		// GET /api/projects/{pid}/health
		case len(parts) == 2 && parts[1] == "health":
			api.handleProjectHealth(w, r)
		// GET /api/projects/{pid}/dashboard
		case len(parts) == 2 && parts[1] == "dashboard":
			api.handleProjectDashboard(w, r)
		default:
			http.NotFound(w, r)
		}
	})))
	mux.Handle("/api/overview", auth(http.HandlerFunc(api.handleOverview)))

	// Metrics endpoint
	if reg != nil {
		mux.Handle("/api/metrics", auth(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
	}

	// WebSocket (auth via token query param)
	mux.HandleFunc("/ws/events", api.handleWebSocket)
	mux.HandleFunc("/ws/global", api.handleGlobalWebSocket)
	mux.HandleFunc("/ws/projects/", api.handleProjectWebSocket)

	// Static frontend files embedded at build time
	webContent, err := fs.Sub(webFS, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load embedded web assets")
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	return &Server{
		api: api,
		db:  db,
		reg: reg,
		server: &http.Server{
			Addr:         addr,
			Handler:      limitRequestBody(mux),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
}

// SetDaemonController wires pause/resume controls to the daemon.
func (s *Server) SetDaemonController(c DaemonController) {
	s.api.SetDaemonController(c)
}

// SetChannelHealth registers a channel health checker for the status endpoint.
func (s *Server) SetChannelHealth(name string, h interface{ IsConnected() bool }) {
	s.api.SetChannelHealth(name, h)
}

// SetMCPHealthProvider wires a provider for MCP server health into the status endpoint.
func (s *Server) SetMCPHealthProvider(p MCPHealthProvider) {
	s.api.SetMCPHealthProvider(p)
}

// SetPromptSnapshotQuerier wires the prompt snapshot querier for GET /api/prompts/versions.
func (s *Server) SetPromptSnapshotQuerier(q PromptSnapshotQuerier) {
	s.api.SetPromptSnapshotQuerier(q)
}

// SetTrackerSyncer wires a TrackerSyncer for the POST /api/daemon/sync endpoint.
func (s *Server) SetTrackerSyncer(syncer TrackerSyncer) {
	s.api.SetTrackerSyncer(syncer)
}

// SetConfigProvider wires a ConfigProvider for the config summary endpoint.
func (s *Server) SetConfigProvider(p ConfigProvider) {
	s.api.SetConfigProvider(p)
}

// SetCommandRegistry wires a command registry for the commands endpoints.
func (s *Server) SetCommandRegistry(r *command.Registry) {
	s.api.SetCommandRegistry(r)
}

// ProjectRegistry provides access to project workers for the multi-project API.
type ProjectRegistry interface {
	ListProjects() ([]project.IndexEntry, error)
	GetWorker(id string) (*project.Worker, bool)
	GetProject(id string) (*project.ProjectConfig, string, error)
	CreateProject(cfg *project.ProjectConfig) (string, error)
	UpdateProject(id string, cfg *project.ProjectConfig) error
	DeleteProject(id string) error
}

// SetProjectRegistry wires the project manager for project-scoped API endpoints.
func (s *Server) SetProjectRegistry(r ProjectRegistry) {
	s.api.SetProjectRegistry(r)
}

// SetGlobalEmitter wires a global event emitter for the /ws/global WebSocket endpoint.
func (s *Server) SetGlobalEmitter(e EventSubscriber) {
	s.api.SetGlobalEmitter(e)
}

// Handler returns the HTTP handler, useful for testing with httptest.NewServer.
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}

// Start begins listening for HTTP connections. Blocks until the server stops.
func (s *Server) Start() error {
	log.Info().Str("addr", s.server.Addr).Msg("Dashboard server starting")
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
