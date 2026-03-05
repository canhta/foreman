package llm

import (
	"context"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// SharedRateLimiter provides per-provider rate limiting using token buckets.
type SharedRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	config   models.RateLimitConfig
}

// NewSharedRateLimiter creates a shared rate limiter.
func NewSharedRateLimiter(config models.RateLimitConfig) *SharedRateLimiter {
	return &SharedRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		config:   config,
	}
}

// effectiveRPM returns the configured RequestsPerMinute, defaulting to 50 if <= 0.
func (r *SharedRateLimiter) effectiveRPM() int {
	if r.config.RequestsPerMinute <= 0 {
		return 50
	}
	return r.config.RequestsPerMinute
}

// Wait blocks until the rate limiter allows the request or the context is cancelled.
func (r *SharedRateLimiter) Wait(ctx context.Context, provider string) error {
	limiter := r.getOrCreate(provider)
	return limiter.Wait(ctx)
}

// OnRateLimit adjusts the limiter when a 429 response is received.
func (r *SharedRateLimiter) OnRateLimit(provider string, retryAfterSecs int) {
	if retryAfterSecs <= 0 {
		retryAfterSecs = 60 // conservative default
	}
	log.Warn().Str("provider", provider).Int("retry_after_secs", retryAfterSecs).Msg("rate limit hit, throttling requests")

	limiter := r.getOrCreate(provider)
	// Temporarily reduce the rate
	limiter.SetLimit(rate.Every(time.Duration(retryAfterSecs) * time.Second))

	// Restore after the retry-after period
	go func() {
		time.Sleep(time.Duration(retryAfterSecs) * time.Second)
		limiter.SetLimit(rate.Every(time.Minute / time.Duration(r.effectiveRPM())))
	}()
}

func (r *SharedRateLimiter) getOrCreate(provider string) *rate.Limiter {
	r.mu.RLock()
	limiter, ok := r.limiters[provider]
	r.mu.RUnlock()
	if ok {
		return limiter
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, ok = r.limiters[provider]; ok {
		return limiter
	}

	rpm := r.effectiveRPM()
	burst := r.config.BurstSize
	if burst <= 0 {
		burst = 10
	}

	limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), burst)
	r.limiters[provider] = limiter
	return limiter
}
