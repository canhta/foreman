package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GitHubIssuesTracker implements IssueTracker for GitHub Issues.
type GitHubIssuesTracker struct {
	client      *http.Client
	baseURL     string
	token       string
	owner       string
	repo        string
	pickupLabel string
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
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	User   ghUser    `json:"user"`
	Labels []ghLabel `json:"labels"`
	Number int       `json:"number"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

func (g *GitHubIssuesTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	// NOTE: fetches up to 100 issues per call; pagination not implemented.
	u, err := url.Parse(fmt.Sprintf("%s/repos/%s/%s/issues",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo)))
	if err != nil {
		return nil, fmt.Errorf("building URL: %w", err)
	}
	q := u.Query()
	q.Set("labels", g.pickupLabel)
	q.Set("state", "open")
	q.Set("per_page", "100")
	u.RawQuery = q.Encode()

	body, err := g.doGet(ctx, u.String())
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
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%s",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo), url.PathEscape(externalID))
	body, err := g.doGet(ctx, u)
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

// UpdateStatus closes the GitHub issue when status is "done".
// Other statuses are silently ignored — GitHub Issues lack custom workflow states.
func (g *GitHubIssuesTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	if status == "done" {
		u := fmt.Sprintf("%s/repos/%s/%s/issues/%s",
			g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo), url.PathEscape(externalID))
		return g.doRequestWithBody(ctx, "PATCH", u, map[string]string{"state": "closed"})
	}
	return nil
}

func (g *GitHubIssuesTracker) AddComment(ctx context.Context, externalID, comment string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%s/comments",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo), url.PathEscape(externalID))
	return g.doRequestWithBody(ctx, "POST", u, map[string]string{"body": comment})
}

func (g *GitHubIssuesTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return g.AddComment(ctx, externalID, fmt.Sprintf("🤖 PR created: %s", prURL))
}

func (g *GitHubIssuesTracker) AddLabel(ctx context.Context, externalID, label string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%s/labels",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo), url.PathEscape(externalID))
	return g.doRequestWithBody(ctx, "POST", u, map[string][]string{"labels": {label}})
}

func (g *GitHubIssuesTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%s/labels/%s",
		g.baseURL, url.PathEscape(g.owner), url.PathEscape(g.repo),
		url.PathEscape(externalID), url.PathEscape(label))
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return err
	}
	g.setHeaders(req)
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

func (g *GitHubIssuesTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	ticket, err := g.GetTicket(ctx, externalID)
	if err != nil {
		return false, err
	}
	return containsLabel(ticket.Labels, label), nil
}

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

func (g *GitHubIssuesTracker) doRequestWithBody(ctx context.Context, method, url string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
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
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
}

var _ IssueTracker = (*GitHubIssuesTracker)(nil)
