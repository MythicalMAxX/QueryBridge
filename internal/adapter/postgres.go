// Package adapter provides PostgreSQL database adapter implementation.
package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresAdapter implements the Adapter interface for PostgreSQL.
type PostgresAdapter struct {
	name   string
	config PostgresConfig
	db     *sql.DB
}

// PostgresConfig holds PostgreSQL connection configuration.
type PostgresConfig struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	SSLMode        string
	MaxConnections int
	ConnectTimeout int
}

// NewPostgresAdapter creates a new PostgreSQL adapter.
func NewPostgresAdapter(name string, config PostgresConfig) *PostgresAdapter {
	if config.MaxConnections <= 0 {
		config.MaxConnections = 10
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 10
	}

	return &PostgresAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection pool to PostgreSQL.
func (a *PostgresAdapter) Connect(ctx context.Context) error {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
		a.config.Host,
		a.config.Port,
		a.config.User,
		a.config.Password,
		a.config.Database,
		a.config.SSLMode,
		a.config.ConnectTimeout,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("opening postgres connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(a.config.MaxConnections)
	db.SetMaxIdleConns(a.config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(30 * time.Minute)

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("pinging postgres: %w", err)
	}

	a.db = db
	return nil
}

// Close closes all connections in the pool.
func (a *PostgresAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *PostgresAdapter) Type() string {
	return "postgres"
}

// Name returns the unique name of this adapter.
func (a *PostgresAdapter) Name() string {
	return a.name
}

// Ping checks if the database is reachable.
func (a *PostgresAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// DiscoverSchema retrieves the complete database schema.
func (a *PostgresAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "postgres",
		Tables:       make(map[string]*Table),
		DiscoveredAt: time.Now(),
	}

	// Get all tables
	tables, err := a.discoverTables(ctx)
	if err != nil {
		return nil, err
	}

	for _, table := range tables {
		// Get columns for each table
		columns, err := a.discoverColumns(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, err
		}
		table.Columns = columns

		// Get primary key
		pk, err := a.discoverPrimaryKey(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, err
		}
		table.PrimaryKey = pk

		// Mark primary key columns
		for i := range table.Columns {
			for _, pkCol := range pk {
				if table.Columns[i].Name == pkCol {
					table.Columns[i].IsPrimaryKey = true
				}
			}
		}

		// Get foreign keys
		fks, err := a.discoverForeignKeys(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, err
		}
		table.ForeignKeys = fks

		// Get indexes
		indexes, err := a.discoverIndexes(ctx, table.Schema, table.Name)
		if err != nil {
			return nil, err
		}
		table.Indexes = indexes

		// Get approximate row count
		rowCount, err := a.estimateRowCount(ctx, table.Schema, table.Name)
		if err == nil {
			table.RowCount = rowCount
		}

		key := table.Name
		if table.Schema != "" && table.Schema != "public" {
			key = table.Schema + "." + table.Name
		}
		schema.Tables[key] = table
	}

	return schema, nil
}

func (a *PostgresAdapter) discoverTables(ctx context.Context) ([]*Table, error) {
	query := `
		SELECT 
			table_schema,
			table_name
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		AND table_type = 'BASE TABLE'
		ORDER BY table_schema, table_name
	`

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying tables: %w", err)
	}
	defer rows.Close()

	var tables []*Table
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, fmt.Errorf("scanning table row: %w", err)
		}
		tables = append(tables, &Table{
			Name:   name,
			Schema: schema,
		})
	}

	return tables, rows.Err()
}

