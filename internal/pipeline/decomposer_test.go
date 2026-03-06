package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decomposeMockLLM returns a canned decomposition response.
type decomposeMockLLM struct {
	response string
}

func (m *decomposeMockLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response}, nil
}
func (m *decomposeMockLLM) ProviderName() string                { return "mock" }
func (m *decomposeMockLLM) HealthCheck(_ context.Context) error { return nil }

// mockTracker records CreateTicket calls.
type mockTracker struct {
	labels   map[string][]string
	comments map[string][]string
	created  []tracker.CreateTicketRequest
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
func (m *mockTracker) FetchReadyTickets(_ context.Context) ([]tracker.Ticket, error) {
	return nil, nil
}
func (m *mockTracker) GetTicket(_ context.Context, _ string) (*tracker.Ticket, error) {
	return nil, nil
}
func (m *mockTracker) UpdateStatus(_ context.Context, _, _ string) error     { return nil }
func (m *mockTracker) AttachPR(_ context.Context, _, _ string) error         { return nil }
func (m *mockTracker) RemoveLabel(_ context.Context, _, _ string) error      { return nil }
func (m *mockTracker) HasLabel(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (m *mockTracker) ProviderName() string                                  { return "mock" }

func TestDecomposer_Execute(t *testing.T) {
	result := DecompositionResult{
		Children: []ChildTicketSpec{
			{Title: "Setup auth models", Description: "Create user model", AcceptanceCriteria: []string{"User table exists"}, EstimatedComplexity: "low"},
			{Title: "Implement login", Description: "Add login endpoint", AcceptanceCriteria: []string{"POST /login works"}, EstimatedComplexity: "medium", DependsOn: []string{"Setup auth models"}},
		},
		Rationale: "Ticket covers two distinct features",
	}
	respJSON, _ := json.Marshal(result)

	llm := &decomposeMockLLM{response: string(respJSON)}
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
