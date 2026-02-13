// Package schema provides schema discovery, registration, and field exclusion
// for the MCP server.
package schema

import (
	"context"
	"strings"
	"sync"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/metadata"
)

// Registry maintains an in-memory registry of all database schemas.
// It is thread-safe and supports dynamic schema updates.
type Registry struct {
	mu       sync.RWMutex
	schemas  map[string]*adapter.Schema // keyed by database name
	denylist map[string]bool            // lowercase field names to deny
	graph    *RelationshipGraph
	meta     *metadata.Store
}

// NewRegistry creates a new schema registry with the given denylist and metadata store.
func NewRegistry(denyFields []string, meta *metadata.Store) *Registry {
	denylist := make(map[string]bool, len(denyFields))
	for _, field := range denyFields {
		denylist[strings.ToLower(field)] = true
	}

	return &Registry{
		schemas:  make(map[string]*adapter.Schema),
		denylist: denylist,
		graph:    NewRelationshipGraph(),
		meta:     meta,
	}
}

// Register adds or updates a schema in the registry.
// It automatically applies the denylist and updates the relationship graph.
func (r *Registry) Register(schema *adapter.Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Apply denylist to all fields
	r.applyDenylist(schema)

	// Apply annotations from metadata store
	r.applyAnnotations(schema)

	// Store the schema
	r.schemas[schema.Database] = schema

	// Update relationship graph
	r.graph.AddSchema(schema)
}

func (r *Registry) applyAnnotations(schema *adapter.Schema) {
	if r.meta == nil {
		return
	}

	tableAnns, colAnns, err := r.meta.GetAnnotations(schema.Database)
	if err != nil {
		return
	}

	for name, table := range schema.Tables {
		if desc, ok := tableAnns[name]; ok {
			table.Description = desc
		}
		if cols, ok := colAnns[name]; ok {
			for i := range table.Columns {
				if desc, ok := cols[table.Columns[i].Name]; ok {
					table.Columns[i].Description = desc
				}
			}
		}
	}
}

// Get retrieves a schema by database name.
func (r *Registry) Get(database string) (*adapter.Schema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	return schema, ok
}

// GetAll returns all registered schemas.
func (r *Registry) GetAll() map[string]*adapter.Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*adapter.Schema, len(r.schemas))
	for k, v := range r.schemas {
		result[k] = v
	}
	return result
}

// GetTable retrieves a specific table from a database.
func (r *Registry) GetTable(database, table string) (*adapter.Table, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok || schema.Tables == nil {
		return nil, false
	}

	tbl, ok := schema.Tables[table]
	return tbl, ok
}

// GetCollection retrieves a specific collection from a MongoDB database.
func (r *Registry) GetCollection(database, collection string) (*adapter.Collection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok || schema.Collections == nil {
		return nil, false
	}

	coll, ok := schema.Collections[collection]
	return coll, ok
}

// IsDenied checks if a field name is in the denylist.
func (r *Registry) IsDenied(fieldName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.denylist[strings.ToLower(fieldName)]
}

// GetSafeColumns returns only non-denied columns for a table.
func (r *Registry) GetSafeColumns(database, table string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok {
		return nil, &SchemaNotFoundError{Database: database}
	}

	tbl, ok := schema.Tables[table]
	if !ok {
		return nil, &TableNotFoundError{Database: database, Table: table}
	}

	var safe []string
	for _, col := range tbl.Columns {
		if !col.Denied {
			safe = append(safe, col.Name)
		}
	}
	return safe, nil
}

// GetSafeFields returns only non-denied fields for a MongoDB collection.
func (r *Registry) GetSafeFields(database, collection string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok {
		return nil, &SchemaNotFoundError{Database: database}
	}

	coll, ok := schema.Collections[collection]
	if !ok {
		return nil, &CollectionNotFoundError{Database: database, Collection: collection}
	}

	var safe []string
	for _, field := range coll.Fields {
		if !field.Denied {
			safe = append(safe, field.Name)
		}
	}
	return safe, nil
}

