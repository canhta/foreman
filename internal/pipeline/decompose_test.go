package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsDecomposition(t *testing.T) {
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   10,
		MaxScopeKeywords: 2,
	}

	tests := []struct {
		ticket *models.Ticket
		name   string
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

// --- LLM-Assisted decomposition check tests ---

// llmAssistMock is a controllable LLM stub for decomp-check tests.
type llmAssistMock struct {
	response string
	err      error
	called   bool
}

func (m *llmAssistMock) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return &models.LlmResponse{Content: m.response}, nil
}
func (m *llmAssistMock) ProviderName() string                { return "mock" }
func (m *llmAssistMock) HealthCheck(_ context.Context) error { return nil }

// recordingDB captures RecordEvent calls.
type recordingDB struct {
	events []*models.EventRecord
}

func (r *recordingDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	r.events = append(r.events, e)
	return nil
}

func makeDecomposerWithLLMAssist(llmMock LLMProvider, db DecompEventRecorder, cfg *models.DecomposeConfig) *Decomposer {
	return &Decomposer{
		llm:     llmMock,
		tracker: newMockTracker(),
		cfg:     cfg,
		db:      db,
	}
}

// TestNeedsDecomposition_HeuristicTakesPrecedence: heuristic says yes → LLM never called.
func TestNeedsDecomposition_HeuristicTakesPrecedence(t *testing.T) {
	llmMock := &llmAssistMock{response: "NO: ticket is small"}
	db := &recordingDB{}
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   5, // low threshold so heuristic triggers
		MaxScopeKeywords: 10,
		LLMAssist:        true,
	}
	d := makeDecomposerWithLLMAssist(llmMock, db, cfg)

	// Description has >5 words → heuristic triggers
	ticket := &models.Ticket{
		ID:          "T1",
		Title:       "Big ticket",
		Description: "implement the full user authentication system",
	}

	result, err := d.NeedsDecomposition(context.Background(), ticket)
	require.NoError(t, err)
	assert.True(t, result, "heuristic should force decomposition")
	assert.False(t, llmMock.called, "LLM must NOT be called when heuristic triggers")

	require.Len(t, db.events, 1)
	assert.Equal(t, "decomposition_check", db.events[0].EventType)
	assert.Contains(t, db.events[0].Details, `"llm_result":"skipped"`)
}

// TestNeedsDecomposition_LLMOverridesNegativeHeuristic: heuristic=false, LLM=YES → returns true.
func TestNeedsDecomposition_LLMOverridesNegativeHeuristic(t *testing.T) {
	llmMock := &llmAssistMock{response: "YES: ticket spans multiple subsystems"}
	db := &recordingDB{}
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   500, // high threshold so heuristic does NOT trigger
		MaxScopeKeywords: 50,
		LLMAssist:        true,
	}
	d := makeDecomposerWithLLMAssist(llmMock, db, cfg)

	ticket := &models.Ticket{
		ID:          "T2",
		Title:       "Small ticket",
		Description: "fix the login button",
	}

	result, err := d.NeedsDecomposition(context.Background(), ticket)
	require.NoError(t, err)
	assert.True(t, result, "LLM YES should override negative heuristic")
	assert.True(t, llmMock.called, "LLM must be called when heuristic says no and llm_assist=true")

	require.Len(t, db.events, 1)
	assert.Contains(t, db.events[0].Details, `"llm_result":"yes"`)
}

// TestNeedsDecomposition_LLMFailure_FallsBackToHeuristic: LLM error → returns heuristic result (false).
func TestNeedsDecomposition_LLMFailure_FallsBackToHeuristic(t *testing.T) {
	llmMock := &llmAssistMock{err: errors.New("connection refused")}
	db := &recordingDB{}
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   500,
		MaxScopeKeywords: 50,
		LLMAssist:        true,
	}
	d := makeDecomposerWithLLMAssist(llmMock, db, cfg)

	ticket := &models.Ticket{
		ID:          "T3",
		Title:       "Small ticket",
		Description: "fix the login button",
	}

	result, err := d.NeedsDecomposition(context.Background(), ticket)
	require.NoError(t, err, "LLM error should be swallowed, not returned")
	assert.False(t, result, "should fall back to heuristic result (false)")

	require.Len(t, db.events, 1)
	assert.Contains(t, db.events[0].Details, `"llm_result":"error"`)
}

// TestNeedsDecomposition_LLMAssistDisabled_SkipsLLM: llm_assist=false → LLM never called.
func TestNeedsDecomposition_LLMAssistDisabled_SkipsLLM(t *testing.T) {
	llmMock := &llmAssistMock{response: "YES: ticket is huge"}
	db := &recordingDB{}
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   500,
		MaxScopeKeywords: 50,
		LLMAssist:        false, // disabled
	}
	d := makeDecomposerWithLLMAssist(llmMock, db, cfg)

	ticket := &models.Ticket{
		ID:          "T4",
		Title:       "Small ticket",
		Description: "fix the login button",
	}

	result, err := d.NeedsDecomposition(context.Background(), ticket)
	require.NoError(t, err)
	assert.False(t, result, "should return heuristic result, LLM disabled")
	assert.False(t, llmMock.called, "LLM must NOT be called when llm_assist=false")

	require.Len(t, db.events, 1)
	assert.Contains(t, db.events[0].Details, `"llm_result":"skipped"`)
}
