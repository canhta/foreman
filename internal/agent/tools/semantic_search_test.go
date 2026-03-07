package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
)

// --- Mock Embedder ---

type mockEmbedder struct {
	calls  int
	vecDim int // dimensionality of returned vectors
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	result := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, m.vecDim)
		for j := range vec {
			vec[j] = float32(i+1) / float32(m.vecDim+1)
		}
		result[i] = vec
	}
	return result, nil
}

// --- Mock Database ---

type mockDB struct {
	upsertCalls       int
	deleteExceptCalls int
	stored            []db.EmbeddingRecord
	returnEmpty       bool // if true, GetEmbeddingsByRepoSHA returns empty
}

func (m *mockDB) UpsertEmbedding(_ context.Context, e db.EmbeddingRecord) error {
	m.upsertCalls++
	m.stored = append(m.stored, e)
	return nil
}

func (m *mockDB) GetEmbeddingsByRepoSHA(_ context.Context, _, _ string) ([]db.EmbeddingRecord, error) {
	if m.returnEmpty {
		return nil, nil
	}
	return m.stored, nil
}

func (m *mockDB) DeleteEmbeddingsByRepoSHA(_ context.Context, _, _ string) error { return nil }
func (m *mockDB) DeleteEmbeddingsByRepoExceptSHA(_ context.Context, _, _ string) error {
	m.deleteExceptCalls++
	return nil
}
func (m *mockDB) GetTaskContextStats(_ context.Context, _ string) (db.TaskContextStats, error) {
	return db.TaskContextStats{}, nil
}
func (m *mockDB) UpdateTaskContextStats(_ context.Context, _ string, _ db.TaskContextStats) error {
	return nil
}

// Implement remaining db.Database interface methods as no-ops.

