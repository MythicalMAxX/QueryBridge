package query

import (
	"testing"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
)

func TestCrossJoinInner(t *testing.T) {
	left := &adapter.Result{
		Columns: []string{"id", "name"},
		Rows: []adapter.Row{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
			{"id": 3, "name": "Charlie"},
		},
	}

	right := &adapter.Result{
		Columns: []string{"user_id", "order"},
		Rows: []adapter.Row{
			{"user_id": 1, "order": "Order1"},
			{"user_id": 1, "order": "Order2"},
			{"user_id": 2, "order": "Order3"},
		},
	}

	cfg := &CrossJoinConfig{
		LeftResult:  left,
		RightResult: right,
		LeftKey:     "id",
		RightKey:    "user_id",
		JoinType:    InnerJoin,
		LeftAlias:   "users",
		RightAlias:  "orders",
	}

	result, err := CrossJoin(cfg)
	if err != nil {
		t.Fatalf("CrossJoin failed: %v", err)
	}

	// Should have 3 rows: Alice-Order1, Alice-Order2, Bob-Order3
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}

	// Check columns are prefixed
	expectedCols := []string{"users.id", "users.name", "orders.user_id", "orders.order"}
	if len(result.Columns) != len(expectedCols) {
		t.Errorf("expected %d columns, got %d", len(expectedCols), len(result.Columns))
	}
}

func TestCrossJoinLeft(t *testing.T) {
	left := &adapter.Result{
		Columns: []string{"id", "name"},
		Rows: []adapter.Row{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
			{"id": 3, "name": "Charlie"}, // No matching order
		},
	}

	right := &adapter.Result{
		Columns: []string{"user_id", "order"},
		Rows: []adapter.Row{
			{"user_id": 1, "order": "Order1"},
			{"user_id": 2, "order": "Order2"},
		},
	}

	cfg := &CrossJoinConfig{
		LeftResult:  left,
		RightResult: right,
		LeftKey:     "id",
		RightKey:    "user_id",
		JoinType:    LeftJoin,
		LeftAlias:   "users",
		RightAlias:  "orders",
	}

	result, err := CrossJoin(cfg)
	if err != nil {
		t.Fatalf("CrossJoin failed: %v", err)
	}

	// Should have 3 rows: Alice-Order1, Bob-Order2, Charlie-NULL
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}
}

func TestCrossJoinEmptyResults(t *testing.T) {
	left := &adapter.Result{
		Columns: []string{"id"},
		Rows:    []adapter.Row{},
	}

	right := &adapter.Result{
		Columns: []string{"id"},
		Rows:    []adapter.Row{{"id": 1}},
	}

	cfg := &CrossJoinConfig{
		LeftResult:  left,
		RightResult: right,
		LeftKey:     "id",
		RightKey:    "id",
		JoinType:    InnerJoin,
	}

	result, err := CrossJoin(cfg)
	if err != nil {
		t.Fatalf("CrossJoin failed: %v", err)
	}

	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result.Rows))
	}
}

func TestCrossJoinMissingParams(t *testing.T) {
	// Test missing left result
	_, err := CrossJoin(&CrossJoinConfig{
		LeftResult: nil,
		RightResult: &adapter.Result{},
		LeftKey:     "id",
		RightKey:    "id",
	})
	if err == nil {
		t.Error("expected error for missing left result")
	}

	// Test missing join keys
	_, err = CrossJoin(&CrossJoinConfig{
		LeftResult:  &adapter.Result{},
		RightResult: &adapter.Result{},
		LeftKey:     "",
		RightKey:    "id",
	})
	if err == nil {
		t.Error("expected error for missing join key")
	}
}
