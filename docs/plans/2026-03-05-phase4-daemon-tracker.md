# Phase 4: Daemon + Tracker — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Foreman autonomous: a 24/7 daemon that polls issue trackers (GitHub Issues, Jira), picks up labeled tickets, runs them through the pipeline, respects cost budgets, handles rate limits, prevents file conflicts between parallel tickets, recovers from crashes, and exposes a full CLI.

**Architecture:** The daemon is a goroutine pool managed by a scheduler. Each worker runs a full pipeline for one ticket. A shared rate limiter (token bucket) prevents LLM API abuse. The cost controller enforces per-ticket, per-day, and per-month budgets. The file reservation layer uses the database to prevent parallel tickets from conflicting. Issue trackers are behind a Go interface with GitHub Issues as the first implementation. The CLI uses cobra subcommands for lifecycle, monitoring, and cost reporting.

**Tech Stack:** Go 1.26, golang.org/x/time/rate (token bucket), go-resty/v2 (HTTP client), cobra (CLI), zerolog (logging), existing Phase 1-3 packages

---

### Task 1: Issue Tracker Interface + Local File Tracker

**Files:**
- Create: `internal/tracker/tracker.go`
- Create: `internal/tracker/local_file.go`
- Test: `internal/tracker/local_file_test.go`

**Step 1: Write the failing test**

```go
// internal/tracker/local_file_test.go
package tracker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFileTracker_FetchReadyTickets(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id":         "LOCAL-1",
		"title":               "Add user endpoint",
		"description":         "Create a REST endpoint for user management.",
		"acceptance_criteria": "GET /users returns 200",
		"labels":              []string{"foreman-ready"},
		"priority":            "medium",
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-1.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "LOCAL-1", tickets[0].ExternalID)
	assert.Equal(t, "Add user endpoint", tickets[0].Title)
}

func TestLocalFileTracker_FetchReadyTickets_NoLabel(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-2",
		"title":       "Not ready",
		"description": "This ticket has no foreman label.",
		"labels":      []string{"other-label"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-2.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestLocalFileTracker_GetTicket(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-3",
		"title":       "Fix bug",
		"description": "Fix the nil pointer bug in handler.",
		"labels":      []string{"foreman-ready"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-3.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	result, err := tracker.GetTicket(context.Background(), "LOCAL-3")
	require.NoError(t, err)
	assert.Equal(t, "Fix bug", result.Title)
}

func TestLocalFileTracker_GetTicket_NotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tickets"), 0o755))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	_, err := tracker.GetTicket(context.Background(), "NOPE-999")
	assert.Error(t, err)
}

func TestLocalFileTracker_AddComment(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-4",
		"title":       "Test",
		"description": "Test ticket.",
		"labels":      []string{},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-4.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	err := tracker.AddComment(context.Background(), "LOCAL-4", "Foreman started working")
	require.NoError(t, err)

	// Check comment was saved
	commentsFile := filepath.Join(ticketsDir, "LOCAL-4.comments.json")
	_, err = os.Stat(commentsFile)
	assert.NoError(t, err)
}

func TestLocalFileTracker_AddLabel(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-5",
		"title":       "Test",
		"description": "Test ticket.",
		"labels":      []string{"existing"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-5.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	err := tracker.AddLabel(context.Background(), "LOCAL-5", "new-label")
	require.NoError(t, err)

	hasLabel, err := tracker.HasLabel(context.Background(), "LOCAL-5", "new-label")
	require.NoError(t, err)
	assert.True(t, hasLabel)
}

func TestLocalFileTracker_ProviderName(t *testing.T) {
	tracker := NewLocalFileTracker("/tmp", "foreman-ready")
	assert.Equal(t, "local_file", tracker.ProviderName())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestLocalFileTracker -v`
Expected: FAIL — `NewLocalFileTracker` not defined

**Step 3: Write the interface and implementation**

```go
// internal/tracker/tracker.go
package tracker

import (
	"context"
	"time"
)

// Ticket represents an issue from any tracker.
type Ticket struct {
	ExternalID         string
	Title              string
	Description        string
	AcceptanceCriteria string
	Labels             []string
	Priority           string
	Assignee           string
	Reporter           string
	Comments           []TicketComment
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TicketComment is a single comment on a ticket.
type TicketComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// IssueTracker abstracts Jira, GitHub Issues, Linear, etc.
type IssueTracker interface {
	FetchReadyTickets(ctx context.Context) ([]Ticket, error)
	GetTicket(ctx context.Context, externalID string) (*Ticket, error)
	UpdateStatus(ctx context.Context, externalID string, status string) error
	AddComment(ctx context.Context, externalID string, comment string) error
	AttachPR(ctx context.Context, externalID string, prURL string) error
	AddLabel(ctx context.Context, externalID string, label string) error
	RemoveLabel(ctx context.Context, externalID string, label string) error
	HasLabel(ctx context.Context, externalID string, label string) (bool, error)
	ProviderName() string
}
```

```go
// internal/tracker/local_file.go
package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// localTicket is the JSON shape of a local ticket file.
type localTicket struct {
	ExternalID         string   `json:"external_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Labels             []string `json:"labels"`
	Priority           string   `json:"priority"`
	Status             string   `json:"status"`
}

// LocalFileTracker reads tickets from JSON files in a directory.
// Used for local development and testing.
type LocalFileTracker struct {
	dir         string
	pickupLabel string
}

// NewLocalFileTracker creates a local file tracker.
func NewLocalFileTracker(dir, pickupLabel string) *LocalFileTracker {
	return &LocalFileTracker{dir: dir, pickupLabel: pickupLabel}
}

func (t *LocalFileTracker) ticketsDir() string {
	return filepath.Join(t.dir, "tickets")
}

func (t *LocalFileTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	entries, err := os.ReadDir(t.ticketsDir())
	if err != nil {
		return nil, fmt.Errorf("reading tickets dir: %w", err)
	}

	var tickets []Ticket
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		// Skip comment files
		if len(entry.Name()) > 14 && entry.Name()[len(entry.Name())-14:] == ".comments.json" {
			continue
		}

		lt, err := t.readTicketFile(filepath.Join(t.ticketsDir(), entry.Name()))
		if err != nil {
			continue
		}
		if !containsLabel(lt.Labels, t.pickupLabel) {
			continue
		}
		tickets = append(tickets, toTicket(lt))
	}
	return tickets, nil
}

func (t *LocalFileTracker) GetTicket(ctx context.Context, externalID string) (*Ticket, error) {
	path := filepath.Join(t.ticketsDir(), externalID+".json")
	lt, err := t.readTicketFile(path)
	if err != nil {
		return nil, fmt.Errorf("ticket %s not found: %w", externalID, err)
	}
	ticket := toTicket(lt)
	return &ticket, nil
}

func (t *LocalFileTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	return t.updateField(externalID, func(lt *localTicket) { lt.Status = status })
}

func (t *LocalFileTracker) AddComment(ctx context.Context, externalID, comment string) error {
	commentsFile := filepath.Join(t.ticketsDir(), externalID+".comments.json")
	var comments []map[string]string

	if data, err := os.ReadFile(commentsFile); err == nil {
		json.Unmarshal(data, &comments)
	}

	comments = append(comments, map[string]string{
		"author":     "foreman",
		"body":       comment,
		"created_at": time.Now().Format(time.RFC3339),
	})

	data, _ := json.MarshalIndent(comments, "", "  ")
	return os.WriteFile(commentsFile, data, 0o644)
}

func (t *LocalFileTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return t.AddComment(ctx, externalID, fmt.Sprintf("PR created: %s", prURL))
}

func (t *LocalFileTracker) AddLabel(ctx context.Context, externalID, label string) error {
	return t.updateField(externalID, func(lt *localTicket) {
		if !containsLabel(lt.Labels, label) {
			lt.Labels = append(lt.Labels, label)
		}
	})
}

func (t *LocalFileTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	return t.updateField(externalID, func(lt *localTicket) {
		filtered := make([]string, 0, len(lt.Labels))
		for _, l := range lt.Labels {
			if l != label {
				filtered = append(filtered, l)
			}
		}
		lt.Labels = filtered
	})
}

func (t *LocalFileTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	lt, err := t.readTicketFile(filepath.Join(t.ticketsDir(), externalID+".json"))
	if err != nil {
		return false, err
	}
	return containsLabel(lt.Labels, label), nil
}

func (t *LocalFileTracker) ProviderName() string { return "local_file" }

func (t *LocalFileTracker) readTicketFile(path string) (*localTicket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lt localTicket
	if err := json.Unmarshal(data, &lt); err != nil {
		return nil, err
	}
	return &lt, nil
}

