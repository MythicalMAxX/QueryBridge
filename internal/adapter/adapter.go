// Package adapter defines the database adapter interface and common types
// used across all database implementations.
package adapter

import (
	"context"
	"time"
)

// Adapter is the core interface that all database adapters must implement.
// It provides a unified API for database operations regardless of the underlying database type.
type Adapter interface {
	// Connect establishes a connection to the database.
	// It should create connection pools as appropriate for the database type.
	Connect(ctx context.Context) error

	// Close cleanly shuts down the adapter, closing all connections.
	Close() error

	// DiscoverSchema retrieves the database schema including tables, columns,
	// data types, and relationships.
	DiscoverSchema(ctx context.Context) (*Schema, error)

	// Execute runs a query and returns all results in memory.
	// Use only for small result sets; for large data, use Stream instead.
	Execute(ctx context.Context, query *Query) (*Result, error)

	// Stream returns a channel that yields rows one at a time.
	// The channel is closed when all rows have been sent or an error occurs.
	// The caller must read from both the row channel and error channel.
	Stream(ctx context.Context, query *Query) (RowStream, error)

	// Type returns the database type identifier (postgres, mysql, mongodb).
	Type() string

	// Name returns the unique name of this database connection.
	Name() string

	// Ping checks if the database connection is healthy.
	Ping(ctx context.Context) error
}

// RowStream represents a streaming result set.
type RowStream interface {
	// Next advances to the next row. Returns false when no more rows.
	Next() bool

	// Row returns the current row data.
	Row() Row

	// Err returns any error that occurred during iteration.
	Err() error

	// Close releases resources associated with the stream.
	Close() error
}

// Schema represents the complete schema of a database.
type Schema struct {
	// Database is the database/schema name
	Database string `json:"database"`

	// Type is the database type (postgres, mysql, mongodb)
	Type string `json:"type"`

	// Tables contains the schema for SQL tables
	Tables map[string]*Table `json:"tables,omitempty"`

	// Collections contains the schema for MongoDB collections
	Collections map[string]*Collection `json:"collections,omitempty"`

	// DiscoveredAt is when this schema was discovered
	DiscoveredAt time.Time `json:"discovered_at"`
}

