// internal/context/cache_test.go
package context

import (
	goctx "context"
	"testing"

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
