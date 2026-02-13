// Package adapter provides ClickHouse database adapter implementation.
package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

// ClickHouseAdapter implements the Adapter interface for ClickHouse.
type ClickHouseAdapter struct {
	name   string
	config ClickHouseConfig
	db     *sql.DB
}

// ClickHouseConfig holds ClickHouse connection configuration.
type ClickHouseConfig struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	MaxConnections int
}

// NewClickHouseAdapter creates a new ClickHouse adapter.
func NewClickHouseAdapter(name string, config ClickHouseConfig) *ClickHouseAdapter {
	if config.Port == 0 {
		config.Port = 9000
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 10
	}
	return &ClickHouseAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection pool to ClickHouse.
func (a *ClickHouseAdapter) Connect(ctx context.Context) error {
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s:%d/%s",
		a.config.User, a.config.Password, a.config.Host, a.config.Port, a.config.Database)

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("opening clickhouse connection: %w", err)
	}

	db.SetMaxOpenConns(a.config.MaxConnections)
	db.SetMaxIdleConns(a.config.MaxConnections / 2)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("pinging clickhouse: %w", err)
	}

	a.db = db
	return nil
}

// Close closes the database connection pool.
func (a *ClickHouseAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *ClickHouseAdapter) Type() string {
	return "clickhouse"
}

// Name returns the unique name of this adapter.
func (a *ClickHouseAdapter) Name() string {
	return a.name
}

// Ping checks if the database is reachable.
func (a *ClickHouseAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// DiscoverSchema retrieves the complete database schema.
func (a *ClickHouseAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "clickhouse",
		Tables:       make(map[string]*Table),
		DiscoveredAt: time.Now(),
	}

	tables, err := a.discoverTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering tables: %w", err)
	}

	for _, table := range tables {
		columns, err := a.discoverColumns(ctx, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering columns for %s: %w", table.Name, err)
		}
		table.Columns = columns

		rowCount, _ := a.estimateRowCount(ctx, table.Name)
		table.RowCount = rowCount

		schema.Tables[table.Name] = table
	}

	return schema, nil
}

func (a *ClickHouseAdapter) discoverTables(ctx context.Context) ([]*Table, error) {
	query := `SELECT name FROM system.tables WHERE database = ?`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []*Table
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, &Table{Name: name})
	}
	return tables, rows.Err()
}

func (a *ClickHouseAdapter) discoverColumns(ctx context.Context, tableName string) ([]Column, error) {
	query := `SELECT name, type, default_kind FROM system.columns WHERE database = ? AND table = ? ORDER BY position`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var name, dataType, defaultKind string
		if err := rows.Scan(&name, &dataType, &defaultKind); err != nil {
			return nil, err
		}
		columns = append(columns, Column{
			Name:           name,
			Type:           dataType,
			NormalizedType: normalizeClickHouseType(dataType),
			Nullable:       strings.HasPrefix(dataType, "Nullable"),
		})
	}
	return columns, rows.Err()
}

func (a *ClickHouseAdapter) estimateRowCount(ctx context.Context, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT count() FROM `%s`", tableName)
	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// Execute runs a query and returns all results.
func (a *ClickHouseAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	sql, args, err := a.buildQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var resultRows []Row
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(Row)
		for i, col := range cols {
			row[col] = convertClickHouseValue(values[i])
		}
		resultRows = append(resultRows, row)
	}

	return &Result{
		Columns:       cols,
		Rows:          resultRows,
		RowCount:      len(resultRows),
		ExecutionTime: time.Since(start),
	}, nil
}

// Stream returns a streaming result set.
func (a *ClickHouseAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	sq, args, err := a.buildQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx, sq, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}

	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}

	return &clickhouseRowStream{rows: rows, columns: cols}, nil
}

