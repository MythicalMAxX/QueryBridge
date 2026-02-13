// Package schema provides relationship graph for cross-table/collection mapping.
package schema

import (
	"sync"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
)

// RelationshipGraph maintains a graph of relationships between tables/collections.
type RelationshipGraph struct {
	mu    sync.RWMutex
	edges map[string][]Relationship // keyed by "database.table"
}

// Relationship represents a relationship between two entities.
type Relationship struct {
	// Type is the relationship type: foreign_key, reference, inferred
	Type string `json:"type"`

	// FromDatabase is the source database
	FromDatabase string `json:"from_database"`

	// FromTable is the source table/collection
	FromTable string `json:"from_table"`

	// FromColumns are the source columns/fields
	FromColumns []string `json:"from_columns"`

	// ToDatabase is the target database
	ToDatabase string `json:"to_database"`

	// ToTable is the target table/collection
	ToTable string `json:"to_table"`

	// ToColumns are the target columns/fields
	ToColumns []string `json:"to_columns"`

	// Cardinality is the relationship cardinality: one-to-one, one-to-many, many-to-many
	Cardinality string `json:"cardinality,omitempty"`
}

// NewRelationshipGraph creates a new relationship graph.
func NewRelationshipGraph() *RelationshipGraph {
	return &RelationshipGraph{
		edges: make(map[string][]Relationship),
	}
}

// AddSchema extracts and adds relationships from a schema.
func (g *RelationshipGraph) AddSchema(schema *adapter.Schema) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Extract relationships from SQL foreign keys
	for _, table := range schema.Tables {
		key := schema.Database + "." + table.Name

		for _, fk := range table.ForeignKeys {
			rel := Relationship{
				Type:         "foreign_key",
				FromDatabase: schema.Database,
				FromTable:    table.Name,
				FromColumns:  fk.Columns,
				ToDatabase:   schema.Database, // Assume same database for FK
				ToTable:      fk.ReferencedTable,
				ToColumns:    fk.ReferencedColumns,
				Cardinality:  "many-to-one",
			}
			g.edges[key] = append(g.edges[key], rel)

			// Add reverse relationship
			reverseKey := schema.Database + "." + fk.ReferencedTable
			reverseRel := Relationship{
				Type:         "foreign_key",
				FromDatabase: schema.Database,
				FromTable:    fk.ReferencedTable,
				FromColumns:  fk.ReferencedColumns,
				ToDatabase:   schema.Database,
				ToTable:      table.Name,
				ToColumns:    fk.Columns,
				Cardinality:  "one-to-many",
			}
			g.edges[reverseKey] = append(g.edges[reverseKey], reverseRel)
		}
	}

	// Infer relationships from MongoDB field patterns
	for _, coll := range schema.Collections {
		key := schema.Database + "." + coll.Name

		for _, field := range coll.Fields {
			// Look for common reference patterns like *_id, *Id, *_ref
			if isReferenceField(field.Name) {
				// Try to infer the referenced collection
				refColl := inferReferencedCollection(field.Name)
				if refColl != "" {
					rel := Relationship{
						Type:         "inferred",
						FromDatabase: schema.Database,
						FromTable:    coll.Name,
						FromColumns:  []string{field.Name},
						ToDatabase:   schema.Database,
						ToTable:      refColl,
						ToColumns:    []string{"_id"},
						Cardinality:  "many-to-one",
					}
					g.edges[key] = append(g.edges[key], rel)
				}
			}
		}
	}
}

// GetRelationships returns all relationships for a table/collection.
func (g *RelationshipGraph) GetRelationships(database, table string) []Relationship {
	g.mu.RLock()
	defer g.mu.RUnlock()

	key := database + "." + table
	return g.edges[key]
}

// FindPath finds a relationship path between two entities.
// Returns nil if no path exists.
func (g *RelationshipGraph) FindPath(fromDB, fromTable, toDB, toTable string) []Relationship {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// BFS to find shortest path
	type node struct {
		db    string
		table string
		path  []Relationship
	}

	visited := make(map[string]bool)
	queue := []node{{db: fromDB, table: fromTable, path: nil}}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		key := curr.db + "." + curr.table
		if visited[key] {
			continue
		}
		visited[key] = true

		if curr.db == toDB && curr.table == toTable {
			return curr.path
		}

		for _, rel := range g.edges[key] {
			newPath := append([]Relationship{}, curr.path...)
			newPath = append(newPath, rel)
			queue = append(queue, node{
				db:    rel.ToDatabase,
				table: rel.ToTable,
				path:  newPath,
			})
		}
	}

	return nil
}

// AddManualRelationship adds a manually defined relationship.
func (g *RelationshipGraph) AddManualRelationship(rel Relationship) {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := rel.FromDatabase + "." + rel.FromTable
	g.edges[key] = append(g.edges[key], rel)
}

// GetAllRelationships returns all relationships in the graph.
func (g *RelationshipGraph) GetAllRelationships() map[string][]Relationship {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string][]Relationship, len(g.edges))
	for k, v := range g.edges {
		result[k] = v
	}
	return result
}

// isReferenceField checks if a field name looks like a reference.
func isReferenceField(name string) bool {
	// Common patterns: user_id, userId, user_ref, userRef
	suffixes := []string{"_id", "Id", "_ref", "Ref", "_ID", "_oid"}
	for _, suffix := range suffixes {
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

// inferReferencedCollection tries to infer the collection name from a reference field.
func inferReferencedCollection(fieldName string) string {
	// Strip common suffixes and pluralize
	suffixes := []string{"_id", "Id", "_ref", "Ref", "_ID", "_oid"}
	for _, suffix := range suffixes {
		if len(fieldName) > len(suffix) && fieldName[len(fieldName)-len(suffix):] == suffix {
			base := fieldName[:len(fieldName)-len(suffix)]
			// Simple pluralization (could be enhanced)
			return base + "s"
		}
	}
	return ""
}
