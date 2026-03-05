package tracker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

var _ IssueTracker = (*JiraTracker)(nil)

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

func (j *JiraTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	jql := fmt.Sprintf("project=%s AND labels=%s AND status!=Done", j.project, j.label)
	url := fmt.Sprintf("%s/rest/api/2/search?jql=%s", j.baseURL, url.QueryEscape(jql))

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

	tickets := make([]Ticket, 0, len(result.Issues))
	for _, issue := range result.Issues {
		t := Ticket{
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

func (j *JiraTracker) GetTicket(ctx context.Context, externalID string) (*Ticket, error) {
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

	t := &Ticket{
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
	data, err := json.Marshal(map[string]string{"body": comment})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
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

func (j *JiraTracker) AddLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s", j.baseURL, externalID)
	data, err := json.Marshal(map[string]interface{}{
		"update": map[string]interface{}{
			"labels": []map[string]string{{"add": label}},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(data))
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
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira label update: status %d", resp.StatusCode)
	}
	return nil
}

func (j *JiraTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s", j.baseURL, externalID)
	data, err := json.Marshal(map[string]interface{}{
		"update": map[string]interface{}{
			"labels": []map[string]string{{"remove": label}},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(data))
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
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira label update: status %d", resp.StatusCode)
	}
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
