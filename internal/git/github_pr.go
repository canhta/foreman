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
		return nil, fmt.Errorf("GitHub API returned %d for %s/%s (head=%s base=%s): %s",
			resp.StatusCode, g.owner, g.repo, ghReq.Head, ghReq.Base, string(respBody))
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

// Ensure GitHubPRCreator implements PRCreator.
var _ PRCreator = (*GitHubPRCreator)(nil)
