package integration

import (
	"testing"

	appcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/stretchr/testify/assert"
)

// TestErrorClassifierSelectsRetryPrompt verifies that ClassifyRetryError correctly
// identifies compile errors from feedback containing "undefined:" and "build failed".
func TestErrorClassifierSelectsRetryPrompt(t *testing.T) {
	feedback := "undefined: SomeFunction\nbuild failed"
	result := pipeline.ClassifyRetryError(feedback)
	assert.Equal(t, pipeline.ErrorTypeCompile, result,
		"expected compile error classification for feedback containing 'undefined:' and 'build failed'")
}

// TestContextCacheInvalidatedAfterFileChange verifies that after warming the cache
// and calling Invalidate, GetSourceFiles returns nil (cache is cleared).
func TestContextCacheInvalidatedAfterFileChange(t *testing.T) {
	cache := appcontext.NewContextCache()

	// Warm the cache by scanning a known directory.
	workDir := "../fixtures/sample_repo"
	files := appcontext.GetOrListSourceFiles(cache, workDir)
	assert.NotNil(t, files, "expected GetOrListSourceFiles to return files after warming")
	assert.NotNil(t, cache.GetSourceFiles(), "expected cache to be warm after GetOrListSourceFiles")

	// After invalidation, the cache should be cleared.
	cache.Invalidate()
	assert.Nil(t, cache.GetSourceFiles(),
		"expected GetSourceFiles to return nil after Invalidate")
}
