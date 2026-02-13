// Package adapter provides MySQL database adapter implementation.
package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLAdapter implements the Adapter interface for MySQL.
type MySQLAdapter struct {
	name   string
	config MySQLConfig
	db     *sql.DB
}

// MySQLConfig holds MySQL connection configuration.
type MySQLConfig struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	MaxConnections int
	ConnectTimeout int
}

// NewMySQLAdapter creates a new MySQL adapter.
func NewMySQLAdapter(name string, config MySQLConfig) *MySQLAdapter {
	if config.Port == 0 {
		config.Port = 3306
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 10
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10
	}
	return &MySQLAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection pool to MySQL.
func (a *MySQLAdapter) Connect(ctx context.Context) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=%ds",
		a.config.User,
		a.config.Password,
		a.config.Host,
		a.config.Port,
		a.config.Database,
		a.config.ConnectTimeout,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("opening mysql connection: %w", err)
	}

	db.SetMaxOpenConns(a.config.MaxConnections)
	db.SetMaxIdleConns(a.config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("pinging mysql: %w", err)
	}

	a.db = db
	return nil
}

// Close closes all connections in the pool.
func (a *MySQLAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *MySQLAdapter) Type() string {
	return "mysql"
}

// Name returns the unique name of this adapter.
func (a *MySQLAdapter) Name() string {
	return a.name
}

// Ping checks if the database is reachable.
func (a *MySQLAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// DiscoverSchema retrieves the complete database schema.
func (a *MySQLAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "mysql",
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

		fks, err := a.discoverForeignKeys(ctx, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering foreign keys for %s: %w", table.Name, err)
		}
		table.ForeignKeys = fks

		indexes, err := a.discoverIndexes(ctx, table.Name)
		if err != nil {
			return nil, fmt.Errorf("discovering indexes for %s: %w", table.Name, err)
		}
		table.Indexes = indexes

		rowCount, _ := a.estimateRowCount(ctx, table.Name)
		table.RowCount = rowCount

		// Mark PK and FK columns
		pkSet := make(map[string]bool)
		for _, col := range pk {
			pkSet[col] = true
		}
		fkCols := make(map[string]bool)
		for _, fk := range fks {
			for _, col := range fk.Columns {
				fkCols[col] = true
			}
		}
		for i := range table.Columns {
			if pkSet[table.Columns[i].Name] {
				table.Columns[i].IsPrimaryKey = true
			}
			if fkCols[table.Columns[i].Name] {
				table.Columns[i].IsForeignKey = true
			}
		}

		schema.Tables[table.Name] = table
	}

	return schema, nil
}

func (a *MySQLAdapter) discoverTables(ctx context.Context) ([]*Table, error) {
	query := `
		SELECT TABLE_NAME 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`
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

func (a *MySQLAdapter) discoverColumns(ctx context.Context, tableName string) ([]Column, error) {
	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database, tableName)
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
			NormalizedType: normalizeMySQLType(dataType),
			Nullable:       nullable == "YES",
			DefaultValue:   defaultVal.String,
		})
	}
	return columns, rows.Err()
}