// Table represents a SQL table schema.
type Table struct {
	Name        string       `json:"name"`
	Schema      string       `json:"schema,omitempty"` // PostgreSQL schema (e.g., "public")
	Description string       `json:"description,omitempty"`
	Columns     []Column     `json:"columns"`
	PrimaryKey  []string     `json:"primary_key,omitempty"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
	Indexes     []Index      `json:"indexes,omitempty"`
	RowCount    int64        `json:"row_count,omitempty"` // Approximate row count
}

// Column represents a table column.
type Column struct {
	Name         string `json:"name"`
	Type         string `json:"type"`          // Native database type
	Description  string `json:"description,omitempty"`
	NormalizedType string `json:"normalized_type"` // Normalized type (string, int, float, bool, datetime, json)
	Nullable     bool   `json:"nullable"`
	DefaultValue string `json:"default_value,omitempty"`
	IsPrimaryKey bool   `json:"is_primary_key,omitempty"`
	IsForeignKey bool   `json:"is_foreign_key,omitempty"`
	Denied       bool   `json:"denied"` // True if field is in denylist
}

// ForeignKey represents a foreign key relationship.
type ForeignKey struct {
	Name            string   `json:"name"`
	Columns         []string `json:"columns"`
	ReferencedTable string   `json:"referenced_table"`
	ReferencedColumns []string `json:"referenced_columns"`
	OnDelete        string   `json:"on_delete,omitempty"`
	OnUpdate        string   `json:"on_update,omitempty"`
}

// Index represents a table index.
type Index struct {
	Name     string   `json:"name"`
	Columns  []string `json:"columns"`
	IsUnique bool     `json:"is_unique"`
	Type     string   `json:"type,omitempty"` // btree, hash, gin, etc.
}

// Collection represents a MongoDB collection schema (inferred).
type Collection struct {
	Name       string  `json:"name"`
	Fields     []Field `json:"fields"`
	SampleSize int     `json:"sample_size"` // Number of documents sampled
	DocCount   int64   `json:"doc_count,omitempty"`
}

// Field represents a MongoDB document field (inferred).
type Field struct {
	Name       string  `json:"name"`
	Types      []string `json:"types"`      // Multiple types possible in MongoDB
	Nullable   bool    `json:"nullable"`
	IsArray    bool    `json:"is_array"`
	NestedFields []Field `json:"nested_fields,omitempty"` // For embedded documents
	Denied     bool    `json:"denied"`
}

// Query represents a database query request.
type Query struct {
	// Type is the query type: select, aggregate, count
	Type string `json:"type"`

	// Table is the target table or collection name
	Table string `json:"table"`

	// Columns lists the columns to select (empty = all non-denied columns)
	Columns []string `json:"columns,omitempty"`

	// Filters contains WHERE conditions
	Filters []Filter `json:"filters,omitempty"`

	// Joins specifies table joins (SQL only)
	Joins []Join `json:"joins,omitempty"`

	// OrderBy specifies sorting
	OrderBy []OrderBy `json:"order_by,omitempty"`

	// GroupBy specifies grouping columns
	GroupBy []string `json:"group_by,omitempty"`

	// Having contains HAVING conditions (for aggregates)
	Having []Filter `json:"having,omitempty"`

	// Limit is the maximum number of rows to return
	Limit int `json:"limit,omitempty"`

	// Offset is the number of rows to skip (for offset pagination)
	Offset int `json:"offset,omitempty"`

	// Cursor is the cursor value for cursor-based pagination
	Cursor string `json:"cursor,omitempty"`

	// CursorColumn is the column used for cursor pagination
	CursorColumn string `json:"cursor_column,omitempty"`

	// Aggregations specifies aggregate functions
	Aggregations []Aggregation `json:"aggregations,omitempty"`

	// Raw is a raw SQL/MongoDB query (use with caution, validated against schema)
	Raw string `json:"raw,omitempty"`

	// Parameters for parameterized queries
	Parameters []interface{} `json:"parameters,omitempty"`
}

// Filter represents a WHERE condition.
type Filter struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"` // eq, ne, gt, gte, lt, lte, like, in, not_in, is_null, is_not_null
	Value    interface{} `json:"value"`
	Logic    string      `json:"logic,omitempty"` // and, or (for combining multiple filters)
}

// Join represents a table join.
type Join struct {
	Type       string   `json:"type"` // inner, left, right, full
	Table      string   `json:"table"`
	Alias      string   `json:"alias,omitempty"`
	Conditions []Filter `json:"conditions"`
}

// OrderBy represents a sort specification.
type OrderBy struct {
	Column    string `json:"column"`
	Direction string `json:"direction"` // asc, desc
}

// Aggregation represents an aggregate function.
type Aggregation struct {
	Function string `json:"function"` // count, sum, avg, min, max
	Column   string `json:"column"`
	Alias    string `json:"alias"`
}

// Result represents a query result set.
type Result struct {
	// Columns lists the column names in order
	Columns []string `json:"columns"`

	// Rows contains the result data
	Rows []Row `json:"rows"`

	// RowCount is the number of rows returned
	RowCount int `json:"row_count"`

	// TotalCount is the total number of matching rows (before limit)
	TotalCount int64 `json:"total_count,omitempty"`

	// NextCursor is the cursor for the next page (cursor pagination)
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more rows available
	HasMore bool `json:"has_more"`

	// ExecutionTime is the query execution time
	ExecutionTime time.Duration `json:"execution_time"`
}

// Row represents a single row of data.
type Row map[string]interface{}

// QueryValidationError represents a validation error for a query.
type QueryValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *QueryValidationError) Error() string {
	return e.Message
}