func (a *PostgresAdapter) discoverColumns(ctx context.Context, schemaName, tableName string) ([]Column, error) {
	query := `
		SELECT 
			column_name,
			data_type,
			udt_name,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying columns: %w", err)
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var name, dataType, udtName, nullable string
		var defaultVal sql.NullString

		if err := rows.Scan(&name, &dataType, &udtName, &nullable, &defaultVal); err != nil {
			return nil, fmt.Errorf("scanning column row: %w", err)
		}

		col := Column{
			Name:           name,
			Type:           dataType,
			NormalizedType: normalizePostgresType(dataType, udtName),
			Nullable:       nullable == "YES",
		}
		if defaultVal.Valid {
			col.DefaultValue = defaultVal.String
		}
		columns = append(columns, col)
	}

	return columns, rows.Err()
}

func (a *PostgresAdapter) discoverPrimaryKey(ctx context.Context, schemaName, tableName string) ([]string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		AND i.indisprimary
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		// Primary key might not exist, which is okay
		return nil, nil
	}
	defer rows.Close()

	var pks []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("scanning pk row: %w", err)
		}
		pks = append(pks, pk)
	}

	return pks, rows.Err()
}

func (a *PostgresAdapter) discoverForeignKeys(ctx context.Context, schemaName, tableName string) ([]ForeignKey, error) {
	query := `
		SELECT
			tc.constraint_name,
			kcu.column_name,
			ccu.table_name AS referenced_table,
			ccu.column_name AS referenced_column,
			rc.delete_rule,
			rc.update_rule
		FROM information_schema.table_constraints AS tc
		JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		JOIN information_schema.referential_constraints AS rc
			ON rc.constraint_name = tc.constraint_name
			AND rc.constraint_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		AND tc.table_schema = $1
		AND tc.table_name = $2
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying foreign keys: %w", err)
	}
	defer rows.Close()

	fkMap := make(map[string]*ForeignKey)
	for rows.Next() {
		var name, column, refTable, refColumn, onDelete, onUpdate string
		if err := rows.Scan(&name, &column, &refTable, &refColumn, &onDelete, &onUpdate); err != nil {
			return nil, fmt.Errorf("scanning fk row: %w", err)
		}

		if fk, exists := fkMap[name]; exists {
			fk.Columns = append(fk.Columns, column)
			fk.ReferencedColumns = append(fk.ReferencedColumns, refColumn)
		} else {
			fkMap[name] = &ForeignKey{
				Name:              name,
				Columns:           []string{column},
				ReferencedTable:   refTable,
				ReferencedColumns: []string{refColumn},
				OnDelete:          onDelete,
				OnUpdate:          onUpdate,
			}
		}
	}

	var fks []ForeignKey
	for _, fk := range fkMap {
		fks = append(fks, *fk)
	}
	return fks, rows.Err()
}

