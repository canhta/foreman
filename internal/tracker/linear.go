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

var _ IssueTracker = (*LinearTracker)(nil)

// LinearTracker implements IssueTracker for Linear.app via GraphQL.
type LinearTracker struct {
	apiKey  string
	label   string
	baseURL string
	client  *http.Client
}

// NewLinearTracker creates a Linear tracker.
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

func (l *LinearTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
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

	tickets := make([]Ticket, 0, len(result.Data.Issues.Nodes))
	for _, issue := range result.Data.Issues.Nodes {
		t := Ticket{
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

func (l *LinearTracker) GetTicket(ctx context.Context, externalID string) (*Ticket, error) {
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
		return nil, fmt.Errorf("linear unmarshal: %w", err)
	}

	issue := result.Data.Issue
	t := &Ticket{
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
	return t, nil
}

// UpdateStatus updates a Linear issue's state via GraphQL mutations.
// Full state transition mapping is deferred; silently ignored for now.
func (l *LinearTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	return nil
}

func (l *LinearTracker) AddComment(ctx context.Context, externalID, comment string) error {
	mutation := fmt.Sprintf(`mutation { commentCreate(input: { issueId: "%s", body: %q }) { success } }`,
		externalID, comment)
	_, err := l.graphql(ctx, mutation)
	return err
}

func (l *LinearTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return l.AddComment(ctx, externalID, fmt.Sprintf("PR created: %s", prURL))
}

// AddLabel adds a label to a Linear issue.
// Full label mutation support is deferred.
func (l *LinearTracker) AddLabel(ctx context.Context, externalID, label string) error {
	return nil
}

// RemoveLabel removes a label from a Linear issue.
// Full label mutation support is deferred.
func (l *LinearTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	return nil
}

func (l *LinearTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	ticket, err := l.GetTicket(ctx, externalID)
	if err != nil {
		return false, err
	}
	return containsLabel(ticket.Labels, label), nil
}

func (l *LinearTracker) graphql(ctx context.Context, query string) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, fmt.Errorf("linear marshal: %w", err)
	}

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
		return nil, fmt.Errorf("linear read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("linear API error (status %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}
