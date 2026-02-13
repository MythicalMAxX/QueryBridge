// Package mcp provides the MCP tool definitions.
package mcp

// GetTools returns all available MCP tools.
func GetTools() []Tool {
	return []Tool{
		{
			Name:        "describe_databases",
			Description: "List all connected databases with their types and connection status",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "describe_tables",
			Description: "List tables or collections in a database with their schemas (columns, types, relationships). Sensitive fields are excluded.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Database name to describe"},
				},
				Required: []string{"database"},
			},
		},
		{
			Name:        "execute_query",
			Description: "Execute a query and return results. Use for small result sets (<1000 rows). Supports filters, joins, aggregations, sorting, and pagination.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Target database name"},
					"query": {
						Type:        "object",
						Description: "Query specification",
						Properties: map[string]Property{
							"table":   {Type: "string", Description: "Table or collection name"},
							"columns": {Type: "array", Items: &Property{Type: "string"}, Description: "Columns to select (empty = all safe columns)"},
							"filters": {Type: "array", Description: "Filter conditions [{column, operator, value}]"},
							"order_by": {Type: "array", Description: "Sort order [{column, direction}]"},
							"limit":   {Type: "integer", Description: "Maximum rows to return"},
							"offset":  {Type: "integer", Description: "Rows to skip"},
						},
						Required: []string{"table"},
					},
					"use_cache": {Type: "boolean", Description: "Use cached results if available (default: true)"},
					"cache_ttl": {Type: "integer", Description: "Cache TTL in seconds (default: 300)"},
				},
				Required: []string{"database", "query"},
			},
		},
		{
			Name:        "stream_query",
			Description: "Stream query results in batches. Use for large result sets to avoid memory issues.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database":   {Type: "string", Description: "Target database name"},
					"query":      {Type: "object", Description: "Query specification (same as execute_query)"},
					"batch_size": {Type: "integer", Description: "Rows per batch (default: 1000)"},
				},
				Required: []string{"database", "query"},
			},
		},
		{
			Name:        "export_query",
			Description: "Export query results to a file (CSV, JSONL, Excel, or Parquet). Returns file path when complete.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Target database name"},
					"query":    {Type: "object", Description: "Query specification"},
					"format":   {Type: "string", Enum: []string{"csv", "jsonl", "excel", "parquet"}, Description: "Export format"},
					"filename": {Type: "string", Description: "Output filename (optional)"},
				},
				Required: []string{"database", "query", "format"},
			},
		},
		{
			Name:        "multi_source_query",
			Description: "Execute queries across multiple databases. Results can be returned separately or merged.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"queries": {
						Type:        "array",
						Description: "Array of queries, each with {database, query, alias}",
					},
					"merge": {
						Type:        "object",
						Description: "Optional merge config {type: 'union'|'join', join_key, join_type}",
					},
				},
				Required: []string{"queries"},
			},
		},
		{
			Name:        "explain_query",
			Description: "Show the execution plan for a query. Helps understand query performance and optimization opportunities. Supported for SQL databases (PostgreSQL, MySQL, SQLite, SQL Server).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Target database name"},
					"query": {
						Type:        "object",
						Description: "Query specification",
						Properties: map[string]Property{
							"table":   {Type: "string", Description: "Table or collection name"},
							"columns": {Type: "array", Items: &Property{Type: "string"}, Description: "Columns to select"},
							"filters": {Type: "array", Description: "Filter conditions"},
							"order_by": {Type: "array", Description: "Sort order"},
							"limit":   {Type: "integer", Description: "Maximum rows"},
							"raw":     {Type: "string", Description: "Raw SQL query to explain"},
						},
					},
				},
				Required: []string{"database"},
			},
		},
		// Sprint 4 Tools
		{
			Name:        "cross_db_join",
			Description: "Join results from two different databases on a common key. Supports inner, left, and right joins.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"left_query": {
						Type:        "object",
						Description: "First query {database, query, alias}",
					},
					"right_query": {
						Type:        "object",
						Description: "Second query {database, query, alias}",
					},
					"join_type": {Type: "string", Enum: []string{"inner", "left", "right"}, Description: "Type of join (default: inner)"},
					"left_key":  {Type: "string", Description: "Column to join on from left query"},
					"right_key": {Type: "string", Description: "Column to join on from right query"},
				},
				Required: []string{"left_query", "right_query", "left_key", "right_key"},
			},
		},
		{
			Name:        "list_templates",
			Description: "List saved query templates. Optionally filter by database.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Filter templates by database (optional)"},
				},
			},
		},
		{
			Name:        "execute_template",
			Description: "Execute a saved query template with parameter values.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"template": {Type: "string", Description: "Template name or ID"},
					"params":   {Type: "object", Description: "Parameter values {param_name: value}"},
				},
				Required: []string{"template"},
			},
		},
		{
			Name:        "save_template",
			Description: "Save a query as a reusable template with optional parameters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":        {Type: "string", Description: "Template name"},
					"description": {Type: "string", Description: "Template description"},
					"database":    {Type: "string", Description: "Target database"},
					"query":       {Type: "object", Description: "Query specification (use {{param}} for placeholders)"},
					"parameters": {
						Type:        "array",
						Description: "Parameter definitions [{name, type, required, default, description}]",
					},
				},
				Required: []string{"name", "database", "query"},
			},
		},
		{
			Name:        "schedule_query",
			Description: "Schedule a query to run periodically. Results can be exported automatically.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":     {Type: "string", Description: "Schedule name"},
					"database": {Type: "string", Description: "Target database"},
					"query":    {Type: "object", Description: "Query specification"},
					"schedule": {Type: "string", Description: "Interval: '1h', '30m', 'daily', 'hourly', etc."},
					"export": {
						Type:        "object",
						Description: "Optional export config {format, path, filename}",
					},
				},
				Required: []string{"name", "database", "query", "schedule"},
			},
		},
		{
			Name:        "list_schedules",
			Description: "List all scheduled query jobs.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Filter by database (optional)"},
				},
			},
		},
		{
			Name:        "cancel_schedule",
			Description: "Cancel a scheduled query job.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"schedule": {Type: "string", Description: "Schedule name or ID"},
				},
				Required: []string{"schedule"},
			},
		},
		{
			Name:        "cache_stats",
			Description: "Get cache statistics including hit rate, size, and entries.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "clear_cache",
			Description: "Clear the query result cache.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Clear cache for specific database only (optional)"},
				},
			},
		},
		{
			Name:        "annotate_schema",
			Description: "Add natural language descriptions or tags to databases, tables, or columns to improve AI understanding.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database":    {Type: "string", Description: "Database name"},
					"table":       {Type: "string", Description: "Table name"},
					"column":      {Type: "string", Description: "Column name (optional)"},
					"description": {Type: "string", Description: "Natural language description or tags"},
				},
				Required: []string{"database", "table", "description"},
			},
		},
		{
			Name:        "register_resource",
			Description: "Expose a specific database query as a native MCP resource.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"uri":         {Type: "string", Description: "Unique resource URI (e.g., db://summary)"},
					"name":        {Type: "string", Description: "Display name for the resource"},
					"description": {Type: "string", Description: "Optional description"},
					"mimeType":    {Type: "string", Description: "MIME type (e.g., text/markdown, application/json)"},
					"database":    {Type: "string", Description: "Database name"},
					"query":       {Type: "object", Description: "Query definition to back this resource"},
				},
				Required: []string{"uri", "name", "database", "query"},
			},
		},
		{
			Name:        "analyze_performance",
			Description: "Analyze a query's execution plan and provide AI-powered optimization suggestions (indexes, refactors).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"database": {Type: "string", Description: "Database name"},
					"query":    {Type: "object", Description: "Query definition to analyze"},
				},
				Required: []string{"database", "query"},
			},
		},
		{
			Name:        "manage_rbac",
			Description: "View active roles and permissions for security auditing.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "view_audit_logs",
			Description: "Explore recent system activity and database operations from the audit log.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"limit": {Type: "integer", Description: "Number of entries to return (default 50)"},
					"query": {Type: "string", Description: "Optional filter keyword"},
				},
			},
		},
	}
}