func (t *LocalFileTracker) updateField(externalID string, fn func(*localTicket)) error {
	path := filepath.Join(t.ticketsDir(), externalID+".json")
	lt, err := t.readTicketFile(path)
	if err != nil {
		return err
	}
	fn(lt)
	data, _ := json.MarshalIndent(lt, "", "  ")
	return os.WriteFile(path, data, 0o644)
}

func toTicket(lt *localTicket) Ticket {
	return Ticket{
		ExternalID:         lt.ExternalID,
		Title:              lt.Title,
		Description:        lt.Description,
		AcceptanceCriteria: lt.AcceptanceCriteria,
		Labels:             lt.Labels,
		Priority:           lt.Priority,
	}
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

var _ IssueTracker = (*LocalFileTracker)(nil)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tracker/ -run TestLocalFileTracker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tracker/tracker.go internal/tracker/local_file.go internal/tracker/local_file_test.go
git commit -m "feat: add issue tracker interface and local file implementation for dev/testing"
```

---

### Task 2: GitHub Issues Tracker

**Files:**
- Create: `internal/tracker/github_issues.go`
- Test: `internal/tracker/github_issues_test.go`

**Step 1: Write the failing test**

```go
// internal/tracker/github_issues_test.go
package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubIssuesTracker_FetchReadyTickets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Query().Get("labels"), "foreman-ready")
		assert.Equal(t, "open", r.URL.Query().Get("state"))

		issues := []map[string]interface{}{
			{
				"number": 42,
				"title":  "Add user endpoint",
				"body":   "Create REST endpoint\n\n## Acceptance Criteria\n- GET /users returns 200",
				"labels": []map[string]string{{"name": "foreman-ready"}},
			},
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "42", tickets[0].ExternalID)
	assert.Equal(t, "Add user endpoint", tickets[0].Title)
}

func TestGitHubIssuesTracker_AddComment(t *testing.T) {
	var postedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/issues/42/comments")
		json.NewDecoder(r.Body).Decode(&postedBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int{"id": 1})
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	err := tracker.AddComment(context.Background(), "42", "Foreman started")
	require.NoError(t, err)
	assert.Equal(t, "Foreman started", postedBody["body"])
}

func TestGitHubIssuesTracker_AddLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/issues/42/labels")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]string{{"name": "new-label"}})
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	err := tracker.AddLabel(context.Background(), "42", "new-label")
	require.NoError(t, err)
}

