// Package adapter provides SQL Server (MSSQL) database adapter implementation.
package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
)

// SQLServerAdapter implements the Adapter interface for SQL Server.
type SQLServerAdapter struct {
	name   string
	config SQLServerConfig
	db     *sql.DB
}

// SQLServerConfig holds SQL Server connection configuration.
type SQLServerConfig struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	MaxConnections int
}

// NewSQLServerAdapter creates a new SQL Server adapter.
func NewSQLServerAdapter(name string, config SQLServerConfig) *SQLServerAdapter {
	if config.Port == 0 {
		config.Port = 1433
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 10
	}
	return &SQLServerAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection pool to SQL Server.
func (a *SQLServerAdapter) Connect(ctx context.Context) error {
	connStr := fmt.Sprintf("server=%s;port=%d;database=%s;user id=%s;password=%s",
		a.config.Host, a.config.Port, a.config.Database, a.config.User, a.config.Password)

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("opening sqlserver connection: %w", err)
	}

	db.SetMaxOpenConns(a.config.MaxConnections)
	db.SetMaxIdleConns(a.config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("pinging sqlserver: %w", err)
	}

	a.db = db
	return nil
}

// Close closes the database connection pool.
func (a *SQLServerAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *SQLServerAdapter) Type() string {
	return "sqlserver"
}

// Name returns the unique name of this adapter.
func (a *SQLServerAdapter) Name() string {
	return a.name
}

// Ping checks if the database is reachable.
func (a *SQLServerAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// DiscoverSchema retrieves the complete database schema.
func (a *SQLServerAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "sqlserver",
		Tables:       make(map[string]*Table),
		DiscoveredAt: time.Now(),
	}

	tables, err := a.discoverTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering tables: %w", err)
	}

	for _, table := range tables {
		columns, err := a.discoverColumns(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering columns for %s: %w", table.Name, err)
		}
		table.Columns = columns

		pk, err := a.discoverPrimaryKey(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering primary key for %s: %w", table.Name, err)
		}
		table.PrimaryKey = pk

		indexes, err := a.discoverIndexes(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering indexes for %s: %w", table.Name, err)
		}
		table.Indexes = indexes

		rowCount, _ := a.estimateRowCount(ctx, table.Schema, table.Name)
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

		key := fmt.Sprintf("%s.%s", table.Schema, table.Name)
		schema.Tables[key] = table
	}

	return schema, nil
}

func (a *SQLServerAdapter) discoverTables(ctx context.Context) ([]*Table, error) {
	query := `
		SELECT TABLE_SCHEMA, TABLE_NAME 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_CATALOG = @db
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`
	rows, err := a.db.QueryContext(ctx, query, sql.Named("db", a.config.Database))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []*Table
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		tables = append(tables, &Table{Name: name, Schema: schema})
	}
	return tables, rows.Err()
}