// ValidateQuery checks if a query accesses only allowed tables and columns.
func (r *Registry) ValidateQuery(database string, query *adapter.Query) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok {
		return &SchemaNotFoundError{Database: database}
	}

	// Skip table/collection validation for raw queries
	// Raw SQL queries bypass the schema validation as they may reference
	// arbitrary tables, joins, subqueries, etc.
	if query.Raw != "" {
		return nil
	}

	// Check main table
	if schema.Tables != nil {
		table, ok := schema.Tables[query.Table]
		if !ok {
			return &TableNotFoundError{Database: database, Table: query.Table}
		}

		// Check selected columns
		for _, col := range query.Columns {
			if err := r.validateColumn(table, col); err != nil {
				return err
			}
		}

		// Check filter columns
		for _, filter := range query.Filters {
			if err := r.validateColumn(table, filter.Column); err != nil {
				return err
			}
		}

		// Check order by columns
		for _, ob := range query.OrderBy {
			if err := r.validateColumn(table, ob.Column); err != nil {
				return err
			}
		}
	} else if schema.Collections != nil {
		coll, ok := schema.Collections[query.Table]
		if !ok {
			return &CollectionNotFoundError{Database: database, Collection: query.Table}
		}

		// Check selected columns/fields
		for _, col := range query.Columns {
			if err := r.validateField(coll, col); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateColumn checks if a column exists and is not denied.
func (r *Registry) validateColumn(table *adapter.Table, colName string) error {
	for _, col := range table.Columns {
		if col.Name == colName {
			if col.Denied {
				return &DeniedFieldError{Field: colName}
			}
			return nil
		}
	}
	return &ColumnNotFoundError{Table: table.Name, Column: colName}
}

// validateField checks if a field exists and is not denied (MongoDB).
func (r *Registry) validateField(coll *adapter.Collection, fieldName string) error {
	for _, field := range coll.Fields {
		if field.Name == fieldName {
			if field.Denied {
				return &DeniedFieldError{Field: fieldName}
			}
			return nil
		}
	}
	return &FieldNotFoundError{Collection: coll.Name, Field: fieldName}
}

// applyDenylist marks denied fields in the schema.
func (r *Registry) applyDenylist(schema *adapter.Schema) {
	// Apply to SQL tables
	for _, table := range schema.Tables {
		for i := range table.Columns {
			if r.denylist[strings.ToLower(table.Columns[i].Name)] {
				table.Columns[i].Denied = true
			}
		}
	}

	// Apply to MongoDB collections
	for _, coll := range schema.Collections {
		r.applyDenylistToFields(coll.Fields)
	}
}

// applyDenylistToFields recursively marks denied fields.
func (r *Registry) applyDenylistToFields(fields []adapter.Field) {
	for i := range fields {
		if r.denylist[strings.ToLower(fields[i].Name)] {
			fields[i].Denied = true
		}
		if len(fields[i].NestedFields) > 0 {
			r.applyDenylistToFields(fields[i].NestedFields)
		}
	}
}

// AddDenyField adds a new field to the denylist and updates all schemas.
func (r *Registry) AddDenyField(field string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	field = strings.ToLower(field)
	if r.denylist[field] {
		return // Already denied
	}

	r.denylist[field] = true

	// Re-apply denylist to all schemas
	for _, schema := range r.schemas {
		r.applyDenylist(schema)
	}
}

// GetRelationships returns relationships for a table.
func (r *Registry) GetRelationships(database, table string) []Relationship {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.graph.GetRelationships(database, table)
}

// GetSafeSchema returns a copy of the schema with denied fields removed entirely.
// This is what gets exposed to the AI tool layer.
func (r *Registry) GetSafeSchema(database string) (*adapter.Schema, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[database]
	if !ok {
		return nil, &SchemaNotFoundError{Database: database}
	}

	// Create a deep copy with denied fields removed
	safe := &adapter.Schema{
		Database:     schema.Database,
		Type:         schema.Type,
		DiscoveredAt: schema.DiscoveredAt,
	}

	if schema.Tables != nil {
		safe.Tables = make(map[string]*adapter.Table)
		for name, table := range schema.Tables {
			safeTable := &adapter.Table{
				Name:        table.Name,
				Schema:      table.Schema,
				PrimaryKey:  table.PrimaryKey,
				ForeignKeys: table.ForeignKeys,
				Indexes:     table.Indexes,
				RowCount:    table.RowCount,
			}
			for _, col := range table.Columns {
				if !col.Denied {
					safeTable.Columns = append(safeTable.Columns, col)
				}
			}
			safe.Tables[name] = safeTable
		}
	}

	if schema.Collections != nil {
		safe.Collections = make(map[string]*adapter.Collection)
		for name, coll := range schema.Collections {
			safeColl := &adapter.Collection{
				Name:       coll.Name,
				SampleSize: coll.SampleSize,
				DocCount:   coll.DocCount,
			}
			safeColl.Fields = r.filterDeniedFields(coll.Fields)
			safe.Collections[name] = safeColl
		}
	}

	return safe, nil
}

func (r *Registry) filterDeniedFields(fields []adapter.Field) []adapter.Field {
	var safe []adapter.Field
	for _, f := range fields {
		if !f.Denied {
			safeCopy := f
			if len(f.NestedFields) > 0 {
				safeCopy.NestedFields = r.filterDeniedFields(f.NestedFields)
			}
			safe = append(safe, safeCopy)
		}
	}
	return safe
}

// Refresh re-discovers schemas for all registered adapters.
func (r *Registry) Refresh(ctx context.Context, adapters []adapter.Adapter) error {
	for _, a := range adapters {
		schema, err := a.DiscoverSchema(ctx)
		if err != nil {
			return err
		}
		r.Register(schema)
	}
	return nil
}

// Error types for schema operations

type SchemaNotFoundError struct {
	Database string
}

func (e *SchemaNotFoundError) Error() string {
	return "schema not found for database: " + e.Database
}

type TableNotFoundError struct {
	Database string
	Table    string
}

func (e *TableNotFoundError) Error() string {
	return "table '" + e.Table + "' not found in database: " + e.Database
}

type CollectionNotFoundError struct {
	Database   string
	Collection string
}

func (e *CollectionNotFoundError) Error() string {
	return "collection '" + e.Collection + "' not found in database: " + e.Database
}

type ColumnNotFoundError struct {
	Table  string
	Column string
}

func (e *ColumnNotFoundError) Error() string {
	return "column '" + e.Column + "' not found in table: " + e.Table
}

type FieldNotFoundError struct {
	Collection string
	Field      string
}

func (e *FieldNotFoundError) Error() string {
	return "field '" + e.Field + "' not found in collection: " + e.Collection
}

type DeniedFieldError struct {
	Field string
}

func (e *DeniedFieldError) Error() string {
	return "access to field '" + e.Field + "' is denied"
}