func TestGitHubIssuesTracker_ProviderName(t *testing.T) {
	tracker := NewGitHubIssuesTracker("", "", "", "", "")
	assert.Equal(t, "github", tracker.ProviderName())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestGitHubIssuesTracker -v`
Expected: FAIL — `NewGitHubIssuesTracker` not defined

**Step 3: Write minimal implementation**

```go
// internal/tracker/github_issues.go
package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// GitHubIssuesTracker implements IssueTracker for GitHub Issues.
type GitHubIssuesTracker struct {
	baseURL     string
	token       string
	owner       string
	repo        string
	pickupLabel string
	client      *http.Client
}

// NewGitHubIssuesTracker creates a GitHub Issues tracker.
func NewGitHubIssuesTracker(baseURL, token, owner, repo, pickupLabel string) *GitHubIssuesTracker {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &GitHubIssuesTracker{
		baseURL:     baseURL,
		token:       token,
		owner:       owner,
		repo:        repo,
		pickupLabel: pickupLabel,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
	User   ghUser    `json:"user"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

func (g *GitHubIssuesTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues?labels=%s&state=open&per_page=30",
		g.baseURL, g.owner, g.repo, g.pickupLabel)

	body, err := g.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var issues []ghIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("decoding issues: %w", err)
	}

	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		labels := make([]string, len(issue.Labels))
		for i, l := range issue.Labels {
			labels[i] = l.Name
		}
		tickets = append(tickets, Ticket{
			ExternalID:  strconv.Itoa(issue.Number),
			Title:       issue.Title,
			Description: issue.Body,
			Labels:      labels,
			Reporter:    issue.User.Login,
		})
	}
	return tickets, nil
}

func (g *GitHubIssuesTracker) GetTicket(ctx context.Context, externalID string) (*Ticket, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s", g.baseURL, g.owner, g.repo, externalID)
	body, err := g.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var issue ghIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("decoding issue: %w", err)
	}

	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}

	return &Ticket{
		ExternalID:  strconv.Itoa(issue.Number),
		Title:       issue.Title,
		Description: issue.Body,
		Labels:      labels,
		Reporter:    issue.User.Login,
	}, nil
}

func (g *GitHubIssuesTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	// GitHub Issues don't have custom statuses — use labels or close/reopen
	if status == "done" {
		return g.doPost(ctx, fmt.Sprintf("%s/repos/%s/%s/issues/%s",
			g.baseURL, g.owner, g.repo, externalID),
			map[string]string{"state": "closed"})
	}
	return nil
}

func (g *GitHubIssuesTracker) AddComment(ctx context.Context, externalID, comment string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s/comments", g.baseURL, g.owner, g.repo, externalID)
	return g.doPost(ctx, url, map[string]string{"body": comment})
}

func (g *GitHubIssuesTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return g.AddComment(ctx, externalID, fmt.Sprintf("🤖 PR created: %s", prURL))
}

func (g *GitHubIssuesTracker) AddLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s/labels", g.baseURL, g.owner, g.repo, externalID)
	return g.doPost(ctx, url, map[string][]string{"labels": {label}})
}

func (g *GitHubIssuesTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s/labels/%s", g.baseURL, g.owner, g.repo, externalID, label)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	g.setHeaders(req)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (g *GitHubIssuesTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	ticket, err := g.GetTicket(ctx, externalID)
	if err != nil {
		return false, err
	}
	return containsLabel(ticket.Labels, label), nil
}

func (g *GitHubIssuesTracker) ProviderName() string { return "github" }

func (g *GitHubIssuesTracker) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	g.setHeaders(req)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (g *GitHubIssuesTracker) doPost(ctx context.Context, url string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (g *GitHubIssuesTracker) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
}

var _ IssueTracker = (*GitHubIssuesTracker)(nil)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tracker/ -run TestGitHubIssuesTracker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tracker/github_issues.go internal/tracker/github_issues_test.go
git commit -m "feat: add GitHub Issues tracker with REST API integration"
```

---

### Task 3: Cost Controller

**Files:**
- Create: `internal/telemetry/cost_controller.go`
- Test: `internal/telemetry/cost_controller_test.go`

**Step 1: Write the failing test**

```go
// internal/telemetry/cost_controller_test.go
package telemetry

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostController_CheckTicketBudget_WithinLimit(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 15.0,
		AlertThresholdPct:   80,
	})

	err := cc.CheckTicketBudget(5.0)
	assert.NoError(t, err)
}

func TestCostController_CheckTicketBudget_Exceeded(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 15.0,
		AlertThresholdPct:   80,
	})

	err := cc.CheckTicketBudget(16.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "budget exceeded")
}

func TestCostController_CheckTicketBudget_AlertThreshold(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 10.0,
		AlertThresholdPct:   80,
	})

	alert := cc.ShouldAlert(8.5, 10.0) // 85% > 80%
	assert.True(t, alert)
}

func TestCostController_CheckTicketBudget_BelowAlert(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 10.0,
		AlertThresholdPct:   80,
	})

	alert := cc.ShouldAlert(7.0, 10.0) // 70% < 80%
	assert.False(t, alert)
}

func TestCostController_CheckDailyBudget(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerDayUSD: 150.0,
	})

	assert.NoError(t, cc.CheckDailyBudget(100.0))
	assert.Error(t, cc.CheckDailyBudget(160.0))
}

func TestCostController_CheckMonthlyBudget(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerMonthUSD: 3000.0,
	})

	assert.NoError(t, cc.CheckMonthlyBudget(2500.0))
	assert.Error(t, cc.CheckMonthlyBudget(3100.0))
}

func TestCostController_CalculateCost(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		Pricing: map[string]models.PricingConfig{
			"anthropic:claude-sonnet-4-5-20250929": {Input: 3.0, Output: 15.0},
		},
	})

	cost := cc.CalculateCost("anthropic:claude-sonnet-4-5-20250929", 10000, 2000)
	// (10000/1M)*3.0 + (2000/1M)*15.0 = 0.03 + 0.03 = 0.06
	require.InDelta(t, 0.06, cost, 0.001)
}

func TestCostController_CalculateCost_UnknownModel(t *testing.T) {
	cc := NewCostController(models.CostConfig{})
	cost := cc.CalculateCost("unknown:model", 10000, 2000)
	// Unknown model should use fallback pricing
	assert.True(t, cost > 0)
}

func TestCostController_CheckTaskCallCap(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxLlmCallsPerTask: 8,
	})

	assert.NoError(t, cc.CheckTaskCallCap(7))
	assert.Error(t, cc.CheckTaskCallCap(8))
	assert.Error(t, cc.CheckTaskCallCap(10))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/ -run TestCostController -v`
Expected: FAIL — `NewCostController` not defined

**Step 3: Write minimal implementation**

```go
// internal/telemetry/cost_controller.go
package telemetry

import (
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// CostController enforces cost budgets at ticket, daily, and monthly levels.
type CostController struct {
	config models.CostConfig
}

// NewCostController creates a cost controller.
func NewCostController(config models.CostConfig) *CostController {
	return &CostController{config: config}
}

// CheckTicketBudget returns an error if the ticket cost exceeds the per-ticket budget.
func (c *CostController) CheckTicketBudget(currentCost float64) error {
	if c.config.MaxCostPerTicketUSD > 0 && currentCost > c.config.MaxCostPerTicketUSD {
		return fmt.Errorf("ticket budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerTicketUSD)
	}
	return nil
}

// CheckDailyBudget returns an error if the daily cost exceeds the per-day budget.
func (c *CostController) CheckDailyBudget(currentCost float64) error {
	if c.config.MaxCostPerDayUSD > 0 && currentCost > c.config.MaxCostPerDayUSD {
		return fmt.Errorf("daily budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerDayUSD)
	}
	return nil
}

// CheckMonthlyBudget returns an error if the monthly cost exceeds the per-month budget.
func (c *CostController) CheckMonthlyBudget(currentCost float64) error {
	if c.config.MaxCostPerMonthUSD > 0 && currentCost > c.config.MaxCostPerMonthUSD {
		return fmt.Errorf("monthly budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerMonthUSD)
	}
	return nil
}

// ShouldAlert returns true if the current cost exceeds the alert threshold percentage of the limit.
func (c *CostController) ShouldAlert(currentCost, limit float64) bool {
	if limit <= 0 || c.config.AlertThresholdPct <= 0 {
		return false
	}
	threshold := limit * float64(c.config.AlertThresholdPct) / 100.0
	return currentCost >= threshold
}

// CalculateCost computes the USD cost for a given model and token counts.
func (c *CostController) CalculateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := c.config.Pricing[model]
	if !ok {
		// Fallback pricing for unknown models
		pricing = models.PricingConfig{Input: 3.0, Output: 15.0}
	}
	return (float64(inputTokens)/1_000_000)*pricing.Input +
		(float64(outputTokens)/1_000_000)*pricing.Output
}

// CheckTaskCallCap returns an error if the task has reached the LLM call cap.
func (c *CostController) CheckTaskCallCap(currentCalls int) error {
	if c.config.MaxLlmCallsPerTask > 0 && currentCalls >= c.config.MaxLlmCallsPerTask {
		return fmt.Errorf("task LLM call cap reached: %d >= %d", currentCalls, c.config.MaxLlmCallsPerTask)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/telemetry/ -run TestCostController -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/cost_controller.go internal/telemetry/cost_controller_test.go
git commit -m "feat: add cost controller with ticket/daily/monthly budget enforcement"
```

---

### Task 4: Shared Rate Limiter

**Files:**
- Create: `internal/llm/ratelimiter.go`
- Test: `internal/llm/ratelimiter_test.go`

**Step 1: Write the failing test**

```go
// internal/llm/ratelimiter_test.go
package llm

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedRateLimiter_Wait(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 600, // 10/sec — fast enough for tests
		BurstSize:         10,
	})

	ctx := context.Background()
	start := time.Now()
	err := rl.Wait(ctx, "anthropic")
	require.NoError(t, err)
	elapsed := time.Since(start)

	// First call should be nearly instant due to burst
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestSharedRateLimiter_SeparateProviders(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 600,
		BurstSize:         5,
	})

	ctx := context.Background()
	// Different providers get separate limiters
	require.NoError(t, rl.Wait(ctx, "anthropic"))
	require.NoError(t, rl.Wait(ctx, "openai"))
}

func TestSharedRateLimiter_ContextCancellation(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 1, // Very slow
		BurstSize:         1,
	})

	ctx := context.Background()
	// Use up the burst
	require.NoError(t, rl.Wait(ctx, "anthropic"))

	// Next call should block — cancel it
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := rl.Wait(cancelCtx, "anthropic")
	assert.Error(t, err) // Should fail due to context timeout
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestSharedRateLimiter -v`
Expected: FAIL — `NewSharedRateLimiter` not defined

**Step 3: Write minimal implementation**

```go
// internal/llm/ratelimiter.go
package llm

import (
	"context"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
	"golang.org/x/time/rate"
)

// SharedRateLimiter provides per-provider rate limiting using token buckets.
type SharedRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	config   models.RateLimitConfig
}

// NewSharedRateLimiter creates a shared rate limiter.
func NewSharedRateLimiter(config models.RateLimitConfig) *SharedRateLimiter {
	return &SharedRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		config:   config,
	}
}

// Wait blocks until the rate limiter allows the request or the context is cancelled.
func (r *SharedRateLimiter) Wait(ctx context.Context, provider string) error {
	limiter := r.getOrCreate(provider)
	return limiter.Wait(ctx)
}

// OnRateLimit adjusts the limiter when a 429 response is received.
func (r *SharedRateLimiter) OnRateLimit(provider string, retryAfterSecs int) {
	limiter := r.getOrCreate(provider)
	// Temporarily reduce the rate
	limiter.SetLimit(rate.Every(time.Duration(retryAfterSecs) * time.Second))

	// Restore after the retry-after period
	go func() {
		time.Sleep(time.Duration(retryAfterSecs) * time.Second)
		limiter.SetLimit(rate.Every(time.Minute / time.Duration(r.config.RequestsPerMinute)))
	}()
}

func (r *SharedRateLimiter) getOrCreate(provider string) *rate.Limiter {
	r.mu.RLock()
	limiter, ok := r.limiters[provider]
	r.mu.RUnlock()
	if ok {
		return limiter
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, ok = r.limiters[provider]; ok {
		return limiter
	}

	rpm := r.config.RequestsPerMinute
	if rpm <= 0 {
		rpm = 50
	}
	burst := r.config.BurstSize
	if burst <= 0 {
		burst = 10
	}

	limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), burst)
	r.limiters[provider] = limiter
	return limiter
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestSharedRateLimiter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/ratelimiter.go internal/llm/ratelimiter_test.go
git commit -m "feat: add shared rate limiter with per-provider token buckets"
```

---

### Task 5: File Reservation Layer (Scheduler)

**Files:**
- Create: `internal/daemon/scheduler.go`
- Test: `internal/daemon/scheduler_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/scheduler_test.go
package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDB implements the minimal DB interface needed for scheduler tests.
type mockDB struct {
	reservations map[string]string // path → ticketID
	reserved     map[string][]string // ticketID → paths
}

func newMockDB() *mockDB {
	return &mockDB{
		reservations: make(map[string]string),
		reserved:     make(map[string][]string),
	}
}

func (m *mockDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	return m.reservations, nil
}

func (m *mockDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	for _, p := range paths {
		m.reservations[p] = ticketID
	}
	m.reserved[ticketID] = paths
	return nil
}

func (m *mockDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	for _, p := range m.reserved[ticketID] {
		delete(m.reservations, p)
	}
	delete(m.reserved, ticketID)
	return nil
}

func TestScheduler_TryReserve_NoConflict(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	err := sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go", "src/models.go"})
	require.NoError(t, err)

	// Verify files are reserved
	reserved, _ := db.GetReservedFiles(context.Background())
	assert.Equal(t, "ticket-1", reserved["src/handler.go"])
	assert.Equal(t, "ticket-1", reserved["src/models.go"])
}

func TestScheduler_TryReserve_Conflict(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	// First ticket reserves files
	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))

	// Second ticket conflicts
	err := sched.TryReserve(context.Background(), "ticket-2", []string{"src/handler.go", "src/other.go"})
	assert.Error(t, err)

	var conflictErr *FileConflictError
	assert.ErrorAs(t, err, &conflictErr)
	assert.Len(t, conflictErr.Conflicts, 1)
	assert.Contains(t, conflictErr.Conflicts[0], "src/handler.go")
}

func TestScheduler_Release(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))
	sched.Release(context.Background(), "ticket-1")

	// After release, another ticket can reserve the same file
	err := sched.TryReserve(context.Background(), "ticket-2", []string{"src/handler.go"})
	assert.NoError(t, err)
}

func TestScheduler_TryReserve_SameTicket(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))

	// Same ticket re-reserving should not conflict
	err := sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go", "src/new.go"})
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestScheduler -v`
Expected: FAIL — `NewScheduler` not defined

**Step 3: Write minimal implementation**

```go
// internal/daemon/scheduler.go
package daemon

import (
	"context"
	"fmt"
	"strings"
)

// FileReserver is the database subset needed by the scheduler.
type FileReserver interface {
	GetReservedFiles(ctx context.Context) (map[string]string, error)
	ReserveFiles(ctx context.Context, ticketID string, paths []string) error
	ReleaseFiles(ctx context.Context, ticketID string) error
}

// FileConflictError indicates file reservation conflicts.
type FileConflictError struct {
	Conflicts []string
}

func (e *FileConflictError) Error() string {
	return fmt.Sprintf("file reservation conflict: %s", strings.Join(e.Conflicts, ", "))
}

// Scheduler manages file reservations for parallel ticket processing.
type Scheduler struct {
	db FileReserver
}

// NewScheduler creates a scheduler.
func NewScheduler(db FileReserver) *Scheduler {
	return &Scheduler{db: db}
}

// TryReserve attempts to reserve files for a ticket. Returns FileConflictError if any
// files are held by another ticket.
func (s *Scheduler) TryReserve(ctx context.Context, ticketID string, files []string) error {
	reserved, err := s.db.GetReservedFiles(ctx)
	if err != nil {
		return fmt.Errorf("getting reserved files: %w", err)
	}

	var conflicts []string
	for _, f := range files {
		if owner, ok := reserved[f]; ok && owner != ticketID {
			conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", f, owner))
		}
	}

	if len(conflicts) > 0 {
		return &FileConflictError{Conflicts: conflicts}
	}

	return s.db.ReserveFiles(ctx, ticketID, files)
}

// Release removes all file reservations for a ticket.
func (s *Scheduler) Release(ctx context.Context, ticketID string) {
	s.db.ReleaseFiles(ctx, ticketID)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestScheduler -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/scheduler.go internal/daemon/scheduler_test.go
git commit -m "feat: add file reservation scheduler for parallel ticket conflict prevention"
```

---

### Task 6: Crash Recovery

**Files:**
- Create: `internal/daemon/recovery.go`
- Test: `internal/daemon/recovery_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/recovery_test.go
package daemon

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestClassifyRecovery_PlanningPhase(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusPlanning,
		LastCompletedTaskSeq: 0,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryReplan, action.Action)
}

func TestClassifyRecovery_ImplementingWithProgress(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusImplementing,
		LastCompletedTaskSeq: 3,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryResume, action.Action)
	assert.Equal(t, 3, action.ResumeFromSeq)
}

func TestClassifyRecovery_ReviewingPhase(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusReviewing,
		LastCompletedTaskSeq: 5,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryResume, action.Action)
	assert.Equal(t, 5, action.ResumeFromSeq)
}

func TestClassifyRecovery_AlreadyDone(t *testing.T) {
	ticket := &models.Ticket{
		Status: models.TicketStatusDone,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoverySkip, action.Action)
}

func TestResetTasksForRecovery(t *testing.T) {
	tasks := []models.Task{
		{Sequence: 1, Status: models.TaskStatusDone},
		{Sequence: 2, Status: models.TaskStatusDone},
		{Sequence: 3, Status: models.TaskStatusImplementing}, // Was in progress
		{Sequence: 4, Status: models.TaskStatusPending},
	}

	toReset := TasksToReset(tasks, 2) // Last completed was seq 2
	assert.Len(t, toReset, 1)
	assert.Equal(t, 3, toReset[0].Sequence) // Task 3 should be reset
}

func TestResetTasksForRecovery_NoneToReset(t *testing.T) {
	tasks := []models.Task{
		{Sequence: 1, Status: models.TaskStatusDone},
		{Sequence: 2, Status: models.TaskStatusPending},
	}

	toReset := TasksToReset(tasks, 1)
	assert.Empty(t, toReset)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run "TestClassifyRecovery|TestResetTasks" -v`
Expected: FAIL — `ClassifyRecovery` not defined

**Step 3: Write minimal implementation**

```go
// internal/daemon/recovery.go
package daemon

import "github.com/canhta/foreman/internal/models"

// RecoveryAction describes what to do with an in-progress ticket after a crash.
type RecoveryAction string

const (
	RecoveryReplan RecoveryAction = "replan"  // Start over from planning
	RecoveryResume RecoveryAction = "resume"  // Resume from last completed task
	RecoverySkip   RecoveryAction = "skip"    // Already complete, do nothing
)

// RecoveryPlan describes how to recover a specific ticket.
type RecoveryPlan struct {
	Action        RecoveryAction
	ResumeFromSeq int // Only set when Action == RecoveryResume
}

// ClassifyRecovery determines how to recover an in-progress ticket.
func ClassifyRecovery(ticket *models.Ticket) RecoveryPlan {
	switch ticket.Status {
	case models.TicketStatusDone, models.TicketStatusFailed, models.TicketStatusPartial, models.TicketStatusBlocked:
		return RecoveryPlan{Action: RecoverySkip}

	case models.TicketStatusPlanning, models.TicketStatusPlanValidating:
		if ticket.LastCompletedTaskSeq == 0 {
			return RecoveryPlan{Action: RecoveryReplan}
		}
		return RecoveryPlan{Action: RecoveryResume, ResumeFromSeq: ticket.LastCompletedTaskSeq}

	case models.TicketStatusImplementing, models.TicketStatusReviewing:
		return RecoveryPlan{Action: RecoveryResume, ResumeFromSeq: ticket.LastCompletedTaskSeq}

	default:
		// Queued or unknown — re-queue
		return RecoveryPlan{Action: RecoveryReplan}
	}
}

// TasksToReset returns tasks that were in progress at crash time and need resetting to pending.
func TasksToReset(tasks []models.Task, lastCompletedSeq int) []models.Task {
	var toReset []models.Task
	for _, task := range tasks {
		if task.Sequence <= lastCompletedSeq {
			continue // Already committed, leave as done
		}
		if task.Status != models.TaskStatusPending && task.Status != models.TaskStatusDone {
			// Was in progress when crash happened — needs reset
			toReset = append(toReset, task)
		}
	}
	return toReset
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run "TestClassifyRecovery|TestResetTasks" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/recovery.go internal/daemon/recovery_test.go
git commit -m "feat: add crash recovery with replan/resume/skip classification"
```

---

### Task 7: Daemon Core

**Files:**
- Create: `internal/daemon/daemon.go`
- Test: `internal/daemon/daemon_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/daemon_test.go
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonConfig_Defaults(t *testing.T) {
	cfg := DefaultDaemonConfig()
	assert.Equal(t, 60, cfg.PollIntervalSecs)
	assert.Equal(t, 300, cfg.IdlePollIntervalSecs)
	assert.Equal(t, 3, cfg.MaxParallelTickets)
}

func TestDaemon_NewDaemon(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)
	require.NotNil(t, d)
	assert.False(t, d.IsRunning())
}

func TestDaemon_StartStop(t *testing.T) {
	cfg := DefaultDaemonConfig()
	cfg.PollIntervalSecs = 1 // Fast for tests
	d := NewDaemon(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	go d.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	assert.True(t, d.IsRunning())

	cancel()
	time.Sleep(100 * time.Millisecond)
	assert.False(t, d.IsRunning())
}

func TestDaemon_Pause_Resume(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)

	assert.False(t, d.IsPaused())
	d.Pause()
	assert.True(t, d.IsPaused())
	d.Resume()
	assert.False(t, d.IsPaused())
}

func TestDaemon_Status(t *testing.T) {
	cfg := DefaultDaemonConfig()
	d := NewDaemon(cfg)

	status := d.Status()
	assert.Equal(t, "stopped", status.State)
	assert.Equal(t, 0, status.ActivePipelines)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestDaemon -v`
Expected: FAIL — `NewDaemon` not defined

**Step 3: Write minimal implementation**

```go
// internal/daemon/daemon.go
package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DaemonConfig holds daemon configuration.
type DaemonConfig struct {
	PollIntervalSecs     int
	IdlePollIntervalSecs int
	MaxParallelTickets   int
}

// DefaultDaemonConfig returns sensible defaults.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollIntervalSecs:     60,
		IdlePollIntervalSecs: 300,
		MaxParallelTickets:   3,
	}
}

// DaemonStatus holds the current state of the daemon.
type DaemonStatus struct {
	State           string    // "running", "paused", "stopped"
	ActivePipelines int
	StartedAt       time.Time
	Uptime          time.Duration
}

// Daemon is the main 24/7 event loop.
type Daemon struct {
	config    DaemonConfig
	running   atomic.Bool
	paused    atomic.Bool
	startedAt time.Time
	active    atomic.Int32
	mu        sync.Mutex
}

// NewDaemon creates a new daemon.
func NewDaemon(config DaemonConfig) *Daemon {
	return &Daemon{config: config}
}

// Start begins the daemon's poll loop. Blocks until ctx is cancelled.
func (d *Daemon) Start(ctx context.Context) {
	d.running.Store(true)
	d.startedAt = time.Now()
	defer d.running.Store(false)

	pollInterval := time.Duration(d.config.PollIntervalSecs) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d.paused.Load() {
				continue
			}
			// Poll cycle — to be wired to tracker + pipeline in integration
		}
	}
}

// IsRunning returns whether the daemon is currently running.
func (d *Daemon) IsRunning() bool {
	return d.running.Load()
}

// IsPaused returns whether the daemon is paused.
func (d *Daemon) IsPaused() bool {
	return d.paused.Load()
}

// Pause pauses the daemon's polling.
func (d *Daemon) Pause() {
	d.paused.Store(true)
}

// Resume resumes the daemon's polling.
func (d *Daemon) Resume() {
	d.paused.Store(false)
}

// Status returns the current daemon status.
func (d *Daemon) Status() DaemonStatus {
	state := "stopped"
	if d.running.Load() {
		if d.paused.Load() {
			state = "paused"
		} else {
			state = "running"
		}
	}

	var uptime time.Duration
	if d.running.Load() {
		uptime = time.Since(d.startedAt)
	}

	return DaemonStatus{
		State:           state,
		ActivePipelines: int(d.active.Load()),
		StartedAt:       d.startedAt,
		Uptime:          uptime,
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestDaemon -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/daemon_test.go
git commit -m "feat: add daemon core with poll loop, pause/resume, and status reporting"
```

---

### Task 8: CLI — `run` Command

**Files:**
- Create: `cmd/run.go`
- Test: `cmd/run_test.go`

**Step 1: Write the failing test**

```go
// cmd/run_test.go
package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunCmd_Exists(t *testing.T) {
	cmd := newRunCmd()
	assert.Equal(t, "run", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestRunCmd_HasDryRunFlag(t *testing.T) {
	cmd := newRunCmd()
	flag := cmd.Flags().Lookup("dry-run")
	assert.NotNil(t, flag)
}

func TestRunCmd_RequiresArgs(t *testing.T) {
	cmd := newRunCmd()
	// cobra.ExactArgs(1) means 0 args should error
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestRunCmd_AcceptsOneArg(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.Args(cmd, []string{"PROJ-123"})
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestRunCmd -v`
Expected: FAIL — `newRunCmd` not defined

**Step 3: Write minimal implementation**

```go
// cmd/run.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single ticket through the pipeline",
		Long:  "Run a specific ticket by external ID (e.g., PROJ-123 or GitHub issue number).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ticketID := args[0]
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run for ticket: %s (plan only)\n", ticketID)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Running pipeline for ticket: %s\n", ticketID)
			// Pipeline execution will be wired here
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan only — show tasks, estimated cost, files")
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCmd())
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestRunCmd -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/run.go cmd/run_test.go
git commit -m "feat: add 'foreman run' CLI command with --dry-run flag"
```

---

### Task 9: CLI — `start`, `stop`, `status` Commands

**Files:**
- Create: `cmd/start.go`
- Create: `cmd/stop.go`
- Create: `cmd/status.go`

**Step 1: Write the failing test**

```go
// cmd/lifecycle_test.go
package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStartCmd_Exists(t *testing.T) {
	cmd := newStartCmd()
	assert.Equal(t, "start", cmd.Use)
	flag := cmd.Flags().Lookup("daemon")
	assert.NotNil(t, flag)
}

func TestStopCmd_Exists(t *testing.T) {
	cmd := newStopCmd()
	assert.Equal(t, "stop", cmd.Use)
}

func TestStatusCmd_Exists(t *testing.T) {
	cmd := newStatusCmd()
	assert.Equal(t, "status", cmd.Use)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run "TestStartCmd|TestStopCmd|TestStatusCmd" -v`
Expected: FAIL

**Step 3: Write minimal implementations**

```go
// cmd/start.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var daemonMode bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemonMode {
				fmt.Fprintln(cmd.OutOrStdout(), "Starting Foreman daemon in background...")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Starting Foreman daemon in foreground...")
			}
			// Daemon wiring will happen in integration
			return nil
		},
	}

	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run in background")
	return cmd
}

func init() {
	rootCmd.AddCommand(newStartCmd())
}
```

```go
// cmd/stop.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Stopping Foreman daemon...")
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newStopCmd())
}
```

```go
// cmd/status.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status and active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Foreman status: not running")
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newStatusCmd())
}
```

**Step 4: Create the test file and run tests**

Run: `go test ./cmd/ -run "TestStartCmd|TestStopCmd|TestStatusCmd" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/start.go cmd/stop.go cmd/status.go cmd/lifecycle_test.go
git commit -m "feat: add start, stop, status CLI commands for daemon lifecycle"
```

---

### Task 10: CLI — `cost`, `ps`, `doctor` Commands

**Files:**
- Create: `cmd/cost.go`
- Create: `cmd/ps.go`
- Create: `cmd/doctor.go`

**Step 1: Write the failing test**

```go
// cmd/utility_test.go
package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostCmd_Exists(t *testing.T) {
	cmd := newCostCmd()
	assert.Equal(t, "cost", cmd.Use)
}

func TestCostCmd_AcceptsSubcommand(t *testing.T) {
	cmd := newCostCmd()
	err := cmd.Args(cmd, []string{"today"})
	assert.NoError(t, err)
}

func TestPsCmd_Exists(t *testing.T) {
	cmd := newPsCmd()
	assert.Equal(t, "ps", cmd.Use)
	flag := cmd.Flags().Lookup("all")
	assert.NotNil(t, flag)
}

func TestDoctorCmd_Exists(t *testing.T) {
	cmd := newDoctorCmd()
	assert.Equal(t, "doctor", cmd.Use)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run "TestCostCmd|TestPsCmd|TestDoctorCmd" -v`
Expected: FAIL

**Step 3: Write minimal implementations**

```go
// cmd/cost.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show cost breakdown",
		Long:  "Show cost breakdown: today, week, month, or per-ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			period := args[0]
			fmt.Fprintf(cmd.OutOrStdout(), "Cost breakdown for: %s\n", period)
			fmt.Fprintln(cmd.OutOrStdout(), "(no data yet)")
			return nil
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(newCostCmd())
}
```

```go
// cmd/ps.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPsCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showAll {
				fmt.Fprintln(cmd.OutOrStdout(), "All pipelines (including completed):")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Active pipelines:")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "(none)")
			return nil
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all pipelines including completed")
	return cmd
}

func init() {
	rootCmd.AddCommand(newPsCmd())
}
```

```go
// cmd/doctor.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Health check all configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Running health checks...")
			fmt.Fprintln(cmd.OutOrStdout(), "  LLM provider: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Issue tracker: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Git: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Database: (not configured)")
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newDoctorCmd())
}
```

**Step 4: Create the test file and run tests**

Run: `go test ./cmd/ -run "TestCostCmd|TestPsCmd|TestDoctorCmd" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/cost.go cmd/ps.go cmd/doctor.go cmd/utility_test.go
git commit -m "feat: add cost, ps, and doctor CLI commands"
```

---

### Task 11: Install golang.org/x/time dependency

**Step 1: Install the dependency**

```bash
cd /Users/canh/Projects/Indies/Foreman
go get golang.org/x/time@latest
go mod tidy
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add golang.org/x/time dependency for rate limiting"
```

> **Note:** This task should be run BEFORE Task 4 (Shared Rate Limiter), since it depends on `golang.org/x/time/rate`.

---

### Task 12: Jira Tracker

**Files:**
- Create: `internal/tracker/jira.go`
- Create: `internal/tracker/jira_test.go`

**Step 1: Write the failing test**

```go
// internal/tracker/jira_test.go
package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraTracker_FetchReadyTickets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "/rest/api/2/search")
		assert.Equal(t, "Basic dXNlcjp0b2tlbg==", r.Header.Get("Authorization"))

		resp := map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"key": "PROJ-123",
					"fields": map[string]interface{}{
						"summary":     "Add login page",
						"description": "Build login page with email/password.",
						"labels":      []string{"foreman"},
						"priority":    map[string]string{"name": "Medium"},
						"assignee":    map[string]string{"displayName": "Alice"},
						"reporter":    map[string]string{"displayName": "Bob"},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "PROJ-123", tickets[0].ExternalID)
	assert.Equal(t, "Add login page", tickets[0].Title)
}

func TestJiraTracker_AddComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/2/issue/PROJ-1/comment")
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	err := tracker.AddComment(context.Background(), "PROJ-1", "Test comment")
	require.NoError(t, err)
}

func TestJiraTracker_ProviderName(t *testing.T) {
	tracker := NewJiraTracker("http://localhost", "u", "t", "P", "f")
	assert.Equal(t, "jira", tracker.ProviderName())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestJira -v`
Expected: FAIL — NewJiraTracker not defined

**Step 3: Write minimal implementation**

```go
// internal/tracker/jira.go
package tracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type JiraTracker struct {
	baseURL    string
	authHeader string
	project    string
	label      string
	client     *http.Client
}

func NewJiraTracker(baseURL, username, apiToken, project, pickupLabel string) *JiraTracker {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + apiToken))
	return &JiraTracker{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authHeader: "Basic " + auth,
		project:    project,
		label:      pickupLabel,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (j *JiraTracker) ProviderName() string { return "jira" }

type jiraSearchResponse struct {
	Issues []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	Key    string     `json:"key"`
	Fields jiraFields `json:"fields"`
}

type jiraFields struct {
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      []string          `json:"labels"`
	Priority    *jiraPriority     `json:"priority"`
	Assignee    *jiraUser         `json:"assignee"`
	Reporter    *jiraUser         `json:"reporter"`
	Comment     *jiraCommentBlock `json:"comment"`
}

type jiraPriority struct {
	Name string `json:"name"`
}

type jiraUser struct {
	DisplayName string `json:"displayName"`
}

type jiraCommentBlock struct {
	Comments []jiraComment `json:"comments"`
}

type jiraComment struct {
	Author jiraUser `json:"author"`
	Body   string   `json:"body"`
}

func (j *JiraTracker) FetchReadyTickets(ctx context.Context) ([]TrackerTicket, error) {
	jql := fmt.Sprintf("project=%s AND labels=%s AND status!=Done", j.project, j.label)
	url := fmt.Sprintf("%s/rest/api/2/search?jql=%s", j.baseURL, jql)

	resp, err := j.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("jira search: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result jiraSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("jira unmarshal: %w", err)
	}

	tickets := make([]TrackerTicket, 0, len(result.Issues))
	for _, issue := range result.Issues {
		t := TrackerTicket{
			ExternalID:  issue.Key,
			Title:       issue.Fields.Summary,
			Description: issue.Fields.Description,
			Labels:      issue.Fields.Labels,
		}
		if issue.Fields.Priority != nil {
			t.Priority = issue.Fields.Priority.Name
		}
		if issue.Fields.Assignee != nil {
			t.Assignee = issue.Fields.Assignee.DisplayName
		}
		if issue.Fields.Reporter != nil {
			t.Reporter = issue.Fields.Reporter.DisplayName
		}
		tickets = append(tickets, t)
	}
	return tickets, nil
}

func (j *JiraTracker) GetTicket(ctx context.Context, externalID string) (*TrackerTicket, error) {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s", j.baseURL, externalID)
	resp, err := j.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var issue jiraIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("jira unmarshal: %w", err)
	}

	t := &TrackerTicket{
		ExternalID:  issue.Key,
		Title:       issue.Fields.Summary,
		Description: issue.Fields.Description,
		Labels:      issue.Fields.Labels,
	}
	return t, nil
}

func (j *JiraTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	// Jira status transitions require knowing transition IDs — simplified for now
	return nil
}

func (j *JiraTracker) AddComment(ctx context.Context, externalID, comment string) error {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s/comment", j.baseURL, externalID)
	payload := fmt.Sprintf(`{"body":%q}`, comment)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", j.authHeader)
	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("jira add comment: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira add comment: status %d", resp.StatusCode)
	}
	return nil
}

func (j *JiraTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return j.AddComment(ctx, externalID, fmt.Sprintf("PR created: %s", prURL))
}

func (j *JiraTracker) AssignTicket(ctx context.Context, externalID, assignee string) error {
	return nil // Not implemented for MVP
}

func (j *JiraTracker) AddLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s", j.baseURL, externalID)
	payload := fmt.Sprintf(`{"update":{"labels":[{"add":%q}]}}`, label)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", j.authHeader)
	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (j *JiraTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s", j.baseURL, externalID)
	payload := fmt.Sprintf(`{"update":{"labels":[{"remove":%q}]}}`, label)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", j.authHeader)
	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (j *JiraTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	ticket, err := j.GetTicket(ctx, externalID)
	if err != nil {
		return false, err
	}
	for _, l := range ticket.Labels {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}

func (j *JiraTracker) doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", j.authHeader)
	req.Header.Set("Content-Type", "application/json")
	return j.client.Do(req)
}
```

**Step 4: Run tests**

Run: `go test ./internal/tracker/ -run TestJira -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tracker/jira.go internal/tracker/jira_test.go
git commit -m "feat(tracker): add Jira Cloud tracker implementation"
```

---

### Task 13: Linear Tracker

**Files:**
- Create: `internal/tracker/linear.go`
- Create: `internal/tracker/linear_test.go`

**Step 1: Write the failing test**

```go
// internal/tracker/linear_test.go
package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinearTracker_FetchReadyTickets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer lin-key", r.Header.Get("Authorization"))

		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"issues": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"identifier":  "ENG-42",
							"title":       "Fix dashboard crash",
							"description": "The dashboard crashes when clicking settings.",
							"priority":    float64(2),
							"labels": map[string]interface{}{
								"nodes": []map[string]interface{}{
									{"name": "foreman"},
								},
							},
							"assignee": map[string]interface{}{
								"name": "Alice",
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tracker := NewLinearTracker("lin-key", "foreman", srv.URL)
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "ENG-42", tickets[0].ExternalID)
	assert.Equal(t, "Fix dashboard crash", tickets[0].Title)
}

func TestLinearTracker_ProviderName(t *testing.T) {
	tracker := NewLinearTracker("key", "foreman", "")
	assert.Equal(t, "linear", tracker.ProviderName())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestLinear -v`
Expected: FAIL — NewLinearTracker not defined

**Step 3: Write minimal implementation**

```go
// internal/tracker/linear.go
package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type LinearTracker struct {
	apiKey  string
	label   string
	baseURL string
	client  *http.Client
}

func NewLinearTracker(apiKey, pickupLabel, baseURL string) *LinearTracker {
	if baseURL == "" {
		baseURL = "https://api.linear.app"
	}
	return &LinearTracker{
		apiKey:  apiKey,
		label:   pickupLabel,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (l *LinearTracker) ProviderName() string { return "linear" }

type linearGraphQLResponse struct {
	Data struct {
		Issues struct {
			Nodes []linearIssue `json:"nodes"`
		} `json:"issues"`
	} `json:"data"`
}

type linearIssue struct {
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    float64 `json:"priority"`
	Labels      struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Assignee *struct {
		Name string `json:"name"`
	} `json:"assignee"`
}

func (l *LinearTracker) FetchReadyTickets(ctx context.Context) ([]TrackerTicket, error) {
	query := fmt.Sprintf(`{
		issues(filter: { labels: { name: { eq: "%s" } }, state: { type: { neq: "completed" } } }) {
			nodes {
				identifier title description priority
				labels { nodes { name } }
				assignee { name }
			}
		}
	}`, l.label)

	body, err := l.graphql(ctx, query)
	if err != nil {
		return nil, err
	}

	var result linearGraphQLResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("linear unmarshal: %w", err)
	}

	tickets := make([]TrackerTicket, 0, len(result.Data.Issues.Nodes))
	for _, issue := range result.Data.Issues.Nodes {
		t := TrackerTicket{
			ExternalID:  issue.Identifier,
			Title:       issue.Title,
			Description: issue.Description,
			Priority:    fmt.Sprintf("%d", int(issue.Priority)),
		}
		for _, lbl := range issue.Labels.Nodes {
			t.Labels = append(t.Labels, lbl.Name)
		}
		if issue.Assignee != nil {
			t.Assignee = issue.Assignee.Name
		}
		tickets = append(tickets, t)
	}
	return tickets, nil
}

func (l *LinearTracker) GetTicket(ctx context.Context, externalID string) (*TrackerTicket, error) {
	query := fmt.Sprintf(`{
		issue(id: "%s") {
			identifier title description priority
			labels { nodes { name } }
			assignee { name }
		}
	}`, externalID)

	body, err := l.graphql(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Issue linearIssue `json:"issue"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	t := &TrackerTicket{
		ExternalID:  result.Data.Issue.Identifier,
		Title:       result.Data.Issue.Title,
		Description: result.Data.Issue.Description,
	}
	return t, nil
}

func (l *LinearTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	return nil // Linear state transitions handled via GraphQL mutations — deferred
}

func (l *LinearTracker) AddComment(ctx context.Context, externalID, comment string) error {
	query := fmt.Sprintf(`mutation { commentCreate(input: { issueId: "%s", body: %q }) { success } }`, externalID, comment)
	_, err := l.graphql(ctx, query)
	return err
}

func (l *LinearTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return l.AddComment(ctx, externalID, fmt.Sprintf("PR created: %s", prURL))
}

func (l *LinearTracker) AssignTicket(ctx context.Context, externalID, assignee string) error {
	return nil
}

func (l *LinearTracker) AddLabel(ctx context.Context, externalID, label string) error {
	return nil // Linear labels via GraphQL mutations — deferred
}

func (l *LinearTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	return nil
}

func (l *LinearTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	ticket, err := l.GetTicket(ctx, externalID)
	if err != nil {
		return false, err
	}
	for _, lbl := range ticket.Labels {
		if lbl == label {
			return true, nil
		}
	}
	return false, nil
}

func (l *LinearTracker) graphql(ctx context.Context, query string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.apiKey)

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("linear API error (status %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tracker/ -run TestLinear -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tracker/linear.go internal/tracker/linear_test.go
git commit -m "feat(tracker): add Linear tracker implementation (GraphQL)"
```

---

### Task 14: PostgreSQL Database Implementation

**Files:**
- Create: `internal/db/postgres.go`
- Create: `internal/db/postgres_test.go`

> **Note:** Tests use a stub/mock approach since actual PostgreSQL is not available in CI without setup. Integration tests with a real PostgreSQL are deferred.

**Step 1: Write the failing test**

```go
// internal/db/postgres_test.go
package db

import (
	"testing"
)

func TestNewPostgresDB_InvalidURL(t *testing.T) {
	_, err := NewPostgresDB("invalid://url", 5)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestNewPostgresDB -v`
Expected: FAIL — NewPostgresDB not defined

**Step 3: Write minimal implementation**

```go
// internal/db/postgres.go
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/canhta/foreman/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

type PostgresDB struct {
	db *sqlx.DB
}

func NewPostgresDB(url string, maxConns int) (*PostgresDB, error) {
	db, err := sqlx.Connect("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	db.SetMaxOpenConns(maxConns)

	// Run schema migrations
	if err := runPostgresSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres schema: %w", err)
	}

	return &PostgresDB{db: db}, nil
}

func runPostgresSchema(db *sqlx.DB) error {
	_, err := db.Exec(SchemaSQL)
	return err
}

func (p *PostgresDB) Close() error { return p.db.Close() }

// All methods delegate to sqlx with the same queries as SQLite.
// PostgreSQL-compatible SQL is nearly identical for our schema.

func (p *PostgresDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, acceptance_criteria, labels, priority, status, repo_url, branch_name, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		t.ID, t.ExternalID, t.Title, t.Description, t.AcceptanceCriteria, "[]", t.Priority, t.Status, t.RepoURL, t.BranchName, t.CreatedAt, t.UpdatedAt)
	return err
}

func (p *PostgresDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tickets SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
	return err
}

func (p *PostgresDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	var t models.Ticket
	err := p.db.GetContext(ctx, &t, `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE id=$1`, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &t, err
}

func (p *PostgresDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	var t models.Ticket
	err := p.db.GetContext(ctx, &t, `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE external_id=$1`, externalID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &t, err
}

func (p *PostgresDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets`
	args := []interface{}{}
	if len(filter.StatusIn) > 0 {
		query += ` WHERE status = ANY($1)`
		args = append(args, filter.StatusIn)
	}
	query += ` ORDER BY created_at DESC`
	var tickets []models.Ticket
	err := p.db.SelectContext(ctx, &tickets, query, args...)
	return tickets, err
}

func (p *PostgresDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tickets SET last_completed_task_seq=$1 WHERE id=$2`, taskSeq, ticketID)
	return err
}

func (p *PostgresDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	for _, t := range tasks {
		_, err := p.db.ExecContext(ctx,
			`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria, files_to_modify, status, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			t.ID, ticketID, t.Sequence, t.Title, t.Description, "[]", "[]", t.Status, t.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tasks SET status=$1 WHERE id=$2`, status, id)
	return err
}

func (p *PostgresDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	var count int
	err := p.db.GetContext(ctx, &count, `UPDATE tasks SET total_llm_calls=total_llm_calls+1 WHERE id=$1 RETURNING total_llm_calls`, id)
	return count, err
}

func (p *PostgresDB) RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO llm_calls (id, ticket_id, task_id, role, provider, model, attempt, tokens_input, tokens_output, cost_usd, duration_ms, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		call.ID, call.TicketID, call.TaskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs, call.Status, call.CreatedAt)
	return err
}

func (p *PostgresDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt)
	return err
}

func (p *PostgresDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	var handoffs []models.HandoffRecord
	err := p.db.SelectContext(ctx, &handoffs,
		`SELECT id, ticket_id, from_role, to_role, key, value, created_at FROM handoffs WHERE ticket_id=$1 AND (to_role=$2 OR to_role IS NULL)`,
		ticketID, forRole)
	return handoffs, err
}

func (p *PostgresDB) SaveProgressPattern(ctx context.Context, pp *models.ProgressPattern) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		pp.ID, pp.TicketID, pp.PatternKey, pp.PatternValue, pp.Directories, pp.CreatedAt)
	return err
}

func (p *PostgresDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	var patterns []models.ProgressPattern
	err := p.db.SelectContext(ctx, &patterns,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, created_at FROM progress_patterns WHERE ticket_id=$1`,
		ticketID)
	return patterns, err
}

func (p *PostgresDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	for _, path := range paths {
		_, err := p.db.ExecContext(ctx,
			`INSERT INTO file_reservations (file_path, ticket_id, reserved_at) VALUES ($1,$2,NOW()) ON CONFLICT DO NOTHING`,
			path, ticketID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE file_reservations SET released_at=NOW() WHERE ticket_id=$1 AND released_at IS NULL`, ticketID)
	return err
}

func (p *PostgresDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT file_path, ticket_id FROM file_reservations WHERE released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var path, tid string
		rows.Scan(&path, &tid)
		result[path] = tid
	}
	return result, nil
}

