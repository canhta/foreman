# Ticket Decomposition & PR Merge Lifecycle — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add ticket auto-decomposition into child tracker issues and PR merge polling with lifecycle completion, enabling post_merge hooks for deployment skills.

**Architecture:** A new `Decompose` pipeline stage detects oversized tickets (deterministic heuristics), uses LLM to generate 3-6 child ticket specs, and creates them via a new `CreateTicket()` tracker interface method. A dedicated `MergeChecker` daemon goroutine polls open PRs for merge/close status, fires `post_merge` hooks, and auto-closes parent tickets when all children merge.

**Tech Stack:** Go 1.24, SQLite, zerolog, stretchr/testify, existing LLM/tracker/skills interfaces

---

### Task 1: Add New Ticket Statuses

**Files:**
- Modify: `internal/models/pipeline.go:7-18`

**Step 1: Add the five new TicketStatus constants**

After `TicketStatusBlocked` (line 18), add:

```go
TicketStatusDecomposing   TicketStatus = "decomposing"
TicketStatusDecomposed    TicketStatus = "decomposed"
TicketStatusAwaitingMerge TicketStatus = "awaiting_merge"
TicketStatusMerged        TicketStatus = "merged"
TicketStatusPRClosed      TicketStatus = "pr_closed"
```

**Step 2: Verify compilation**

Run: `go build ./internal/models/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/models/pipeline.go
git commit -m "feat(models): add decompose and merge lifecycle ticket statuses"
```

---

### Task 2: Add Parent/Child Fields to Ticket Model

**Files:**
- Modify: `internal/models/ticket.go:5-34`

**Step 1: Add three fields to the Ticket struct**

Add after `IsPartial bool` (line 33):

```go
ParentTicketID string
ChildTicketIDs []string
DecomposeDepth int
```

**Step 2: Verify compilation**

Run: `go build ./internal/models/...`
Expected: success

**Step 3: Commit**

```bash
git add internal/models/ticket.go
git commit -m "feat(models): add parent/child ticket fields for decomposition"
```

---

### Task 3: Add DecomposeConfig and Extend DaemonConfig/HooksConfig

**Files:**
- Modify: `internal/models/config.go:1-5` (add DecomposeConfig to Config struct)
- Modify: `internal/models/config.go:50-59` (add MergeCheckIntervalSecs to DaemonConfig)
- Modify: `internal/models/config.go:197-205` (add Decompose field to Config, PostMerge to HooksConfig)

**Step 1: Add DecomposeConfig struct**

Add after `RateLimitConfig` (line 157):

```go
type DecomposeConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	MaxTicketWords   int    `mapstructure:"max_ticket_words"`
	MaxScopeKeywords int    `mapstructure:"max_scope_keywords"`
	ApprovalLabel    string `mapstructure:"approval_label"`
	ParentLabel      string `mapstructure:"parent_label"`
}
```

**Step 2: Add Decompose field to Config struct**

Add after the `RateLimit` field (line 18):

```go
Decompose DecomposeConfig `mapstructure:"decompose"`
```

**Step 3: Add MergeCheckIntervalSecs to DaemonConfig**

Add after `TaskTimeoutMinutes` (line 58):

```go
MergeCheckIntervalSecs int `mapstructure:"merge_check_interval_secs"`
```

**Step 4: Add PostMerge to HooksConfig**

Add after `PostPR` (line 204):

```go
PostMerge []string `mapstructure:"post_merge"`
```

**Step 5: Verify compilation**

Run: `go build ./internal/models/...`
Expected: success

**Step 6: Commit**

```bash
git add internal/models/config.go
git commit -m "feat(models): add DecomposeConfig, merge check interval, post_merge hook config"
```

---

### Task 4: Add Config Defaults for New Fields

**Files:**
- Modify: `internal/config/config.go:41-130` (add defaults in setDefaults)

**Step 1: Add decompose defaults**

Add after the `rate_limit` defaults block (after line 105):

```go
v.SetDefault("decompose.enabled", false)
v.SetDefault("decompose.max_ticket_words", 150)
v.SetDefault("decompose.max_scope_keywords", 2)
v.SetDefault("decompose.approval_label", "foreman-ready")
v.SetDefault("decompose.parent_label", "foreman-decomposed")
```

**Step 2: Add merge check interval default**

Add after `daemon.task_timeout_minutes` default (after line 46):

```go
v.SetDefault("daemon.merge_check_interval_secs", 300)
```

**Step 3: Verify compilation**

