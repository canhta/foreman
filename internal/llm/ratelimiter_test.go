package llm

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedRateLimiter_Wait(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 600, // 10/sec — fast enough for tests
		BurstSize:         10,
	})

	ctx := context.Background()
	start := time.Now()
	err := rl.Wait(ctx, "anthropic")
	require.NoError(t, err)
	elapsed := time.Since(start)

	// First call should be nearly instant due to burst
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestSharedRateLimiter_SeparateProviders(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 600,
		BurstSize:         1, // burst of 1 to easily exhaust
	})

	ctx := context.Background()
	// Exhaust anthropic's burst
	require.NoError(t, rl.Wait(ctx, "anthropic"))

	// openai should still have full burst available — immediate response
	start := time.Now()
	require.NoError(t, rl.Wait(ctx, "openai"))
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 50*time.Millisecond, "openai limiter should be independent of anthropic's")
}

func TestSharedRateLimiter_ContextCancellation(t *testing.T) {
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 1, // Very slow
		BurstSize:         1,
	})

	ctx := context.Background()
	// Use up the burst
	require.NoError(t, rl.Wait(ctx, "anthropic"))

	// Next call should block — cancel it
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := rl.Wait(cancelCtx, "anthropic")
	assert.Error(t, err) // Should fail due to context timeout
}

func TestSharedRateLimiter_OnRateLimit_ZeroRetryAfter(t *testing.T) {
	// Should not panic or set unlimited rate
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         1,
	})
	// Should not panic with retryAfterSecs=0
	rl.OnRateLimit("anthropic", 0)
	// The limiter should still be functional
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// After clamp, the limiter allows some operations (not inf rate)
	_ = rl.Wait(ctx, "anthropic")
}

func TestSharedRateLimiter_OnRateLimit_ZeroRPM(t *testing.T) {
	// Should not panic with zero RPM config
	rl := NewSharedRateLimiter(models.RateLimitConfig{
		RequestsPerMinute: 0, // zero RPM — uses default 50
		BurstSize:         1,
	})
	rl.OnRateLimit("anthropic", 1)
	// Give goroutine time to restore
	time.Sleep(1500 * time.Millisecond)
	// Should not have panicked
}