func (p *PostgresDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := p.db.GetContext(ctx, &cost, `SELECT COALESCE(SUM(cost_usd),0) FROM llm_calls WHERE ticket_id=$1`, ticketID)
	return cost, err
}

func (p *PostgresDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := p.db.GetContext(ctx, &cost, `SELECT COALESCE(total_usd,0) FROM cost_daily WHERE date=$1`, date)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (p *PostgresDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES ($1,$2) ON CONFLICT (date) DO UPDATE SET total_usd=cost_daily.total_usd+$2`,
		date, amount)
	return err
}

func (p *PostgresDB) RecordEvent(ctx context.Context, e *models.EventRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO events (id, ticket_id, task_id, event_type, severity, message, details, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.TicketID, e.TaskID, e.EventType, "info", e.EventType, e.Metadata, e.CreatedAt)
	return err
}

func (p *PostgresDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	var events []models.EventRecord
	err := p.db.SelectContext(ctx, &events,
		`SELECT id, ticket_id, task_id, event_type, created_at FROM events WHERE ticket_id=$1 ORDER BY created_at DESC LIMIT $2`,
		ticketID, limit)
	return events, err
}

func (p *PostgresDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES ($1,$2,NOW())`,
		tokenHash, name)
	return err
}

func (p *PostgresDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var exists bool
	err := p.db.GetContext(ctx, &exists,
		`SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE token_hash=$1 AND revoked=FALSE)`,
		tokenHash)
	return exists, err
}
```

**Step 4: Run tests**

Run: `go test ./internal/db/ -run TestNewPostgresDB -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/postgres.go internal/db/postgres_test.go
git commit -m "feat(db): add PostgreSQL implementation of Database interface"
```

---

### Task 15: Clarification Timeout Checker

**Files:**
- Modify: `internal/daemon/daemon.go` (created in Task 7)
- Create: `internal/daemon/clarification_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/clarification_test.go
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type mockClarificationDB struct {
	tickets        []models.Ticket
	updatedStatus  map[string]models.TicketStatus
	events         []*models.EventRecord
}

