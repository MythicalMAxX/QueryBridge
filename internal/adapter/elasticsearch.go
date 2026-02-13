// Package adapter provides Elasticsearch database adapter implementation.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ElasticsearchAdapter implements the Adapter interface for Elasticsearch.
type ElasticsearchAdapter struct {
	name   string
	config ElasticsearchConfig
	client *http.Client
	url    string
}

// ElasticsearchConfig holds Elasticsearch connection configuration.
type ElasticsearchConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	UseSSL   bool
}

// NewElasticsearchAdapter creates a new Elasticsearch adapter.
func NewElasticsearchAdapter(name string, config ElasticsearchConfig) *ElasticsearchAdapter {
	if config.Port == 0 {
		config.Port = 9200
	}

	protocol := "http"
	if config.UseSSL {
		protocol = "https"
	}

	return &ElasticsearchAdapter{
		name:   name,
		config: config,
		url:    fmt.Sprintf("%s://%s:%d", protocol, config.Host, config.Port),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Connect verifies connectivity to Elasticsearch.
func (a *ElasticsearchAdapter) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", a.url, nil)
	if err != nil {
		return err
	}

	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to elasticsearch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("elasticsearch returned status: %d", resp.StatusCode)
	}

	return nil
}

// Close closes the HTTP client (no-op for HTTP).
func (a *ElasticsearchAdapter) Close() error {
	return nil
}

// Type returns the database type identifier.
func (a *ElasticsearchAdapter) Type() string {
	return "elasticsearch"
}

// Name returns the unique name of this adapter.
func (a *ElasticsearchAdapter) Name() string {
	return a.name
}

// Ping checks if Elasticsearch is reachable.
func (a *ElasticsearchAdapter) Ping(ctx context.Context) error {
	return a.Connect(ctx)
}

// DiscoverSchema retrieves all indices as the schema.
func (a *ElasticsearchAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.name,
		Type:         "elasticsearch",
		Collections:  make(map[string]*Collection),
		DiscoveredAt: time.Now(),
	}

	// Get all indices
	req, err := http.NewRequestWithContext(ctx, "GET", a.url+"/_cat/indices?format=json", nil)
	if err != nil {
		return nil, err
	}
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching indices: %w", err)
	}
	defer resp.Body.Close()

	var indices []struct {
		Index    string `json:"index"`
		Health   string `json:"health"`
		Status   string `json:"status"`
		DocsCount string `json:"docs.count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&indices); err != nil {
		return nil, err
	}

	for _, idx := range indices {
		if strings.HasPrefix(idx.Index, ".") {
			continue // Skip system indices
		}

		// Get mapping for each index
		fields, _ := a.getIndexMapping(ctx, idx.Index)

		var docCount int64
		fmt.Sscanf(idx.DocsCount, "%d", &docCount)

		schema.Collections[idx.Index] = &Collection{
			Name:     idx.Index,
			DocCount: docCount,
			Fields:   fields,
		}
	}

	return schema, nil
}

func (a *ElasticsearchAdapter) getIndexMapping(ctx context.Context, index string) ([]Field, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/%s/_mapping", a.url, index), nil)
	if err != nil {
		return nil, err
	}
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var mappingResp map[string]struct {
		Mappings struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mappingResp); err != nil {
		return nil, err
	}

	var fields []Field
	for _, mapping := range mappingResp {
		for name, prop := range mapping.Mappings.Properties {
			fields = append(fields, Field{
				Name:  name,
				Types: []string{prop.Type},
			})
		}
		break
	}
	return fields, nil
}

// Execute runs an Elasticsearch query.
func (a *ElasticsearchAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	index := query.Table
	if index == "" {
		index = "*"
	}

	// Build search query
	searchQuery := a.buildSearchQuery(query)

	body, _ := json.Marshal(searchQuery)
	req, err := http.NewRequestWithContext(ctx, "POST", 
		fmt.Sprintf("%s/%s/_search", a.url, index), 
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.User != "" {
		req.SetBasicAuth(a.config.User, a.config.Password)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing search: %w", err)
	}
	defer resp.Body.Close()

	var searchResp struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID     string                 `json:"_id"`
				Index  string                 `json:"_index"`
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	// Build rows
	var rows []Row
	var columns []string
	columnSet := make(map[string]bool)

	for _, hit := range searchResp.Hits.Hits {
		row := make(Row)
		row["_id"] = hit.ID
		row["_index"] = hit.Index

		for k, v := range hit.Source {
			row[k] = v
			if !columnSet[k] {
				columns = append(columns, k)
				columnSet[k] = true
			}
		}
		rows = append(rows, row)
	}

	// Prepend system columns
	columns = append([]string{"_id", "_index"}, columns...)

	return &Result{
		Columns:       columns,
		Rows:          rows,
		RowCount:      len(rows),
		TotalCount:    searchResp.Hits.Total.Value,
		HasMore:       int64(len(rows)) < searchResp.Hits.Total.Value,
		ExecutionTime: time.Since(start),
	}, nil
}

func (a *ElasticsearchAdapter) buildSearchQuery(query *Query) map[string]interface{} {
	q := map[string]interface{}{
		"size": 10,
	}

	if query.Limit > 0 {
		q["size"] = query.Limit
	}
	if query.Offset > 0 {
		q["from"] = query.Offset
	}

	// Build query
	if len(query.Filters) > 0 {
		must := []map[string]interface{}{}
		for _, f := range query.Filters {
			switch f.Operator {
			case "eq", "=", "":
				must = append(must, map[string]interface{}{
					"term": map[string]interface{}{f.Column: f.Value},
				})
			case "like":
				must = append(must, map[string]interface{}{
					"wildcard": map[string]interface{}{f.Column: f.Value},
				})
			case "gt", ">":
				must = append(must, map[string]interface{}{
					"range": map[string]interface{}{f.Column: map[string]interface{}{"gt": f.Value}},
				})
			case "gte", ">=":
				must = append(must, map[string]interface{}{
					"range": map[string]interface{}{f.Column: map[string]interface{}{"gte": f.Value}},
				})
			case "lt", "<":
				must = append(must, map[string]interface{}{
					"range": map[string]interface{}{f.Column: map[string]interface{}{"lt": f.Value}},
				})
			case "lte", "<=":
				must = append(must, map[string]interface{}{
					"range": map[string]interface{}{f.Column: map[string]interface{}{"lte": f.Value}},
				})
			}
		}
		q["query"] = map[string]interface{}{
			"bool": map[string]interface{}{"must": must},
		}
	} else {
		q["query"] = map[string]interface{}{"match_all": map[string]interface{}{}}
	}

	// Sort
	if len(query.OrderBy) > 0 {
		var sort []map[string]interface{}
		for _, ob := range query.OrderBy {
			sort = append(sort, map[string]interface{}{
				ob.Column: map[string]interface{}{"order": strings.ToLower(ob.Direction)},
			})
		}
		q["sort"] = sort
	}

	return q
}

// Stream returns a streaming result set.
func (a *ElasticsearchAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}

	return &esRowStream{
		rows:    result.Rows,
		columns: result.Columns,
		index:   -1,
	}, nil
}

type esRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *esRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}

func (s *esRowStream) Row() Row {
	return s.current
}

func (s *esRowStream) Err() error {
	return nil
}

func (s *esRowStream) Close() error {
	return nil
}