func (a *ClickHouseAdapter) buildQuery(q *Query) (string, []interface{}, error) {
	if q.Raw != "" {
		return q.Raw, q.Parameters, nil
	}

	var sb strings.Builder
	var args []interface{}

	// SELECT clause
	sb.WriteString("SELECT ")
	if len(q.Columns) == 0 {
		sb.WriteString("*")
	} else {
		for i, col := range q.Columns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteClickHouseIdentifier(col))
		}
	}

	// Aggregations
	for i, agg := range q.Aggregations {
		if i > 0 || len(q.Columns) > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s(%s) AS %s",
			strings.ToLower(agg.Function),
			quoteClickHouseIdentifier(agg.Column),
			quoteClickHouseIdentifier(agg.Alias),
		))
	}

	// FROM clause
	sb.WriteString(" FROM ")
	sb.WriteString(quoteClickHouseIdentifier(q.Table))

	// WHERE clause
	if len(q.Filters) > 0 {
		sb.WriteString(" WHERE ")
		for i, filter := range q.Filters {
			if i > 0 {
				logic := filter.Logic
				if logic == "" {
					logic = "AND"
				}
				sb.WriteString(fmt.Sprintf(" %s ", strings.ToUpper(logic)))
			}
			clause := buildClickHouseFilterClause(filter)
			sb.WriteString(clause)
			if filter.Operator != "is_null" && filter.Operator != "is_not_null" {
				args = append(args, filter.Value)
			}
		}
	}

	// GROUP BY clause
	if len(q.GroupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		for i, col := range q.GroupBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteClickHouseIdentifier(col))
		}
	}

	// ORDER BY clause
	if len(q.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, ob := range q.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteClickHouseIdentifier(ob.Column))
			if strings.ToUpper(ob.Direction) == "DESC" {
				sb.WriteString(" DESC")
			} else {
				sb.WriteString(" ASC")
			}
		}
	}

	// LIMIT and OFFSET
	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}
	if q.Offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", q.Offset))
	}

	return sb.String(), args, nil
}

func buildClickHouseFilterClause(filter Filter) string {
	col := quoteClickHouseIdentifier(filter.Column)
	switch filter.Operator {
	case "eq", "=", "":
		return fmt.Sprintf("%s = ?", col)
	case "ne", "!=", "<>":
		return fmt.Sprintf("%s != ?", col)
	case "gt", ">":
		return fmt.Sprintf("%s > ?", col)
	case "gte", ">=":
		return fmt.Sprintf("%s >= ?", col)
	case "lt", "<":
		return fmt.Sprintf("%s < ?", col)
	case "lte", "<=":
		return fmt.Sprintf("%s <= ?", col)
	case "like":
		return fmt.Sprintf("%s LIKE ?", col)
	case "in":
		return fmt.Sprintf("%s IN (?)", col)
	case "is_null":
		return fmt.Sprintf("%s IS NULL", col)
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", col)
	default:
		return fmt.Sprintf("%s = ?", col)
	}
}

func quoteClickHouseIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "\\`") + "`"
}

func normalizeClickHouseType(dataType string) string {
	dataType = strings.ToLower(dataType)
	switch {
	case strings.Contains(dataType, "int"):
		return "int"
	case strings.Contains(dataType, "float"), strings.Contains(dataType, "double"),
		strings.Contains(dataType, "decimal"):
		return "float"
	case strings.Contains(dataType, "string"), strings.Contains(dataType, "fixedstring"),
		strings.Contains(dataType, "uuid"), strings.Contains(dataType, "enum"):
		return "string"
	case strings.Contains(dataType, "bool"):
		return "bool"
	case strings.Contains(dataType, "date"), strings.Contains(dataType, "datetime"):
		return "datetime"
	case strings.Contains(dataType, "array"):
		return "json"
	default:
		return "string"
	}
}

func convertClickHouseValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case nil:
		return nil
	default:
		return val
	}
}

type clickhouseRowStream struct {
	rows    *sql.Rows
	columns []string
	current Row
	err     error
}

func (s *clickhouseRowStream) Next() bool {
	if !s.rows.Next() {
		return false
	}

	values := make([]interface{}, len(s.columns))
	valuePtrs := make([]interface{}, len(s.columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	if err := s.rows.Scan(valuePtrs...); err != nil {
		s.err = err
		return false
	}

	s.current = make(Row)
	for i, col := range s.columns {
		s.current[col] = convertClickHouseValue(values[i])
	}
	return true
}

func (s *clickhouseRowStream) Row() Row    { return s.current }
func (s *clickhouseRowStream) Err() error  { if s.err != nil { return s.err }; return s.rows.Err() }
func (s *clickhouseRowStream) Close() error { return s.rows.Close() }
