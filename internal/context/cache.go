// internal/context/cache.go
package context

import (
	goctx "context"
	"sync"
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
type ContextCache struct {
	repoInfo    *RepoInfo
	sourceFiles []string
	mu          sync.RWMutex
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
func (c *ContextCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoInfo = nil
	c.sourceFiles = nil
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
		if info := cache.GetRepoInfo(); info != nil {
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
		if files := cache.GetSourceFiles(); files != nil {
			return files
		}
	}
	files := listSourceFiles(workDir)
	if cache != nil {
		cache.SetSourceFiles(files)
	}
	return files
}
