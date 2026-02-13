// Package query provides query planning, validation, and execution.
// This file adds cross-database join support.
package query

import (
	"fmt"
	"strings"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
)

// JoinType represents the type of join operation.
type JoinType string

const (
	InnerJoin JoinType = "inner"
	LeftJoin  JoinType = "left"
	RightJoin JoinType = "right"
)

// CrossJoinConfig holds configuration for a cross-database join.
type CrossJoinConfig struct {
	LeftResult  *adapter.Result
	RightResult *adapter.Result
	LeftKey     string
	RightKey    string
	JoinType    JoinType
	LeftAlias   string
	RightAlias  string
}

// CrossJoin performs an in-memory join between two result sets from different databases.
// Uses hash join algorithm for O(n+m) performance.
func CrossJoin(cfg *CrossJoinConfig) (*adapter.Result, error) {
	if cfg.LeftResult == nil || cfg.RightResult == nil {
		return nil, fmt.Errorf("both left and right results are required")
	}

	if cfg.LeftKey == "" || cfg.RightKey == "" {
		return nil, fmt.Errorf("join keys are required")
	}

	if cfg.JoinType == "" {
		cfg.JoinType = InnerJoin
	}

	if cfg.LeftAlias == "" {
		cfg.LeftAlias = "left"
	}
	if cfg.RightAlias == "" {
		cfg.RightAlias = "right"
	}

	// Build hash index on right side for O(1) lookup
	rightIndex := buildHashIndex(cfg.RightResult.Rows, cfg.RightKey)

	// Build merged columns list
	columns := buildMergedColumns(cfg.LeftResult.Columns, cfg.RightResult.Columns, cfg.LeftAlias, cfg.RightAlias)

	// Perform join
	var rows []adapter.Row

	switch cfg.JoinType {
	case InnerJoin:
		rows = innerJoin(cfg.LeftResult.Rows, rightIndex, cfg.LeftKey, cfg.RightKey, cfg.LeftAlias, cfg.RightAlias)
	case LeftJoin:
		rows = leftJoin(cfg.LeftResult.Rows, rightIndex, cfg.RightResult.Columns, cfg.LeftKey, cfg.RightKey, cfg.LeftAlias, cfg.RightAlias)
	case RightJoin:
		// Right join is left join with swapped operands
		leftIndex := buildHashIndex(cfg.LeftResult.Rows, cfg.LeftKey)
		rows = leftJoin(cfg.RightResult.Rows, leftIndex, cfg.LeftResult.Columns, cfg.RightKey, cfg.LeftKey, cfg.RightAlias, cfg.LeftAlias)
	default:
		return nil, fmt.Errorf("unsupported join type: %s", cfg.JoinType)
	}

	return &adapter.Result{
		Columns: columns,
		Rows:    rows,
	}, nil
}

// buildHashIndex creates a hash index on a set of rows keyed by the specified column.
func buildHashIndex(rows []adapter.Row, keyColumn string) map[interface{}][]adapter.Row {
	index := make(map[interface{}][]adapter.Row)
	for _, row := range rows {
		key := getColumnValue(row, keyColumn)
		if key != nil {
			index[key] = append(index[key], row)
		}
	}
	return index
}

// getColumnValue extracts a column value, handling nested paths like "user.id".
func getColumnValue(row adapter.Row, column string) interface{} {
	// Handle simple column names
	if val, ok := row[column]; ok {
		return val
	}

	// Handle nested paths
	parts := strings.Split(column, ".")
	current := interface{}(map[string]interface{}(row))
	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return nil
		}
	}
	return current
}

// buildMergedColumns creates the column list for the joined result.
func buildMergedColumns(leftCols, rightCols []string, leftAlias, rightAlias string) []string {
	columns := make([]string, 0, len(leftCols)+len(rightCols))

	for _, col := range leftCols {
		columns = append(columns, fmt.Sprintf("%s.%s", leftAlias, col))
	}
	for _, col := range rightCols {
		columns = append(columns, fmt.Sprintf("%s.%s", rightAlias, col))
	}

	return columns
}

// mergeRows combines two rows with aliases.
func mergeRows(leftRow, rightRow adapter.Row, leftAlias, rightAlias string) adapter.Row {
	merged := make(adapter.Row)

	for k, v := range leftRow {
		merged[fmt.Sprintf("%s.%s", leftAlias, k)] = v
	}
	for k, v := range rightRow {
		merged[fmt.Sprintf("%s.%s", rightAlias, k)] = v
	}

	return merged
}

// innerJoin performs an inner join between left rows and right index.
func innerJoin(leftRows []adapter.Row, rightIndex map[interface{}][]adapter.Row, leftKey, rightKey, leftAlias, rightAlias string) []adapter.Row {
	var result []adapter.Row

	for _, leftRow := range leftRows {
		leftValue := getColumnValue(leftRow, leftKey)
		if leftValue == nil {
			continue
		}

		rightRows, found := rightIndex[leftValue]
		if !found {
			continue
		}

		for _, rightRow := range rightRows {
			result = append(result, mergeRows(leftRow, rightRow, leftAlias, rightAlias))
		}
	}

	return result
}

// leftJoin performs a left join, including unmatched left rows.
func leftJoin(leftRows []adapter.Row, rightIndex map[interface{}][]adapter.Row, rightCols []string, leftKey, rightKey, leftAlias, rightAlias string) []adapter.Row {
	var result []adapter.Row

	// Create null right row for unmatched
	nullRightRow := make(adapter.Row)
	for _, col := range rightCols {
		nullRightRow[col] = nil
	}

	for _, leftRow := range leftRows {
		leftValue := getColumnValue(leftRow, leftKey)

		rightRows, found := rightIndex[leftValue]
		if !found || leftValue == nil {
			// No match - include with nulls
			result = append(result, mergeRows(leftRow, nullRightRow, leftAlias, rightAlias))
			continue
		}

		for _, rightRow := range rightRows {
			result = append(result, mergeRows(leftRow, rightRow, leftAlias, rightAlias))
		}
	}

	return result
}
