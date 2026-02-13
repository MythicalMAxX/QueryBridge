// Package cache provides an LRU cache for query results.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Entry represents a cached query result.
type Entry struct {
	Data      interface{}
	CreatedAt time.Time
	TTL       time.Duration
	Hits      int
}

// IsExpired checks if the cache entry has expired.
func (e *Entry) IsExpired() bool {
	if e.TTL == 0 {
		return false
	}
	return time.Since(e.CreatedAt) > e.TTL
}

// Cache provides an LRU cache with TTL support for query results.
type Cache struct {
	mu       sync.RWMutex
	entries  map[string]*Entry
	order    []string // For LRU tracking
	maxSize  int
	defaultTTL time.Duration
	hits     int64
	misses   int64
}

// Config holds cache configuration.
type Config struct {
	MaxSize    int           // Maximum number of entries
	DefaultTTL time.Duration // Default TTL for entries
}

// DefaultConfig returns default cache configuration.
func DefaultConfig() Config {
	return Config{
		MaxSize:    100,
		DefaultTTL: 5 * time.Minute,
	}
}

// New creates a new cache with the given configuration.
func New(cfg Config) *Cache {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 100
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}

	c := &Cache{
		entries:    make(map[string]*Entry),
		order:      make([]string, 0, cfg.MaxSize),
		maxSize:    cfg.MaxSize,
		defaultTTL: cfg.DefaultTTL,
	}

	// Start cleanup goroutine
	go c.cleanup()

	return c
}

// GenerateKey creates a cache key from query parameters.
func GenerateKey(database string, query interface{}) string {
	data, _ := json.Marshal(map[string]interface{}{
		"database": database,
		"query":    query,
	})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Get retrieves an entry from the cache.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}

	if entry.IsExpired() {
		c.remove(key)
		c.misses++
		return nil, false
	}

	// Update LRU order
	c.moveToFront(key)
	entry.Hits++
	c.hits++

	return entry.Data, true
}

// Set stores an entry in the cache.
func (c *Cache) Set(key string, data interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ttl == 0 {
		ttl = c.defaultTTL
	}

	// Remove if exists to update
	if _, exists := c.entries[key]; exists {
		c.remove(key)
	}

	// Evict oldest if at capacity
	for len(c.entries) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[len(c.order)-1]
		c.remove(oldest)
	}

	c.entries[key] = &Entry{
		Data:      data,
		CreatedAt: time.Now(),
		TTL:       ttl,
		Hits:      0,
	}
	c.order = append([]string{key}, c.order...)
}

// Invalidate removes an entry from the cache.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(key)
}

// InvalidateDatabase removes all entries for a specific database.
func (c *Cache) InvalidateDatabase(database string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// We need to decode keys to check database, so we track separately
	// For now, clear all (simpler implementation)
	c.entries = make(map[string]*Entry)
	c.order = c.order[:0]
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*Entry)
	c.order = c.order[:0]
}

// Stats returns cache statistics.
func (c *Cache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hitRate := 0.0
	total := c.hits + c.misses
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100
	}

	return map[string]interface{}{
		"size":       len(c.entries),
		"max_size":   c.maxSize,
		"hits":       c.hits,
		"misses":     c.misses,
		"hit_rate":   hitRate,
		"default_ttl": c.defaultTTL.String(),
	}
}

// remove removes an entry (must hold lock).
func (c *Cache) remove(key string) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// moveToFront moves a key to the front of the LRU list (must hold lock).
func (c *Cache) moveToFront(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append([]string{key}, c.order...)
			break
		}
	}
}

// cleanup periodically removes expired entries.
func (c *Cache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		for key, entry := range c.entries {
			if entry.IsExpired() {
				c.remove(key)
			}
		}
		c.mu.Unlock()
	}
}
