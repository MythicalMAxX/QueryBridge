package cache

import (
	"testing"
	"time"
)

func TestCacheSetAndGet(t *testing.T) {
	c := New(Config{MaxSize: 10, DefaultTTL: time.Minute})

	// Test set and get
	c.Set("key1", "value1", 0)
	val, ok := c.Get("key1")
	if !ok {
		t.Error("expected cache hit, got miss")
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got %v", val)
	}
}

func TestCacheMiss(t *testing.T) {
	c := New(Config{MaxSize: 10, DefaultTTL: time.Minute})

	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss, got hit")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := New(Config{MaxSize: 10, DefaultTTL: 50 * time.Millisecond})

	c.Set("key1", "value1", 50*time.Millisecond)
	
	// Should hit immediately
	_, ok := c.Get("key1")
	if !ok {
		t.Error("expected cache hit before expiry")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("key1")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	c := New(Config{MaxSize: 3, DefaultTTL: time.Minute})

	c.Set("key1", "value1", 0)
	c.Set("key2", "value2", 0)
	c.Set("key3", "value3", 0)

	// Access key1 to make it recently used
	c.Get("key1")

	// Add key4, should evict key2 (oldest)
	c.Set("key4", "value4", 0)

	// key1 should still exist
	_, ok := c.Get("key1")
	if !ok {
		t.Error("expected key1 to exist")
	}

	// key4 should exist
	_, ok = c.Get("key4")
	if !ok {
		t.Error("expected key4 to exist")
	}
}

func TestCacheStats(t *testing.T) {
	c := New(Config{MaxSize: 10, DefaultTTL: time.Minute})

	c.Set("key1", "value1", 0)
	c.Get("key1") // hit
	c.Get("key1") // hit
	c.Get("key2") // miss

	stats := c.Stats()
	if stats["hits"].(int64) != 2 {
		t.Errorf("expected 2 hits, got %v", stats["hits"])
	}
	if stats["misses"].(int64) != 1 {
		t.Errorf("expected 1 miss, got %v", stats["misses"])
	}
}

func TestCacheClear(t *testing.T) {
	c := New(Config{MaxSize: 10, DefaultTTL: time.Minute})

	c.Set("key1", "value1", 0)
	c.Set("key2", "value2", 0)

	c.Clear()

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected cache to be cleared")
	}
}

func TestGenerateKey(t *testing.T) {
	key1 := GenerateKey("postgres", map[string]interface{}{"table": "users"})
	key2 := GenerateKey("postgres", map[string]interface{}{"table": "users"})
	key3 := GenerateKey("postgres", map[string]interface{}{"table": "orders"})

	if key1 != key2 {
		t.Error("same params should generate same key")
	}
	if key1 == key3 {
		t.Error("different params should generate different keys")
	}
}
