package tracker

import (
	"bytes"
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
	client     *http.Client
	baseURL    string
	authHeader string
	project    string
	label      string
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

// ── API v3 types ────────────────────────────────────────────────────────────

type jiraSearchResponse struct {
	Issues []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	Key    string     `json:"key"`
	Fields jiraFields `json:"fields"`
}

// jiraFields uses json.RawMessage for description because v3 returns
// Atlassian Document Format (ADF) objects, not plain strings.
type jiraFields struct {
	Priority    *jiraPriority     `json:"priority"`
	Assignee    *jiraUser         `json:"assignee"`
	Reporter    *jiraUser         `json:"reporter"`
	Comment     *jiraCommentBlock `json:"comment"`
	Summary     string            `json:"summary"`
	Description json.RawMessage   `json:"description"`
	Labels      []string          `json:"labels"`
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
	Author jiraUser        `json:"author"`
	Body   json.RawMessage `json:"body"`
}

// adfToText extracts plain text from an Atlassian Document Format (ADF) value.
// ADF is the rich-text format used by Jira REST API v3 for description fields.
// Returns an empty string if the value is null or unparseable.
func adfToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// Guard: plain JSON string (API v2 fallback or test stubs).
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var node map[string]interface{}
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	return extractADFText(node)
}

func extractADFText(node map[string]interface{}) string {
	if t, _ := node["type"].(string); t == "text" {
		text, _ := node["text"].(string)
		return text
	}
	content, _ := node["content"].([]interface{})
	parts := make([]string, 0, len(content))
	for _, child := range content {
		if childMap, ok := child.(map[string]interface{}); ok {
			if t := extractADFText(childMap); t != "" {
				parts = append(parts, t)
			}
		}
	}
	sep := " "
	if t, _ := node["type"].(string); t == "paragraph" || t == "heading" || t == "listItem" {
		sep = "\n"
	}
	return strings.Join(parts, sep)
}

// adfDoc wraps plain text into a minimal ADF document for write operations.
func adfDoc(text string) map[string]interface{} {
	return map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []map[string]interface{}{
			{
				"type": "paragraph",
				"content": []map[string]interface{}{
					{"type": "text", "text": text},
				},
			},
		},
	}
}

// ── IssueTracker implementation ─────────────────────────────────────────────

func (j *JiraTracker) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	fields := map[string]interface{}{
		"project":     map[string]string{"key": j.project},
		"summary":     req.Title,
		"description": adfDoc(req.Description),
		"labels":      req.Labels,
		"issuetype":   map[string]string{"name": "Task"},
	}
	if req.ParentID != "" {
		fields["parent"] = map[string]string{"key": req.ParentID}
	}

	payload := map[string]interface{}{"fields": fields}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jira marshal: %w", err)
	}

	u := fmt.Sprintf("%s/rest/api/3/issue", j.baseURL)
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

func (j *JiraTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	jql := fmt.Sprintf("project=%s AND labels=%s AND status!=Done", j.project, j.label)

	payload, err := json.Marshal(map[string]interface{}{
		"jql":        jql,
		"maxResults": 50,
		"fields":     []string{"summary", "description", "labels", "priority", "assignee", "reporter", "comment"},
	})
	if err != nil {
		return nil, fmt.Errorf("jira marshal: %w", err)
	}

	u := fmt.Sprintf("%s/rest/api/3/search/jql", j.baseURL)
	resp, err := j.doPost(ctx, u, payload)
	if err != nil {
		return nil, fmt.Errorf("jira search: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jira search: status %d: %s", resp.StatusCode, string(body))
	}

	var result jiraSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("jira unmarshal: %w", err)
	}

	tickets := make([]Ticket, 0, len(result.Issues))
	for _, issue := range result.Issues {
		t := Ticket{
			ExternalID:  issue.Key,
			Title:       issue.Fields.Summary,
			Description: adfToText(issue.Fields.Description),
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
	u := fmt.Sprintf("%s/rest/api/3/issue/%s", j.baseURL, externalID)
	resp, err := j.doGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jira get issue: status %d: %s", resp.StatusCode, string(body))
	}

	var issue jiraIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("jira unmarshal: %w", err)
	}

	return &Ticket{
		ExternalID:  issue.Key,
		Title:       issue.Fields.Summary,
		Description: adfToText(issue.Fields.Description),
		Labels:      issue.Fields.Labels,
	}, nil
}

func (j *JiraTracker) UpdateStatus(_ context.Context, _ string, _ string) error {
	// Jira status transitions require knowing transition IDs — simplified for now.
	return nil
}

func (j *JiraTracker) AddComment(ctx context.Context, externalID, comment string) error {
	u := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", j.baseURL, externalID)
	data, err := json.Marshal(map[string]interface{}{"body": adfDoc(comment)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(data))
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
	u := fmt.Sprintf("%s/rest/api/3/issue/%s", j.baseURL, externalID)
	data, err := json.Marshal(map[string]interface{}{
		"update": map[string]interface{}{
			"labels": []map[string]string{{"add": label}},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", u, bytes.NewReader(data))
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
	u := fmt.Sprintf("%s/rest/api/3/issue/%s", j.baseURL, externalID)
	data, err := json.Marshal(map[string]interface{}{
		"update": map[string]interface{}{
			"labels": []map[string]string{{"remove": label}},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", u, bytes.NewReader(data))
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

func (j *JiraTracker) doPost(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", j.authHeader)
	req.Header.Set("Content-Type", "application/json")
	return j.client.Do(req)
}
