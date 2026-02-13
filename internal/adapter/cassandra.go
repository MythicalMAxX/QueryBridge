// Package adapter provides Apache Cassandra database adapter implementation.
package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// CassandraAdapter implements the Adapter interface for Apache Cassandra.
type CassandraAdapter struct {
	name    string
	config  CassandraConfig
	session *gocql.Session
}

// CassandraConfig holds Cassandra connection configuration.
type CassandraConfig struct {
	Hosts    []string
	Port     int
	Keyspace string
	User     string
	Password string
}

// NewCassandraAdapter creates a new Cassandra adapter.
func NewCassandraAdapter(name string, config CassandraConfig) *CassandraAdapter {
	if config.Port == 0 {
		config.Port = 9042
	}
	return &CassandraAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection to Cassandra.
func (a *CassandraAdapter) Connect(ctx context.Context) error {
	cluster := gocql.NewCluster(a.config.Hosts...)
	cluster.Port = a.config.Port
	cluster.Keyspace = a.config.Keyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 10 * time.Second

	if a.config.User != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: a.config.User,
			Password: a.config.Password,
		}
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("creating cassandra session: %w", err)
	}

	a.session = session
	return nil
}

// Close closes the Cassandra session.
func (a *CassandraAdapter) Close() error {
	if a.session != nil {
		a.session.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *CassandraAdapter) Type() string {
	return "cassandra"
}

// Name returns the unique name of this adapter.
func (a *CassandraAdapter) Name() string {
	return a.name
}

// Ping checks if Cassandra is reachable.
func (a *CassandraAdapter) Ping(ctx context.Context) error {
	return a.session.Query("SELECT now() FROM system.local").Exec()
}

// DiscoverSchema retrieves tables from the keyspace.
func (a *CassandraAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Keyspace,
		Type:         "cassandra",
		Tables:       make(map[string]*Table),
		DiscoveredAt: time.Now(),
	}

	// Get tables
	iter := a.session.Query(`SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?`, a.config.Keyspace).Iter()
	var tableName string
	for iter.Scan(&tableName) {
		columns, _ := a.discoverColumns(tableName)
		schema.Tables[tableName] = &Table{
			Name:    tableName,
			Columns: columns,
		}
	}

	return schema, iter.Close()
}

func (a *CassandraAdapter) discoverColumns(tableName string) ([]Column, error) {
	var columns []Column
	iter := a.session.Query(`SELECT column_name, type FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?`,
		a.config.Keyspace, tableName).Iter()

	var name, colType string
	for iter.Scan(&name, &colType) {
		columns = append(columns, Column{
			Name:           name,
			Type:           colType,
			NormalizedType: normalizeCassandraType(colType),
		})
	}
	return columns, iter.Close()
}

// Execute runs a CQL query.
func (a *CassandraAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	cql := a.buildQuery(query)
	iter := a.session.Query(cql).Iter()

	cols := make([]string, len(iter.Columns()))
	for i, col := range iter.Columns() {
		cols[i] = col.Name
	}

	var rows []Row
	for {
		row := make(map[string]interface{})
		if !iter.MapScan(row) {
			break
		}
		rows = append(rows, row)
	}

	return &Result{
		Columns:       cols,
		Rows:          rows,
		RowCount:      len(rows),
		ExecutionTime: time.Since(start),
	}, iter.Close()
}

func (a *CassandraAdapter) buildQuery(q *Query) string {
	if q.Raw != "" {
		return q.Raw
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")

	if len(q.Columns) == 0 {
		sb.WriteString("*")
	} else {
		sb.WriteString(strings.Join(q.Columns, ", "))
	}

	sb.WriteString(" FROM ")
	sb.WriteString(q.Table)

	if len(q.Filters) > 0 {
		sb.WriteString(" WHERE ")
		for i, f := range q.Filters {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("%s = '%v'", f.Column, f.Value))
		}
		sb.WriteString(" ALLOW FILTERING")
	}

	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}

	return sb.String()
}

// Stream returns a streaming result set.
func (a *CassandraAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return &cassandraRowStream{rows: result.Rows, columns: result.Columns, index: -1}, nil
}

func normalizeCassandraType(t string) string {
	t = strings.ToLower(t)
	switch {
	case strings.Contains(t, "int"), strings.Contains(t, "bigint"):
		return "int"
	case strings.Contains(t, "float"), strings.Contains(t, "double"), strings.Contains(t, "decimal"):
		return "float"
	case strings.Contains(t, "text"), strings.Contains(t, "varchar"), strings.Contains(t, "ascii"):
		return "string"
	case strings.Contains(t, "bool"):
		return "bool"
	case strings.Contains(t, "timestamp"), strings.Contains(t, "date"), strings.Contains(t, "time"):
		return "datetime"
	case strings.Contains(t, "uuid"):
		return "string"
	default:
		return "string"
	}
}

type cassandraRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *cassandraRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}
func (s *cassandraRowStream) Row() Row    { return s.current }
func (s *cassandraRowStream) Err() error  { return nil }
func (s *cassandraRowStream) Close() error { return nil }
