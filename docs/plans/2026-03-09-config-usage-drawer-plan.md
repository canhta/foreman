# Config & Usage Drawer + Real-time Enrichments — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a settings drawer to the dashboard showing config summary and usage attribution, enrich real-time events with runner/model info, and add a `foreman config` CLI command.

**Architecture:** Three new backend endpoints (config summary, activity breakdown, Claude Code usage), a new frontend drawer component with two tabs, enriched WebSocket events with runner/model fields, and a new CLI command. Config is passed to the dashboard API via the existing `SetX` pattern.

**Tech Stack:** Go (backend API, CLI), Svelte 5 + TypeScript (frontend), SQLite/PostgreSQL (aggregation queries)

---

### Task 1: Pass full config to Dashboard API

**Files:**
- Modify: `internal/dashboard/api.go` (lines 101-115, 151-159)
- Modify: `internal/dashboard/server.go` (line 85-86)
- Modify: `cmd/start.go` (line 433)
- Modify: `cmd/dashboard.go` (line 55)
- Modify: `tests/integration/dashboard_test.go` (lines 45, 65, 89, 114)

**Step 1: Add config field to API struct**

In `internal/dashboard/api.go`, add a `ConfigProvider` interface and field:

```go
// ConfigProvider supplies the active configuration for the dashboard.
type ConfigProvider interface {
	GetConfig() *models.Config
}
```

Add to the `API` struct:

```go
type API struct {
	// ... existing fields ...
	configProvider ConfigProvider
}
```

Add setter method:

```go
func (a *API) SetConfigProvider(p ConfigProvider) {
	a.configProvider = p
}
```

**Step 2: Wire config provider in server.go**

In `internal/dashboard/server.go`, add a `SetConfigProvider` method to `Server`:

```go
func (s *Server) SetConfigProvider(p ConfigProvider) {
	s.api.SetConfigProvider(p)
}
```

**Step 3: Create a simple config provider wrapper**

In `cmd/start.go` and `cmd/dashboard.go`, after creating the server, wire the config:

```go
// Simple ConfigProvider that wraps a *models.Config
type staticConfigProvider struct{ cfg *models.Config }
func (s *staticConfigProvider) GetConfig() *models.Config { return s.cfg }
```

Add after `dashboard.NewServer(...)`:

```go
srv.SetConfigProvider(&staticConfigProvider{cfg: cfg})
```

Do the same in `cmd/dashboard.go`.

**Step 4: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go cmd/start.go cmd/dashboard.go
git commit -m "feat(dashboard): add ConfigProvider interface for config summary endpoint"
```

---

### Task 2: Config summary API endpoint

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`

**Step 1: Add the config summary response struct and handler**

In `internal/dashboard/api.go`, add:

```go
// configSummary is the JSON response for GET /api/config/summary.
type configSummary struct {
	LLM         configLLM         `json:"llm"`
	Tracker     configTracker     `json:"tracker"`
	Git         configGit         `json:"git"`
	AgentRunner configAgentRunner `json:"agent_runner"`
	Cost        configCost        `json:"cost"`
	Daemon      configDaemon      `json:"daemon"`
	Database    configDatabase    `json:"database"`
	MCP         configMCP         `json:"mcp"`
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
	TurnLimit   int    `json:"turn_limit"`
	TokenBudget int    `json:"token_budget"`
}

type configCost struct {
	DailyBudget     float64 `json:"daily_budget"`
	MonthlyBudget   float64 `json:"monthly_budget"`
	PerTicketBudget float64 `json:"per_ticket_budget"`
	AlertThreshold  float64 `json:"alert_threshold"`
}

type configDaemon struct {
	MaxParallelTickets int    `json:"max_parallel_tickets"`
	MaxParallelTasks   int    `json:"max_parallel_tasks"`
	WorkDir            string `json:"work_dir"`
	LogLevel           string `json:"log_level"`
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

// redactKey returns a redacted version of an API key, showing only the last 4 characters.
func redactKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	prefix := key[:7]
	if len(prefix) > 7 {
		prefix = key[:7]
	}
	return prefix + "..." + key[len(key)-4:]
}

func (a *API) handleConfigSummary(w http.ResponseWriter, r *http.Request) {
	if a.configProvider == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	cfg := a.configProvider.GetConfig()

	// Build models map from config
	models := map[string]string{
		"planner":          cfg.Models.Planner,
		"implementer":      cfg.Models.Implementer,
		"spec_reviewer":    cfg.Models.SpecReviewer,
		"quality_reviewer": cfg.Models.QualityReviewer,
		"final_reviewer":   cfg.Models.FinalReviewer,
	}

	// Determine API key to show (redacted)
	apiKey := ""
	switch cfg.LLM.DefaultProvider {
	case "anthropic":
		apiKey = redactKey(cfg.LLM.Anthropic.APIKey)
	case "openai":
		apiKey = redactKey(cfg.LLM.OpenAI.APIKey)
	case "openrouter":
		apiKey = redactKey(cfg.LLM.OpenRouter.APIKey)
	}

	// Determine DB path
	dbPath := cfg.Database.SQLite.Path
	if cfg.Database.Driver == "postgres" {
		dbPath = redactKey(cfg.Database.Postgres.URL)
	}

	// Collect MCP server names
	mcpServers := make([]string, 0, len(cfg.MCP.Servers))
	for _, s := range cfg.MCP.Servers {
		mcpServers = append(mcpServers, s.Name)
	}

	summary := configSummary{
		LLM: configLLM{
			Provider: cfg.LLM.DefaultProvider,
			Models:   models,
			APIKey:   apiKey,
		},
		Tracker: configTracker{
			Provider:     cfg.Tracker.Provider,
			PollInterval: cfg.Daemon.PollInterval.String(),
		},
		Git: configGit{
			Provider:     cfg.Git.Provider,
			CloneURL:     cfg.Git.CloneURL,
			BranchPrefix: cfg.Git.BranchPrefix,
			AutoMerge:    cfg.Git.AutoMerge,
		},
		AgentRunner: configAgentRunner{
			Provider:    cfg.AgentRunner.Provider,
			TurnLimit:   cfg.AgentRunner.TurnLimit,
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
			WorkDir:            cfg.Daemon.WorkDir,
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
```