Run: `go build ./internal/config/...`
Expected: success

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add defaults for decompose and merge check interval"
```

---

### Task 5: Extend Database Schema for Parent/Child Tickets

**Files:**
- Modify: `internal/db/schema.go:3-140`

**Step 1: Add parent_ticket_id and decompose_depth columns to tickets table**

Add after `pr_number INTEGER,` (line 17):

```sql
parent_ticket_id TEXT DEFAULT '',
decompose_depth INTEGER DEFAULT 0,
```

**Step 2: Add index for parent ticket lookups**

Add after the last CREATE INDEX (line 139):

```sql
CREATE INDEX IF NOT EXISTS idx_tickets_parent ON tickets(parent_ticket_id);
```

**Step 3: Verify compilation**

Run: `go build ./internal/db/...`
Expected: success

**Step 4: Commit**

```bash
git add internal/db/schema.go
git commit -m "feat(db): add parent_ticket_id and decompose_depth to schema"
```

---

### Task 6: Update SQLite Implementation for Parent/Child Fields

**Files:**
- Modify: `internal/db/sqlite.go:52-88` (CreateTicket, scanTicket, GetTicket, GetTicketByExternalID queries)
- Modify: `internal/db/sqlite.go:90-127` (ListTickets query)
- Test: `internal/db/sqlite_test.go`

**Step 1: Write the failing test**

Create test for parent/child ticket operations. Add to `internal/db/sqlite_test.go`:

```go
func TestSQLiteDB_ParentChildTickets(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	parent := &models.Ticket{
		ID: "parent-1", ExternalID: "EXT-1", Title: "Parent",
		Description: "Parent ticket", Status: models.TicketStatusDecomposed,
		DecomposeDepth: 0, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, db.CreateTicket(ctx, parent))

	child := &models.Ticket{
		ID: "child-1", ExternalID: "EXT-2", Title: "Child",
		Description: "Child ticket", Status: models.TicketStatusQueued,
		ParentTicketID: "EXT-1", DecomposeDepth: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, db.CreateTicket(ctx, child))

	// Verify parent fields persisted
	got, err := db.GetTicket(ctx, "parent-1")
	require.NoError(t, err)
	assert.Equal(t, 0, got.DecomposeDepth)

	// Verify child fields persisted
	got, err = db.GetTicket(ctx, "child-1")
	require.NoError(t, err)
	assert.Equal(t, "EXT-1", got.ParentTicketID)
	assert.Equal(t, 1, got.DecomposeDepth)

	// Test GetChildTickets
	children, err := db.GetChildTickets(ctx, "EXT-1")
	require.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "child-1", children[0].ID)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestSQLiteDB_ParentChildTickets -v`
Expected: FAIL — `GetChildTickets` doesn't exist, CreateTicket doesn't include new columns

**Step 3: Update CreateTicket to include new columns**

In `sqlite.go:52-58`, update the INSERT to include `parent_ticket_id, decompose_depth`:

```go
func (s *SQLiteDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, parent_ticket_id, decompose_depth, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.ParentTicketID, t.DecomposeDepth, t.CreatedAt, t.UpdatedAt,
	)
	return err
}
```

**Step 4: Update scanTicket to read new columns**

Update `scanTicket` (line 79-88) and all SELECT queries that use it to include the new columns:

```go
func (s *SQLiteDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
		&t.ParentTicketID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}
```

Update `GetTicket` (line 69-72):

```go
func (s *SQLiteDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, decompose_depth, created_at, updated_at FROM tickets WHERE id = ?`, id))
}
```

Update `GetTicketByExternalID` (line 74-77):

```go
func (s *SQLiteDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, decompose_depth, created_at, updated_at FROM tickets WHERE external_id = ?`, externalID))
}
```

Update `ListTickets` query (line 91) and scan (line 120):

```go
query := `SELECT id, external_id, title, description, status, parent_ticket_id, decompose_depth, created_at, updated_at FROM tickets WHERE 1=1`
```

And the scan inside the loop:

```go
if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
	&t.ParentTicketID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
```

**Step 5: Add GetChildTickets to Database interface and SQLite implementation**

Add to `internal/db/db.go` after `ListTickets` (line 16):

```go
GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error)
```

Add implementation to `internal/db/sqlite.go`:

```go
func (s *SQLiteDB) GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, decompose_depth, created_at, updated_at
		 FROM tickets WHERE parent_ticket_id = ?`, parentExternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}
```

**Step 6: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestSQLiteDB_ParentChildTickets -v`
Expected: PASS

**Step 7: Run all DB tests to check for regressions**

Run: `go test ./internal/db/ -v`
Expected: All PASS (existing tests may need SELECT column updates too)

**Step 8: Commit**

```bash
git add internal/db/db.go internal/db/sqlite.go internal/db/sqlite_test.go
git commit -m "feat(db): support parent/child ticket storage and GetChildTickets query"
```

---

### Task 7: Add CreateTicket to Tracker Interface

**Files:**
- Modify: `internal/tracker/tracker.go:30-41`

**Step 1: Add CreateTicketRequest type and CreateTicket method**

Add after the `TicketComment` struct (line 28):

```go
// CreateTicketRequest describes a new ticket to create in the tracker.
type CreateTicketRequest struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Labels             []string
	ParentID           string
	Metadata           map[string]string
}
```

Add `CreateTicket` to the `IssueTracker` interface (after `ProviderName()`, line 40):

```go
CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error)
```

**Step 2: Verify compilation fails (implementations don't satisfy interface)**

Run: `go build ./internal/tracker/...`
Expected: FAIL — all four tracker implementations are missing `CreateTicket`

**Step 3: Commit**

```bash
git add internal/tracker/tracker.go
git commit -m "feat(tracker): add CreateTicket to IssueTracker interface"
```

---

### Task 8: Implement CreateTicket for GitHub Issues Tracker

**Files:**
- Modify: `internal/tracker/github_issues.go`
- Test: `internal/tracker/github_issues_test.go`

**Step 1: Write the failing test**

Create `internal/tracker/github_issues_test.go`:

```go
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

