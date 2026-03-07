package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/llm"
)

// SearchResult is a single semantic search hit.
type SearchResult struct {
	Snippet   string  `json:"snippet"` // first 200 chars of chunk text
	File      string  `json:"file"`
	Score     float32 `json:"score"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
}

// SemanticSearchTool implements semantic/conceptual code search using embeddings.
// It is disabled (returns an informative message) if no Embedder is configured.
type SemanticSearchTool struct {
	db       db.Database
	embedder llm.Embedder // nil means disabled
}

func (t *SemanticSearchTool) Name() string { return "semantic_search" }
func (t *SemanticSearchTool) Description() string {
	return "Search the codebase semantically using natural language or code descriptions. Returns ranked file chunks by conceptual similarity."
}
func (t *SemanticSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type": "object",
"properties": {
  "query": {"type": "string", "description": "Natural language or code description to search for"},
  "top_k": {"type": "integer", "description": "Number of results to return (default 5, max 20)"}
},
"required": ["query"]
}`)
}

// indexExtensions is the set of file extensions we index for semantic search.
var indexExtensions = map[string]bool{
	".go": true, ".ts": true, ".js": true, ".py": true,
	".rs": true, ".java": true, ".cs": true, ".md": true,
}

// skipDirs are directories we never descend into during indexing.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
}

func (t *SemanticSearchTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.embedder == nil {
		return "semantic_search is disabled: set llm.embedding_provider and llm.embedding_model in foreman.toml", nil
	}

	var args struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("semantic_search: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("semantic_search: query is required")
	}
	topK := args.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > 20 {
		topK = 20
	}

	// Get HEAD SHA.
	headSHA, err := getHeadSHA(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("semantic_search: get HEAD SHA: %w", err)
	}

	// Try cache.
	cached, err := t.db.GetEmbeddingsByRepoSHA(ctx, workDir, headSHA)
	if err != nil {
		return "", fmt.Errorf("semantic_search: get cached embeddings: %w", err)
	}

	if len(cached) == 0 {
		// Build index.
		cached, err = t.buildIndex(ctx, workDir, headSHA)
		if err != nil {
			return "", fmt.Errorf("semantic_search: build index: %w", err)
		}
	}

	// Embed the query.
	queryVecs, err := t.embedder.Embed(ctx, []string{args.Query})
	if err != nil {
		return "", fmt.Errorf("semantic_search: embed query: %w", err)
	}
	if len(queryVecs) == 0 || len(queryVecs[0]) == 0 {
		return "", fmt.Errorf("semantic_search: empty query vector")
	}
	queryVec := queryVecs[0]

	// Score all chunks.
	type scoredChunk struct {
		rec   db.EmbeddingRecord
		score float32
	}
	scored := make([]scoredChunk, 0, len(cached))
	for _, rec := range cached {
		s := db.CosineSimilarity(queryVec, rec.Vector)
		scored = append(scored, scoredChunk{rec: rec, score: s})
	}

	// Sort descending by score.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top_k.
	if topK > len(scored) {
		topK = len(scored)
	}
	results := make([]SearchResult, 0, topK)
	for _, sc := range scored[:topK] {
		snippet := sc.rec.ChunkText
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		relPath, _ := filepath.Rel(workDir, sc.rec.FilePath)
		if relPath == "" {
			relPath = sc.rec.FilePath
		}
		results = append(results, SearchResult{
			File:      relPath,
			StartLine: sc.rec.StartLine,
			EndLine:   sc.rec.EndLine,
			Score:     sc.score,
			Snippet:   snippet,
		})
	}

	out, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("semantic_search: marshal results: %w", err)
	}
	return string(out), nil
}

// buildIndex walks workDir, chunks all eligible files, embeds them, and stores in DB.
func (t *SemanticSearchTool) buildIndex(ctx context.Context, workDir, headSHA string) ([]db.EmbeddingRecord, error) {
	// Collect all chunks.
	var chunks []appcontext.Chunk
	err := filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !indexExtensions[ext] {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		fileChunks := appcontext.ChunkFile(path, string(content))
		chunks = append(chunks, fileChunks...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}

	if len(chunks) == 0 {
		return nil, nil
	}

	// Extract texts for embedding.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}

	// Embed all chunks (Embedder handles internal batching).
	vecs, err := t.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed chunks: %w", err)
	}
	if len(vecs) != len(chunks) {
		return nil, fmt.Errorf("embed chunks: got %d vectors for %d chunks", len(vecs), len(chunks))
	}

	// Upsert each chunk into DB and collect records.
	records := make([]db.EmbeddingRecord, 0, len(chunks))
	for i, c := range chunks {
		rec := db.EmbeddingRecord{
			RepoPath:  workDir,
			HeadSHA:   headSHA,
			FilePath:  c.FilePath,
			ChunkText: c.Text,
			Vector:    vecs[i],
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
		}
		if upsertErr := t.db.UpsertEmbedding(ctx, rec); upsertErr != nil {
			return nil, fmt.Errorf("upsert embedding: %w", upsertErr)
		}
		records = append(records, rec)
	}
	return records, nil
}

// getHeadSHA runs `git rev-parse HEAD` in workDir and returns the trimmed output.
func getHeadSHA(ctx context.Context, workDir string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}
