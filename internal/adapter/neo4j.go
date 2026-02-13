// Package adapter provides Neo4j graph database adapter implementation.
package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jAdapter implements the Adapter interface for Neo4j.
type Neo4jAdapter struct {
	name   string
	config Neo4jConfig
	driver neo4j.DriverWithContext
}

// Neo4jConfig holds Neo4j connection configuration.
type Neo4jConfig struct {
	URI      string
	User     string
	Password string
	Database string
}

// NewNeo4jAdapter creates a new Neo4j adapter.
func NewNeo4jAdapter(name string, config Neo4jConfig) *Neo4jAdapter {
	if config.URI == "" {
		config.URI = "bolt://localhost:7687"
	}
	if config.Database == "" {
		config.Database = "neo4j"
	}
	return &Neo4jAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection to Neo4j.
func (a *Neo4jAdapter) Connect(ctx context.Context) error {
	driver, err := neo4j.NewDriverWithContext(a.config.URI, neo4j.BasicAuth(a.config.User, a.config.Password, ""))
	if err != nil {
		return fmt.Errorf("creating neo4j driver: %w", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("verifying neo4j connectivity: %w", err)
	}

	a.driver = driver
	return nil
}

// Close closes the Neo4j driver.
func (a *Neo4jAdapter) Close() error {
	if a.driver != nil {
		return a.driver.Close(context.Background())
	}
	return nil
}

// Type returns the database type identifier.
func (a *Neo4jAdapter) Type() string { return "neo4j" }

// Name returns the unique name of this adapter.
func (a *Neo4jAdapter) Name() string { return a.name }

// Ping checks if Neo4j is reachable.
func (a *Neo4jAdapter) Ping(ctx context.Context) error {
	return a.driver.VerifyConnectivity(ctx)
}

// DiscoverSchema retrieves labels and relationship types.
func (a *Neo4jAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "neo4j",
		Collections:  make(map[string]*Collection),
		DiscoveredAt: time.Now(),
	}

	session := a.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: a.config.Database})
	defer session.Close(ctx)

	// Get node labels
	result, err := session.Run(ctx, "CALL db.labels()", nil)
	if err == nil {
		for result.Next(ctx) {
			label := result.Record().Values[0].(string)
			// Count nodes with this label
			countResult, _ := session.Run(ctx, fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) as cnt", label), nil)
			var count int64
			if countResult.Next(ctx) {
				count, _ = countResult.Record().Values[0].(int64)
			}
			schema.Collections[label] = &Collection{
				Name:     label,
				DocCount: count,
				Fields:   []Field{{Name: "id", Types: []string{"integer"}}},
			}
		}
	}

	return schema, nil
}

// Execute runs a Cypher query.
func (a *Neo4jAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	session := a.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: a.config.Database})
	defer session.Close(ctx)

	cypher := a.buildQuery(query)
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, fmt.Errorf("executing cypher: %w", err)
	}

	var rows []Row
	var columns []string
	colSet := make(map[string]bool)

	for result.Next(ctx) {
		record := result.Record()
		row := make(Row)

		for _, key := range record.Keys {
			val := record.AsMap()[key]
			row[key] = convertNeo4jValue(val)
			if !colSet[key] {
				columns = append(columns, key)
				colSet[key] = true
			}
		}
		rows = append(rows, row)
	}

	return &Result{
		Columns:       columns,
		Rows:          rows,
		RowCount:      len(rows),
		ExecutionTime: time.Since(start),
	}, result.Err()
}

func (a *Neo4jAdapter) buildQuery(q *Query) string {
	if q.Raw != "" {
		return q.Raw
	}

	// Build a simple Cypher query
	var sb strings.Builder
	label := q.Table

	sb.WriteString(fmt.Sprintf("MATCH (n:`%s`) ", label))

	if len(q.Filters) > 0 {
		sb.WriteString("WHERE ")
		for i, f := range q.Filters {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("n.%s = '%v'", f.Column, f.Value))
		}
	}

	sb.WriteString(" RETURN n")

	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}

	return sb.String()
}

func convertNeo4jValue(v interface{}) interface{} {
	switch val := v.(type) {
	case neo4j.Node:
		props := val.Props
		props["_id"] = val.ElementId
		props["_labels"] = val.Labels
		return props
	case neo4j.Relationship:
		props := val.Props
		props["_id"] = val.ElementId
		props["_type"] = val.Type
		return props
	default:
		return val
	}
}

// Stream returns a streaming result set.
func (a *Neo4jAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return &neo4jRowStream{rows: result.Rows, columns: result.Columns, index: -1}, nil
}

type neo4jRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *neo4jRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}
func (s *neo4jRowStream) Row() Row    { return s.current }
func (s *neo4jRowStream) Err() error  { return nil }
func (s *neo4jRowStream) Close() error { return nil }
