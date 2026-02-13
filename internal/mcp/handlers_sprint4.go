// Package mcp provides Sprint 4 tool handlers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/cache"
	"github.com/MythicalMAxX/QueryBridge/internal/query"
	"github.com/MythicalMAxX/QueryBridge/internal/scheduler"
	"github.com/MythicalMAxX/QueryBridge/internal/template"
)

// Sprint4Components holds Sprint 4 feature components.
type Sprint4Components struct {
	Cache         *cache.Cache
	Templates     *template.Store
	Scheduler     *scheduler.Scheduler
}

// CrossDBJoinParams holds parameters for cross_db_join tool.
type CrossDBJoinParams struct {
	LeftQuery  SingleSourceQuery `json:"left_query"`
	RightQuery SingleSourceQuery `json:"right_query"`
	JoinType   string            `json:"join_type"`
	LeftKey    string            `json:"left_key"`
	RightKey   string            `json:"right_key"`
}

// ListTemplatesParams holds parameters for list_templates tool.
type ListTemplatesParams struct {
	Database string `json:"database"`
}

// ExecuteTemplateParams holds parameters for execute_template tool.
type ExecuteTemplateParams struct {
	Template string                 `json:"template"`
	Params   map[string]interface{} `json:"params"`
}

// SaveTemplateParams holds parameters for save_template tool.
type SaveTemplateParams struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Database    string                 `json:"database"`
	Query       map[string]interface{} `json:"query"`
	Parameters  []template.Parameter   `json:"parameters"`
}

// ScheduleQueryParams holds parameters for schedule_query tool.
type ScheduleQueryParams struct {
	Name     string                  `json:"name"`
	Database string                  `json:"database"`
	Query    map[string]interface{}  `json:"query"`
	Schedule string                  `json:"schedule"`
	Export   *scheduler.ExportConfig `json:"export"`
}

// ListSchedulesParams holds parameters for list_schedules tool.
type ListSchedulesParams struct {
	Database string `json:"database"`
}

// CancelScheduleParams holds parameters for cancel_schedule tool.
type CancelScheduleParams struct {
	Schedule string `json:"schedule"`
}

// ClearCacheParams holds parameters for clear_cache tool.
type ClearCacheParams struct {
	Database string `json:"database"`
}