func (a *SQLServerAdapter) discoverColumns(ctx context.Context, schema, tableName string) ([]Column, error) {
	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = @schema AND TABLE_NAME = @table
		ORDER BY ORDINAL_POSITION
	`
	rows, err := a.db.QueryContext(ctx, query, 
		sql.Named("schema", schema), sql.Named("table", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var name, dataType, nullable string
		var defaultVal sql.NullString
		if err := rows.Scan(&name, &dataType, &nullable, &defaultVal); err != nil {
			return nil, err
		}
		columns = append(columns, Column{
			Name:           name,
			Type:           dataType,
			NormalizedType: normalizeSQLServerType(dataType),
			Nullable:       nullable == "YES",
			DefaultValue:   defaultVal.String,
		})
	}
	return columns, rows.Err()
}

func (a *SQLServerAdapter) discoverPrimaryKey(ctx context.Context, schema, tableName string) ([]string, error) {
	query := `
		SELECT c.COLUMN_NAME
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE c 
			ON tc.CONSTRAINT_NAME = c.CONSTRAINT_NAME
		WHERE tc.TABLE_SCHEMA = @schema AND tc.TABLE_NAME = @table 
			AND tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
	`
	rows, err := a.db.QueryContext(ctx, query,
		sql.Named("schema", schema), sql.Named("table", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkColumns []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		pkColumns = append(pkColumns, col)
	}
	return pkColumns, rows.Err()
}

func (a *SQLServerAdapter) discoverIndexes(ctx context.Context, schema, tableName string) ([]Index, error) {
	query := `
		SELECT i.name, c.name, i.is_unique
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE s.name = @schema AND t.name = @table AND i.name IS NOT NULL
		ORDER BY i.name, ic.key_ordinal
	`
	rows, err := a.db.QueryContext(ctx, query,
		sql.Named("schema", schema), sql.Named("table", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexMap := make(map[string]*Index)
	var indexOrder []string

	for rows.Next() {
		var indexName, colName string
		var isUnique bool
		if err := rows.Scan(&indexName, &colName, &isUnique); err != nil {
			return nil, err
		}

		if idx, ok := indexMap[indexName]; ok {
			idx.Columns = append(idx.Columns, colName)
		} else {
			indexMap[indexName] = &Index{
				Name:     indexName,
				Columns:  []string{colName},
				IsUnique: isUnique,
				Type:     "btree",
			}
			indexOrder = append(indexOrder, indexName)
		}
	}

	var indexes []Index
	for _, name := range indexOrder {
		indexes = append(indexes, *indexMap[name])
	}
	return indexes, rows.Err()
}

func (a *SQLServerAdapter) estimateRowCount(ctx context.Context, schema, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM [%s].[%s]", schema, tableName)
	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// Execute runs a query and returns all results.
func (a *SQLServerAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
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
			row[col] = convertSQLServerValue(values[i])
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
func (a *SQLServerAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
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

	return &sqlserverRowStream{rows: rows, columns: cols}, nil
}

func (a *SQLServerAdapter) buildQuery(q *Query) (string, []interface{}, error) {
	if q.Raw != "" {
		return q.Raw, q.Parameters, nil
	}

	var sb strings.Builder
	var args []interface{}

	// SELECT with TOP for limit
	sb.WriteString("SELECT ")
	if q.Limit > 0 && len(q.OrderBy) == 0 {
		sb.WriteString(fmt.Sprintf("TOP %d ", q.Limit))
	}

	if len(q.Columns) == 0 {
		sb.WriteString("*")
	} else {
		for i, col := range q.Columns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteSQLServerIdentifier(col))
		}
	}

	// FROM clause
	sb.WriteString(" FROM ")
	sb.WriteString(quoteSQLServerIdentifier(q.Table))

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
			clause := buildSQLServerFilterClause(filter, i+1)
			sb.WriteString(clause)
			if filter.Operator != "is_null" && filter.Operator != "is_not_null" {
				args = append(args, sql.Named(fmt.Sprintf("p%d", i+1), filter.Value))
			}
		}
	}

	// ORDER BY clause
	if len(q.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, ob := range q.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteSQLServerIdentifier(ob.Column))
			if strings.ToUpper(ob.Direction) == "DESC" {
				sb.WriteString(" DESC")
			} else {
				sb.WriteString(" ASC")
			}
		}

		// OFFSET FETCH for pagination with ORDER BY
		if q.Offset > 0 || q.Limit > 0 {
			offset := q.Offset
			if offset < 0 {
				offset = 0
			}
			sb.WriteString(fmt.Sprintf(" OFFSET %d ROWS", offset))
			if q.Limit > 0 {
				sb.WriteString(fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", q.Limit))
			}
		}
	}

	return sb.String(), args, nil
}

func buildSQLServerFilterClause(filter Filter, argNum int) string {
	col := quoteSQLServerIdentifier(filter.Column)
	param := fmt.Sprintf("@p%d", argNum)
	switch filter.Operator {
	case "eq", "=", "":
		return fmt.Sprintf("%s = %s", col, param)
	case "ne", "!=", "<>":
		return fmt.Sprintf("%s <> %s", col, param)
	case "gt", ">":
		return fmt.Sprintf("%s > %s", col, param)
	case "gte", ">=":
		return fmt.Sprintf("%s >= %s", col, param)
	case "lt", "<":
		return fmt.Sprintf("%s < %s", col, param)
	case "lte", "<=":
		return fmt.Sprintf("%s <= %s", col, param)
	case "like":
		return fmt.Sprintf("%s LIKE %s", col, param)
	case "is_null":
		return fmt.Sprintf("%s IS NULL", col)
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", col)
	default:
		return fmt.Sprintf("%s = %s", col, param)
	}
}

func quoteSQLServerIdentifier(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

func normalizeSQLServerType(dataType string) string {
	dataType = strings.ToLower(dataType)
	switch {
	case strings.Contains(dataType, "int"):
		return "int"
	case strings.Contains(dataType, "decimal"), strings.Contains(dataType, "float"),
		strings.Contains(dataType, "money"), strings.Contains(dataType, "numeric"), strings.Contains(dataType, "real"):
		return "float"
	case strings.Contains(dataType, "char"), strings.Contains(dataType, "text"):
		return "string"
	case strings.Contains(dataType, "bit"):
		return "bool"
	case strings.Contains(dataType, "date"), strings.Contains(dataType, "time"):
		return "datetime"
	case strings.Contains(dataType, "binary"), strings.Contains(dataType, "image"):
		return "binary"
	case strings.Contains(dataType, "uniqueidentifier"):
		return "string"
	default:
		return "string"
	}
}

func convertSQLServerValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case nil:
		return nil
	default:
		return val
	}
}

type sqlserverRowStream struct {
	rows    *sql.Rows
	columns []string
	current Row
	err     error
}

func (s *sqlserverRowStream) Next() bool {
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
		s.current[col] = convertSQLServerValue(values[i])
	}
	return true
}

func (s *sqlserverRowStream) Row() Row    { return s.current }
func (s *sqlserverRowStream) Err() error  { if s.err != nil { return s.err }; return s.rows.Err() }
func (s *sqlserverRowStream) Close() error { return s.rows.Close() }
