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

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
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

	// Metrics endpoint
	if reg != nil {
		mux.Handle("/api/metrics", auth(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
	}

	// WebSocket (auth via token query param)
	mux.HandleFunc("/ws/events", api.handleWebSocket)

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
