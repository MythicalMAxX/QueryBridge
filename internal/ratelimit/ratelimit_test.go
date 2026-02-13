package ratelimit

import (
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60,
		BurstSize:         5,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	// First 5 requests should be allowed (burst)
	for i := 0; i < 5; i++ {
		if !limiter.Allow("client1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	if limiter.Allow("client1") {
		t.Error("6th request should be denied (burst exhausted)")
	}
}

func TestRateLimiterTokenRefill(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60, // 1 per second
		BurstSize:         2,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	// Exhaust burst
	limiter.Allow("client1")
	limiter.Allow("client1")

	// Should be denied
	if limiter.Allow("client1") {
		t.Error("should be denied after burst")
	}

	// Wait for token refill (1 second = 1 token)
	time.Sleep(1100 * time.Millisecond)

	// Should be allowed again
	if !limiter.Allow("client1") {
		t.Error("should be allowed after token refill")
	}
}

func TestRateLimiterMultipleClients(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60,
		BurstSize:         3,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	// Client 1 uses their limit
	limiter.Allow("client1")
	limiter.Allow("client1")
	limiter.Allow("client1")

	// Client 1 should be denied
	if limiter.Allow("client1") {
		t.Error("client1 should be rate limited")
	}

	// Client 2 should still be allowed (independent limit)
	if !limiter.Allow("client2") {
		t.Error("client2 should not be rate limited")
	}
}

func TestRateLimiterRemaining(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60,
		BurstSize:         5,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	// New client should have full burst
	remaining := limiter.Remaining("newclient")
	if remaining != 5 {
		t.Errorf("expected 5 remaining, got %d", remaining)
	}

	// Use some requests
	limiter.Allow("newclient")
	limiter.Allow("newclient")

	remaining = limiter.Remaining("newclient")
	if remaining > 3 {
		t.Errorf("expected at most 3 remaining, got %d", remaining)
	}
}

func TestRateLimiterReset(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60,
		BurstSize:         3,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	// Exhaust limit
	limiter.Allow("client1")
	limiter.Allow("client1")
	limiter.Allow("client1")

	if limiter.Allow("client1") {
		t.Error("should be rate limited")
	}

	// Reset
	limiter.Reset("client1")

	// Should be allowed again
	if !limiter.Allow("client1") {
		t.Error("should be allowed after reset")
	}
}

func TestRateLimiterStats(t *testing.T) {
	limiter := New(Config{
		RequestsPerMinute: 60,
		BurstSize:         5,
		CleanupInterval:   time.Minute,
	})
	defer limiter.Stop()

	limiter.Allow("client1")
	limiter.Allow("client2")

	stats := limiter.Stats()
	if stats["active_clients"].(int) != 2 {
		t.Errorf("expected 2 active clients, got %v", stats["active_clients"])
	}
}