func (m *mockClarificationDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	var result []models.Ticket
	for _, t := range m.tickets {
		for _, s := range filter.StatusIn {
			if t.Status == s {
				result = append(result, t)
			}
		}
	}
	return result, nil
}

func (m *mockClarificationDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.updatedStatus[id] = status
	return nil
}

func (m *mockClarificationDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	m.events = append(m.events, e)
	return nil
}

type mockClarificationTracker struct {
	comments      map[string][]string
	removedLabels map[string][]string
}

func (m *mockClarificationTracker) AddComment(_ context.Context, externalID, comment string) error {
	m.comments[externalID] = append(m.comments[externalID], comment)
	return nil
}

func (m *mockClarificationTracker) RemoveLabel(_ context.Context, externalID, label string) error {
	m.removedLabels[externalID] = append(m.removedLabels[externalID], label)
	return nil
}

func TestCheckClarificationTimeouts(t *testing.T) {
	past := time.Now().Add(-25 * time.Hour)
	db := &mockClarificationDB{
		tickets: []models.Ticket{
			{
				ID:         "t1",
				ExternalID: "PROJ-1",
				Status:     models.TicketStatusClarificationNeeded,
				ClarificationRequestedAt: &past,
			},
		},
		updatedStatus: make(map[string]models.TicketStatus),
	}
	tracker := &mockClarificationTracker{
		comments:      make(map[string][]string),
		removedLabels: make(map[string][]string),
	}

	checkClarificationTimeouts(context.Background(), db, tracker, 24, "foreman:clarification")

	if db.updatedStatus["t1"] != models.TicketStatusBlocked {
		t.Errorf("expected blocked, got %s", db.updatedStatus["t1"])
	}
	if len(tracker.comments["PROJ-1"]) == 0 {
		t.Error("expected comment on timed-out ticket")
	}
	if len(tracker.removedLabels["PROJ-1"]) == 0 {
		t.Error("expected clarification label removed")
	}
}

