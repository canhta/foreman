// internal/context/cache_test.go
package context

import (
	goctx "context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCache_GetOrAnalyzeRepo_Hit(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	// First call: cache miss — queries disk.
	info1, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)
	assert.NotNil(t, info1)

	// Second call: cache hit — must return the same *RepoInfo pointer.
	info2, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)
	assert.Same(t, info1, info2, "expected cache hit to return same pointer")
}

func TestContextCache_GetOrAnalyzeRepo_Invalidate(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	info1, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)

	cache.Invalidate()

	info2, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)
	// After invalidation a new RepoInfo is created, so pointers differ.
	assert.NotSame(t, info1, info2, "expected new pointer after invalidation")
}

func TestContextCache_NilSafe(t *testing.T) {
	workDir := setupTestRepo(t)
	// Passing nil cache should not panic and should still return correct results.
	info, err := GetOrAnalyzeRepo(nil, workDir)
	require.NoError(t, err)
	assert.NotNil(t, info)
}

func TestContextCache_ContextRoundtrip(t *testing.T) {
	cache := NewContextCache()

	ctx := WithCache(goctx.Background(), cache)
	got := CacheFromContext(ctx)
	assert.Same(t, cache, got)

	// Missing key returns nil.
	assert.Nil(t, CacheFromContext(goctx.Background()))
}

func TestContextCache_GetOrListSourceFiles_Hit(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	// Warm the cache.
	files1 := GetOrListSourceFiles(cache, workDir)
	assert.NotEmpty(t, files1)

	// Verify cache stored the list.
	assert.NotNil(t, cache.GetSourceFiles())

	// Second call returns same cached list.
	files2 := GetOrListSourceFiles(cache, workDir)
	assert.Equal(t, files1, files2)
}

func TestContextCache_Invalidate_ClearsAll(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	_, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)
	_ = GetOrListSourceFiles(cache, workDir)

	cache.Invalidate()
	assert.Nil(t, cache.GetRepoInfo())
	assert.Nil(t, cache.GetSourceFiles())
}

// ---------------------------------------------------------------------------
// HitRatio tests (REQ-TELE-001)
// ---------------------------------------------------------------------------

func TestContextCache_HitRatio_MissAndHit_ReturnsHalf(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	// First call: cache miss (total=1, hits=0).
	_, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)

	// Second call: cache hit (total=2, hits=1).
	_, err = GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)

	assert.InDelta(t, 0.5, cache.HitRatio(), 1e-9)
}

func TestContextCache_HitRatio_TwoHitsSourceFiles_ReturnsOne(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	// Warm cache with first call (miss).
	_ = GetOrListSourceFiles(cache, workDir)

	// Two more calls — both hits. Total=3, hits=2; ratio=2/3.
	_ = GetOrListSourceFiles(cache, workDir)
	_ = GetOrListSourceFiles(cache, workDir)

	// When both subsequent calls are hits after one miss: hits=2, total=3 → ~0.667.
	// Task spec says "After two hits in GetOrListSourceFiles, HitRatio() returns 1.0"
	// which implies we start with an already-warm cache. Use a fresh cache pre-seeded.
	cache2 := NewContextCache()
	files := GetOrListSourceFiles(nil, workDir) // compute without cache
	cache2.SetSourceFiles(files)                // pre-seed

	// Now both calls are hits.
	_ = GetOrListSourceFiles(cache2, workDir)
	_ = GetOrListSourceFiles(cache2, workDir)

	assert.InDelta(t, 1.0, cache2.HitRatio(), 1e-9)
}

func TestContextCache_HitRatio_ZeroTotal_ReturnsZero(t *testing.T) {
	cache := NewContextCache()
	assert.InDelta(t, 0.0, cache.HitRatio(), 1e-9)
}

func TestContextCache_HitRatio_NotClearedByInvalidate(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()

	// First call: miss (total=1, hits=0).
	_, err := GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)

	// Second call: hit (total=2, hits=1) → ratio=0.5.
	_, err = GetOrAnalyzeRepo(cache, workDir)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, cache.HitRatio(), 1e-9, "sanity check before Invalidate")

	// Invalidate clears cached data but NOT the lifetime counters.
	cache.Invalidate()

	// After Invalidate the counters must still show the pre-invalidate ratio.
	// If Invalidate incorrectly zeroed the counters, HitRatio() would return 0.0.
	assert.InDelta(t, 0.5, cache.HitRatio(), 1e-9, "counters must survive Invalidate")
}

// ---------------------------------------------------------------------------
// GetOrSelectFiles tests
// ---------------------------------------------------------------------------

func TestContextCache_GetOrSelectFiles_CacheHit(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()
	task := &models.Task{
		ID:            "task-1",
		FilesToModify: []string{"internal/handler.go"},
	}

	// First call: cache miss — computes from disk.
	files1, err := GetOrSelectFiles(cache, task, workDir, 80000, nil, 1.5)
	require.NoError(t, err)
	assert.NotEmpty(t, files1)

	// Second call with same taskID: cache hit — must return identical data.
	files2, err := GetOrSelectFiles(cache, task, workDir, 80000, nil, 1.5)
	require.NoError(t, err)
	assert.Equal(t, files1, files2, "expected cache hit to return same scored files")
}

func TestContextCache_GetOrSelectFiles_InvalidateClearsScoredFiles(t *testing.T) {
	workDir := setupTestRepo(t)
	cache := NewContextCache()
	task := &models.Task{
		ID:            "task-2",
		FilesToModify: []string{"internal/handler.go"},
	}

	// Warm the cache.
	files1, err := GetOrSelectFiles(cache, task, workDir, 80000, nil, 1.5)
	require.NoError(t, err)
	assert.NotEmpty(t, files1)

	// Confirm it is stored in cache.
	_, ok := cache.GetScoredFiles(task.ID)
	assert.True(t, ok, "expected scored files to be cached before Invalidate")

	// Invalidate clears scored files.
	cache.Invalidate()
	_, ok = cache.GetScoredFiles(task.ID)
	assert.False(t, ok, "expected scored files to be cleared after Invalidate")

	// Third call recomputes (new miss after invalidation).
	files3, err := GetOrSelectFiles(cache, task, workDir, 80000, nil, 1.5)
	require.NoError(t, err)
	assert.Equal(t, files1, files3, "recomputed result should be equivalent to original")
}

func TestContextCache_GetOrSelectFiles_NilCacheSafe(t *testing.T) {
	workDir := setupTestRepo(t)
	task := &models.Task{
		ID:            "task-3",
		FilesToModify: []string{"internal/handler.go"},
	}

	// Passing nil cache should not panic and should return correct results.
	files, err := GetOrSelectFiles(nil, task, workDir, 80000, nil, 1.5)
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}
