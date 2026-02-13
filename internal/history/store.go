// Package history provides query history storage and retrieval.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Entry represents a single query history entry.
type Entry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Database  string    `json:"database"`
	Query     string    `json:"query"`
	Tool      string    `json:"tool"`
	RowCount  int       `json:"row_count"`
	Duration  int64     `json:"duration_ms"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// Store provides persistent storage for query history using JSON file.
type Store struct {
	path    string
	entries []Entry
	nextID  int64
	mu      sync.RWMutex
}

// ListOptions specifies filtering options for listing history.
type ListOptions struct {
	Limit    int
	Offset   int
	Database string
	Tool     string
	Success  *bool
	FromTime *time.Time
	ToTime   *time.Time
}

// NewStore creates a new history store with JSON file backend.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:    path,
		entries: []Entry{},
		nextID:  1,
	}

	// Try to load existing history
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		var entries []Entry
		if err := json.Unmarshal(data, &entries); err == nil {
			s.entries = entries
			// Find max ID
			for _, e := range entries {
				if e.ID >= s.nextID {
					s.nextID = e.ID + 1
				}
			}
		}
	}

	return s, nil
}

// save persists the history to disk.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling history: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("writing history file: %w", err)
	}

	return nil
}

// Record adds a new entry to the history.
func (s *Store) Record(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	entry.ID = s.nextID
	s.nextID++

	// Prepend to keep newest first
	s.entries = append([]Entry{entry}, s.entries...)

	// Keep only last 1000 entries
	if len(s.entries) > 1000 {
		s.entries = s.entries[:1000]
	}

	return s.save()
}

// List retrieves history entries based on filter options.
func (s *Store) List(opts ListOptions) ([]Entry, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Set defaults
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000
	}

	// Filter entries
	var filtered []Entry
	for _, e := range s.entries {
		if opts.Database != "" && e.Database != opts.Database {
			continue
		}
		if opts.Tool != "" && e.Tool != opts.Tool {
			continue
		}
		if opts.Success != nil && e.Success != *opts.Success {
			continue
		}
		if opts.FromTime != nil && e.Timestamp.Before(*opts.FromTime) {
			continue
		}
		if opts.ToTime != nil && e.Timestamp.After(*opts.ToTime) {
			continue
		}
		filtered = append(filtered, e)
	}

	total := len(filtered)

	// Sort by timestamp descending (newest first)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	// Apply pagination
	start := opts.Offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + opts.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], total, nil
}

// Get retrieves a single history entry by ID.
func (s *Store) Get(id int64) (*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.entries {
		if e.ID == id {
			return &e, nil
		}
	}

	return nil, nil
}

// Clear removes all history entries.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = []Entry{}
	s.nextID = 1

	return s.save()
}

// Close closes the store (no-op for file-based store).
func (s *Store) Close() error {
	return nil
}

// Stats returns statistics about the history.
func (s *Store) Stats() (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_queries"] = len(s.entries)

	// Success rate
	successCount := 0
	var totalDuration int64
	for _, e := range s.entries {
		if e.Success {
			successCount++
		}
		totalDuration += e.Duration
	}

	if len(s.entries) > 0 {
		stats["success_rate"] = float64(successCount) / float64(len(s.entries)) * 100
		stats["avg_duration_ms"] = float64(totalDuration) / float64(len(s.entries))
	} else {
		stats["success_rate"] = 100.0
		stats["avg_duration_ms"] = 0.0
	}

	// Queries by database
	dbStats := make(map[string]int)
	for _, e := range s.entries {
		dbStats[e.Database]++
	}
	stats["by_database"] = dbStats

	return stats, nil
}