func TestCheckClarificationTimeouts_NotExpired(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	db := &mockClarificationDB{
		tickets: []models.Ticket{
			{
				ID:         "t2",
				ExternalID: "PROJ-2",
				Status:     models.TicketStatusClarificationNeeded,
				ClarificationRequestedAt: &recent,
			},
		},
		updatedStatus: make(map[string]models.TicketStatus),
	}
	tracker := &mockClarificationTracker{
		comments:      make(map[string][]string),
		removedLabels: make(map[string][]string),
	}

	checkClarificationTimeouts(context.Background(), db, tracker, 24, "foreman:clarification")

	if _, ok := db.updatedStatus["t2"]; ok {
		t.Error("should not update non-expired ticket")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestCheckClarificationTimeouts -v`
Expected: FAIL — checkClarificationTimeouts not defined

**Step 3: Write minimal implementation**

Add to daemon package (new file or append to `daemon.go`):

```go
// internal/daemon/clarification.go
package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// ClarificationDB is the subset of db.Database needed for timeout checks.
type ClarificationDB interface {
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// ClarificationTracker is the subset of tracker.IssueTracker needed for timeout checks.
type ClarificationTracker interface {
	AddComment(ctx context.Context, externalID, comment string) error
	RemoveLabel(ctx context.Context, externalID, label string) error
}

func checkClarificationTimeouts(ctx context.Context, db ClarificationDB, tracker ClarificationTracker, timeoutHours int, clarificationLabel string) {
	tickets, err := db.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{models.TicketStatusClarificationNeeded},
	})
	if err != nil {
		return
	}

	timeout := time.Duration(timeoutHours) * time.Hour
	for _, t := range tickets {
		if t.ClarificationRequestedAt == nil || time.Since(*t.ClarificationRequestedAt) <= timeout {
			continue
		}

		tracker.AddComment(ctx, t.ExternalID, fmt.Sprintf(
			"No response received after %d hours. Marking as blocked. "+
				"Re-apply the pickup label to retry after updating the ticket.",
			timeoutHours,
		))
		tracker.RemoveLabel(ctx, t.ExternalID, clarificationLabel)
		db.UpdateTicketStatus(ctx, t.ID, models.TicketStatusBlocked)
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/ -run TestCheckClarificationTimeouts -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/clarification.go internal/daemon/clarification_test.go
git commit -m "feat(daemon): add clarification timeout checker"
```

---

### Task 16: `shouldPickUp` Re-Entry Guard

**Files:**
- Modify: `internal/daemon/daemon.go` or create `internal/daemon/pickup.go`
- Create: `internal/daemon/pickup_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/pickup_test.go
package daemon

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockPickupDB struct {
	tickets map[string]*models.Ticket
}

func (m *mockPickupDB) GetTicketByExternalID(_ context.Context, externalID string) (*models.Ticket, error) {
	t, ok := m.tickets[externalID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return t, nil
}

type mockPickupTracker struct {
	labels map[string][]string
}

func (m *mockPickupTracker) HasLabel(_ context.Context, externalID, label string) (bool, error) {
	for _, l := range m.labels[externalID] {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}

func TestShouldPickUp_NewTicket(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{}}
	tracker := &mockPickupTracker{labels: map[string][]string{}}

	if !shouldPickUp(context.Background(), db, tracker, "NEW-1", "foreman:clarification") {
		t.Error("expected true for new ticket")
	}
}

func TestShouldPickUp_ClarificationWithLabel(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-1": {ID: "t1", ExternalID: "PROJ-1", Status: models.TicketStatusClarificationNeeded},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{
		"PROJ-1": {"foreman:clarification"},
	}}

	if shouldPickUp(context.Background(), db, tracker, "PROJ-1", "foreman:clarification") {
		t.Error("expected false — still has clarification label")
	}
}

func TestShouldPickUp_ClarificationLabelRemoved(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-2": {ID: "t2", ExternalID: "PROJ-2", Status: models.TicketStatusClarificationNeeded},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{
		"PROJ-2": {},
	}}

	if !shouldPickUp(context.Background(), db, tracker, "PROJ-2", "foreman:clarification") {
		t.Error("expected true — clarification label was removed (author responded)")
	}
}