func TestGitHubIssuesTracker_CreateTicket(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/owner/repo/issues" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"number":   42,
				"html_url": "https://github.com/owner/repo/issues/42",
				"url":      "https://api.github.com/repos/owner/repo/issues/42",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "owner", "repo", "foreman-ready")
	ticket, err := tracker.CreateTicket(context.Background(), CreateTicketRequest{
		Title:       "Child ticket 1",
		Description: "Implement login form",
		Labels:      []string{"foreman-ready-pending"},
		ParentID:    "10",
	})

	require.NoError(t, err)
	assert.Equal(t, "42", ticket.ExternalID)
	assert.Equal(t, "Child ticket 1", ticket.Title)
	assert.Equal(t, []string{"foreman-ready-pending"}, receivedBody["labels"])
	// Verify parent reference in body
	assert.Contains(t, receivedBody["body"], "Parent: #10")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestGitHubIssuesTracker_CreateTicket -v`
Expected: FAIL — `CreateTicket` method doesn't exist

**Step 3: Implement CreateTicket on GitHubIssuesTracker**

Add to `internal/tracker/github_issues.go` before the `doGet` method:

```go
func (g *GitHubIssuesTracker) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	body := req.Description
	if req.ParentID != "" {
		body = fmt.Sprintf("Parent: #%s\n\n%s", req.ParentID, body)
	}
	if req.AcceptanceCriteria != "" {
		body += "\n\n## Acceptance Criteria\n" + req.AcceptanceCriteria
	}

	payload := map[string]interface{}{
		"title":  req.Title,
		"body":   body,
		"labels": req.Labels,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling create issue request: %w", err)
	}

	u := fmt.Sprintf("%s/repos/%s/%s/issues",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	g.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(respBody))
	}

	var issue ghIssue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("decoding issue response: %w", err)
	}

	return &Ticket{
		ExternalID:  strconv.Itoa(issue.Number),
		Title:       issue.Title,
		Description: issue.Body,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tracker/ -run TestGitHubIssuesTracker_CreateTicket -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tracker/github_issues.go internal/tracker/github_issues_test.go
git commit -m "feat(tracker): implement CreateTicket for GitHub Issues"
```

---

### Task 9: Implement CreateTicket for Jira, Linear, and LocalFile Trackers

**Files:**
- Modify: `internal/tracker/jira.go`
- Modify: `internal/tracker/linear.go`
- Modify: `internal/tracker/local_file.go`

**Step 1: Add CreateTicket to JiraTracker**

Add to `internal/tracker/jira.go`:

```go
func (j *JiraTracker) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	fields := map[string]interface{}{
		"project": map[string]string{"key": j.project},
		"summary": req.Title,
		"description": req.Description,
		"labels":  req.Labels,
		"issuetype": map[string]string{"name": "Task"},
	}
	if req.ParentID != "" {
		fields["parent"] = map[string]string{"key": req.ParentID}
	}

	payload := map[string]interface{}{"fields": fields}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jira marshal: %w", err)
	}

	u := fmt.Sprintf("%s/rest/api/2/issue", j.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", j.authHeader)

	resp, err := j.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jira create issue: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jira create issue: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("jira unmarshal: %w", err)
	}

	return &Ticket{
		ExternalID:  result.Key,
		Title:       req.Title,
		Description: req.Description,
	}, nil
}
```

**Step 2: Add CreateTicket to LinearTracker**

Add to `internal/tracker/linear.go`:

```go
func (l *LinearTracker) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	description := req.Description
	if req.ParentID != "" {
		description = fmt.Sprintf("Parent: %s\n\n%s", req.ParentID, description)
	}

	mutation := fmt.Sprintf(
		`mutation { issueCreate(input: { title: %q, description: %q }) { success issue { identifier title description } } }`,
		req.Title, description)

	body, err := l.graphql(ctx, mutation)
	if err != nil {
		return nil, fmt.Errorf("linear create issue: %w", err)
	}

	var result struct {
		Data struct {
			IssueCreate struct {
				Issue linearIssue `json:"issue"`
			} `json:"issueCreate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("linear unmarshal: %w", err)
	}

	issue := result.Data.IssueCreate.Issue
	return &Ticket{
		ExternalID:  issue.Identifier,
		Title:       issue.Title,
		Description: issue.Description,
	}, nil
}
```

**Step 3: Add CreateTicket to LocalFileTracker**

Add to `internal/tracker/local_file.go`:

```go
func (t *LocalFileTracker) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	// Generate a simple ID from timestamp
	externalID := fmt.Sprintf("local-%d", time.Now().UnixNano())

	lt := &localTicket{
		ExternalID:         externalID,
		Title:              req.Title,
		Description:        req.Description,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Labels:             req.Labels,
		Status:             "open",
	}

	if err := os.MkdirAll(t.ticketsDir(), 0o755); err != nil {
		return nil, fmt.Errorf("creating tickets dir: %w", err)
	}

	data, err := json.MarshalIndent(lt, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling ticket: %w", err)
	}

	path := filepath.Join(t.ticketsDir(), externalID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("writing ticket file: %w", err)
	}

	return &Ticket{
		ExternalID:         externalID,
		Title:              req.Title,
		Description:        req.Description,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Labels:             req.Labels,
	}, nil
}
```

**Step 4: Verify all tracker implementations compile**

Run: `go build ./internal/tracker/...`
Expected: success

**Step 5: Commit**

```bash
git add internal/tracker/jira.go internal/tracker/linear.go internal/tracker/local_file.go
git commit -m "feat(tracker): implement CreateTicket for Jira, Linear, and LocalFile trackers"
```

---

### Task 10: Add PRChecker Interface and GitHub Implementation

**Files:**
- Create: `internal/git/pr_checker.go`
- Create: `internal/git/pr_checker_test.go`

**Step 1: Write the failing test**

Create `internal/git/pr_checker_test.go`:

```go
package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubPRChecker_GetPRStatus_Merged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/pulls/42", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":     "closed",
			"merged":    true,
			"merged_at": "2026-03-06T12:00:00Z",
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 42)

	require.NoError(t, err)
	assert.Equal(t, "merged", status.State)
	assert.NotNil(t, status.MergedAt)
}

func TestGitHubPRChecker_GetPRStatus_Open(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":  "open",
			"merged": false,
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, "open", status.State)
}

func TestGitHubPRChecker_GetPRStatus_ClosedNotMerged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":     "closed",
			"merged":    false,
			"closed_at": "2026-03-06T12:00:00Z",
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, "closed", status.State)
	assert.NotNil(t, status.ClosedAt)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestGitHubPRChecker -v`
Expected: FAIL — types don't exist

**Step 3: Create `internal/git/pr_checker.go`**

```go
package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PRMergeStatus holds the merge state of a pull request.
type PRMergeStatus struct {
	State    string     // "open", "merged", "closed"
	MergedAt *time.Time
	ClosedAt *time.Time
}

// PRChecker checks the merge status of pull requests.
type PRChecker interface {
	GetPRStatus(ctx context.Context, prNumber int) (PRMergeStatus, error)
}

// GitHubPRChecker checks PR status via the GitHub REST API.
type GitHubPRChecker struct {
	client  *http.Client
	baseURL string
	token   string
	owner   string
	repo    string
}

// NewGitHubPRChecker creates a GitHub PR status checker.
func NewGitHubPRChecker(baseURL, token, owner, repo string) *GitHubPRChecker {
	return &GitHubPRChecker{
		baseURL: baseURL,
		token:   token,
		owner:   owner,
		repo:    repo,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

type ghPRStatusResponse struct {
	State    string     `json:"state"`
	Merged   bool       `json:"merged"`
	MergedAt *time.Time `json:"merged_at"`
	ClosedAt *time.Time `json:"closed_at"`
}

// GetPRStatus returns the current merge status of a pull request.
func (g *GitHubPRChecker) GetPRStatus(ctx context.Context, prNumber int) (PRMergeStatus, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", g.baseURL, g.owner, g.repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return PRMergeStatus{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.client.Do(req)
	if err != nil {
		return PRMergeStatus{}, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return PRMergeStatus{}, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}

	var pr ghPRStatusResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return PRMergeStatus{}, fmt.Errorf("decoding PR response: %w", err)
	}

	status := PRMergeStatus{
		MergedAt: pr.MergedAt,
		ClosedAt: pr.ClosedAt,
	}
	if pr.Merged {
		status.State = "merged"
	} else if pr.State == "closed" {
		status.State = "closed"
	} else {
		status.State = "open"
	}

	return status, nil
}

var _ PRChecker = (*GitHubPRChecker)(nil)
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run TestGitHubPRChecker -v`
Expected: All 3 PASS

**Step 5: Commit**

```bash
git add internal/git/pr_checker.go internal/git/pr_checker_test.go
git commit -m "feat(git): add PRChecker interface and GitHub implementation"
```

---

### Task 11: Add post_merge to Valid Skill Triggers

**Files:**
- Modify: `internal/skills/loader.go:60-64`

**Step 1: Add "post_merge" to validTriggers map**

At line 63, add:

```go
"post_merge": true,
```

**Step 2: Update the error message in LoadSkill**

At line 89, update the error message to include `post_merge`:

```go
return nil, fmt.Errorf("invalid trigger '%s' in skill '%s' (valid: post_lint, pre_pr, post_pr, post_merge)", skill.Trigger, skill.ID)
```

**Step 3: Verify compilation**

Run: `go build ./internal/skills/...`
Expected: success

**Step 4: Commit**

```bash
git add internal/skills/loader.go
git commit -m "feat(skills): add post_merge as valid skill trigger"
```

---

### Task 12: Implement Scope Detection (NeedsDecomposition)

**Files:**
- Create: `internal/pipeline/decompose.go`
- Create: `internal/pipeline/decompose_test.go`

**Step 1: Write the failing tests**

Create `internal/pipeline/decompose_test.go`:

```go
package pipeline

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNeedsDecomposition(t *testing.T) {
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   10,
		MaxScopeKeywords: 2,
	}

	tests := []struct {
		name   string
		ticket *models.Ticket
		want   bool
	}{
		{
			name:   "disabled config",
			ticket: &models.Ticket{Description: "a very long description that exceeds ten words for sure definitely"},
			want:   false,
		},
		{
			name:   "short ticket",
			ticket: &models.Ticket{Description: "fix the login button"},
			want:   false,
		},
		{
			name:   "long ticket exceeds word limit",
			ticket: &models.Ticket{Description: "implement the full user authentication system with OAuth2 support and email verification and password reset"},
			want:   true,
		},
		{
			name:   "scope keywords exceed threshold",
			ticket: &models.Ticket{Description: "add login and also add signup plus add password reset"},
			want:   true,
		},
		{
			name:   "child ticket never decomposes",
			ticket: &models.Ticket{Description: "implement the full user authentication system with OAuth2 support and email verification and password reset", DecomposeDepth: 1},
			want:   false,
		},
		{
			name:   "vague and long - no acceptance criteria",
			ticket: &models.Ticket{Description: strings.Repeat("word ", 101), AcceptanceCriteria: ""},
			want:   true,
		},
		{
			name:   "long but has acceptance criteria",
			ticket: &models.Ticket{Description: strings.Repeat("word ", 101), AcceptanceCriteria: "User can log in"},
			want:   true, // still exceeds word count
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			if tt.name == "disabled config" {
				c = &models.DecomposeConfig{Enabled: false}
			}
			got := NeedsDecomposition(tt.ticket, c)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCountScopeKeywords(t *testing.T) {
	assert.Equal(t, 0, countScopeKeywords("fix the login button"))
	assert.Equal(t, 3, countScopeKeywords("add login and also add signup plus add password reset"))
	assert.Equal(t, 1, countScopeKeywords("do this additionally"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestNeedsDecomposition -v`
Expected: FAIL — functions don't exist

**Step 3: Create `internal/pipeline/decompose.go`**

```go
package pipeline

import (
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// scopeKeywords are words that suggest a ticket covers multiple features.
var scopeKeywords = []string{"and", "also", "plus", "additionally"}

// NeedsDecomposition returns true if the ticket is too large for a single PR.
func NeedsDecomposition(ticket *models.Ticket, cfg *models.DecomposeConfig) bool {
	if !cfg.Enabled || ticket.DecomposeDepth > 0 {
		return false
	}
	wordCount := len(strings.Fields(ticket.Description))
	if wordCount > cfg.MaxTicketWords {
		return true
	}
	if countScopeKeywords(ticket.Description) > cfg.MaxScopeKeywords {
		return true
	}
	vagueAndLong := ticket.AcceptanceCriteria == "" && wordCount > 100
	return vagueAndLong
}

// countScopeKeywords counts occurrences of scope-expanding words.
func countScopeKeywords(text string) int {
	lower := strings.ToLower(text)
	count := 0
	for _, kw := range scopeKeywords {
		// Count word-boundary matches by splitting on spaces
		words := strings.Fields(lower)
		for _, w := range words {
			if w == kw {
				count++
			}
		}
	}
	return count
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -run "TestNeedsDecomposition|TestCountScopeKeywords" -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/pipeline/decompose.go internal/pipeline/decompose_test.go
git commit -m "feat(pipeline): add NeedsDecomposition scope detection"
```

---

### Task 13: Implement Decomposer (LLM + Tracker Integration)

**Files:**
- Modify: `internal/pipeline/decompose.go` (add Decomposer struct and Execute method)
- Create: `internal/pipeline/decomposer_test.go`

**Step 1: Write the failing test**

Create `internal/pipeline/decomposer_test.go`:

```go
package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM returns a canned decomposition response.
type mockLLM struct {
	response string
}

func (m *mockLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response}, nil
}
func (m *mockLLM) ProviderName() string                        { return "mock" }
func (m *mockLLM) HealthCheck(_ context.Context) error         { return nil }

// mockTracker records CreateTicket calls.
type mockTracker struct {
	created []tracker.CreateTicketRequest
	labels  map[string][]string
	comments map[string][]string
}

func newMockTracker() *mockTracker {
	return &mockTracker{
		labels:   make(map[string][]string),
		comments: make(map[string][]string),
	}
}

func (m *mockTracker) CreateTicket(_ context.Context, req tracker.CreateTicketRequest) (*tracker.Ticket, error) {
	m.created = append(m.created, req)
	return &tracker.Ticket{
		ExternalID: fmt.Sprintf("CHILD-%d", len(m.created)),
		Title:      req.Title,
	}, nil
}
func (m *mockTracker) AddLabel(_ context.Context, id, label string) error {
	m.labels[id] = append(m.labels[id], label)
	return nil
}
func (m *mockTracker) AddComment(_ context.Context, id, comment string) error {
	m.comments[id] = append(m.comments[id], comment)
	return nil
}
func (m *mockTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) { return nil, nil }
func (m *mockTracker) GetTicket(_ context.Context, _ string) (*tracker.Ticket, error) { return nil, nil }
func (m *mockTracker) UpdateStatus(_ context.Context, _, _ string) error { return nil }
func (m *mockTracker) AttachPR(_ context.Context, _, _ string) error { return nil }
func (m *mockTracker) RemoveLabel(_ context.Context, _, _ string) error { return nil }
func (m *mockTracker) HasLabel(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (m *mockTracker) ProviderName() string { return "mock" }

func TestDecomposer_Execute(t *testing.T) {
	result := DecompositionResult{
		Children: []ChildTicketSpec{
			{Title: "Setup auth models", Description: "Create user model", AcceptanceCriteria: []string{"User table exists"}, EstimatedComplexity: "low"},
			{Title: "Implement login", Description: "Add login endpoint", AcceptanceCriteria: []string{"POST /login works"}, EstimatedComplexity: "medium", DependsOn: []string{"Setup auth models"}},
		},
		Rationale: "Ticket covers two distinct features",
	}
	respJSON, _ := json.Marshal(result)

	llm := &mockLLM{response: string(respJSON)}
	tr := newMockTracker()
	cfg := &models.DecomposeConfig{
		ApprovalLabel: "foreman-ready",
		ParentLabel:   "foreman-decomposed",
	}

	decomposer := NewDecomposer(llm, tr, cfg)
	ticket := &models.Ticket{
		ID: "T1", ExternalID: "EXT-1", Title: "Auth system",
		Description: "Implement full auth", Status: models.TicketStatusQueued,
	}

	childIDs, err := decomposer.Execute(context.Background(), ticket)
	require.NoError(t, err)

	assert.Len(t, childIDs, 2)
	assert.Len(t, tr.created, 2)
	assert.Equal(t, "Setup auth models", tr.created[0].Title)
	assert.Equal(t, "EXT-1", tr.created[0].ParentID)
	assert.Contains(t, tr.created[0].Labels, "foreman-ready-pending")
	assert.Contains(t, tr.labels["EXT-1"], "foreman-decomposed")
	assert.Len(t, tr.comments["EXT-1"], 1)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestDecomposer_Execute -v`
Expected: FAIL — `Decomposer` type doesn't exist

**Step 3: Add Decomposer to `internal/pipeline/decompose.go`**

Append to the existing file:

```go
// DecompositionResult is the structured output from the decomposition LLM call.
type DecompositionResult struct {
	Children  []ChildTicketSpec `json:"children"`
	Rationale string            `json:"rationale"`
}

// ChildTicketSpec describes a single child ticket to create.
type ChildTicketSpec struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	EstimatedComplexity string  `json:"estimated_complexity"`
	DependsOn          []string `json:"depends_on"`
}

// Decomposer breaks oversized tickets into child tracker issues.
type Decomposer struct {
	llm     LLMProvider
	tracker tracker.IssueTracker
	cfg     *models.DecomposeConfig
}

// NewDecomposer creates a new Decomposer.
func NewDecomposer(llm LLMProvider, tr tracker.IssueTracker, cfg *models.DecomposeConfig) *Decomposer {
	return &Decomposer{llm: llm, tracker: tr, cfg: cfg}
}

// Execute decomposes a ticket into child issues and creates them in the tracker.
// Returns the external IDs of the created child tickets.
func (d *Decomposer) Execute(ctx context.Context, ticket *models.Ticket) ([]string, error) {
	// 1. Generate child specs via LLM
	result, err := d.generateChildSpecs(ctx, ticket)
	if err != nil {
		return nil, fmt.Errorf("generating child specs: %w", err)
	}

	// 2. Create each child in tracker
	var childIDs []string
	for _, spec := range result.Children {
		created, err := d.tracker.CreateTicket(ctx, tracker.CreateTicketRequest{
			Title:              spec.Title,
			Description:        spec.Description,
			AcceptanceCriteria: strings.Join(spec.AcceptanceCriteria, "\n"),
			Labels:             []string{d.cfg.ApprovalLabel + "-pending"},
			ParentID:           ticket.ExternalID,
			Metadata:           map[string]string{"foreman_depth": "1"},
		})
		if err != nil {
			return childIDs, fmt.Errorf("creating child ticket %q: %w", spec.Title, err)
		}
		childIDs = append(childIDs, created.ExternalID)
	}

	// 3. Label parent as decomposed
	if err := d.tracker.AddLabel(ctx, ticket.ExternalID, d.cfg.ParentLabel); err != nil {
		return childIDs, fmt.Errorf("labeling parent: %w", err)
	}

	// 4. Comment on parent with summary
	comment := d.formatComment(result, childIDs)
	if err := d.tracker.AddComment(ctx, ticket.ExternalID, comment); err != nil {
		return childIDs, fmt.Errorf("commenting on parent: %w", err)
	}

	return childIDs, nil
}

func (d *Decomposer) generateChildSpecs(ctx context.Context, ticket *models.Ticket) (*DecompositionResult, error) {
	prompt := fmt.Sprintf(`Decompose this ticket into 3-6 child tickets. Each child should represent one reviewable PR that is independently testable.

Title: %s
Description: %s
Acceptance Criteria: %s

Return a JSON object with this schema:
{"children": [{"title": "...", "description": "...", "acceptance_criteria": ["..."], "estimated_complexity": "low|medium|high", "depends_on": ["title of other child"]}], "rationale": "..."}`,
		ticket.Title, ticket.Description, ticket.AcceptanceCriteria)

	resp, err := d.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: "You are a technical project manager. Decompose tickets into focused, implementable child tickets.",
		UserPrompt:   prompt,
		MaxTokens:    4096,
		Temperature:  0.2,
	})
	if err != nil {
		return nil, err
	}

	var result DecompositionResult
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w", err)
	}
	return &result, nil
}

func (d *Decomposer) formatComment(result *DecompositionResult, childIDs []string) string {
	var sb strings.Builder
	sb.WriteString("## Ticket Decomposed\n\n")
	sb.WriteString(fmt.Sprintf("**Rationale:** %s\n\n", result.Rationale))
	sb.WriteString("**Child tickets:**\n")
	for i, spec := range result.Children {
		id := "pending"
		if i < len(childIDs) {
			id = "#" + childIDs[i]
		}
		sb.WriteString(fmt.Sprintf("- %s (%s) — %s\n", spec.Title, id, spec.EstimatedComplexity))
	}
	sb.WriteString(fmt.Sprintf("\nApprove children by changing their label to `%s`.\n", d.cfg.ApprovalLabel))
	return sb.String()
}
```

Add the required imports to the file: `context`, `encoding/json`, `fmt`, `strings`, and the tracker package.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestDecomposer_Execute -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/decompose.go internal/pipeline/decomposer_test.go
git commit -m "feat(pipeline): implement Decomposer with LLM and tracker integration"
```

---

### Task 14: Implement MergeChecker

**Files:**
- Create: `internal/daemon/merge_checker.go`
- Create: `internal/daemon/merge_checker_test.go`

**Step 1: Write the failing tests**

Create `internal/daemon/merge_checker_test.go`:

```go
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDB implements the subset of db.Database needed by MergeChecker.
type mockDB struct {
	tickets      map[string]*models.Ticket
	statusUpdates map[string]models.TicketStatus
}

func newMockDB() *mockDB {
	return &mockDB{
		tickets:       make(map[string]*models.Ticket),
		statusUpdates: make(map[string]models.TicketStatus),
	}
}

func (m *mockDB) ListTickets(_ context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	var result []models.Ticket
	for _, t := range m.tickets {
		if filter.Status != "" && string(t.Status) == filter.Status {
			result = append(result, *t)
		}
		for _, s := range filter.StatusIn {
			if t.Status == s {
				result = append(result, *t)
				break
			}
		}
	}
	return result, nil
}

func (m *mockDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	m.statusUpdates[id] = status
	if t, ok := m.tickets[id]; ok {
		t.Status = status
	}
	return nil
}

func (m *mockDB) GetTicketByExternalID(_ context.Context, extID string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ExternalID == extID {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockDB) GetChildTickets(_ context.Context, parentExtID string) ([]models.Ticket, error) {
	var children []models.Ticket
	for _, t := range m.tickets {
		if t.ParentTicketID == parentExtID {
			children = append(children, *t)
		}
	}
	return children, nil
}

// mockPRChecker returns configured statuses per PR number.
type mockPRChecker struct {
	statuses map[int]git.PRMergeStatus
}

func (m *mockPRChecker) GetPRStatus(_ context.Context, prNumber int) (git.PRMergeStatus, error) {
	if s, ok := m.statuses[prNumber]; ok {
		return s, nil
	}
	return git.PRMergeStatus{State: "open"}, nil
}

func TestMergeChecker_HandleMerged(t *testing.T) {
	db := newMockDB()
	db.tickets["T1"] = &models.Ticket{
		ID: "T1", ExternalID: "EXT-1", Status: models.TicketStatusAwaitingMerge, PRNumber: 42,
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		42: {State: "merged", MergedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	assert.Equal(t, models.TicketStatusMerged, db.statusUpdates["T1"])
}

func TestMergeChecker_HandleClosed(t *testing.T) {
	db := newMockDB()
	db.tickets["T1"] = &models.Ticket{
		ID: "T1", ExternalID: "EXT-1", Status: models.TicketStatusAwaitingMerge, PRNumber: 10,
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		10: {State: "closed", ClosedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	assert.Equal(t, models.TicketStatusPRClosed, db.statusUpdates["T1"])
}

func TestMergeChecker_ParentCompletion(t *testing.T) {
	db := newMockDB()
	db.tickets["parent"] = &models.Ticket{
		ID: "parent", ExternalID: "EXT-P", Status: models.TicketStatusDecomposed,
	}
	db.tickets["child1"] = &models.Ticket{
		ID: "child1", ExternalID: "EXT-C1", Status: models.TicketStatusAwaitingMerge,
		PRNumber: 1, ParentTicketID: "EXT-P",
	}
	db.tickets["child2"] = &models.Ticket{
		ID: "child2", ExternalID: "EXT-C2", Status: models.TicketStatusMerged,
		ParentTicketID: "EXT-P",
	}

	now := time.Now()
	prChecker := &mockPRChecker{statuses: map[int]git.PRMergeStatus{
		1: {State: "merged", MergedAt: &now},
	}}

	mc := NewMergeChecker(db, prChecker, nil, nil, zerolog.Nop())
	mc.checkAll(context.Background())

	// child1 merged
	assert.Equal(t, models.TicketStatusMerged, db.statusUpdates["child1"])
	// parent should be done since both children are now merged
	assert.Equal(t, models.TicketStatusDone, db.statusUpdates["parent"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestMergeChecker -v`
Expected: FAIL — `MergeChecker` type doesn't exist

**Step 3: Create `internal/daemon/merge_checker.go`**

```go
package daemon

import (
	"context"
	"time"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/rs/zerolog"
)

// MergeCheckerDB is the subset of db.Database needed by MergeChecker.
type MergeCheckerDB interface {
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
	GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error)
}

// MergeChecker polls open PRs and updates ticket lifecycle on merge/close.
type MergeChecker struct {
	db         MergeCheckerDB
	prChecker  git.PRChecker
	hookRunner *skills.HookRunner
	tracker    tracker.IssueTracker
	log        zerolog.Logger
}

// NewMergeChecker creates a new MergeChecker.
func NewMergeChecker(db MergeCheckerDB, prChecker git.PRChecker, hookRunner *skills.HookRunner, tr tracker.IssueTracker, log zerolog.Logger) *MergeChecker {
	return &MergeChecker{
		db:         db,
		prChecker:  prChecker,
		hookRunner: hookRunner,
		tracker:    tr,
		log:        log,
	}
}

// Start begins the merge check poll loop. Blocks until ctx is cancelled.
func (m *MergeChecker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

func (m *MergeChecker) checkAll(ctx context.Context) {
	tickets, err := m.db.ListTickets(ctx, models.TicketFilter{
		Status: string(models.TicketStatusAwaitingMerge),
	})
	if err != nil {
		m.log.Warn().Err(err).Msg("failed to list awaiting_merge tickets")
		return
	}

	for i := range tickets {
		ticket := &tickets[i]
		if ticket.PRNumber == 0 {
			continue
		}

		status, err := m.prChecker.GetPRStatus(ctx, ticket.PRNumber)
		if err != nil {
			m.log.Warn().Err(err).Str("ticket", ticket.ID).Int("pr", ticket.PRNumber).Msg("failed to check PR status")
			continue
		}

		switch status.State {
		case "merged":
			m.handleMerged(ctx, ticket)
		case "closed":
			m.handleClosed(ctx, ticket)
		}
	}
}

func (m *MergeChecker) handleMerged(ctx context.Context, ticket *models.Ticket) {
	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusMerged); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to merged")
		return
	}
	ticket.Status = models.TicketStatusMerged

	// Fire post_merge hooks
	if m.hookRunner != nil {
		sCtx := &skills.SkillContext{Ticket: ticket}
		m.hookRunner.RunHook(ctx, "post_merge", sCtx)
	}

	// Check parent completion if this is a child ticket
	if ticket.ParentTicketID != "" {
		m.checkParentCompletion(ctx, ticket.ParentTicketID)
	}
}

func (m *MergeChecker) handleClosed(ctx context.Context, ticket *models.Ticket) {
	if err := m.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusPRClosed); err != nil {
		m.log.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to update ticket to pr_closed")
	}
}

func (m *MergeChecker) checkParentCompletion(ctx context.Context, parentExternalID string) {
	parent, err := m.db.GetTicketByExternalID(ctx, parentExternalID)
	if err != nil || parent == nil {
		return
	}
	if parent.Status != models.TicketStatusDecomposed {
		return
	}

	children, err := m.db.GetChildTickets(ctx, parentExternalID)
	if err != nil {
		m.log.Warn().Err(err).Str("parent", parentExternalID).Msg("failed to get child tickets")
		return
	}

	for _, child := range children {
		if child.Status != models.TicketStatusMerged {
			return
		}
	}

	// All children merged — close parent
	if err := m.db.UpdateTicketStatus(ctx, parent.ID, models.TicketStatusDone); err != nil {
		m.log.Error().Err(err).Str("parent", parent.ID).Msg("failed to close parent ticket")
		return
	}

	if m.tracker != nil {
		_ = m.tracker.UpdateStatus(ctx, parent.ExternalID, "done")
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/ -run TestMergeChecker -v`
Expected: All 3 PASS

**Step 5: Commit**

```bash
git add internal/daemon/merge_checker.go internal/daemon/merge_checker_test.go
git commit -m "feat(daemon): implement MergeChecker with parent completion"
```

---

### Task 15: Wire MergeChecker into Daemon Start

**Files:**
- Modify: `internal/daemon/daemon.go:15-23` (add fields to DaemonConfig)
- Modify: `internal/daemon/daemon.go:44-53` (add fields to Daemon struct)
- Modify: `internal/daemon/daemon.go:68-119` (wire in Start method)

**Step 1: Add MergeCheckIntervalSecs to daemon's DaemonConfig**

Add after `TaskTimeoutMinutes` (line 22):

```go
MergeCheckIntervalSecs int
```

Update `DefaultDaemonConfig` (line 33):

```go
MergeCheckIntervalSecs: 300,
```

**Step 2: Add dependencies to Daemon struct**

Add fields after `db db.Database` (line 48):

```go
prChecker  git.PRChecker
hookRunner *skills.HookRunner
tracker    tracker.IssueTracker
```

Add setter methods:

```go
func (d *Daemon) SetPRChecker(checker git.PRChecker) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prChecker = checker
}

func (d *Daemon) SetHookRunner(runner *skills.HookRunner) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hookRunner = runner
}

func (d *Daemon) SetTracker(tr tracker.IssueTracker) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tracker = tr
}
```

**Step 3: Start MergeChecker goroutine in Start method**

After the Docker cleanup block (after line 102), before the poll loop, add:

```go
// Start merge checker goroutine
if d.prChecker != nil {
	mc := NewMergeChecker(database, d.prChecker, d.hookRunner, d.tracker, log.Logger)
	interval := time.Duration(d.config.MergeCheckIntervalSecs) * time.Second
	go mc.Start(ctx, interval)
}
```

Add required imports: `"github.com/canhta/foreman/internal/git"`, `"github.com/canhta/foreman/internal/skills"`, `"github.com/canhta/foreman/internal/tracker"`.

**Step 4: Verify compilation**

Run: `go build ./internal/daemon/...`
Expected: success

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat(daemon): wire MergeChecker goroutine into daemon start"
```

---

### Task 16: Run Full Test Suite and Fix Regressions

**Step 1: Run all tests**

Run: `go test ./... -v -count=1`

**Step 2: Fix any compilation errors or test failures**

Common issues to watch for:
- Existing mock implementations in tests that don't implement the new `CreateTicket` or `GetChildTickets` methods
- SELECT column mismatches in existing DB tests after schema changes
- Import cycles from new dependencies

**Step 3: Verify clean build**

Run: `go build ./...`
Expected: success

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve test regressions from decompose and merge lifecycle changes"
```

---

### Task 17: Verify Integration (Manual Smoke Test)

**Step 1: Verify NeedsDecomposition works end-to-end**

Run: `go test ./internal/pipeline/ -run TestNeedsDecomposition -v`

**Step 2: Verify MergeChecker lifecycle**

Run: `go test ./internal/daemon/ -run TestMergeChecker -v`

**Step 3: Verify PRChecker works**

Run: `go test ./internal/git/ -run TestGitHubPRChecker -v`

**Step 4: Verify all tracker implementations compile**

Run: `go build ./internal/tracker/...`

**Step 5: Run full suite one final time**

Run: `go test ./... -count=1`
Expected: All PASS

**Step 6: Commit summary**

```bash
git add -A
git commit -m "feat: ticket auto-decomposition and PR merge lifecycle

- Add NeedsDecomposition scope detection (deterministic heuristics)
- Add Decomposer: LLM generates child ticket specs, creates in tracker
- Add CreateTicket to IssueTracker interface (GitHub, Jira, Linear, LocalFile)
- Add PRChecker interface and GitHub implementation
- Add MergeChecker daemon goroutine: polls PRs, fires post_merge hooks
- Add parent auto-completion when all child PRs merge
- Add 5 new ticket statuses: decomposing, decomposed, awaiting_merge, merged, pr_closed
- Add post_merge as valid skill trigger
- Add DecomposeConfig with sensible defaults"
```