func (a *PostgresAdapter) discoverIndexes(ctx context.Context, schemaName, tableName string) ([]Index, error) {
	query := `
		SELECT
			i.relname AS index_name,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) AS columns,
			ix.indisunique,
			am.amname
		FROM pg_class t
		JOIN pg_index ix ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_am am ON am.oid = i.relam
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE t.relkind = 'r'
		AND n.nspname = $1
		AND t.relname = $2
		AND NOT ix.indisprimary
		GROUP BY i.relname, ix.indisunique, am.amname
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, nil // Indexes are optional, don't fail
	}
	defer rows.Close()

	var indexes []Index
	for rows.Next() {
		var name, indexType string
		var columns []string
		var isUnique bool

		if err := rows.Scan(&name, &columns, &isUnique, &indexType); err != nil {
			continue // Skip problematic indexes
		}

		indexes = append(indexes, Index{
			Name:     name,
			Columns:  columns,
			IsUnique: isUnique,
			Type:     indexType,
		})
	}

	return indexes, nil
}

func (a *PostgresAdapter) estimateRowCount(ctx context.Context, schemaName, tableName string) (int64, error) {
	// Use pg_class for fast estimate
	query := `
		SELECT reltuples::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`

	var count int64
	err := a.db.QueryRowContext(ctx, query, schemaName, tableName).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Execute runs a query and returns all results.
func (a *PostgresAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	sqlQuery, args, err := a.buildQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("getting columns: %w", err)
	}

	result := &Result{
		Columns: columns,
		Rows:    []Row{},
	}

	// Scan all rows
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}

		row := make(Row)
		for i, col := range columns {
			row[col] = convertValue(values[i])
		}
		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	result.RowCount = len(result.Rows)
	result.ExecutionTime = time.Since(start)

	// Check if there are more rows (for pagination)
	if query.Limit > 0 && len(result.Rows) == query.Limit {
		result.HasMore = true
		if query.CursorColumn != "" && len(result.Rows) > 0 {
			lastRow := result.Rows[len(result.Rows)-1]
			if cursorVal, ok := lastRow[query.CursorColumn]; ok {
				result.NextCursor = fmt.Sprintf("%v", cursorVal)
			}
		}
	}

	return result, nil
}

// Stream returns a streaming result set.
func (a *PostgresAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	sqlQuery, args, err := a.buildQuery(query)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("executing stream query: %w", err)
	}

	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("getting columns: %w", err)
	}

	return &postgresRowStream{
		rows:    rows,
		columns: columns,
	}, nil
}

// buildQuery constructs a SQL query from the Query struct.
func (a *PostgresAdapter) buildQuery(q *Query) (string, []interface{}, error) {
	if q.Raw != "" {
		return q.Raw, q.Parameters, nil
	}

	var builder strings.Builder
	var args []interface{}
	argNum := 1

	// SELECT clause
	builder.WriteString("SELECT ")
	if len(q.Columns) == 0 {
		builder.WriteString("*")
	} else {
		for i, col := range q.Columns {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(quoteIdentifier(col))
		}
	}

	// Aggregations
	for _, agg := range q.Aggregations {
		if len(q.Columns) > 0 || len(q.Aggregations) > 1 {
			builder.WriteString(", ")
		}
		builder.WriteString(fmt.Sprintf("%s(%s) AS %s",
			strings.ToUpper(agg.Function),
			quoteIdentifier(agg.Column),
			quoteIdentifier(agg.Alias),
		))
	}

	// FROM clause
	builder.WriteString(" FROM ")
	builder.WriteString(quoteIdentifier(q.Table))

	// JOIN clauses
	for _, join := range q.Joins {
		builder.WriteString(fmt.Sprintf(" %s JOIN %s", strings.ToUpper(join.Type), quoteIdentifier(join.Table)))
		if join.Alias != "" {
			builder.WriteString(" AS ")
			builder.WriteString(quoteIdentifier(join.Alias))
		}
		if len(join.Conditions) > 0 {
			builder.WriteString(" ON ")
			for i, cond := range join.Conditions {
				if i > 0 {
					builder.WriteString(" AND ")
				}
				builder.WriteString(fmt.Sprintf("%s = %s", quoteIdentifier(cond.Column), cond.Value))
			}
		}
	}

	// WHERE clause
	if len(q.Filters) > 0 {
		builder.WriteString(" WHERE ")
		for i, filter := range q.Filters {
			if i > 0 {
				logic := filter.Logic
				if logic == "" {
					logic = "AND"
				}
				builder.WriteString(fmt.Sprintf(" %s ", strings.ToUpper(logic)))
			}

			clause, newArgNum := buildFilterClause(filter, argNum)
			builder.WriteString(clause)
			if filter.Operator != "is_null" && filter.Operator != "is_not_null" {
				args = append(args, filter.Value)
				argNum = newArgNum
			}
		}
	}

	// Cursor pagination
	if q.Cursor != "" && q.CursorColumn != "" {
		if len(q.Filters) == 0 {
			builder.WriteString(" WHERE ")
		} else {
			builder.WriteString(" AND ")
		}
		builder.WriteString(fmt.Sprintf("%s > $%d", quoteIdentifier(q.CursorColumn), argNum))
		args = append(args, q.Cursor)
		argNum++
	}

	// GROUP BY clause
	if len(q.GroupBy) > 0 {
		builder.WriteString(" GROUP BY ")
		for i, col := range q.GroupBy {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(quoteIdentifier(col))
		}
	}

	// HAVING clause
	if len(q.Having) > 0 {
		builder.WriteString(" HAVING ")
		for i, filter := range q.Having {
			if i > 0 {
				logic := filter.Logic
				if logic == "" {
					logic = "AND"
				}
				builder.WriteString(fmt.Sprintf(" %s ", strings.ToUpper(logic)))
			}
			clause, newArgNum := buildFilterClause(filter, argNum)
			builder.WriteString(clause)
			args = append(args, filter.Value)
			argNum = newArgNum
		}
	}

	// ORDER BY clause
	if len(q.OrderBy) > 0 {
		builder.WriteString(" ORDER BY ")
		for i, ob := range q.OrderBy {
			if i > 0 {
				builder.WriteString(", ")
			}
			dir := "ASC"
			if strings.ToUpper(ob.Direction) == "DESC" {
				dir = "DESC"
			}
			builder.WriteString(fmt.Sprintf("%s %s", quoteIdentifier(ob.Column), dir))
		}
	} else if q.CursorColumn != "" {
		// Default ordering for cursor pagination
		builder.WriteString(fmt.Sprintf(" ORDER BY %s ASC", quoteIdentifier(q.CursorColumn)))
	}

	// LIMIT clause
	if q.Limit > 0 {
		builder.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}

	// OFFSET clause
	if q.Offset > 0 {
		builder.WriteString(fmt.Sprintf(" OFFSET %d", q.Offset))
	}

	return builder.String(), args, nil
}

func buildFilterClause(filter Filter, argNum int) (string, int) {
	col := quoteIdentifier(filter.Column)
	switch filter.Operator {
	case "eq", "=":
		return fmt.Sprintf("%s = $%d", col, argNum), argNum + 1
	case "ne", "!=", "<>":
		return fmt.Sprintf("%s <> $%d", col, argNum), argNum + 1
	case "gt", ">":
		return fmt.Sprintf("%s > $%d", col, argNum), argNum + 1
	case "gte", ">=":
		return fmt.Sprintf("%s >= $%d", col, argNum), argNum + 1
	case "lt", "<":
		return fmt.Sprintf("%s < $%d", col, argNum), argNum + 1
	case "lte", "<=":
		return fmt.Sprintf("%s <= $%d", col, argNum), argNum + 1
	case "like":
		return fmt.Sprintf("%s LIKE $%d", col, argNum), argNum + 1
	case "ilike":
		return fmt.Sprintf("%s ILIKE $%d", col, argNum), argNum + 1
	case "in":
		return fmt.Sprintf("%s = ANY($%d)", col, argNum), argNum + 1
	case "not_in":
		return fmt.Sprintf("%s <> ALL($%d)", col, argNum), argNum + 1
	case "is_null":
		return fmt.Sprintf("%s IS NULL", col), argNum
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", col), argNum
	default:
		return fmt.Sprintf("%s = $%d", col, argNum), argNum + 1
	}
}

func quoteIdentifier(s string) string {
	// Handle qualified names like schema.table
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		return `"` + parts[0] + `"."` + parts[1] + `"`
	}
	return `"` + s + `"`
}

func normalizePostgresType(dataType, udtName string) string {
	switch dataType {
	case "character varying", "varchar", "character", "char", "text", "name", "citext":
		return "string"
	case "integer", "smallint", "bigint", "serial", "smallserial", "bigserial":
		return "int"
	case "real", "double precision", "numeric", "decimal", "money":
		return "float"
	case "boolean":
		return "bool"
	case "date", "timestamp without time zone", "timestamp with time zone", "time without time zone", "time with time zone":
		return "datetime"
	case "json", "jsonb":
		return "json"
	case "uuid":
		return "string"
	case "bytea":
		return "binary"
	case "ARRAY":
		return "array"
	default:
		return "string"
	}
}

func convertValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	case nil:
		return nil
	default:
		return val
	}
}

// postgresRowStream implements RowStream for PostgreSQL.
type postgresRowStream struct {
	rows    *sql.Rows
	columns []string
	current Row
	err     error
}

func (s *postgresRowStream) Next() bool {
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
		s.current[col] = convertValue(values[i])
	}

	return true
}

func (s *postgresRowStream) Row() Row {
	return s.current
}

func (s *postgresRowStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}

func (s *postgresRowStream) Close() error {
	return s.rows.Close()
}

// Explain returns the execution plan for a query.
func (a *PostgresAdapter) Explain(ctx context.Context, q *Query) (string, error) {
	sqlQuery, args, err := a.buildQuery(q)
	if err != nil {
		return "", err
	}

	// Use EXPLAIN ANALYZE for actual execution statistics
	explainQuery := "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT TEXT) " + sqlQuery

	rows, err := a.db.QueryContext(ctx, explainQuery, args...)
	if err != nil {
		return "", fmt.Errorf("executing explain: %w", err)
	}
	defer rows.Close()

	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", fmt.Errorf("scanning explain row: %w", err)
		}
		planLines = append(planLines, line)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterating explain rows: %w", err)
	}

	return strings.Join(planLines, "\n"), nil
}