**Step 2: Register the route**

In `internal/dashboard/server.go`, add after the existing `/api/events` route:

```go
mux.Handle("/api/config/summary", auth(http.HandlerFunc(api.handleConfigSummary)))
```

**Step 3: Run tests**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./...
```

**Step 4: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go
git commit -m "feat(dashboard): add GET /api/config/summary endpoint with redacted config"
```

---

### Task 3: Activity breakdown API endpoint

**Files:**
- Modify: `internal/db/db.go` (add interface method)
- Modify: `internal/db/sqlite.go` (implement)
- Modify: `internal/db/postgres.go` (implement)
- Modify: `internal/dashboard/api.go` (add handler + DashboardDB method)
- Modify: `internal/dashboard/server.go` (register route)

**Step 1: Define the aggregate types**

In `internal/dashboard/api.go`, add response structs:

```go
type activityBreakdown struct {
	ByRunner    []runnerStat    `json:"by_runner"`
	ByModel     []modelStat     `json:"by_model"`
	ByRole      []roleStat      `json:"by_role"`
	RecentCalls []recentLlmCall `json:"recent_calls"`
}

type runnerStat struct {
	Runner   string  `json:"runner"`
	Calls    int     `json:"calls"`
	TokensIn int64   `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
	CostUSD  float64 `json:"cost_usd"`
}

type modelStat struct {
	Model    string  `json:"model"`
	Calls    int     `json:"calls"`
	TokensIn int64   `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
	CostUSD  float64 `json:"cost_usd"`
}

type roleStat struct {
	Role    string  `json:"role"`
	Runner  string  `json:"runner"`
	Model   string  `json:"model"`
	Calls   int     `json:"calls"`
	CostUSD float64 `json:"cost_usd"`
}

type recentLlmCall struct {
	TicketID    string  `json:"ticket_id"`
	TicketTitle string  `json:"ticket_title"`
	TaskTitle   string  `json:"task_title"`
	Role        string  `json:"role"`
	Runner      string  `json:"runner"`
	Model       string  `json:"model"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	CostUSD     float64 `json:"cost_usd"`
	Status      string  `json:"status"`
	DurationMs  int     `json:"duration_ms"`
	Timestamp   string  `json:"timestamp"`
}
```

**Step 2: Add DB interface method**

In `internal/dashboard/api.go`, add to `DashboardDB` interface:

```go
GetLlmCallAggregates(ctx context.Context, since time.Time) (byRunner []db.RunnerAggregate, byModel []db.ModelAggregate, byRole []db.RoleAggregate, err error)
GetRecentLlmCalls(ctx context.Context, limit int) ([]db.RecentLlmCall, error)
```

**Step 3: Define aggregate types in db package**

In `internal/db/db.go`, add:

```go
type RunnerAggregate struct {
	Runner    string
	Calls     int
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
}

type ModelAggregate struct {
	Model     string
	Calls     int
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
}

type RoleAggregate struct {
	Role    string
	Runner  string
	Model   string
	Calls   int
	CostUSD float64
}

