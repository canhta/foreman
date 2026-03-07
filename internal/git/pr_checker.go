package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PR state constants.
const (
	PRStateOpen   = "open"
	PRStateMerged = "merged"
	PRStateClosed = "closed"
)

// PRMergeStatus holds the merge state of a pull request.
type PRMergeStatus struct {
	MergedAt *time.Time
	ClosedAt *time.Time
	State    string
	HeadSHA  string
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
	Head     ghPRHead   `json:"head"`
	MergedAt *time.Time `json:"merged_at"`
	ClosedAt *time.Time `json:"closed_at"`
	State    string     `json:"state"`
	Merged   bool       `json:"merged"`
}

type ghPRHead struct {
	SHA string `json:"sha"`
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
		HeadSHA:  pr.Head.SHA,
	}
	if pr.Merged {
		status.State = PRStateMerged
	} else if pr.State == "closed" {
		status.State = PRStateClosed
	} else {
		status.State = PRStateOpen
	}

	return status, nil
}

var _ PRChecker = (*GitHubPRChecker)(nil)
