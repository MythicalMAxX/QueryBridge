package template

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateStore(t *testing.T) {
	// Create temp file for testing
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "templates.json")

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Test save template
	tmpl := &Template{
		Name:        "get_users",
		Description: "Get users by status",
		Database:    "postgres",
		Query: map[string]interface{}{
			"table": "users",
			"filters": []map[string]interface{}{
				{"column": "status", "operator": "=", "value": "{{status}}"},
			},
		},
		Parameters: []Parameter{
			{Name: "status", Type: "string", Required: true},
		},
	}

	err = store.Save(tmpl)
	if err != nil {
		t.Fatalf("failed to save template: %v", err)
	}

	if tmpl.ID == "" {
		t.Error("expected ID to be generated")
	}

	// Test get template
	retrieved, err := store.Get(tmpl.ID)
	if err != nil {
		t.Fatalf("failed to get template: %v", err)
	}
	if retrieved.Name != "get_users" {
		t.Errorf("expected name 'get_users', got %s", retrieved.Name)
	}

	// Test get by name
	byName, err := store.Get("get_users")
	if err != nil {
		t.Fatalf("failed to get template by name: %v", err)
	}
	if byName.ID != tmpl.ID {
		t.Error("get by name returned wrong template")
	}

	// Test list templates
	list := store.List("")
	if len(list) != 1 {
		t.Errorf("expected 1 template, got %d", len(list))
	}

	// Test list by database
	list = store.List("postgres")
	if len(list) != 1 {
		t.Error("expected to find template for postgres")
	}

	list = store.List("mongodb")
	if len(list) != 0 {
		t.Error("expected no templates for mongodb")
	}

	// Test delete
	err = store.Delete(tmpl.ID)
	if err != nil {
		t.Fatalf("failed to delete template: %v", err)
	}

	_, err = store.Get(tmpl.ID)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestApplyParameters(t *testing.T) {
	query := map[string]interface{}{
		"table": "users",
		"filters": []interface{}{
			map[string]interface{}{
				"column":   "status",
				"operator": "=",
				"value":    "{{status}}",
			},
		},
		"limit": "{{limit}}",
	}

	params := map[string]interface{}{
		"status": "active",
		"limit":  10,
	}

	result, err := ApplyParameters(query, params)
	if err != nil {
		t.Fatalf("ApplyParameters failed: %v", err)
	}

	// Check that limit is now 10
	if result["limit"] != float64(10) { // JSON numbers are float64
		t.Errorf("expected limit 10, got %v", result["limit"])
	}
}

func TestStorePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "templates.json")

	// Create and save template
	store1, _ := NewStore(storePath)
	store1.Save(&Template{
		Name:     "test_template",
		Database: "postgres",
		Query:    map[string]interface{}{"table": "users"},
	})
	store1.Close()

	// Verify file exists
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("template file should exist")
	}

	// Create new store and verify template is loaded
	store2, _ := NewStore(storePath)
	list := store2.List("")
	if len(list) != 1 {
		t.Errorf("expected 1 template after reload, got %d", len(list))
	}
}

func TestIncrementUsage(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(filepath.Join(tmpDir, "templates.json"))

	tmpl := &Template{
		Name:     "usage_test",
		Database: "postgres",
		Query:    map[string]interface{}{"table": "users"},
	}
	store.Save(tmpl)

	if tmpl.UsageCount != 0 {
		t.Error("initial usage count should be 0")
	}

	store.IncrementUsage(tmpl.ID)
	store.IncrementUsage(tmpl.ID)

	retrieved, _ := store.Get(tmpl.ID)
	if retrieved.UsageCount != 2 {
		t.Errorf("expected usage count 2, got %d", retrieved.UsageCount)
	}
}
