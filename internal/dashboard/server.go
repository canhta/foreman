package dashboard

import (
	"context"
	"embed"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

//go:embed web
var webFS embed.FS

// Server is the HTTP server for the Foreman dashboard.
type Server struct {
	api    *API
	db     DashboardDB
	reg    *prometheus.Registry
	server *http.Server
}

// NewServer creates a new dashboard Server and registers all HTTP routes.
func NewServer(db DashboardDB, emitter EventSubscriber, reg *prometheus.Registry, version, host string, port int) *Server {
	api := NewAPI(db, emitter, version)

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
		default:
			api.handleGetTicket(w, r)
		}
	})))
	mux.Handle("/api/costs/today", auth(http.HandlerFunc(api.handleCostsToday)))
	mux.Handle("/api/pipeline/active", auth(http.HandlerFunc(api.handleActivePipelines)))
	mux.Handle("/api/costs/week", auth(http.HandlerFunc(api.handleCostsWeek)))
	mux.Handle("/api/costs/month", auth(http.HandlerFunc(api.handleCostsMonth)))
	mux.Handle("/api/costs/budgets", auth(http.HandlerFunc(api.handleCostsBudgets)))
	mux.Handle("/api/daemon/pause", auth(http.HandlerFunc(api.handleDaemonPause)))
	mux.Handle("/api/daemon/resume", auth(http.HandlerFunc(api.handleDaemonResume)))

	// Metrics endpoint (no auth — Prometheus scraper)
	if reg != nil {
		mux.Handle("/api/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	}

	// WebSocket (auth via token query param)
	mux.HandleFunc("/ws/events", api.handleWebSocket)

	// Static frontend files embedded at build time
	webContent, err := fs.Sub(webFS, "web")
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
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
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
