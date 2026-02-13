// Package adapter provides SQLite database adapter implementation.
package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteAdapter implements the Adapter interface for SQLite.
type SQLiteAdapter struct {
	name   string
	config SQLiteConfig
	db     *sql.DB
}

// SQLiteConfig holds SQLite connection configuration.
type SQLiteConfig struct {
	// Path to the SQLite database file (use ":memory:" for in-memory)
	Path string
	// MaxConnections for connection pool (default: 1 for SQLite)
	MaxConnections int
}

// NewSQLiteAdapter creates a new SQLite adapter.
func NewSQLiteAdapter(name string, config SQLiteConfig) *SQLiteAdapter {
	if config.MaxConnections == 0 {
		config.MaxConnections = 1 // SQLite works best with single connection
	}
	return &SQLiteAdapter{
		name:   name,
		config: config,
	}
}

// Connect opens the SQLite database.
func (a *SQLiteAdapter) Connect(ctx context.Context) error {
	db, err := sql.Open("sqlite3", a.config.Path)
	if err != nil {
		return fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite works best with limited connections
	db.SetMaxOpenConns(a.config.MaxConnections)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("pinging sqlite: %w", err)
	}

	a.db = db
	return nil
}

// Close closes the database connection.
func (a *SQLiteAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *SQLiteAdapter) Type() string {
	return "sqlite"
}

// Name returns the unique name of this adapter.
func (a *SQLiteAdapter) Name() string {
	return a.name
}

// Ping checks if the database is reachable.
func (a *SQLiteAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// DiscoverSchema retrieves the complete database schema.
func (a *SQLiteAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.name,
		Type:         "sqlite",
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

		pk, err := a.discoverPrimaryKey(ctx, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering primary key for %s: %w", table.Name, err)
		}
		table.PrimaryKey = pk

		indexes, err := a.discoverIndexes(ctx, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering indexes for %s: %w", table.Name, err)
		}
		table.Indexes = indexes

		rowCount, _ := a.estimateRowCount(ctx, table.Name)
		table.RowCount = rowCount

		// Mark PK columns
		pkSet := make(map[string]bool)
		for _, col := range pk {
			pkSet[col] = true
		}
		for i := range table.Columns {
			if pkSet[table.Columns[i].Name] {
				table.Columns[i].IsPrimaryKey = true
			}
		}

		schema.Tables[table.Name] = table
	}

	return schema, nil
}

func (a *SQLiteAdapter) discoverTables(ctx context.Context) ([]*Table, error) {
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	rows, err := a.db.QueryContext(ctx, query)
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

func (a *SQLiteAdapter) discoverColumns(ctx context.Context, tableName string) ([]Column, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(tableName))
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultVal sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, Column{
			Name:           name,
			Type:           dataType,
			NormalizedType: normalizeSQLiteType(dataType),
			Nullable:       notNull == 0,
			DefaultValue:   defaultVal.String,
			IsPrimaryKey:   pk > 0,
		})
	}
	return columns, rows.Err()
}

func (a *SQLiteAdapter) discoverPrimaryKey(ctx context.Context, tableName string) ([]string, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(tableName))
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkColumns []string
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultVal sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pkColumns = append(pkColumns, name)
		}
	}
	return pkColumns, rows.Err()
}

func (a *SQLiteAdapter) discoverIndexes(ctx context.Context, tableName string) ([]Index, error) {
	query := fmt.Sprintf("PRAGMA index_list(%s)", quoteSQLiteIdentifier(tableName))
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []Index
	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}

		// Get index columns
		colQuery := fmt.Sprintf("PRAGMA index_info(%s)", quoteSQLiteIdentifier(name))
		colRows, err := a.db.QueryContext(ctx, colQuery)
		if err != nil {
			continue
		}

		var cols []string
		for colRows.Next() {
			var seqno, cid int
			var colName string
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				continue
			}
			cols = append(cols, colName)
		}
		colRows.Close()

		indexes = append(indexes, Index{
			Name:     name,
			Columns:  cols,
			IsUnique: unique == 1,
			Type:     "btree",
		})
	}
	return indexes, rows.Err()
}

func (a *SQLiteAdapter) estimateRowCount(ctx context.Context, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteSQLiteIdentifier(tableName))
	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// Execute runs a query and returns all results.
func (a *SQLiteAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
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
			row[col] = convertSQLiteValue(values[i])
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &Result{
		Columns:       cols,
		Rows:          resultRows,
		RowCount:      len(resultRows),
		ExecutionTime: time.Since(start),
	}, nil
}

// Stream returns a streaming result set.
func (a *SQLiteAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	sql, args, err := a.buildQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}

	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}

	return &sqliteRowStream{
		rows:    rows,
		columns: cols,
	}, nil
}

// buildQuery constructs a SQL query from the Query struct.
func (a *SQLiteAdapter) buildQuery(q *Query) (string, []interface{}, error) {
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
			sb.WriteString(quoteSQLiteIdentifier(col))
		}
	}

	// Aggregations
	for i, agg := range q.Aggregations {
		if i > 0 || len(q.Columns) > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s(%s) AS %s",
			strings.ToUpper(agg.Function),
			quoteSQLiteIdentifier(agg.Column),
			quoteSQLiteIdentifier(agg.Alias),
		))
	}

	// FROM clause
	sb.WriteString(" FROM ")
	sb.WriteString(quoteSQLiteIdentifier(q.Table))

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
			clause := buildSQLiteFilterClause(filter)
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
			sb.WriteString(quoteSQLiteIdentifier(col))
		}
	}

	// ORDER BY clause
	if len(q.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, ob := range q.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteSQLiteIdentifier(ob.Column))
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

func buildSQLiteFilterClause(filter Filter) string {
	col := quoteSQLiteIdentifier(filter.Column)
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

func quoteSQLiteIdentifier(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

func normalizeSQLiteType(dataType string) string {
	dataType = strings.ToUpper(dataType)
	switch {
	case strings.Contains(dataType, "INT"):
		return "int"
	case strings.Contains(dataType, "REAL"), strings.Contains(dataType, "FLOAT"),
		strings.Contains(dataType, "DOUBLE"), strings.Contains(dataType, "NUMERIC"):
		return "float"
	case strings.Contains(dataType, "TEXT"), strings.Contains(dataType, "CHAR"),
		strings.Contains(dataType, "CLOB"):
		return "string"
	case strings.Contains(dataType, "BLOB"):
		return "binary"
	case strings.Contains(dataType, "DATE"), strings.Contains(dataType, "TIME"):
		return "datetime"
	default:
		return "string" // SQLite uses dynamic typing
	}
}

func convertSQLiteValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case nil:
		return nil
	default:
		return val
	}
}

// sqliteRowStream implements RowStream for SQLite.
type sqliteRowStream struct {
	rows    *sql.Rows
	columns []string
	current Row
	err     error
}

func (s *sqliteRowStream) Next() bool {
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
		s.current[col] = convertSQLiteValue(values[i])
	}
	return true
}

func (s *sqliteRowStream) Row() Row {
	return s.current
}

func (s *sqliteRowStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}

func (s *sqliteRowStream) Close() error {
	return s.rows.Close()
}
