// Package template provides query template storage and execution.
package template

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"
)

// Template represents a saved query template.
type Template struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Database    string                 `json:"database"`
	Query       map[string]interface{} `json:"query"`
	Parameters  []Parameter            `json:"parameters,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	UsageCount  int                    `json:"usage_count"`
}

// Parameter defines a template parameter.
type Parameter struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // string, int, float, bool
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description,omitempty"`
}

// Store provides storage for query templates.
type Store struct {
	path      string
	templates map[string]*Template
	mu        sync.RWMutex
}

// NewStore creates a new template store.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:      path,
		templates: make(map[string]*Template),
	}

	// Load existing templates
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		var templates []*Template
		if err := json.Unmarshal(data, &templates); err == nil {
			for _, t := range templates {
				s.templates[t.ID] = t
			}
		}
	}

	return s, nil
}

// save persists templates to disk.
func (s *Store) save() error {
	templates := make([]*Template, 0, len(s.templates))
	for _, t := range s.templates {
		templates = append(templates, t)
	}

	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling templates: %w", err)
	}

	return os.WriteFile(s.path, data, 0644)
}

// Save stores a new template.
func (s *Store) Save(t *Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t.ID == "" {
		t.ID = generateID(t.Name)
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	t.UpdatedAt = time.Now()

	s.templates[t.ID] = t
	return s.save()
}

// Get retrieves a template by ID or name.
func (s *Store) Get(idOrName string) (*Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try by ID first
	if t, ok := s.templates[idOrName]; ok {
		return t, nil
	}

	// Try by name
	for _, t := range s.templates {
		if t.Name == idOrName {
			return t, nil
		}
	}

	return nil, fmt.Errorf("template not found: %s", idOrName)
}

// List returns all templates, optionally filtered by database.
func (s *Store) List(database string) []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Template
	for _, t := range s.templates {
		if database == "" || t.Database == database {
			result = append(result, t)
		}
	}
	return result
}

// Delete removes a template.
func (s *Store) Delete(idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try by ID first
	if _, ok := s.templates[idOrName]; ok {
		delete(s.templates, idOrName)
		return s.save()
	}

	// Try by name
	for id, t := range s.templates {
		if t.Name == idOrName {
			delete(s.templates, id)
			return s.save()
		}
	}

	return fmt.Errorf("template not found: %s", idOrName)
}

// IncrementUsage increments the usage count for a template.
func (s *Store) IncrementUsage(idOrName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try by ID first
	if t, ok := s.templates[idOrName]; ok {
		t.UsageCount++
		t.UpdatedAt = time.Now()
		s.save()
		return
	}

	// Try by name
	for _, t := range s.templates {
		if t.Name == idOrName {
			t.UsageCount++
			t.UpdatedAt = time.Now()
			s.save()
			return
		}
	}
}

// ApplyParameters applies parameter values to a template query.
func ApplyParameters(query map[string]interface{}, params map[string]interface{}) (map[string]interface{}, error) {
	// Deep copy the query
	data, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	// Replace {{param}} and "{{param}}" placeholders
	queryStr := string(data)
	
	// Pattern to match "{{param}}" (quoted placeholder for strings)
	reQuoted := regexp.MustCompile(`"\{\{(\w+)\}\}"`)
	queryStr = reQuoted.ReplaceAllStringFunc(queryStr, func(match string) string {
		// Extract param name (remove "{{ and }}")
		paramName := match[3 : len(match)-3]
		if val, ok := params[paramName]; ok {
			// JSON encode the value - already includes quotes for strings
			encoded, err := json.Marshal(val)
			if err != nil {
				return match
			}
			return string(encoded)
		}
		return match
	})

	// Pattern to match {{param}} (unquoted placeholder for non-strings)
	reUnquoted := regexp.MustCompile(`\{\{(\w+)\}\}`)
	queryStr = reUnquoted.ReplaceAllStringFunc(queryStr, func(match string) string {
		paramName := match[2 : len(match)-2] // Remove {{ and }}
		if val, ok := params[paramName]; ok {
			// JSON encode the value
			encoded, err := json.Marshal(val)
			if err != nil {
				return match
			}
			return string(encoded)
		}
		return match
	})

	// Parse back to map
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(queryStr), &result); err != nil {
		return nil, fmt.Errorf("invalid template after parameter substitution: %w", err)
	}

	return result, nil
}

// generateID creates a URL-safe ID from a name.
func generateID(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	id := re.ReplaceAllString(name, "_")
	return fmt.Sprintf("%s_%d", id, time.Now().UnixNano()%10000)
}

// Close closes the store.
func (s *Store) Close() error {
	return nil
}
