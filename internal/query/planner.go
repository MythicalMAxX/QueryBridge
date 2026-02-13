// Package query provides query planning, validation, and execution.
package query

import (
	"context"
	"fmt"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/schema"
)

// QueryParams represents query parameters (matches MCP tool params).
type QueryParams struct {
	Type         string             `json:"type,omitempty"`
	Table        string             `json:"table"`
	Columns      []string           `json:"columns,omitempty"`
	Filters      []FilterParam      `json:"filters,omitempty"`
	Joins        []JoinParam        `json:"joins,omitempty"`
	OrderBy      []OrderByParam     `json:"order_by,omitempty"`
	GroupBy      []string           `json:"group_by,omitempty"`
	Aggregations []AggregationParam `json:"aggregations,omitempty"`
	Limit        int                `json:"limit,omitempty"`
	Offset       int                `json:"offset,omitempty"`
	Cursor       string             `json:"cursor,omitempty"`
	CursorColumn string             `json:"cursor_column,omitempty"`
	Raw          string             `json:"raw,omitempty"`
}

// FilterParam represents a filter condition.
type FilterParam struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
	Logic    string      `json:"logic,omitempty"`
}

// JoinParam represents a join specification.
type JoinParam struct {
	Type       string        `json:"type"`
	Table      string        `json:"table"`
	Alias      string        `json:"alias,omitempty"`
	Conditions []FilterParam `json:"conditions"`
}

// OrderByParam represents a sort specification.
type OrderByParam struct {
	Column    string `json:"column"`
	Direction string `json:"direction"`
}

// AggregationParam represents an aggregation function.
type AggregationParam struct {
	Function string `json:"function"`
	Column   string `json:"column"`
	Alias    string `json:"alias"`
}

// Planner validates and plans query execution.
type Planner struct {
	registry *schema.Registry
	adapters map[string]adapter.Adapter
}

// NewPlanner creates a new query planner.
func NewPlanner(registry *schema.Registry, adapters map[string]adapter.Adapter) *Planner {
	return &Planner{registry: registry, adapters: adapters}
}

// Plan validates and converts query params to an adapter.Query.
func (p *Planner) Plan(database string, params *QueryParams) (*adapter.Query, error) {
	// Validate database exists
	s, ok := p.registry.Get(database)
	if !ok {
		return nil, &schema.SchemaNotFoundError{Database: database}
	}

	query := &adapter.Query{
		Type:         params.Type,
		Table:        params.Table,
		Columns:      params.Columns,
		Limit:        params.Limit,
		Offset:       params.Offset,
		Cursor:       params.Cursor,
		CursorColumn: params.CursorColumn,
		GroupBy:      params.GroupBy,
		Raw:          params.Raw,
	}

	if query.Type == "" {
		query.Type = "select"
	}

	// Convert filters
	for _, f := range params.Filters {
		if p.registry.IsDenied(f.Column) {
			return nil, &schema.DeniedFieldError{Field: f.Column}
		}
		query.Filters = append(query.Filters, adapter.Filter{
			Column: f.Column, Operator: f.Operator, Value: f.Value, Logic: f.Logic,
		})
	}

	// Convert order by
	for _, ob := range params.OrderBy {
		if p.registry.IsDenied(ob.Column) {
			return nil, &schema.DeniedFieldError{Field: ob.Column}
		}
		query.OrderBy = append(query.OrderBy, adapter.OrderBy{
			Column: ob.Column, Direction: ob.Direction,
		})
	}

	// Convert joins
	for _, j := range params.Joins {
		join := adapter.Join{Type: j.Type, Table: j.Table, Alias: j.Alias}
		for _, c := range j.Conditions {
			join.Conditions = append(join.Conditions, adapter.Filter{
				Column: c.Column, Operator: c.Operator, Value: c.Value,
			})
		}
		query.Joins = append(query.Joins, join)
	}

	// Convert aggregations
	for _, a := range params.Aggregations {
		if p.registry.IsDenied(a.Column) {
			return nil, &schema.DeniedFieldError{Field: a.Column}
		}
		query.Aggregations = append(query.Aggregations, adapter.Aggregation{
			Function: a.Function, Column: a.Column, Alias: a.Alias,
		})
	}

	// Validate columns if specified
	if len(query.Columns) > 0 {
		for _, col := range query.Columns {
			if p.registry.IsDenied(col) {
				return nil, &schema.DeniedFieldError{Field: col}
			}
		}
	} else {
		// Auto-select safe columns for SQL
		if s.Tables != nil {
			if cols, err := p.registry.GetSafeColumns(database, params.Table); err == nil {
				query.Columns = cols
			}
		}
	}

	// Validate against schema
	if err := p.registry.ValidateQuery(database, query); err != nil {
		return nil, err
	}

	return query, nil
}

// Execute runs a planned query.
func (p *Planner) Execute(ctx context.Context, database string, query *adapter.Query) (*adapter.Result, error) {
	a, ok := p.adapters[database]
	if !ok {
		return nil, fmt.Errorf("adapter not found for database: %s", database)
	}
	return a.Execute(ctx, query)
}

// Stream returns a streaming result for a planned query.
func (p *Planner) Stream(ctx context.Context, database string, query *adapter.Query) (adapter.RowStream, error) {
	a, ok := p.adapters[database]
	if !ok {
		return nil, fmt.Errorf("adapter not found for database: %s", database)
	}
	return a.Stream(ctx, query)
}

// StripDeniedFields removes denied fields from a result row.
func (p *Planner) StripDeniedFields(row adapter.Row) adapter.Row {
	safe := make(adapter.Row)
	for k, v := range row {
		if !p.registry.IsDenied(k) {
			safe[k] = v
		}
	}
	return safe
}
