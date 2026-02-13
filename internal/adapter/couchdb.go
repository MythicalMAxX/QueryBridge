// Package adapter provides CouchDB database adapter implementation.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CouchDBAdapter implements the Adapter interface for CouchDB.
type CouchDBAdapter struct {
	name   string
	config CouchDBConfig
	client *http.Client
	url    string
}

// CouchDBConfig holds CouchDB connection configuration.
type CouchDBConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// NewCouchDBAdapter creates a new CouchDB adapter.
func NewCouchDBAdapter(name string, config CouchDBConfig) *CouchDBAdapter {
	if config.Port == 0 {
		config.Port = 5984
	}
	return &CouchDBAdapter{
		name:   name,
		config: config,
		url:    fmt.Sprintf("http://%s:%d", config.Host, config.Port),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Connect verifies connectivity to CouchDB.
func (a *CouchDBAdapter) Connect(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url, nil)
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to couchdb: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("couchdb returned status: %d", resp.StatusCode)
	}
	return nil
}

// Close closes the HTTP client (no-op).
func (a *CouchDBAdapter) Close() error { return nil }

// Type returns the database type identifier.
func (a *CouchDBAdapter) Type() string { return "couchdb" }

// Name returns the unique name of this adapter.
func (a *CouchDBAdapter) Name() string { return a.name }

// Ping checks if CouchDB is reachable.
func (a *CouchDBAdapter) Ping(ctx context.Context) error { return a.Connect(ctx) }

// DiscoverSchema retrieves database info.
func (a *CouchDBAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Database,
		Type:         "couchdb",
		Collections:  make(map[string]*Collection),
		DiscoveredAt: time.Now(),
	}

	// Get database info
	dbURL := fmt.Sprintf("%s/%s", a.url, a.config.Database)
	req, _ := http.NewRequestWithContext(ctx, "GET", dbURL, nil)
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dbInfo struct {
		DocCount int64 `json:"doc_count"`
	}
	json.NewDecoder(resp.Body).Decode(&dbInfo)

	schema.Collections["_all_docs"] = &Collection{
		Name:     "_all_docs",
		DocCount: dbInfo.DocCount,
	}

	return schema, nil
}

// Execute runs a CouchDB query.
func (a *CouchDBAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	// Use Mango query API
	findURL := fmt.Sprintf("%s/%s/_find", a.url, a.config.Database)

	selector := map[string]interface{}{}
	for _, f := range query.Filters {
		selector[f.Column] = f.Value
	}
	if len(selector) == 0 {
		selector["_id"] = map[string]interface{}{"$gt": nil}
	}

	body := map[string]interface{}{
		"selector": selector,
		"limit":    query.Limit,
	}
	if query.Limit == 0 {
		body["limit"] = 25
	}
	if len(query.Columns) > 0 {
		body["fields"] = query.Columns
	}

	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", findURL, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Docs []map[string]interface{} `json:"docs"`
	}
	json.Unmarshal(respBody, &result)

	var rows []Row
	var columns []string
	colSet := make(map[string]bool)

	for _, doc := range result.Docs {
		row := make(Row)
		for k, v := range doc {
			row[k] = v
			if !colSet[k] {
				columns = append(columns, k)
				colSet[k] = true
			}
		}
		rows = append(rows, row)
	}

	return &Result{
		Columns:       columns,
		Rows:          rows,
		RowCount:      len(rows),
		ExecutionTime: time.Since(start),
	}, nil
}

// Stream returns a streaming result set.
func (a *CouchDBAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return &couchRowStream{rows: result.Rows, columns: result.Columns, index: -1}, nil
}

type couchRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *couchRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}
func (s *couchRowStream) Row() Row    { return s.current }
func (s *couchRowStream) Err() error  { return nil }
func (s *couchRowStream) Close() error { return nil }
