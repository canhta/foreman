// internal/context/cache.go
package context

import (
	goctx "context"
	"sync"
	"sync/atomic"

	"github.com/canhta/foreman/internal/models"
)

type contextKeyType struct{}

// contextKey is the package-private key used to store a ContextCache in a context.Context.
var contextKey = contextKeyType{}

// ContextCache caches expensive per-pipeline context operations (file tree, source
// file list) so they are computed at most once per ticket rather than once per
// pipeline stage.
//
// It is created once per ticket by the orchestrator, injected into context.Context
// via WithCache, and shared across all pipeline stages for that ticket. Concurrent
// access is safe. After git operations (commit, checkout, rebase) that change
// HEAD, call Invalidate so the next read re-scans from disk.
//
// Field ordering is chosen to minimise GC-scan pointer bytes (all pointer fields
// first, then non-pointer atomics and mutex last).
type ContextCache struct {
	// scoredFiles caches SelectFilesForTask results keyed by task ID.
	// It is cleared on Invalidate because scored results depend on tree state.
	scoredFiles map[string][]ScoredFile
	repoInfo    *RepoInfo
	sourceFiles []string
	// hits and total are lifetime counters for computing the cache hit ratio.
	// They are NOT reset by Invalidate — they track the lifetime ratio.
	hits  atomic.Int64
	total atomic.Int64
	mu    sync.RWMutex
}

// NewContextCache creates an empty ContextCache.
func NewContextCache() *ContextCache {
	return &ContextCache{}
}

// GetRepoInfo returns the cached RepoInfo, or nil if not yet cached.
func (c *ContextCache) GetRepoInfo() *RepoInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.repoInfo
}

// SetRepoInfo stores a RepoInfo in the cache.
func (c *ContextCache) SetRepoInfo(info *RepoInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoInfo = info
}

// GetSourceFiles returns the cached source file list, or nil if not yet cached.
func (c *ContextCache) GetSourceFiles() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sourceFiles
}

// SetSourceFiles stores the source file list in the cache.
func (c *ContextCache) SetSourceFiles(files []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sourceFiles = files
}

// Invalidate clears all cached data.
// Call after git operations that change HEAD (commit, checkout, rebase).
// Note: the hit/miss counters are intentionally NOT cleared — they track the
// lifetime hit ratio across the entire pipeline run.
func (c *ContextCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoInfo = nil
	c.sourceFiles = nil
	c.scoredFiles = nil
}

// HitRatio returns the fraction of cache lookups that were served from the
// cache (hits / total). Returns 0.0 if no lookups have been made yet.
func (c *ContextCache) HitRatio() float64 {
	total := c.total.Load()
	if total == 0 {
		return 0.0
	}
	return float64(c.hits.Load()) / float64(total)
}

// WithCache returns a new context.Context carrying the given ContextCache.
func WithCache(ctx goctx.Context, cache *ContextCache) goctx.Context {
	return goctx.WithValue(ctx, contextKey, cache)
}

// CacheFromContext extracts a ContextCache from the context, or returns nil
// if no cache was stored.
func CacheFromContext(ctx goctx.Context) *ContextCache {
	c, _ := ctx.Value(contextKey).(*ContextCache)
	return c
}

// GetOrAnalyzeRepo returns the cached RepoInfo if available; otherwise it calls
// AnalyzeRepo, caches the result, and returns it. cache may be nil (disables caching).
func GetOrAnalyzeRepo(cache *ContextCache, workDir string) (*RepoInfo, error) {
	if cache != nil {
		cache.total.Add(1)
		if info := cache.GetRepoInfo(); info != nil {
			cache.hits.Add(1)
			return info, nil
		}
	}
	info, err := AnalyzeRepo(workDir)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		cache.SetRepoInfo(info)
	}
	return info, nil
}

// GetOrListSourceFiles returns the cached source file list if available; otherwise
// it scans workDir, caches the result, and returns it. cache may be nil.
func GetOrListSourceFiles(cache *ContextCache, workDir string) []string {
	if cache != nil {
		cache.total.Add(1)
		if files := cache.GetSourceFiles(); files != nil {
			cache.hits.Add(1)
			return files
		}
	}
	files := listSourceFiles(workDir)
	if cache != nil {
		cache.SetSourceFiles(files)
	}
	return files
}

// GetScoredFiles returns the cached scored file list for taskID, or nil/false if
// not yet cached.
func (c *ContextCache) GetScoredFiles(taskID string) ([]ScoredFile, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	files, ok := c.scoredFiles[taskID]
	return files, ok
}

// SetScoredFiles stores a scored file list for taskID in the cache.
func (c *ContextCache) SetScoredFiles(taskID string, files []ScoredFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scoredFiles == nil {
		c.scoredFiles = make(map[string][]ScoredFile)
	}
	c.scoredFiles[taskID] = files
}

// GetOrSelectFiles returns the cached scored file list for the task if available;
// otherwise it calls SelectFilesForTask, caches the result, and returns it.
// cache may be nil (disables caching).
func GetOrSelectFiles(cache *ContextCache, task *models.Task, workDir string, tokenBudget int, fq FeedbackQuerier, feedbackBoost float64, patterns ...models.ProgressPattern) ([]ScoredFile, error) {
	if cache != nil {
		cache.total.Add(1)
		if files, ok := cache.GetScoredFiles(task.ID); ok {
			cache.hits.Add(1)
			return files, nil
		}
	}
	files, err := SelectFilesForTask(task, workDir, tokenBudget, cache, fq, feedbackBoost, patterns...)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		cache.SetScoredFiles(task.ID, files)
	}
	return files, nil
}