func TestShouldPickUp_ActiveTicket(t *testing.T) {
	db := &mockPickupDB{tickets: map[string]*models.Ticket{
		"PROJ-3": {ID: "t3", ExternalID: "PROJ-3", Status: models.TicketStatusImplementing},
	}}
	tracker := &mockPickupTracker{labels: map[string][]string{}}

	if shouldPickUp(context.Background(), db, tracker, "PROJ-3", "foreman:clarification") {
		t.Error("expected false — ticket already active")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestShouldPickUp -v`
Expected: FAIL — shouldPickUp not defined

**Step 3: Write minimal implementation**

```go
// internal/daemon/pickup.go
package daemon

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// PickupDB is the subset of db.Database needed for pickup guard.
type PickupDB interface {
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
}

// PickupTracker is the subset of tracker.IssueTracker needed for pickup guard.
type PickupTracker interface {
	HasLabel(ctx context.Context, externalID, label string) (bool, error)
}

func shouldPickUp(ctx context.Context, db PickupDB, tracker PickupTracker, externalID, clarificationLabel string) bool {
	existing, err := db.GetTicketByExternalID(ctx, externalID)
	if err != nil {
		return true // New ticket, safe to pick up
	}
	if existing.Status == models.TicketStatusClarificationNeeded {
		hasLabel, _ := tracker.HasLabel(ctx, externalID, clarificationLabel)
		return !hasLabel
	}
	return existing.Status == models.TicketStatusQueued
}

// Ensure fmt is used (for the mock in tests)
var _ = fmt.Sprintf
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/ -run TestShouldPickUp -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/pickup.go internal/daemon/pickup_test.go
git commit -m "feat(daemon): add shouldPickUp re-entry guard for clarification flow"
```

---

### Task 17: CLI — `init` and `logs` Commands

**Files:**
- Create: `cmd/init.go`
- Create: `cmd/logs.go`

**Step 1: Create init command**

```go
// cmd/init.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initAnalyze bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize foreman.toml in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := filepath.Join(".", "foreman.toml")
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("foreman.toml already exists")
		}

		template := `# Foreman configuration — see foreman.example.toml for all options

[daemon]
poll_interval_secs = 60
max_parallel_tickets = 3
work_dir = "~/.foreman/work"
log_level = "info"

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[tracker]
provider = "github"
pickup_label = "foreman"

[git]
default_branch = "main"
branch_prefix = "foreman/"
pr_draft = true

[database]
driver = "sqlite"

[database.sqlite]
path = "~/.foreman/foreman.db"
`
		if err := os.WriteFile(configPath, []byte(template), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Println("Created foreman.toml")

		if initAnalyze {
			fmt.Println("Analyzing repository...")
			// TODO: Wire to context.AnalyzeRepo() when available
			fmt.Println("Note: --analyze will generate .foreman-context.md in a future release")
		}

		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&initAnalyze, "analyze", false, "Scan repo and generate .foreman-context.md")
	rootCmd.AddCommand(initCmd)
}
```

**Step 2: Create logs command**

```go
// cmd/logs.go
package cmd

import (
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [TICKET_ID]",
	Short: "Show event log for a ticket",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		ticketID := ""
		if len(args) > 0 {
			ticketID = args[0]
		}

		events, err := database.GetEvents(cmd.Context(), ticketID, 100)
		if err != nil {
			return err
		}

		for _, e := range events {
			fmt.Printf("%s  %-30s  %s\n", e.CreatedAt.Format(time.RFC3339), e.EventType, e.TicketID)
		}

		if logsFollow {
			fmt.Println("\n-- follow mode: polling every 2s (Ctrl+C to stop) --")
			// In follow mode, poll for new events
			// Full implementation deferred — requires event emitter integration
		}

		return nil
	},
}

func init() {
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "Tail events in real time")
	rootCmd.AddCommand(logsCmd)
}
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/init.go cmd/logs.go
git commit -m "feat(cli): add init and logs commands"
```