// crossDBJoin performs a join between two database queries.
func (s *Server) crossDBJoin(ctx context.Context, args json.RawMessage, components *Sprint4Components) (*ToolResult, error) {
	var params CrossDBJoinParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Execute left query
	leftQP := convertQueryParams(&params.LeftQuery.Query)
	leftQ, err := s.planner.Plan(params.LeftQuery.Database, leftQP)
	if err != nil {
		return nil, fmt.Errorf("left query: %w", err)
	}
	leftResult, err := s.planner.Execute(ctx, params.LeftQuery.Database, leftQ)
	if err != nil {
		return nil, fmt.Errorf("left query: %w", err)
	}

	// Execute right query
	rightQP := convertQueryParams(&params.RightQuery.Query)
	rightQ, err := s.planner.Plan(params.RightQuery.Database, rightQP)
	if err != nil {
		return nil, fmt.Errorf("right query: %w", err)
	}
	rightResult, err := s.planner.Execute(ctx, params.RightQuery.Database, rightQ)
	if err != nil {
		return nil, fmt.Errorf("right query: %w", err)
	}

	// Perform cross-database join
	leftAlias := params.LeftQuery.Alias
	if leftAlias == "" {
		leftAlias = params.LeftQuery.Database
	}
	rightAlias := params.RightQuery.Alias
	if rightAlias == "" {
		rightAlias = params.RightQuery.Database
	}

	cfg := &query.CrossJoinConfig{
		LeftResult:  leftResult,
		RightResult: rightResult,
		LeftKey:     params.LeftKey,
		RightKey:    params.RightKey,
		JoinType:    query.JoinType(params.JoinType),
		LeftAlias:   leftAlias,
		RightAlias:  rightAlias,
	}

	joinResult, err := query.CrossJoin(cfg)
	if err != nil {
		return nil, err
	}

	resp := QueryResponse{
		Columns:  joinResult.Columns,
		Rows:     make([]map[string]interface{}, 0, len(joinResult.Rows)),
		RowCount: len(joinResult.Rows),
	}
	for _, row := range joinResult.Rows {
		resp.Rows = append(resp.Rows, row)
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

// listTemplates returns all saved templates.
func (s *Server) listTemplates(ctx context.Context, args json.RawMessage, templates *template.Store) (*ToolResult, error) {
	var params ListTemplatesParams
	json.Unmarshal(args, &params)

	list := templates.List(params.Database)

	data, _ := json.MarshalIndent(map[string]interface{}{
		"templates": list,
		"count":     len(list),
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// executeTemplate runs a saved template with parameters.
func (s *Server) executeTemplate(ctx context.Context, args json.RawMessage, templates *template.Store) (*ToolResult, error) {
	var params ExecuteTemplateParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	tmpl, err := templates.Get(params.Template)
	if err != nil {
		return nil, err
	}

	// Apply parameters
	queryMap, err := template.ApplyParameters(tmpl.Query, params.Params)
	if err != nil {
		return nil, err
	}

	// Convert to QueryParams
	queryJSON, _ := json.Marshal(queryMap)
	var qp QueryParams
	json.Unmarshal(queryJSON, &qp)

	// Execute the query
	queryParams := convertQueryParams(&qp)
	q, err := s.planner.Plan(tmpl.Database, queryParams)
	if err != nil {
		return nil, err
	}

	result, err := s.planner.Execute(ctx, tmpl.Database, q)
	if err != nil {
		return nil, err
	}

	// Increment usage
	templates.IncrementUsage(params.Template)

	resp := QueryResponse{
		Columns:  result.Columns,
		Rows:     make([]map[string]interface{}, 0, len(result.Rows)),
		RowCount: result.RowCount,
	}
	for _, row := range result.Rows {
		resp.Rows = append(resp.Rows, s.planner.StripDeniedFields(row))
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

// saveTemplate saves a new query template.
func (s *Server) saveTemplate(ctx context.Context, args json.RawMessage, templates *template.Store) (*ToolResult, error) {
	var params SaveTemplateParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	tmpl := &template.Template{
		Name:        params.Name,
		Description: params.Description,
		Database:    params.Database,
		Query:       params.Query,
		Parameters:  params.Parameters,
	}

	if err := templates.Save(tmpl); err != nil {
		return nil, err
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"message":  "Template saved successfully",
		"id":       tmpl.ID,
		"name":     tmpl.Name,
		"database": tmpl.Database,
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// scheduleQuery creates a new scheduled query job.
func (s *Server) scheduleQuery(ctx context.Context, args json.RawMessage, sched *scheduler.Scheduler) (*ToolResult, error) {
	var params ScheduleQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	job := &scheduler.Job{
		Name:     params.Name,
		Database: params.Database,
		Query:    params.Query,
		Schedule: params.Schedule,
		Export:   params.Export,
	}

	if err := sched.AddJob(job); err != nil {
		return nil, err
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"message":  "Schedule created successfully",
		"id":       job.ID,
		"name":     job.Name,
		"schedule": job.Schedule,
		"interval": job.Interval.String(),
		"next_run": job.NextRun,
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// listSchedules returns all scheduled jobs.
func (s *Server) listSchedules(ctx context.Context, args json.RawMessage, sched *scheduler.Scheduler) (*ToolResult, error) {
	var params ListSchedulesParams
	json.Unmarshal(args, &params)

	jobs := sched.ListJobs(params.Database)

	data, _ := json.MarshalIndent(map[string]interface{}{
		"schedules": jobs,
		"count":     len(jobs),
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// cancelSchedule removes a scheduled job.
func (s *Server) cancelSchedule(ctx context.Context, args json.RawMessage, sched *scheduler.Scheduler) (*ToolResult, error) {
	var params CancelScheduleParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	if err := sched.RemoveJob(params.Schedule); err != nil {
		return nil, err
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"message":  "Schedule cancelled successfully",
		"schedule": params.Schedule,
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// cacheStats returns cache statistics.
func (s *Server) cacheStats(ctx context.Context, qCache *cache.Cache) (*ToolResult, error) {
	stats := qCache.Stats()
	data, _ := json.MarshalIndent(stats, "", "  ")
	return NewToolResult(string(data)), nil
}

// clearCache clears the query cache.
func (s *Server) clearCache(ctx context.Context, args json.RawMessage, qCache *cache.Cache) (*ToolResult, error) {
	var params ClearCacheParams
	json.Unmarshal(args, &params)

	if params.Database != "" {
		qCache.InvalidateDatabase(params.Database)
	} else {
		qCache.Clear()
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"message":  "Cache cleared successfully",
		"database": params.Database,
	}, "", "  ")
	return NewToolResult(string(data)), nil
}

// executeQueryWithCache wraps executeQuery with caching.
func (s *Server) executeQueryWithCache(ctx context.Context, args json.RawMessage, qCache *cache.Cache) (*ToolResult, error) {
	var params struct {
		Database string      `json:"database"`
		Query    QueryParams `json:"query"`
		UseCache *bool       `json:"use_cache,omitempty"`
		CacheTTL int         `json:"cache_ttl,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Check cache settings
	useCache := true
	if params.UseCache != nil {
		useCache = *params.UseCache
	}
	
	ttl := 5 * time.Minute
	if params.CacheTTL > 0 {
		ttl = time.Duration(params.CacheTTL) * time.Second
	}

	// Generate cache key
	cacheKey := cache.GenerateKey(params.Database, params.Query)

	// Try cache first
	if useCache {
		if cached, ok := qCache.Get(cacheKey); ok {
			// Return cached result with indicator
			if result, ok := cached.(*ToolResult); ok {
				return result, nil
			}
		}
	}

	// Execute query
	result, err := s.executeQuery(ctx, args)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if useCache {
		qCache.Set(cacheKey, result, ttl)
	}

	return result, nil
}

// Helper to convert Row to map
func rowToMap(row adapter.Row) map[string]interface{} {
	return map[string]interface{}(row)
}
