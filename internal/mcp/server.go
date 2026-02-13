// Package mcp provides the MCP server and request handling.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/audit"
	"github.com/MythicalMAxX/QueryBridge/internal/cache"
	"github.com/MythicalMAxX/QueryBridge/internal/query"
	"github.com/MythicalMAxX/QueryBridge/internal/scheduler"
	"github.com/MythicalMAxX/QueryBridge/internal/schema"
	"github.com/MythicalMAxX/QueryBridge/internal/stream"
	"github.com/MythicalMAxX/QueryBridge/internal/template"
)

// Server is the MCP protocol server.
type Server struct {
	info      ServerInfo
	registry  *schema.Registry
	adapters  map[string]adapter.Adapter
	planner   *query.Planner
	exporter  *stream.Exporter
	validator *query.Validator
	auditor   *audit.Logger
	// Sprint 4 components
	cache     *cache.Cache
	templates *template.Store
	scheduler *scheduler.Scheduler
	resources map[string]ResourceEntry // keyed by URI
	mu        sync.RWMutex
}

type ResourceEntry struct {
	Resource
	Database string
	Query    QueryParams
}

// NewServer creates a new MCP server.
// If readOnly is true, all write operations (INSERT, UPDATE, DELETE) will be blocked.
func NewServer(registry *schema.Registry, adapters map[string]adapter.Adapter, exportDir string, readOnly bool, auditor *audit.Logger) *Server {
	if auditor == nil {
		auditor = audit.DisabledLogger()
	}

	// Initialize Sprint 4 components
	queryCache := cache.New(cache.DefaultConfig())
	templateStore, _ := template.NewStore(exportDir + "/templates.json")

	// Create server first
	srv := &Server{
		info: ServerInfo{
			Name:    "QueryBridge",
			Version: "2.0.0",
			Capabilities: ServerCapabilities{
				Tools:     &ToolsCapability{ListChanged: false},
				Resources: &ResourcesCapability{ListChanged: true},
			},
		},
		registry:  registry,
		adapters:  adapters,
		planner:   query.NewPlanner(registry, adapters),
		exporter:  stream.NewExporter(exportDir),
		validator: query.NewValidator(readOnly),
		auditor:   auditor,
		cache:     queryCache,
		templates: templateStore,
		resources: make(map[string]ResourceEntry),
	}

	// Initialize scheduler with query executor
	srv.scheduler = scheduler.New(func(ctx context.Context, database string, queryMap map[string]interface{}) (interface{}, error) {
		queryJSON, _ := json.Marshal(map[string]interface{}{
			"database": database,
			"query":    queryMap,
		})
		return srv.executeQuery(ctx, queryJSON)
	})

	return srv
}

// ServeStdio runs the MCP server on stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := NewErrorResponse(nil, NewError(ParseError, "Parse error", err.Error()))
			encoder.Encode(resp)
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "resources/list":
		return s.handleListResources(req)
	case "resources/read":
		return s.handleReadResource(ctx, req)
	default:
		return NewErrorResponse(req.ID, NewError(MethodNotFound, "Method not found", req.Method))
	}
}

func (s *Server) handleInitialize(req *Request) *Response {
	return NewResponse(req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo":      s.info,
		"capabilities":    s.info.Capabilities,
	})
}

func (s *Server) handleListResources(req *Request) *Response {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var resources []Resource
	for _, entry := range s.resources {
		resources = append(resources, entry.Resource)
	}

	return NewResponse(req.ID, map[string]interface{}{"resources": resources})
}

