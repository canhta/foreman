// internal/git/github_pr.go
package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// PRCreator abstracts PR creation across git hosts.
type PRCreator interface {
	CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error)
}

// GitHubPRCreator creates PRs via the GitHub REST API.
type GitHubPRCreator struct {
	client  *http.Client
	baseURL string
	token   string
	owner   string
	repo    string
}

// NewGitHubPRCreator creates a GitHub PR client.
func NewGitHubPRCreator(baseURL, token, owner, repo string) *GitHubPRCreator {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &GitHubPRCreator{
		baseURL: baseURL,
		token:   token,
		owner:   owner,
		repo:    repo,
		client:  &http.Client{},
	}
}

type githubPRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type githubPRResponse struct {
	URL     string `json:"url"`
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

// CreatePR creates a pull request on GitHub.
func (g *GitHubPRCreator) CreatePR(ctx context.Context, req PrRequest) (*PrResponse, error) {
	ghReq := githubPRRequest{
		Title: req.Title,
		Body:  req.Body,
		Head:  req.HeadBranch,
		Base:  req.BaseBranch,
		Draft: req.Draft,
	}

	body, err := json.Marshal(ghReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling PR request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls", g.baseURL, g.owner, g.repo)

	log.Debug().
		Str("url", url).
		Str("owner", g.owner).
		Str("repo", g.repo).
		Str("head", ghReq.Head).
		Str("base", ghReq.Base).
		Str("title", ghReq.Title).
		Bool("draft", ghReq.Draft).
		Bool("token_set", g.token != "").
		Msg("creating GitHub PR")

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+g.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing PR request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		baseErr := fmt.Errorf("GitHub API returned %d for %s/%s (head=%s base=%s): %s",
			resp.StatusCode, g.owner, g.repo, ghReq.Head, ghReq.Base, string(respBody))

		if resp.StatusCode == http.StatusNotFound {
			// GitHub returns 404 for private repos when the token lacks access,
			// when the head branch doesn't exist on the remote, or when the
			// repo itself doesn't exist. Check repo accessibility to narrow it down.
			if !g.canAccessRepo(ctx) {
				return nil, fmt.Errorf("%w (token lacks access to repo — check that git.github.token has 'repo' scope or fine-grained 'pull_requests:write' + 'contents:read' permissions)", baseErr)
			}
			if !g.branchExists(ctx, ghReq.Head) {
				return nil, fmt.Errorf("%w (head branch %q not found on remote — ensure the branch is pushed before creating a PR)", baseErr, ghReq.Head)
			}
			if !g.branchExists(ctx, ghReq.Base) {
				return nil, fmt.Errorf("%w (base branch %q not found on remote)", baseErr, ghReq.Base)
			}
		}

		if resp.StatusCode == http.StatusUnprocessableEntity {
			return nil, fmt.Errorf("%w (common causes: PR already exists, or head branch has no commits ahead of base)", baseErr)
		}

		return nil, baseErr
	}

	var ghResp githubPRResponse
	if err := json.Unmarshal(respBody, &ghResp); err != nil {
		return nil, fmt.Errorf("decoding PR response: %w", err)
	}

	return &PrResponse{
		Number:  ghResp.Number,
		URL:     ghResp.URL,
		HTMLURL: ghResp.HTMLURL,
	}, nil
}

// canAccessRepo checks whether the configured token can access the repo.
func (g *GitHubPRCreator) canAccessRepo(ctx context.Context) bool {
	url := fmt.Sprintf("%s/repos/%s/%s", g.baseURL, g.owner, g.repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// branchExists checks whether a branch exists on the remote.
func (g *GitHubPRCreator) branchExists(ctx context.Context, branch string) bool {
	url := fmt.Sprintf("%s/repos/%s/%s/branches/%s", g.baseURL, g.owner, g.repo, branch)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Ensure GitHubPRCreator implements PRCreator.
var _ PRCreator = (*GitHubPRCreator)(nil)
