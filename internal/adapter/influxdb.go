// Package adapter provides InfluxDB time-series database adapter implementation.
package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// InfluxDBAdapter implements the Adapter interface for InfluxDB.
type InfluxDBAdapter struct {
	name     string
	config   InfluxDBConfig
	client   influxdb2.Client
	queryAPI api.QueryAPI
}

// InfluxDBConfig holds InfluxDB connection configuration.
type InfluxDBConfig struct {
	URL    string
	Token  string
	Org    string
	Bucket string
}

// NewInfluxDBAdapter creates a new InfluxDB adapter.
func NewInfluxDBAdapter(name string, config InfluxDBConfig) *InfluxDBAdapter {
	if config.URL == "" {
		config.URL = "http://localhost:8086"
	}
	return &InfluxDBAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection to InfluxDB.
func (a *InfluxDBAdapter) Connect(ctx context.Context) error {
	a.client = influxdb2.NewClient(a.config.URL, a.config.Token)

	// Verify connection
	health, err := a.client.Health(ctx)
	if err != nil {
		return fmt.Errorf("checking influxdb health: %w", err)
	}
	if health.Status != "pass" {
		return fmt.Errorf("influxdb unhealthy: %s", health.Status)
	}

	a.queryAPI = a.client.QueryAPI(a.config.Org)
	return nil
}

// Close closes the InfluxDB client.
func (a *InfluxDBAdapter) Close() error {
	if a.client != nil {
		a.client.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *InfluxDBAdapter) Type() string { return "influxdb" }

// Name returns the unique name of this adapter.
func (a *InfluxDBAdapter) Name() string { return a.name }

// Ping checks if InfluxDB is reachable.
func (a *InfluxDBAdapter) Ping(ctx context.Context) error {
	_, err := a.client.Health(ctx)
	return err
}

// DiscoverSchema retrieves measurements from the bucket.
func (a *InfluxDBAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.config.Bucket,
		Type:         "influxdb",
		Collections:  make(map[string]*Collection),
		DiscoveredAt: time.Now(),
	}

	// Get measurements
	query := fmt.Sprintf(`import "influxdata/influxdb/schema"
schema.measurements(bucket: "%s")`, a.config.Bucket)

	result, err := a.queryAPI.Query(ctx, query)
	if err != nil {
		return schema, nil // Return empty schema on error
	}

	for result.Next() {
		measurement := result.Record().Value().(string)
		schema.Collections[measurement] = &Collection{
			Name:   measurement,
			Fields: []Field{{Name: "_time", Types: []string{"datetime"}}, {Name: "_value", Types: []string{"float"}}},
		}
	}

	return schema, nil
}

// Execute runs a Flux query.
func (a *InfluxDBAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	fluxQuery := a.buildQuery(query)
	result, err := a.queryAPI.Query(ctx, fluxQuery)
	if err != nil {
		return nil, fmt.Errorf("executing flux query: %w", err)
	}

	var rows []Row
	var columns []string
	colSet := make(map[string]bool)

	for result.Next() {
		record := result.Record()
		row := make(Row)

		row["_time"] = record.Time()
		row["_measurement"] = record.Measurement()
		row["_field"] = record.Field()
		row["_value"] = record.Value()

		for k, v := range record.Values() {
			row[k] = v
			if !colSet[k] {
				columns = append(columns, k)
				colSet[k] = true
			}
		}
		rows = append(rows, row)
	}

	if result.Err() != nil {
		return nil, result.Err()
	}

	return &Result{
		Columns:       columns,
		Rows:          rows,
		RowCount:      len(rows),
		ExecutionTime: time.Since(start),
	}, nil
}

func (a *InfluxDBAdapter) buildQuery(q *Query) string {
	if q.Raw != "" {
		return q.Raw
	}

	// Build Flux query
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`from(bucket: "%s")`, a.config.Bucket))
	sb.WriteString(` |> range(start: -1h)`) // Default to last hour

	if q.Table != "" {
		sb.WriteString(fmt.Sprintf(` |> filter(fn: (r) => r._measurement == "%s")`, q.Table))
	}

	for _, f := range q.Filters {
		sb.WriteString(fmt.Sprintf(` |> filter(fn: (r) => r.%s == "%v")`, f.Column, f.Value))
	}

	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(` |> limit(n: %d)`, q.Limit))
	}

	return sb.String()
}

// Stream returns a streaming result set.
func (a *InfluxDBAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return &influxRowStream{rows: result.Rows, columns: result.Columns, index: -1}, nil
}

type influxRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *influxRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}
func (s *influxRowStream) Row() Row    { return s.current }
func (s *influxRowStream) Err() error  { return nil }
func (s *influxRowStream) Close() error { return nil }