func (s *Server) handleReadResource(ctx context.Context, req *Request) *Response {
	var params struct {
		URI string `json:"uri"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, NewError(InvalidParams, "Invalid params", err.Error()))
	}

	s.mu.RLock()
	entry, ok := s.resources[params.URI]
	s.mu.RUnlock()

	if !ok {
		return NewErrorResponse(req.ID, NewError(MethodNotFound, "Resource not found", params.URI))
	}

	// Execute the query associated with this resource
	args, _ := json.Marshal(ExecuteQueryParams{
		Database: entry.Database,
		Query:    entry.Query,
	})

	result, err := s.executeQuery(ctx, args)
	if err != nil {
		return NewErrorResponse(req.ID, NewError(InternalError, "Failed to read resource", err.Error()))
	}

	// Extract text from tool result for resource content
	var text string
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}

	return NewResponse(req.ID, ResourceResponse{
		Contents: []ResourceContent{
			{
				URI:      entry.URI,
				MimeType: entry.MimeType,
				Text:     text,
			},
		},
	})
}

func (s *Server) handleListTools(req *Request) *Response {
	return NewResponse(req.ID, map[string]interface{}{"tools": GetTools()})
}

func (s *Server) handleCallTool(ctx context.Context, req *Request) *Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, NewError(InvalidParams, "Invalid params", err.Error()))
	}

	var result *ToolResult
	var err error

	switch params.Name {
	case "describe_databases":
		result, err = s.describeDatabases(ctx)
	case "describe_tables":
		result, err = s.describeTables(ctx, params.Arguments)
	case "execute_query":
		result, err = s.executeQueryWithCache(ctx, params.Arguments, s.cache)
	case "stream_query":
		result, err = s.streamQuery(ctx, params.Arguments)
	case "export_query":
		result, err = s.exportQuery(ctx, params.Arguments)
	case "multi_source_query":
		result, err = s.multiSourceQuery(ctx, params.Arguments)
	case "explain_query":
		result, err = s.explainQuery(ctx, params.Arguments)
	// Sprint 4 tools
	case "cross_db_join":
		result, err = s.crossDBJoin(ctx, params.Arguments, nil)
	case "list_templates":
		result, err = s.listTemplates(ctx, params.Arguments, s.templates)
	case "execute_template":
		result, err = s.executeTemplate(ctx, params.Arguments, s.templates)
	case "save_template":
		result, err = s.saveTemplate(ctx, params.Arguments, s.templates)
	case "schedule_query":
		result, err = s.scheduleQuery(ctx, params.Arguments, s.scheduler)
	case "list_schedules":
		result, err = s.listSchedules(ctx, params.Arguments, s.scheduler)
	case "cancel_schedule":
		result, err = s.cancelSchedule(ctx, params.Arguments, s.scheduler)
	case "cache_stats":
		result, err = s.cacheStats(ctx, s.cache)
	case "clear_cache":
		result, err = s.clearCache(ctx, params.Arguments, s.cache)
	case "annotate_schema":
		result, err = s.annotateSchema(ctx, params.Arguments, s.registry.meta)
	case "register_resource":
		result, err = s.registerResource(ctx, params.Arguments)
	case "analyze_performance":
		result, err = s.analyzePerformance(ctx, params.Arguments)
	case "manage_rbac":
		result, err = s.manageRBAC(ctx)
	case "view_audit_logs":
		result, err = s.viewAuditLogs(ctx, params.Arguments)
	default:
		return NewErrorResponse(req.ID, NewError(MethodNotFound, "Unknown tool", params.Name))
	}

	if err != nil {
		return NewResponse(req.ID, NewErrorToolResult(err.Error()))
	}
	return NewResponse(req.ID, result)
}

func (s *Server) describeDatabases(ctx context.Context) (*ToolResult, error) {
	var databases []DatabaseInfo

	for name, a := range s.adapters {
		status := "connected"
		if err := a.Ping(ctx); err != nil {
			status = "error"
		}

		info := DatabaseInfo{Name: name, Type: a.Type(), Status: status}

		if schema, ok := s.registry.Get(name); ok {
			info.Tables = len(schema.Tables)
			info.Collections = len(schema.Collections)
		}

		databases = append(databases, info)
	}

	data, _ := json.MarshalIndent(DatabasesResponse{Databases: databases}, "", "  ")
	return NewToolResult(string(data)), nil
}

func (s *Server) describeTables(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params DescribeTablesParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	schema, err := s.registry.GetSafeSchema(params.Database)
	if err != nil {
		return nil, err
	}

	resp := TablesResponse{Database: params.Database}

	for _, table := range schema.Tables {
		info := TableInfo{
			Name: table.Name, Schema: table.Schema,
			RowCount: table.RowCount, PrimaryKey: table.PrimaryKey,
			HasRelations: len(table.ForeignKeys) > 0,
		}
		for _, col := range table.Columns {
			info.Columns = append(info.Columns, ColumnInfo{
				Name: col.Name, Type: col.Type, Nullable: col.Nullable,
			})
		}
		resp.Tables = append(resp.Tables, info)
	}

	for _, coll := range schema.Collections {
		info := CollectionInfo{Name: coll.Name, DocCount: coll.DocCount}
		for _, f := range coll.Fields {
			info.Fields = append(info.Fields, FieldInfo{
				Name: f.Name, Types: f.Types, IsArray: f.IsArray,
			})
		}
		resp.Collections = append(resp.Collections, info)
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

// convertQueryParams converts MCP QueryParams to query.QueryParams
func convertQueryParams(p *QueryParams) *query.QueryParams {
	qp := &query.QueryParams{
		Type:         p.Type,
		Table:        p.Table,
		Columns:      p.Columns,
		GroupBy:      p.GroupBy,
		Limit:        p.Limit,
		Offset:       p.Offset,
		Cursor:       p.Cursor,
		CursorColumn: p.CursorColumn,
		Raw:          p.Raw,
	}

	for _, f := range p.Filters {
		qp.Filters = append(qp.Filters, query.FilterParam{
			Column: f.Column, Operator: f.Operator, Value: f.Value, Logic: f.Logic,
		})
	}

	for _, j := range p.Joins {
		jp := query.JoinParam{Type: j.Type, Table: j.Table, Alias: j.Alias}
		for _, c := range j.Conditions {
			jp.Conditions = append(jp.Conditions, query.FilterParam{
				Column: c.Column, Operator: c.Operator, Value: c.Value,
			})
		}
		qp.Joins = append(qp.Joins, jp)
	}

	for _, ob := range p.OrderBy {
		qp.OrderBy = append(qp.OrderBy, query.OrderByParam{
			Column: ob.Column, Direction: ob.Direction,
		})
	}

	for _, a := range p.Aggregations {
		qp.Aggregations = append(qp.Aggregations, query.AggregationParam{
			Function: a.Function, Column: a.Column, Alias: a.Alias,
		})
	}

	return qp
}

func (s *Server) executeQuery(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params ExecuteQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Validate raw queries in read-only mode
	if params.Query.Raw != "" {
		if err := s.validator.ValidateRawQuery(params.Query.Raw); err != nil {
			return nil, err
		}
	} else if s.validator.IsReadOnly() {
		// For structured queries, only SELECT (type not specified) is allowed in read-only mode
		if params.Query.Type != "" && params.Query.Type != "select" {
			return nil, query.ErrWriteNotAllowed
		}
	}

	timeout := 30 * time.Second
	if params.Options != nil && params.Options.Timeout > 0 {
		timeout = time.Duration(params.Options.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	qp := convertQueryParams(&params.Query)
	q, err := s.planner.Plan(params.Database, qp)
	if err != nil {
		return nil, err
	}

	result, err := s.planner.Execute(ctx, params.Database, q)
	if err != nil {
		return nil, err
	}

	resp := QueryResponse{
		Columns:       result.Columns,
		Rows:          make([]map[string]interface{}, 0, len(result.Rows)),
		RowCount:      result.RowCount,
		TotalCount:    result.TotalCount,
		NextCursor:    result.NextCursor,
		HasMore:       result.HasMore,
		ExecutionTime: result.ExecutionTime.String(),
	}

	for _, row := range result.Rows {
		safeRow := s.planner.StripDeniedFields(row)
		resp.Rows = append(resp.Rows, safeRow)
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

func (s *Server) streamQuery(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params StreamQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	qp := convertQueryParams(&params.Query)
	q, err := s.planner.Plan(params.Database, qp)
	if err != nil {
		return nil, err
	}

	rowStream, err := s.planner.Stream(ctx, params.Database, q)
	if err != nil {
		return nil, err
	}
	defer rowStream.Close()

	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	batcher := stream.NewBatchStreamer(rowStream, batchSize)

	var allRows []map[string]interface{}
	for {
		batch, hasMore, err := batcher.NextBatch()
		if err != nil {
			return nil, err
		}
		for _, row := range batch {
			allRows = append(allRows, s.planner.StripDeniedFields(row))
		}
		if !hasMore {
			break
		}
	}

	resp := QueryResponse{Rows: allRows, RowCount: len(allRows)}
	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

func (s *Server) exportQuery(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params ExportQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	qp := convertQueryParams(&params.Query)
	q, err := s.planner.Plan(params.Database, qp)
	if err != nil {
		return nil, err
	}

	rowStream, err := s.planner.Stream(ctx, params.Database, q)
	if err != nil {
		return nil, err
	}
	defer rowStream.Close()

	var result *stream.ExportResult
	switch params.Format {
	case "csv":
		result, err = s.exporter.ExportCSV(ctx, rowStream, params.Filename)
	case "jsonl":
		result, err = s.exporter.ExportJSONL(ctx, rowStream, params.Filename)
	case "excel":
		result, err = s.exporter.ExportExcel(ctx, rowStream, params.Filename)
	case "parquet":
		result, err = s.exporter.ExportParquet(ctx, rowStream, params.Filename)
	default:
		return nil, fmt.Errorf("unsupported format: %s (supported: csv, jsonl, excel, parquet)", params.Format)
	}

	if err != nil {
		return nil, err
	}

	resp := ExportResponse{
		FilePath: result.FilePath,
		RowCount: result.RowCount,
		FileSize: result.FileSize,
		Format:   result.Format,
	}
	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

func (s *Server) multiSourceQuery(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params MultiSourceQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	results := make(map[string]*QueryResponse)

	for _, sq := range params.Queries {
		qp := convertQueryParams(&sq.Query)
		q, err := s.planner.Plan(sq.Database, qp)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", sq.Alias, err)
		}

		result, err := s.planner.Execute(ctx, sq.Database, q)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", sq.Alias, err)
		}

		alias := sq.Alias
		if alias == "" {
			alias = sq.Database
		}

		resp := &QueryResponse{
			Columns:  result.Columns,
			RowCount: result.RowCount,
		}
		for _, row := range result.Rows {
			resp.Rows = append(resp.Rows, s.planner.StripDeniedFields(row))
		}
		results[alias] = resp
	}

	resp := MultiQueryResponse{Results: results}
	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}

// CallToolDirect invokes a tool directly without JSON-RPC encoding.
// This is used for library integration where no subprocess is involved.
func (s *Server) CallToolDirect(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", err
	}

	var result *ToolResult

	switch name {
	case "describe_databases":
		result, err = s.describeDatabases(ctx)
	case "describe_tables":
		result, err = s.describeTables(ctx, argsJSON)
	case "execute_query":
		result, err = s.executeQueryWithCache(ctx, argsJSON, s.cache)
	case "stream_query":
		result, err = s.streamQuery(ctx, argsJSON)
	case "export_query":
		result, err = s.exportQuery(ctx, argsJSON)
	case "multi_source_query":
		result, err = s.multiSourceQuery(ctx, argsJSON)
	case "explain_query":
		result, err = s.explainQuery(ctx, argsJSON)
	// Sprint 4 tools
	case "cross_db_join":
		result, err = s.crossDBJoin(ctx, argsJSON, nil)
	case "list_templates":
		result, err = s.listTemplates(ctx, argsJSON, s.templates)
	case "execute_template":
		result, err = s.executeTemplate(ctx, argsJSON, s.templates)
	case "save_template":
		result, err = s.saveTemplate(ctx, argsJSON, s.templates)
	case "schedule_query":
		result, err = s.scheduleQuery(ctx, argsJSON, s.scheduler)
	case "list_schedules":
		result, err = s.listSchedules(ctx, argsJSON, s.scheduler)
	case "cancel_schedule":
		result, err = s.cancelSchedule(ctx, argsJSON, s.scheduler)
	case "cache_stats":
		result, err = s.cacheStats(ctx, s.cache)
	case "clear_cache":
		result, err = s.clearCache(ctx, argsJSON, s.cache)
	case "annotate_schema":
		result, err = s.annotateSchema(ctx, argsJSON, s.registry.meta)
	case "register_resource":
		result, err = s.registerResource(ctx, argsJSON)
	case "analyze_performance":
		result, err = s.analyzePerformance(ctx, argsJSON)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	if err != nil {
		return "", err
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}
	return "", nil
}

// ExplainQueryParams holds parameters for the explain_query tool.
type ExplainQueryParams struct {
	Database string      `json:"database"`
	Query    QueryParams `json:"query"`
}

// ExplainResponse holds the execution plan response.
type ExplainResponse struct {
	Database string        `json:"database"`
	Plan     []ExplainStep `json:"plan"`
	RawPlan  string        `json:"raw_plan,omitempty"`
	Query    string        `json:"query,omitempty"`
}

// ExplainStep represents a step in the execution plan.
type ExplainStep struct {
	ID            int     `json:"id,omitempty"`
	Operation     string  `json:"operation"`
	Object        string  `json:"object,omitempty"`
	EstimatedRows float64 `json:"estimated_rows,omitempty"`
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
	ActualRows    int     `json:"actual_rows,omitempty"`
	ActualTime    float64 `json:"actual_time_ms,omitempty"`
	Extra         string  `json:"extra,omitempty"`
}

func (s *Server) explainQuery(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var params ExplainQueryParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Get the adapter for this database
	a, ok := s.adapters[params.Database]
	if !ok {
		return nil, fmt.Errorf("database not found: %s", params.Database)
	}

	// Build the query to explain
	qp := convertQueryParams(&params.Query)
	q, err := s.planner.Plan(params.Database, qp)
	if err != nil {
		return nil, err
	}

	// Check if adapter supports explain
	explainer, ok := a.(interface {
		Explain(ctx context.Context, q *adapter.Query) (string, error)
	})
	if !ok {
		// For adapters that don't support explain, return a message
		resp := ExplainResponse{
			Database: params.Database,
			RawPlan:  fmt.Sprintf("EXPLAIN not supported for database type: %s", a.Type()),
		}
		data, _ := json.MarshalIndent(resp, "", "  ")
		return NewToolResult(string(data)), nil
	}

	// Get the explain plan
	rawPlan, err := explainer.Explain(ctx, q)
	if err != nil {
		return nil, err
	}

	resp := ExplainResponse{
		Database: params.Database,
		RawPlan:  rawPlan,
		Query:    q.Raw,
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	return NewToolResult(string(data)), nil
}
