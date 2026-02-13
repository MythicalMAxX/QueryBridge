// Package metadata provides persistence for database annotations.
package metadata

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Store manages database annotations in a SQLite database.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates a new metadata store.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("opening metadata db: %w", err)
	}

	// Create tables
	query := `
	CREATE TABLE IF NOT EXISTS annotations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		database TEXT,
		table_name TEXT,
		column_name TEXT, -- NULL for table annotations
		description TEXT,
		UNIQUE(database, table_name, column_name)
	);`
	
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("creating metadata tables: %w", err)
	}

	return &Store{db: db}, nil
}

// SetAnnotation saves a description for a table or column.
func (s *Store) SetAnnotation(dbName, tableName, colName, desc string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO annotations (database, table_name, column_name, description)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(database, table_name, column_name) DO UPDATE SET description = excluded.description;`

	var col interface{} = colName
	if colName == "" {
		col = nil
	}

	_, err := s.db.Exec(query, dbName, tableName, col, desc)
	return err
}

// GetAnnotations retrieves all annotations for a database.
func (s *Store) GetAnnotations(dbName string) (map[string]string, map[string]map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tableAnns := make(map[string]string)
	colAnns := make(map[string]map[string]string)

	rows, err := s.db.Query("SELECT table_name, column_name, description FROM annotations WHERE database = ?", dbName)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var table, desc string
		var col sql.NullString
		if err := rows.Scan(&table, &col, &desc); err != nil {
			return nil, nil, err
		}

		if !col.Valid || col.String == "" {
			tableAnns[table] = desc
		} else {
			if _, ok := colAnns[table]; !ok {
				colAnns[table] = make(map[string]string)
			}
			colAnns[table][col.String] = desc
		}
	}

	return tableAnns, colAnns, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}
