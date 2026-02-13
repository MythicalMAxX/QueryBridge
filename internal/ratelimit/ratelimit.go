// Package ratelimit provides rate limiting for QueryBridge API.
package ratelimit

import (
	"net/http"
	"sync"
	"time"
)

// Config holds rate limiter configuration.
type Config struct {
	RequestsPerMinute int           `json:"requests_per_minute"`
	BurstSize         int           `json:"burst_size"`
	CleanupInterval   time.Duration `json:"cleanup_interval"`
}

// DefaultConfig returns default rate limiting configuration.
func DefaultConfig() Config {
	return Config{
		RequestsPerMinute: 60,
		BurstSize:         10,
		CleanupInterval:   5 * time.Minute,
	}
}

// bucket represents a token bucket for a single client.
type bucket struct {
	tokens     float64
	lastUpdate time.Time
}

// Limiter provides rate limiting using token bucket algorithm.
type Limiter struct {
	config    Config
	buckets   map[string]*bucket
	mu        sync.RWMutex
	rate      float64 // tokens per second
	stopCh    chan struct{}
}

// New creates a new rate limiter.
func New(cfg Config) *Limiter {
	if cfg.RequestsPerMinute <= 0 {
		cfg.RequestsPerMinute = 60
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = 10
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}

	l := &Limiter{
		config:  cfg,
		buckets: make(map[string]*bucket),
		rate:    float64(cfg.RequestsPerMinute) / 60.0,
		stopCh:  make(chan struct{}),
	}

	// Start cleanup goroutine
	go l.cleanup()

	return l
}

// Allow checks if a request from the given key should be allowed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, exists := l.buckets[key]

	if !exists {
		l.buckets[key] = &bucket{
			tokens:     float64(l.config.BurstSize) - 1,
			lastUpdate: now,
		}
		return true
	}

	// Calculate tokens to add since last update
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens += elapsed * l.rate

	// Cap at burst size
	if b.tokens > float64(l.config.BurstSize) {
		b.tokens = float64(l.config.BurstSize)
	}

	b.lastUpdate = now

	// Check if we have a token available
	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

// Remaining returns the number of remaining tokens for a key.
func (l *Limiter) Remaining(key string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	b, exists := l.buckets[key]
	if !exists {
		return l.config.BurstSize
	}

	// Calculate current tokens
	elapsed := time.Since(b.lastUpdate).Seconds()
	tokens := b.tokens + elapsed*l.rate
	if tokens > float64(l.config.BurstSize) {
		tokens = float64(l.config.BurstSize)
	}

	return int(tokens)
}

// Reset resets the rate limit for a key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// cleanup periodically removes stale buckets.
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(l.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for key, b := range l.buckets {
				// Remove buckets that are full and haven't been used recently
				if now.Sub(b.lastUpdate) > l.config.CleanupInterval {
					delete(l.buckets, key)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop stops the rate limiter cleanup goroutine.
func (l *Limiter) Stop() {
	close(l.stopCh)
}

// Middleware returns an HTTP middleware that enforces rate limits.
func (l *Limiter) Middleware(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			
			if !l.Allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Limit", string(rune(l.config.RequestsPerMinute)))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":true,"message":"rate limit exceeded"}`))
				return
			}

			// Add rate limit headers
			remaining := l.Remaining(key)
			w.Header().Set("X-RateLimit-Limit", string(rune(l.config.RequestsPerMinute)))
			w.Header().Set("X-RateLimit-Remaining", string(rune(remaining)))

			next.ServeHTTP(w, r)
		})
	}
}

// IPKeyFunc returns a key function that uses the client's IP address.
func IPKeyFunc(r *http.Request) string {
	// Try X-Forwarded-For first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// Stats returns rate limiter statistics.
func (l *Limiter) Stats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return map[string]interface{}{
		"active_clients":       len(l.buckets),
		"requests_per_minute":  l.config.RequestsPerMinute,
		"burst_size":           l.config.BurstSize,
	}
}