func (a *MySQLAdapter) discoverPrimaryKey(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION
	`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database, tableName)
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

func (a *MySQLAdapter) discoverForeignKeys(ctx context.Context, tableName string) ([]ForeignKey, error) {
	query := `
		SELECT 
			kcu.CONSTRAINT_NAME,
			kcu.COLUMN_NAME,
			kcu.REFERENCED_TABLE_NAME,
			kcu.REFERENCED_COLUMN_NAME,
			rc.DELETE_RULE,
			rc.UPDATE_RULE
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
		JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
			ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME 
			AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
		WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ? 
			AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION
	`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fkMap := make(map[string]*ForeignKey)
	var fkOrder []string

	for rows.Next() {
		var name, col, refTable, refCol, onDelete, onUpdate string
		if err := rows.Scan(&name, &col, &refTable, &refCol, &onDelete, &onUpdate); err != nil {
			return nil, err
		}

		if fk, ok := fkMap[name]; ok {
			fk.Columns = append(fk.Columns, col)
			fk.ReferencedColumns = append(fk.ReferencedColumns, refCol)
		} else {
			fkMap[name] = &ForeignKey{
				Name:              name,
				Columns:           []string{col},
				ReferencedTable:   refTable,
				ReferencedColumns: []string{refCol},
				OnDelete:          onDelete,
				OnUpdate:          onUpdate,
			}
			fkOrder = append(fkOrder, name)
		}
	}

	var fks []ForeignKey
	for _, name := range fkOrder {
		fks = append(fks, *fkMap[name])
	}
	return fks, rows.Err()
}

func (a *MySQLAdapter) discoverIndexes(ctx context.Context, tableName string) ([]Index, error) {
	query := `
		SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX
	`
	rows, err := a.db.QueryContext(ctx, query, a.config.Database, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexMap := make(map[string]*Index)
	var indexOrder []string

	for rows.Next() {
		var name, col, indexType string
		var nonUnique int
		if err := rows.Scan(&name, &col, &nonUnique, &indexType); err != nil {
			return nil, err
		}

		if idx, ok := indexMap[name]; ok {
			idx.Columns = append(idx.Columns, col)
		} else {
			indexMap[name] = &Index{
				Name:     name,
				Columns:  []string{col},
				IsUnique: nonUnique == 0,
				Type:     strings.ToLower(indexType),
			}
			indexOrder = append(indexOrder, name)
		}
	}

	var indexes []Index
	for _, name := range indexOrder {
		indexes = append(indexes, *indexMap[name])
	}
	return indexes, rows.Err()
}

func (a *MySQLAdapter) estimateRowCount(ctx context.Context, tableName string) (int64, error) {
	query := `
		SELECT TABLE_ROWS 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	`
	var count int64
	err := a.db.QueryRowContext(ctx, query, a.config.Database, tableName).Scan(&count)
	return count, err
}

// Execute runs a query and returns all results.
func (a *MySQLAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
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
			row[col] = convertMySQLValue(values[i])
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
func (a *MySQLAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
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

	return &mysqlRowStream{
		rows:    rows,
		columns: cols,
	}, nil
}

// buildQuery constructs a SQL query from the Query struct.
func (a *MySQLAdapter) buildQuery(q *Query) (string, []interface{}, error) {
	if q.Raw != "" {
		return q.Raw, q.Parameters, nil
	}

	var sb strings.Builder
	var args []interface{}
	argNum := 1

	// SELECT clause
	sb.WriteString("SELECT ")
	if len(q.Columns) == 0 {
		sb.WriteString("*")
	} else {
		for i, col := range q.Columns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteMySQLIdentifier(col))
		}
	}

	// Aggregations
	for i, agg := range q.Aggregations {
		if i > 0 || len(q.Columns) > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s(%s) AS %s",
			strings.ToUpper(agg.Function),
			quoteMySQLIdentifier(agg.Column),
			quoteMySQLIdentifier(agg.Alias),
		))
	}

	// FROM clause
	sb.WriteString(" FROM ")
	sb.WriteString(quoteMySQLIdentifier(q.Table))

	// JOIN clauses
	for _, join := range q.Joins {
		sb.WriteString(fmt.Sprintf(" %s JOIN %s", strings.ToUpper(join.Type), quoteMySQLIdentifier(join.Table)))
		if join.Alias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(quoteMySQLIdentifier(join.Alias))
		}
		if len(join.Conditions) > 0 {
			sb.WriteString(" ON ")
			for i, cond := range join.Conditions {
				if i > 0 {
					sb.WriteString(" AND ")
				}
				clause, newArgNum := buildMySQLFilterClause(cond, argNum)
				sb.WriteString(clause)
				args = append(args, cond.Value)
				argNum = newArgNum
			}
		}
	}

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
			clause, newArgNum := buildMySQLFilterClause(filter, argNum)
			sb.WriteString(clause)
			if filter.Operator != "is_null" && filter.Operator != "is_not_null" {
				args = append(args, filter.Value)
			}
			argNum = newArgNum
		}
	}

	// GROUP BY clause
	if len(q.GroupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		for i, col := range q.GroupBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteMySQLIdentifier(col))
		}
	}

	// HAVING clause
	if len(q.Having) > 0 {
		sb.WriteString(" HAVING ")
		for i, filter := range q.Having {
			if i > 0 {
				logic := filter.Logic
				if logic == "" {
					logic = "AND"
				}
				sb.WriteString(fmt.Sprintf(" %s ", strings.ToUpper(logic)))
			}
			clause, newArgNum := buildMySQLFilterClause(filter, argNum)
			sb.WriteString(clause)
			args = append(args, filter.Value)
			argNum = newArgNum
		}
	}

	// ORDER BY clause
	if len(q.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, ob := range q.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteMySQLIdentifier(ob.Column))
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

func buildMySQLFilterClause(filter Filter, argNum int) (string, int) {
	col := quoteMySQLIdentifier(filter.Column)
	switch filter.Operator {
	case "eq", "=", "":
		return fmt.Sprintf("%s = ?", col), argNum + 1
	case "ne", "!=", "<>":
		return fmt.Sprintf("%s != ?", col), argNum + 1
	case "gt", ">":
		return fmt.Sprintf("%s > ?", col), argNum + 1
	case "gte", ">=":
		return fmt.Sprintf("%s >= ?", col), argNum + 1
	case "lt", "<":
		return fmt.Sprintf("%s < ?", col), argNum + 1
	case "lte", "<=":
		return fmt.Sprintf("%s <= ?", col), argNum + 1
	case "like":
		return fmt.Sprintf("%s LIKE ?", col), argNum + 1
	case "ilike":
		return fmt.Sprintf("LOWER(%s) LIKE LOWER(?)", col), argNum + 1
	case "in":
		return fmt.Sprintf("%s IN (?)", col), argNum + 1
	case "not_in":
		return fmt.Sprintf("%s NOT IN (?)", col), argNum + 1
	case "is_null":
		return fmt.Sprintf("%s IS NULL", col), argNum
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", col), argNum
	default:
		return fmt.Sprintf("%s = ?", col), argNum + 1
	}
}

func quoteMySQLIdentifier(s string) string {
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		for i, p := range parts {
			parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
		}
		return strings.Join(parts, ".")
	}
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func normalizeMySQLType(dataType string) string {
	dataType = strings.ToLower(dataType)
	switch {
	case strings.Contains(dataType, "int"):
		return "int"
	case strings.Contains(dataType, "decimal"), strings.Contains(dataType, "float"),
		strings.Contains(dataType, "double"), strings.Contains(dataType, "numeric"):
		return "float"
	case strings.Contains(dataType, "char"), strings.Contains(dataType, "text"),
		strings.Contains(dataType, "enum"), strings.Contains(dataType, "set"):
		return "string"
	case strings.Contains(dataType, "bool"):
		return "bool"
	case strings.Contains(dataType, "date"), strings.Contains(dataType, "time"):
		return "datetime"
	case strings.Contains(dataType, "json"):
		return "json"
	case strings.Contains(dataType, "blob"), strings.Contains(dataType, "binary"):
		return "binary"
	default:
		return "string"
	}
}

func convertMySQLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case nil:
		return nil
	default:
		return val
	}
}

// mysqlRowStream implements RowStream for MySQL.
type mysqlRowStream struct {
	rows    *sql.Rows
	columns []string
	current Row
	err     error
}

func (s *mysqlRowStream) Next() bool {
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
		s.current[col] = convertMySQLValue(values[i])
	}
	return true
}

func (s *mysqlRowStream) Row() Row {
	return s.current
}

func (s *mysqlRowStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}

func (s *mysqlRowStream) Close() error {
	return s.rows.Close()
}