type RecentLlmCall struct {
	TicketID    string
	TicketTitle string
	TaskTitle   string
	Role        string
	Runner      string
	Model       string
	TokensIn    int
	TokensOut   int
	CostUSD     float64
	Status      string
	DurationMs  int
	CreatedAt   time.Time
}
```

**Step 4: Implement in SQLite**

In `internal/db/sqlite.go`, add:

```go
func (s *SQLiteDB) GetLlmCallAggregates(ctx context.Context, since time.Time) ([]RunnerAggregate, []ModelAggregate, []RoleAggregate, error) {
	sinceStr := since.Format("2006-01-02T15:04:05Z")

	// By runner
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(agent_runner, 'builtin'), COUNT(*), COALESCE(SUM(tokens_input), 0), COALESCE(SUM(tokens_output), 0), COALESCE(SUM(cost_usd), 0)
		 FROM llm_calls WHERE created_at >= ? GROUP BY COALESCE(agent_runner, 'builtin') ORDER BY SUM(cost_usd) DESC`, sinceStr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate by runner: %w", err)
	}
	defer rows.Close()
	var byRunner []RunnerAggregate
	for rows.Next() {
		var r RunnerAggregate
		if err := rows.Scan(&r.Runner, &r.Calls, &r.TokensIn, &r.TokensOut, &r.CostUSD); err != nil {
			return nil, nil, nil, fmt.Errorf("scan runner row: %w", err)
		}
		byRunner = append(byRunner, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	// By model
	rows2, err := s.db.QueryContext(ctx,
		`SELECT model, COUNT(*), COALESCE(SUM(tokens_input), 0), COALESCE(SUM(tokens_output), 0), COALESCE(SUM(cost_usd), 0)
		 FROM llm_calls WHERE created_at >= ? GROUP BY model ORDER BY SUM(cost_usd) DESC`, sinceStr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate by model: %w", err)
	}
	defer rows2.Close()
	var byModel []ModelAggregate
	for rows2.Next() {
		var m ModelAggregate
		if err := rows2.Scan(&m.Model, &m.Calls, &m.TokensIn, &m.TokensOut, &m.CostUSD); err != nil {
			return nil, nil, nil, fmt.Errorf("scan model row: %w", err)
		}
		byModel = append(byModel, m)
	}
	if err := rows2.Err(); err != nil {
		return nil, nil, nil, err
	}

	// By role (with runner + model)
	rows3, err := s.db.QueryContext(ctx,
		`SELECT role, COALESCE(agent_runner, 'builtin'), model, COUNT(*), COALESCE(SUM(cost_usd), 0)
		 FROM llm_calls WHERE created_at >= ? GROUP BY role, COALESCE(agent_runner, 'builtin'), model ORDER BY SUM(cost_usd) DESC`, sinceStr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate by role: %w", err)
	}
	defer rows3.Close()
	var byRole []RoleAggregate
	for rows3.Next() {
		var ro RoleAggregate
		if err := rows3.Scan(&ro.Role, &ro.Runner, &ro.Model, &ro.Calls, &ro.CostUSD); err != nil {
			return nil, nil, nil, fmt.Errorf("scan role row: %w", err)
		}
		byRole = append(byRole, ro)
	}
	if err := rows3.Err(); err != nil {
		return nil, nil, nil, err
	}

	return byRunner, byModel, byRole, nil
}

func (s *SQLiteDB) GetRecentLlmCalls(ctx context.Context, limit int) ([]RecentLlmCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.ticket_id, COALESCE(t.title, ''), COALESCE(tk.title, ''),
		        c.role, COALESCE(c.agent_runner, 'builtin'), c.model,
		        c.tokens_input, c.tokens_output, c.cost_usd, c.status, c.duration_ms, c.created_at
		 FROM llm_calls c
		 LEFT JOIN tickets t ON t.id = c.ticket_id
		 LEFT JOIN tasks tk ON tk.id = c.task_id
		 ORDER BY c.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent llm calls: %w", err)
	}
	defer rows.Close()

	var calls []RecentLlmCall
	for rows.Next() {
		var c RecentLlmCall
		if err := rows.Scan(&c.TicketID, &c.TicketTitle, &c.TaskTitle,
			&c.Role, &c.Runner, &c.Model,
			&c.TokensIn, &c.TokensOut, &c.CostUSD, &c.Status, &c.DurationMs, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent call: %w", err)
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}
```

**Step 5: Implement in PostgreSQL**

Same logic as SQLite but use `$1`, `$2` param syntax and `COALESCE(agent_runner, 'builtin')`.

**Step 6: Add handler**

In `internal/dashboard/api.go`:

```go
func (a *API) handleActivityBreakdown(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}

	since := time.Now().AddDate(0, 0, -days)
	byRunner, byModel, byRole, err := a.db.GetLlmCallAggregates(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	recent, err := a.db.GetRecentLlmCalls(r.Context(), 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Map to response types
	resp := activityBreakdown{
		ByRunner:    make([]runnerStat, len(byRunner)),
		ByModel:     make([]modelStat, len(byModel)),
		ByRole:      make([]roleStat, len(byRole)),
		RecentCalls: make([]recentLlmCall, len(recent)),
	}

	for i, r := range byRunner {
		resp.ByRunner[i] = runnerStat{Runner: r.Runner, Calls: r.Calls, TokensIn: r.TokensIn, TokensOut: r.TokensOut, CostUSD: r.CostUSD}
	}
	for i, m := range byModel {
		resp.ByModel[i] = modelStat{Model: m.Model, Calls: m.Calls, TokensIn: m.TokensIn, TokensOut: m.TokensOut, CostUSD: m.CostUSD}
	}
	for i, ro := range byRole {
		resp.ByRole[i] = roleStat{Role: ro.Role, Runner: ro.Runner, Model: ro.Model, Calls: ro.Calls, CostUSD: ro.CostUSD}
	}
	for i, c := range recent {
		resp.RecentCalls[i] = recentLlmCall{
			TicketID: c.TicketID, TicketTitle: c.TicketTitle, TaskTitle: c.TaskTitle,
			Role: c.Role, Runner: c.Runner, Model: c.Model,
			TokensIn: c.TokensIn, TokensOut: c.TokensOut, CostUSD: c.CostUSD,
			Status: c.Status, DurationMs: c.DurationMs, Timestamp: c.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
```

**Step 7: Register route**

In `internal/dashboard/server.go`:

```go
mux.Handle("/api/usage/activity", auth(http.HandlerFunc(api.handleActivityBreakdown)))
```

**Step 8: Build and test**

```bash
go build ./...
```

**Step 9: Commit**

```bash
git add internal/db/db.go internal/db/sqlite.go internal/db/postgres.go internal/dashboard/api.go internal/dashboard/server.go
git commit -m "feat(dashboard): add GET /api/usage/activity endpoint for LLM call attribution"
```

---

### Task 4: Claude Code usage API endpoint

**Files:**
- Create: `internal/dashboard/claudecode.go`
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`

**Step 1: Create the Claude Code JSONL parser**

Create `internal/dashboard/claudecode.go`:

```go
package dashboard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type claudeCodeUsage struct {
	Available    bool                  `json:"available"`
	Today        *claudeCodeDaySummary `json:"today,omitempty"`
	Last7Days    []claudeCodeDay       `json:"last_7_days,omitempty"`
	TotalSessions int                  `json:"total_sessions,omitempty"`
}

type claudeCodeDaySummary struct {
	Sessions        int     `json:"sessions"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type claudeCodeDay struct {
	Date         string  `json:"date"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// jsonlMessage represents a single message in a Claude Code JSONL session file.
type jsonlMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`
	// Usage fields can appear at the top level or nested
	Usage *jsonlUsage `json:"usage,omitempty"`
}

type jsonlUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// Sonnet 4.6 pricing per million tokens (default assumption)
const (
	sonnetInputPricePerM  = 3.0
	sonnetOutputPricePerM = 15.0
	sonnetCachePricePerM  = 0.30
)

func estimateCost(inputTokens, outputTokens, cacheReadTokens int64) float64 {
	cost := float64(inputTokens) / 1_000_000 * sonnetInputPricePerM
	cost += float64(outputTokens) / 1_000_000 * sonnetOutputPricePerM
	cost += float64(cacheReadTokens) / 1_000_000 * sonnetCachePricePerM
	return cost
}

func parseClaudeCodeUsage() claudeCodeUsage {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeCodeUsage{Available: false}
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return claudeCodeUsage{Available: false}
	}

	// Collect all JSONL files modified in last 7 days
	cutoff := time.Now().AddDate(0, 0, -7)
	var sessionFiles []string

	_ = filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") && info.ModTime().After(cutoff) {
			sessionFiles = append(sessionFiles, path)
		}
		return nil
	})

	if len(sessionFiles) == 0 {
		return claudeCodeUsage{Available: false}
	}

	// Aggregate by date
	dailyStats := make(map[string]*claudeCodeDaySummary)
	totalSessions := 0

	for _, path := range sessionFiles {
		totalSessions++
		info, _ := os.Stat(path)
		dateKey := info.ModTime().Format("2006-01-02")

		day, exists := dailyStats[dateKey]
		if !exists {
			day = &claudeCodeDaySummary{}
			dailyStats[dateKey] = day
		}
		day.Sessions++

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
		for scanner.Scan() {
			var msg jsonlMessage
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				continue
			}
			if msg.Usage != nil {
				day.InputTokens += msg.Usage.InputTokens
				day.OutputTokens += msg.Usage.OutputTokens
				day.CacheReadTokens += msg.Usage.CacheReadInputTokens
			}
		}
		f.Close()
	}

	// Build response
	today := time.Now().Format("2006-01-02")
	result := claudeCodeUsage{
		Available:     true,
		TotalSessions: totalSessions,
	}

	if todayStats, ok := dailyStats[today]; ok {
		todayStats.EstimatedCostUSD = estimateCost(todayStats.InputTokens, todayStats.OutputTokens, todayStats.CacheReadTokens)
		result.Today = todayStats
	}

	// Sort days descending
	var days []claudeCodeDay
	for date, stats := range dailyStats {
		days = append(days, claudeCodeDay{
			Date:         date,
			InputTokens:  stats.InputTokens,
			OutputTokens: stats.OutputTokens,
			CostUSD:      estimateCost(stats.InputTokens, stats.OutputTokens, stats.CacheReadTokens),
		})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date > days[j].Date })
	result.Last7Days = days

	return result
}
```

**Step 2: Add handler**

In `internal/dashboard/api.go`:

```go
func (a *API) handleClaudeCodeUsage(w http.ResponseWriter, _ *http.Request) {
	usage := parseClaudeCodeUsage()
	writeJSON(w, http.StatusOK, usage)
}
```

**Step 3: Register route**

In `internal/dashboard/server.go`:

```go
mux.Handle("/api/usage/claude-code", auth(http.HandlerFunc(api.handleClaudeCodeUsage)))
```

**Step 4: Build**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add internal/dashboard/claudecode.go internal/dashboard/api.go internal/dashboard/server.go
git commit -m "feat(dashboard): add GET /api/usage/claude-code endpoint parsing local JSONL files"
```

---

### Task 5: Enrich WebSocket events with runner and model

**Files:**
- Modify: `internal/dashboard/ws.go`

**Step 1: Add runner and model fields to enrichedEvent**

In `internal/dashboard/ws.go`, update the struct:

```go
type enrichedEvent struct {
	models.EventRecord
	TicketTitle string `json:"ticket_title,omitempty"`
	Submitter   string `json:"submitter,omitempty"`
	Runner      string `json:"runner,omitempty"`
	Model       string `json:"model,omitempty"`
}
```

**Step 2: Extract runner/model from event Details**

Update `enrichEvent` function to parse `Details` JSON and extract `runner`/`model` fields when present:

```go
func (a *API) enrichEvent(ctx context.Context, evt *models.EventRecord) *enrichedEvent {
	enriched := &enrichedEvent{EventRecord: *evt}
	if evt.TicketID != "" {
		ticket, err := a.db.GetTicket(ctx, evt.TicketID)
		if err == nil && ticket != nil {
			enriched.TicketTitle = ticket.Title
			enriched.Submitter = ticket.ChannelSenderID
		}
	}

	// Extract runner and model from Details JSON if present
	if evt.Details != "" {
		var details map[string]interface{}
		if err := json.Unmarshal([]byte(evt.Details), &details); err == nil {
			if r, ok := details["runner"].(string); ok {
				enriched.Runner = r
			}
			if m, ok := details["model"].(string); ok {
				enriched.Model = m
			}
		}
	}

	return enriched
}
```

**Step 3: Build**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add internal/dashboard/ws.go
git commit -m "feat(dashboard): enrich WebSocket events with runner and model from Details"
```

---

### Task 6: Frontend types and state

**Files:**
- Modify: `internal/dashboard/web/src/types.ts`
- Modify: `internal/dashboard/web/src/state.svelte.ts`

**Step 1: Add new types**

In `internal/dashboard/web/src/types.ts`, add at the end:

```typescript
export interface ConfigSummary {
  llm: { provider: string; models: Record<string, string>; api_key: string };
  tracker: { provider: string; poll_interval: string };
  git: { provider: string; clone_url: string; branch_prefix: string; auto_merge: boolean };
  agent_runner: { provider: string; turn_limit: number; token_budget: number };
  cost: { daily_budget: number; monthly_budget: number; per_ticket_budget: number; alert_threshold: number };
  daemon: { max_parallel_tickets: number; max_parallel_tasks: number; work_dir: string; log_level: string };
  database: { driver: string; path: string };
  mcp: { servers: string[] };
  rate_limit: { requests_per_minute: number };
}

export interface ClaudeCodeUsage {
  available: boolean;
  today?: { sessions: number; input_tokens: number; output_tokens: number; cache_read_tokens: number; estimated_cost_usd: number };
  last_7_days?: { date: string; input_tokens: number; output_tokens: number; cost_usd: number }[];
  total_sessions?: number;
}

export interface ActivityBreakdown {
  by_runner: { runner: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_model: { model: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_role: { role: string; runner: string; model: string; calls: number; cost_usd: number }[];
  recent_calls: {
    ticket_id: string; ticket_title: string; task_title: string;
    role: string; runner: string; model: string;
    tokens_in: number; tokens_out: number; cost_usd: number;
    status: string; duration_ms: number; timestamp: string;
  }[];
}
```

**Step 2: Update EventRecord**

Add `runner` and `model` fields to the existing `EventRecord` interface:

```typescript
export interface EventRecord {
  // ... existing fields ...
  runner?: string;
  model?: string;
}
```

**Step 3: Add state and actions**

In `internal/dashboard/web/src/state.svelte.ts`, add to `AppState` class:

```typescript
  settingsOpen = $state(false);
  settingsTab = $state<'config' | 'usage'>('config');
  configSummary = $state<ConfigSummary | null>(null);
  claudeCodeUsage = $state<ClaudeCodeUsage | null>(null);
  activityBreakdown = $state<ActivityBreakdown | null>(null);

  // Live task progress from WebSocket events
  activeTaskProgress = $state<Record<string, {
    turn: number;
    maxTurns: number;
    tokensUsed: number;
    runner: string;
    model: string;
    lastTool?: string;
    lastToolTime?: string;
  }>>({});
```

Add imports at the top:

```typescript
import type {
  Ticket, TicketSummary, Task, EventRecord, LlmCallRecord,
  TeamStat, DayCost, StatusResponse,
  ConfigSummary, ClaudeCodeUsage, ActivityBreakdown,
} from './types';
```

Add functions after existing exports:

```typescript
export async function fetchConfigSummary(): Promise<void> {
  try {
    appState.configSummary = await fetchJSON<ConfigSummary>('/api/config/summary');
  } catch { /* ignore */ }
}

export async function fetchClaudeCodeUsage(): Promise<void> {
  try {
    appState.claudeCodeUsage = await fetchJSON<ClaudeCodeUsage>('/api/usage/claude-code');
  } catch { /* ignore */ }
}

export async function fetchActivityBreakdown(): Promise<void> {
  try {
    appState.activityBreakdown = await fetchJSON<ActivityBreakdown>('/api/usage/activity');
  } catch { /* ignore */ }
}

export function openSettings() {
  appState.settingsOpen = true;
  fetchConfigSummary();
  fetchClaudeCodeUsage();
  fetchActivityBreakdown();
}

export function closeSettings() {
  appState.settingsOpen = false;
}
```

**Step 4: Enrich WebSocket handler with task progress tracking**

In the `ws.onmessage` handler inside `connectWebSocket()`, add after `evt.isNew = true;`:

```typescript
    // Track active task progress from agent events
    if (evt.TaskID && evt.Details) {
      try {
        const details = JSON.parse(evt.Details);
        if (evt.EventType === 'agent_turn_start') {
          appState.activeTaskProgress[evt.TaskID] = {
            ...(appState.activeTaskProgress[evt.TaskID] || {}),
            turn: details.turn_number || 0,
            maxTurns: details.max_turns || 50,
            tokensUsed: details.tokens_used || 0,
            runner: evt.runner || details.runner || '',
            model: evt.model || details.model || '',
          };
        }
        if (evt.EventType === 'agent_tool_end') {
          const existing = appState.activeTaskProgress[evt.TaskID];
          if (existing) {
            existing.lastTool = details.tool_name;
            existing.lastToolTime = evt.CreatedAt;
          }
        }
        if (evt.EventType === 'task_completed' || evt.EventType === 'task_failed') {
          delete appState.activeTaskProgress[evt.TaskID];
        }
      } catch { /* ignore parse errors */ }
    }
```

**Step 5: Commit**

```bash
git add internal/dashboard/web/src/types.ts internal/dashboard/web/src/state.svelte.ts
git commit -m "feat(dashboard): add frontend types, state, and actions for settings drawer"
```

---

### Task 7: SettingsDrawer component — Config tab

**Files:**
- Create: `internal/dashboard/web/src/components/SettingsDrawer.svelte`

**Step 1: Create the SettingsDrawer component**

Create `internal/dashboard/web/src/components/SettingsDrawer.svelte`. The component renders a right-side slide-out drawer with two tabs. Start with Config tab only.

Follow existing component patterns:
- Import from `../state.svelte` and `../format`
- Use `$derived` for computed values
- Match the brutalism design tokens from `app.css`
- Section panels match SystemHealth pattern: `border-2 border-border`, header with `border-l-4 border-l-accent bg-surface-active`
- Tab bar: active tab uses `bg-accent text-bg`, inactive uses `text-muted hover:text-text`
- Key-value rows: label `text-[10px] text-muted tracking-wider`, value `text-xs text-text`
- Drawer: `fixed right-0 top-0 h-full w-[420px] z-[60] bg-surface border-l-2 border-border`
- Backdrop: `fixed inset-0 z-[59] bg-bg/60` with click-to-dismiss
- Transition: CSS `transform: translateX(100%)` to `translateX(0)`, 150ms ease-out
- Close on Escape key

Config sections to render (from `appState.configSummary`):
1. LLM — provider, models (planner/implementer/reviewers), api_key (redacted)
2. TRACKER — provider, poll_interval
3. GIT — provider, clone_url, branch_prefix, auto_merge (ON/OFF)
4. AGENT RUNNER — provider, turn_limit, token_budget (formatted with commas)
5. COST BUDGETS — daily/monthly/per_ticket (formatted as $X.XX), alert_threshold (as %)
6. DAEMON — max_parallel_tickets, max_parallel_tasks, work_dir, log_level
7. DATABASE — driver, path
8. MCP SERVERS — list of server names
9. RATE LIMIT — requests_per_minute

Show loading state (`text-muted tracking-wider` "LOADING...") when `configSummary` is null.

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/SettingsDrawer.svelte
git commit -m "feat(dashboard): add SettingsDrawer component with Config tab"
```

---

### Task 8: SettingsDrawer component — Usage tab

**Files:**
- Modify: `internal/dashboard/web/src/components/SettingsDrawer.svelte`

**Step 1: Add Usage tab content**

Three sections:

**Foreman Costs** — reuse `appState.dailyCost`, `appState.monthlyBudget`, etc. Show budget bars matching SystemHealth `budgetPct`/`budgetBarCls` pattern. Add 7-day mini bar chart using `appState.weekDays`: fixed-height container (48px), vertical bars with `bg-accent`, proportional height to max day cost, `text-[9px]` date labels.

**Activity Breakdown** — from `appState.activityBreakdown`:
- BY RUNNER: horizontal bars, color-coded (see TaskCard `runnerBadgeCls`), showing calls + cost
- BY MODEL: horizontal bars with model names (strip `claude-` prefix for display)
- ROLE MAPPING: compact table rows: role | runner badge | model short name | cost
- RECENT CALLS: last 10, each showing time | role | runner badge | model | ticket title (clickable via `selectTicket`) | cost

**Claude Code** — from `appState.claudeCodeUsage`:
- If `available === false`: show "Claude Code CLI data not found." in `text-muted`
- If available: today's sessions + tokens + cost, then 7-day table (date | tokens in | tokens out | cost)

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/SettingsDrawer.svelte
git commit -m "feat(dashboard): add Usage tab with Foreman costs, activity breakdown, and Claude Code"
```

---

### Task 9: Wire drawer into Header and App

**Files:**
- Modify: `internal/dashboard/web/src/components/Header.svelte`
- Modify: `internal/dashboard/web/src/App.svelte`

**Step 1: Add gear icon button to Header**

In `Header.svelte`, import `openSettings` from state. Add a button between the SYNC button and SYS button in the actions div:

```svelte
<button
  class="px-3 text-muted hover:text-accent hover:bg-surface-hover transition-colors tracking-wider"
  onclick={openSettings}
  title="Settings & Usage"
  aria-label="Open settings drawer"
>CFG</button>
```

(Using text "CFG" rather than a gear emoji, matching the brutalism uppercase text button pattern of PAUSE/SYNC/SYS.)

**Step 2: Add SettingsDrawer to App.svelte**

Import `SettingsDrawer` and render it inside the authenticated view, after the `<Toasts />` component:

```svelte
<script>
  import SettingsDrawer from './components/SettingsDrawer.svelte';
</script>

<!-- After Toasts -->
<SettingsDrawer />
```

The drawer manages its own visibility via `appState.settingsOpen`.

**Step 3: Build the frontend**

```bash
cd internal/dashboard/web && npm run build
```

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/components/Header.svelte internal/dashboard/web/src/App.svelte
git commit -m "feat(dashboard): wire settings drawer into Header and App layout"
```

---

### Task 10: Enrich LiveFeed with runner/model badges

**Files:**
- Modify: `internal/dashboard/web/src/components/LiveFeed.svelte`

**Step 1: Add runner/model badges to event rows**

After the message line and before the ticket link, add a conditional row for runner and model badges when present in the event:

```svelte
{#if evt.runner || evt.model}
  <div class="pl-3.5 mt-0.5 flex items-center gap-1.5">
    {#if evt.runner}
      <span class="text-[9px] border px-1 py-0.5 leading-none {runnerBadgeCls(evt.runner)}">{evt.runner}</span>
    {/if}
    {#if evt.model}
      <span class="text-[9px] text-muted-bright">{shortModel(evt.model)}</span>
    {/if}
  </div>
{/if}
```

Add helper functions:

```typescript
function runnerBadgeCls(runner: string): string {
  if (runner === 'claudecode') return 'text-accent border-accent/40';
  if (runner === 'copilot') return 'text-purple-400 border-purple-400/40';
  return 'text-muted border-border-strong';
}

function shortModel(model: string): string {
  return model.replace('claude-', '');
}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/LiveFeed.svelte
git commit -m "feat(dashboard): show runner and model badges in LiveFeed events"
```

---

### Task 11: Enrich TaskCard with live execution progress

**Files:**
- Modify: `internal/dashboard/web/src/components/TaskCard.svelte`

**Step 1: Add live progress indicator**

Import `appState` already exists. Add a derived value for the task's live progress:

```typescript
let liveProgress = $derived(appState.activeTaskProgress[task.ID]);
```

In the expanded section, after the stats row and before the error section, add a live progress block when the task is active:

```svelte
{#if isActive && liveProgress}
  <div class="px-3 py-2 border-b border-border bg-accent-bg">
    <div class="flex items-center gap-2 text-xs">
      <span class="text-accent animate-pulse">►</span>
      <span class="text-text">TURN {liveProgress.turn}/{liveProgress.maxTurns}</span>
      {#if liveProgress.runner}
        <span class="text-[10px] border px-1 py-0.5 leading-none {runnerBadgeCls(liveProgress.runner)}">{liveProgress.runner}</span>
      {/if}
      {#if liveProgress.model}
        <span class="text-[10px] text-muted-bright">{liveProgress.model.replace('claude-', '')}</span>
      {/if}
    </div>
    {#if liveProgress.lastTool}
      <div class="text-[10px] text-muted mt-1 pl-4">
        Last tool: <span class="text-text">{liveProgress.lastTool}</span>
        {#if liveProgress.lastToolTime}
          <span class="text-muted"> {formatRelative(liveProgress.lastToolTime)}</span>
        {/if}
      </div>
    {/if}
  </div>
{/if}
```

Import `formatRelative` if not already imported.

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/TaskCard.svelte
git commit -m "feat(dashboard): show live execution progress in TaskCard for active tasks"
```

---

### Task 12: CLI `foreman config` command

**Files:**
- Create: `cmd/config.go`

**Step 1: Create the config command**

Create `cmd/config.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show active configuration (redacted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigAndDB()
			if err != nil {
				// Try config-only load (no DB needed)
				cfg2, err2 := loadConfigOnly()
				if err2 != nil {
					return err
				}
				cfg = cfg2
			}

			if jsonOutput {
				// Build summary map (same structure as API endpoint)
				summary := buildConfigSummary(cfg)
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}

			// Human-readable output
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 3, ' ', 0)

			fmt.Fprintln(w, "FOREMAN CONFIGURATION")
			fmt.Fprintln(w, strings.Repeat("=", 40))
			fmt.Fprintln(w)

			section := func(name string) { fmt.Fprintf(w, "%s\n", name) }
			row := func(label, value string) { fmt.Fprintf(w, "  %s\t%s\n", label, value) }

			section("LLM")
			row("Provider:", cfg.LLM.DefaultProvider)
			row("Planner:", cfg.Models.Planner)
			row("Implementer:", cfg.Models.Implementer)
			row("Spec Reviewer:", cfg.Models.SpecReviewer)
			row("Quality Reviewer:", cfg.Models.QualityReviewer)
			row("Final Reviewer:", cfg.Models.FinalReviewer)
			row("API Key:", redactKey(getActiveAPIKey(cfg)))
			fmt.Fprintln(w)

			section("TRACKER")
			row("Provider:", cfg.Tracker.Provider)
			row("Poll Interval:", cfg.Daemon.PollInterval.String())
			fmt.Fprintln(w)

			section("GIT")
			row("Provider:", cfg.Git.Provider)
			row("Clone URL:", cfg.Git.CloneURL)
			row("Branch Prefix:", cfg.Git.BranchPrefix)
			row("Auto Merge:", boolStr(cfg.Git.AutoMerge))
			fmt.Fprintln(w)

			section("AGENT RUNNER")
			row("Provider:", cfg.AgentRunner.Provider)
			row("Turn Limit:", fmt.Sprintf("%d", cfg.AgentRunner.TurnLimit))
			row("Token Budget:", formatNum(cfg.AgentRunner.TokenBudget))
			fmt.Fprintln(w)

			section("COST BUDGETS")
			row("Daily:", fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerDayUSD))
			row("Monthly:", fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerMonthUSD))
			row("Per Ticket:", fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerTicketUSD))
			row("Alert Threshold:", fmt.Sprintf("%.0f%%", cfg.Cost.AlertThresholdPct*100))
			fmt.Fprintln(w)

			section("DAEMON")
			row("Parallel Tickets:", fmt.Sprintf("%d", cfg.Daemon.MaxParallelTickets))
			row("Parallel Tasks:", fmt.Sprintf("%d", cfg.Daemon.MaxParallelTasks))
			row("Work Dir:", cfg.Daemon.WorkDir)
			row("Log Level:", cfg.Daemon.LogLevel)
			fmt.Fprintln(w)

			section("DATABASE")
			row("Driver:", cfg.Database.Driver)
			if cfg.Database.Driver == "postgres" {
				row("URL:", redactKey(cfg.Database.Postgres.URL))
			} else {
				row("Path:", cfg.Database.SQLite.Path)
			}
			fmt.Fprintln(w)

			if len(cfg.MCP.Servers) > 0 {
				section("MCP SERVERS")
				for _, s := range cfg.MCP.Servers {
					row("·", s.Name)
				}
				fmt.Fprintln(w)
			}

			section("RATE LIMIT")
			row("Requests/Min:", fmt.Sprintf("%d", cfg.RateLimit.RequestsPerMinute))

			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func redactKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:7] + "..." + key[len(key)-4:]
}

func getActiveAPIKey(cfg *models.Config) string {
	switch cfg.LLM.DefaultProvider {
	case "anthropic":
		return cfg.LLM.Anthropic.APIKey
	case "openai":
		return cfg.LLM.OpenAI.APIKey
	case "openrouter":
		return cfg.LLM.OpenRouter.APIKey
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func formatNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if n >= 1000 {
		// Simple comma formatting
		parts := []string{}
		for i := len(s); i > 0; i -= 3 {
			start := i - 3
			if start < 0 {
				start = 0
			}
			parts = append([]string{s[start:i]}, parts...)
		}
		return strings.Join(parts, ",")
	}
	return s
}

func init() {
	rootCmd.AddCommand(newConfigCmd())
}
```

Note: `buildConfigSummary` and `loadConfigOnly` are helper functions. `loadConfigOnly` loads config without opening DB. `buildConfigSummary` builds the same JSON structure as the API endpoint. These may need to be added to `cmd/helpers.go`.

**Step 2: Build and test**

```bash
go build ./... && ./foreman config
```

**Step 3: Commit**

```bash
git add cmd/config.go
git commit -m "feat(cli): add 'foreman config' command to display active configuration"
```

---

### Task 13: Build, test, and verify

**Files:** No new files

**Step 1: Build backend**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./...
```

**Step 2: Run existing tests**

```bash
go test ./internal/dashboard/... ./internal/db/... ./cmd/...
```

**Step 3: Build frontend**

```bash
cd internal/dashboard/web && npm run build
```

**Step 4: Full build**

```bash
make dashboard-build
```

**Step 5: Manual verification**

Start the dashboard and verify:
- Gear/CFG button appears in header
- Clicking opens drawer from right side
- Config tab shows all sections with correct data
- Usage tab shows Foreman costs, activity breakdown, and Claude Code data
- LiveFeed shows runner/model badges on events that have them
- TaskCard shows live progress for active tasks
- `foreman config` CLI prints formatted config
- `foreman config --json` prints JSON

**Step 6: Final commit**

```bash
git add -A
git commit -m "feat: config & usage drawer with real-time enrichments"
```

---

## Task Dependency Graph

```
Task 1 (config provider) ──┬── Task 2 (config endpoint) ──┐
                            │                               │
Task 3 (activity endpoint) ─┤                               ├── Task 6 (frontend types/state)
                            │                               │
Task 4 (claude code endpoint)┤                              │
                            │                               │
Task 5 (WS enrichment) ────┘                               │
                                                            │
Task 6 ─── Task 7 (drawer config tab) ── Task 8 (usage tab) ── Task 9 (wire into app)
                                                            │
Task 6 ─── Task 10 (LiveFeed badges)                       │
Task 6 ─── Task 11 (TaskCard progress)                     │
                                                            │
Task 12 (CLI command) ─────────────────────────────────────┘
                                                            │
Task 13 (build & verify) ──────────────────────────────────┘
```

**Parallelizable:** Tasks 1-5 are backend-only and can be done in parallel. Tasks 7-8 are sequential. Tasks 10-11 can be done in parallel with 7-8. Task 12 is independent.
