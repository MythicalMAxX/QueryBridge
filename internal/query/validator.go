// Package query provides query validation and safety checks.
package query

import (
	"errors"
	"regexp"
	"strings"
)

// Common errors for query validation.
var (
	ErrWriteNotAllowed = errors.New("write operations are not allowed in read-only mode")
	ErrDropNotAllowed  = errors.New("DROP operations are not allowed")
	ErrTruncateNotAllowed = errors.New("TRUNCATE operations are not allowed")
)

// QueryType represents the type of SQL operation.
type QueryType string

const (
	QueryTypeSelect   QueryType = "SELECT"
	QueryTypeInsert   QueryType = "INSERT"
	QueryTypeUpdate   QueryType = "UPDATE"
	QueryTypeDelete   QueryType = "DELETE"
	QueryTypeDrop     QueryType = "DROP"
	QueryTypeTruncate QueryType = "TRUNCATE"
	QueryTypeCreate   QueryType = "CREATE"
	QueryTypeAlter    QueryType = "ALTER"
	QueryTypeUnknown  QueryType = "UNKNOWN"
)

// Validator provides query validation and safety checks.
type Validator struct {
	readOnly bool
}

// NewValidator creates a new query validator.
func NewValidator(readOnly bool) *Validator {
	return &Validator{readOnly: readOnly}
}

// IsReadOnly returns whether the validator is in read-only mode.
func (v *Validator) IsReadOnly() bool {
	return v.readOnly
}

// ValidateRawQuery validates a raw SQL query string.
// Returns an error if the query is not allowed in the current mode.
func (v *Validator) ValidateRawQuery(query string) error {
	queryType := DetectQueryType(query)
	return v.ValidateQueryType(queryType)
}

// ValidateQueryType validates a query type against the current mode.
func (v *Validator) ValidateQueryType(queryType QueryType) error {
	// Always block dangerous operations
	switch queryType {
	case QueryTypeDrop:
		return ErrDropNotAllowed
	case QueryTypeTruncate:
		return ErrTruncateNotAllowed
	}

	// In read-only mode, only allow SELECT
	if v.readOnly {
		if !IsReadOnlyQueryType(queryType) {
			return ErrWriteNotAllowed
		}
	}

	return nil
}

// IsReadOnlyQueryType returns true if the query type is read-only.
func IsReadOnlyQueryType(queryType QueryType) bool {
	switch queryType {
	case QueryTypeSelect, QueryTypeUnknown:
		return true
	default:
		return false
	}
}

// IsWriteQueryType returns true if the query type modifies data.
func IsWriteQueryType(queryType QueryType) bool {
	switch queryType {
	case QueryTypeInsert, QueryTypeUpdate, QueryTypeDelete,
		QueryTypeDrop, QueryTypeTruncate, QueryTypeCreate, QueryTypeAlter:
		return true
	default:
		return false
	}
}

// DetectQueryType detects the type of SQL query from the query string.
func DetectQueryType(query string) QueryType {
	// Normalize: trim, uppercase, remove leading comments
	normalized := strings.TrimSpace(query)
	normalized = removeLeadingComments(normalized)
	normalized = strings.ToUpper(normalized)

	// Check for each query type pattern
	patterns := []struct {
		prefix    string
		queryType QueryType
	}{
		{"SELECT", QueryTypeSelect},
		{"INSERT", QueryTypeInsert},
		{"UPDATE", QueryTypeUpdate},
		{"DELETE", QueryTypeDelete},
		{"DROP", QueryTypeDrop},
		{"TRUNCATE", QueryTypeTruncate},
		{"CREATE", QueryTypeCreate},
		{"ALTER", QueryTypeAlter},
		{"WITH", QueryTypeSelect}, // CTEs are typically SELECT
	}

	for _, p := range patterns {
		if strings.HasPrefix(normalized, p.prefix) {
			// For WITH clauses, we need to check if it's followed by SELECT
			if p.prefix == "WITH" {
				if containsWriteOperation(normalized) {
					return QueryTypeInsert // WITH ... INSERT/UPDATE/DELETE
				}
			}
			return p.queryType
		}
	}

	return QueryTypeUnknown
}

// removeLeadingComments removes SQL comments from the beginning of a query.
func removeLeadingComments(query string) string {
	// Remove /* */ style comments
	blockCommentRe := regexp.MustCompile(`(?s)^\s*/\*.*?\*/\s*`)
	for blockCommentRe.MatchString(query) {
		query = blockCommentRe.ReplaceAllString(query, "")
	}

	// Remove -- style comments
	lines := strings.Split(query, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			result = append(result, line)
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// containsWriteOperation checks if a query contains write operations.
func containsWriteOperation(query string) bool {
	writeKeywords := []string{"INSERT", "UPDATE", "DELETE", "DROP", "TRUNCATE"}
	for _, keyword := range writeKeywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}
	return false
}