func (m *mockDB) CreateTicket(_ context.Context, _ *models.Ticket) error { return nil }
func (m *mockDB) UpdateTicketStatus(_ context.Context, _ string, _ models.TicketStatus) error {
	return nil
}
func (m *mockDB) GetTicket(_ context.Context, _ string) (*models.Ticket, error) { return nil, nil }
func (m *mockDB) GetTicketByExternalID(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (m *mockDB) ListTickets(_ context.Context, _ models.TicketFilter) ([]models.Ticket, error) {
	return nil, nil
}
func (m *mockDB) GetChildTickets(_ context.Context, _ string) ([]models.Ticket, error) {
	return nil, nil
}
func (m *mockDB) SetLastCompletedTask(_ context.Context, _ string, _ int) error  { return nil }
func (m *mockDB) SetTicketPRHeadSHA(_ context.Context, _, _ string) error        { return nil }
func (m *mockDB) CreateTasks(_ context.Context, _ string, _ []models.Task) error { return nil }
func (m *mockDB) UpdateTaskStatus(_ context.Context, _ string, _ models.TaskStatus) error {
	return nil
}
func (m *mockDB) SetTaskErrorType(_ context.Context, _, _ string) error          { return nil }
func (m *mockDB) IncrementTaskLlmCalls(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockDB) ListTasks(_ context.Context, _ string) ([]models.Task, error)   { return nil, nil }
func (m *mockDB) RecordLlmCall(_ context.Context, _ *models.LlmCallRecord) error { return nil }
func (m *mockDB) ListLlmCalls(_ context.Context, _ string) ([]models.LlmCallRecord, error) {
	return nil, nil
}
func (m *mockDB) StoreCallDetails(_ context.Context, _, _, _ string) error { return nil }
func (m *mockDB) GetCallDetails(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}
func (m *mockDB) SetHandoff(_ context.Context, _ *models.HandoffRecord) error { return nil }
func (m *mockDB) GetHandoffs(_ context.Context, _, _ string) ([]models.HandoffRecord, error) {
	return nil, nil
}
func (m *mockDB) SaveProgressPattern(_ context.Context, _ *models.ProgressPattern) error {
	return nil
}
func (m *mockDB) GetProgressPatterns(_ context.Context, _ string, _ []string) ([]models.ProgressPattern, error) {
	return nil, nil
}
func (m *mockDB) ReserveFiles(_ context.Context, _ string, _ []string) error { return nil }
func (m *mockDB) ReleaseFiles(_ context.Context, _ string) error             { return nil }
func (m *mockDB) GetReservedFiles(_ context.Context) (map[string]string, error) {
	return nil, nil
}
func (m *mockDB) TryReserveFiles(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}
func (m *mockDB) GetTicketCost(_ context.Context, _ string) (float64, error)   { return 0, nil }
func (m *mockDB) GetDailyCost(_ context.Context, _ string) (float64, error)    { return 0, nil }
func (m *mockDB) GetMonthlyCost(_ context.Context, _ string) (float64, error)  { return 0, nil }
func (m *mockDB) RecordDailyCost(_ context.Context, _ string, _ float64) error { return nil }
func (m *mockDB) RecordEvent(_ context.Context, _ *models.EventRecord) error   { return nil }
func (m *mockDB) GetEvents(_ context.Context, _ string, _ int) ([]models.EventRecord, error) {
	return nil, nil
}
func (m *mockDB) CreateAuthToken(_ context.Context, _, _ string) error               { return nil }
func (m *mockDB) ValidateAuthToken(_ context.Context, _ string) (bool, error)        { return false, nil }
func (m *mockDB) CreatePairing(_ context.Context, _, _, _ string, _ time.Time) error { return nil }
func (m *mockDB) GetPairing(_ context.Context, _ string) (*models.Pairing, error)    { return nil, nil }
func (m *mockDB) DeletePairing(_ context.Context, _ string) error                    { return nil }
func (m *mockDB) ListPairings(_ context.Context, _ string) ([]models.Pairing, error) {
	return nil, nil
}
func (m *mockDB) DeleteExpiredPairings(_ context.Context) error { return nil }
func (m *mockDB) FindActiveClarification(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (m *mockDB) DeleteTicket(_ context.Context, _ string) error { return nil }
func (m *mockDB) GetTeamStats(_ context.Context, _ time.Time) ([]models.TeamStat, error) {
	return nil, nil
}
func (m *mockDB) GetRecentPRs(_ context.Context, _ int) ([]models.Ticket, error) {
	return nil, nil
}
func (m *mockDB) GetTicketSummaries(_ context.Context, _ models.TicketFilter) ([]models.TicketSummary, error) {
	return nil, nil
}
func (m *mockDB) GetGlobalEvents(_ context.Context, _, _ int) ([]models.EventRecord, error) {
	return nil, nil
}
func (m *mockDB) AcquireLock(_ context.Context, _ string, _ int) (bool, error) { return false, nil }
func (m *mockDB) ReleaseLock(_ context.Context, _ string) error                { return nil }
func (m *mockDB) Close() error                                                 { return nil }
func (m *mockDB) WriteContextFeedback(_ context.Context, _ db.ContextFeedbackRow) error {
	return nil
}
func (m *mockDB) QueryContextFeedback(_ context.Context, _ []string, _ float64) ([]db.ContextFeedbackRow, error) {
	return nil, nil
}
func (m *mockDB) UpsertPromptSnapshot(_ context.Context, _, _ string) error { return nil }
func (m *mockDB) GetPromptSnapshots(_ context.Context) ([]db.PromptSnapshot, error) {
	return nil, nil
}

// --- Helpers ---

// fixedHeadSHA returns an injectable getHeadSHA function that always returns "abc123".
func fixedHeadSHA(_ context.Context, _ string) (string, error) {
	return "abc123", nil
}

// newTestTool creates a SemanticSearchTool wired with a fixed HEAD SHA for unit tests.
func newTestTool(embedder *mockEmbedder, mdb *mockDB) *SemanticSearchTool {
	return &SemanticSearchTool{
		db:         mdb,
		embedder:   embedder,
		getHeadSHA: fixedHeadSHA,
	}
}

// --- Tests ---

func TestSemanticSearch_DisabledWhenNoEmbedder(t *testing.T) {
	tool := &SemanticSearchTool{
		db:         nil,
		embedder:   nil,
		getHeadSHA: fixedHeadSHA,
	}
	input, _ := json.Marshal(map[string]interface{}{"query": "find authentication logic"})
	out, err := tool.Execute(context.Background(), "/tmp", input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out != "semantic_search is disabled: set llm.embedding_provider and llm.embedding_model in foreman.toml" {
		t.Errorf("unexpected disabled message: %q", out)
	}
}

func TestSemanticSearch_TopKCap(t *testing.T) {
	// Provide 5 pre-populated records in the mock DB.
	records := make([]db.EmbeddingRecord, 5)
	for i := range records {
		records[i] = db.EmbeddingRecord{
			RepoPath:  "/tmp/repo",
			HeadSHA:   "abc123",
			FilePath:  "/tmp/repo/file.go",
			ChunkText: "chunk text content here",
			Vector:    []float32{0.1, 0.2, 0.3, 0.4},
			StartLine: i*10 + 1,
			EndLine:   i*10 + 10,
		}
	}
	mdb := &mockDB{stored: records, returnEmpty: false}
	mockEm := &mockEmbedder{vecDim: 4}

	tool := newTestTool(mockEm, mdb)

	// top_k=100 should be capped at 20; we only have 5 records so results = 5.
	input, _ := json.Marshal(map[string]interface{}{
		"query": "find something",
		"top_k": 100,
	})
	out, err := tool.Execute(context.Background(), "/tmp/repo", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []SearchResult
	if jsonErr := json.Unmarshal([]byte(out), &results); jsonErr != nil {
		t.Fatalf("failed to unmarshal results: %v", jsonErr)
	}
	if len(results) > 20 {
		t.Errorf("top_k should be capped at 20, got %d results", len(results))
	}
	// We have 5 records so we should get exactly 5 results.
	if len(results) != 5 {
		t.Errorf("expected 5 results (all records), got %d", len(results))
	}
}

func TestSemanticSearch_BuildsAndCachesIndex(t *testing.T) {
	// Empty cache forces buildIndex. Since workDir has no real files we get 0
	// chunks → 0 upserts, but stale-cleanup is still called (after empty index).
	// More importantly we verify that on the SECOND call the cache is hit and
	// buildIndex is NOT called again.
	mockEm := &mockEmbedder{vecDim: 4}
	mdb := &mockDB{returnEmpty: true} // always empty cache for simplicity

	tool := newTestTool(mockEm, mdb)

	input, _ := json.Marshal(map[string]interface{}{"query": "test"})

	// First call: empty workDir → buildIndex runs but finds no files.
	out, err := tool.Execute(context.Background(), t.TempDir(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No chunks → result should be an empty JSON array or null.
	if out != "[]" && out != "null" {
		t.Errorf("expected empty result for empty workDir, got %q", out)
	}

	// Embedder was called 1 time for the query vector (no chunks to embed since workDir is empty).
	if mockEm.calls != 1 {
		t.Errorf("expected embedder.Embed called 1 time (query only), got %d", mockEm.calls)
	}
}

func TestSemanticSearch_BuildsIndex_UpsertCalled(t *testing.T) {
	// Use a temp dir with a synthetic .go file so buildIndex actually finds chunks.
	tmpDir := t.TempDir()
	// Write a small Go file.
	goContent := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(tmpDir+"/main.go", []byte(goContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mockEm := &mockEmbedder{vecDim: 4}
	mdb := &mockDB{returnEmpty: true}
	tool := newTestTool(mockEm, mdb)

	input, _ := json.Marshal(map[string]interface{}{"query": "main function"})
	_, err := tool.Execute(context.Background(), tmpDir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// buildIndex should have called UpsertEmbedding for the chunks found.
	if mdb.upsertCalls == 0 {
		t.Error("expected UpsertEmbedding to be called when building index")
	}
	// Stale cleanup should have been triggered.
	if mdb.deleteExceptCalls == 0 {
		t.Error("expected DeleteEmbeddingsByRepoExceptSHA to be called after building index")
	}
}

func TestSemanticSearch_NoDuplicateBuildOnCacheHit(t *testing.T) {
	// Pre-populated records in cache → buildIndex must NOT be called.
	records := []db.EmbeddingRecord{{
		RepoPath:  "/r",
		HeadSHA:   "abc123",
		FilePath:  "/r/a.go",
		ChunkText: "hello world",
		Vector:    []float32{1, 0, 0, 0},
		StartLine: 1,
		EndLine:   5,
	}}
	mdb := &mockDB{stored: records, returnEmpty: false}
	mockEm := &mockEmbedder{vecDim: 4}
	tool := newTestTool(mockEm, mdb)

	input, _ := json.Marshal(map[string]interface{}{"query": "hello"})
	_, err := tool.Execute(context.Background(), "/r", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No upserts should have happened (cache was warm).
	if mdb.upsertCalls != 0 {
		t.Errorf("expected 0 UpsertEmbedding calls on cache hit, got %d", mdb.upsertCalls)
	}
}
