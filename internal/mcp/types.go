// Package mcp provides MCP (Model Context Protocol) types and server implementation.
package mcp

import (
	"encoding/json"
)

// JSON-RPC 2.0 types for MCP protocol

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

	// Custom error codes for MCP
	SchemaNotFound   = -32001
	TableNotFound    = -32002
	FieldDenied      = -32003
	QueryValidation  = -32004
	DatabaseError    = -32005
	StreamError      = -32006
	ExportError      = -32007
	ConnectionFailed = -32008
	Timeout          = -32009
	Cancelled        = -32010
)

// MCP Protocol types

// ServerInfo describes the MCP server capabilities.
type ServerInfo struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes what the server can do.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates the server supports tools.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates the server supports resources.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates the server supports prompts.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines the JSON Schema for tool input.
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]Property    `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
	AdditionalProperties bool         `json:"additionalProperties,omitempty"`
}

// Property defines a single property in the input schema.
type Property struct {
	Type        string      `json:"type,omitempty"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Items       *Property   `json:"items,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Required    []string    `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// Resource represents an MCP resource definition.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent represents the content of a resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded
}

// ResourceResponse represents a response to resources/read.
type ResourceResponse struct {
	Contents []ResourceContent `json:"contents"`
}

// ToolResult represents the result of a tool invocation.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem represents a piece of content in a tool result.
type ContentItem struct {
	Type     string `json:"type"` // text, image, resource
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // Base64 encoded for binary
	URI      string `json:"uri,omitempty"`
}

// Tool-specific parameter types

// DescribeDatabasesParams for describe_databases tool.
type DescribeDatabasesParams struct{}

// DescribeTablesParams for describe_tables tool.
type DescribeTablesParams struct {
	Database string `json:"database"`
}

// ExecuteQueryParams for execute_query tool.
type ExecuteQueryParams struct {
	Database string                 `json:"database"`
	Query    QueryParams            `json:"query"`
	Options  *QueryOptions          `json:"options,omitempty"`
}

// StreamQueryParams for stream_query tool.
type StreamQueryParams struct {
	Database  string        `json:"database"`
	Query     QueryParams   `json:"query"`
	BatchSize int           `json:"batch_size,omitempty"`
}

// ExportQueryParams for export_query tool.
type ExportQueryParams struct {
	Database string       `json:"database"`
	Query    QueryParams  `json:"query"`
	Format   string       `json:"format"` // csv, jsonl
	Filename string       `json:"filename,omitempty"`
}

// MultiSourceQueryParams for multi_source_query tool.
type MultiSourceQueryParams struct {
	Queries []SingleSourceQuery `json:"queries"`
	Merge   *MergeConfig        `json:"merge,omitempty"`
}

// SingleSourceQuery represents a query to a single database.
type SingleSourceQuery struct {
	Database string      `json:"database"`
	Query    QueryParams `json:"query"`
	Alias    string      `json:"alias,omitempty"`
}

// MergeConfig describes how to merge results from multiple sources.
type MergeConfig struct {
	Type      string   `json:"type"`      // union, join, lookup
	JoinKey   string   `json:"join_key,omitempty"`
	JoinType  string   `json:"join_type,omitempty"` // left, inner
}

// QueryParams represents query parameters from the client.
type QueryParams struct {
	Type         string            `json:"type,omitempty"` // select, aggregate, count
	Table        string            `json:"table"`
	Columns      []string          `json:"columns,omitempty"`
	Filters      []FilterParam     `json:"filters,omitempty"`
	Joins        []JoinParam       `json:"joins,omitempty"`
	OrderBy      []OrderByParam    `json:"order_by,omitempty"`
	GroupBy      []string          `json:"group_by,omitempty"`
	Aggregations []AggregationParam `json:"aggregations,omitempty"`
	Limit        int               `json:"limit,omitempty"`
	Offset       int               `json:"offset,omitempty"`
	Cursor       string            `json:"cursor,omitempty"`
	CursorColumn string            `json:"cursor_column,omitempty"`
	Raw          string            `json:"raw,omitempty"`
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

// QueryOptions represents additional query options.
type QueryOptions struct {
	Timeout int  `json:"timeout,omitempty"` // Seconds
	Explain bool `json:"explain,omitempty"` // Return query plan
}

// Response types

// DatabasesResponse for describe_databases result.
type DatabasesResponse struct {
	Databases []DatabaseInfo `json:"databases"`
}

// DatabaseInfo describes a connected database.
type DatabaseInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"` // connected, disconnected, error
	Tables int    `json:"tables,omitempty"`
	Collections int `json:"collections,omitempty"`
}

// TablesResponse for describe_tables result.
type TablesResponse struct {
	Database    string      `json:"database"`
	Tables      []TableInfo `json:"tables,omitempty"`
	Collections []CollectionInfo `json:"collections,omitempty"`
}

// TableInfo describes a table.
type TableInfo struct {
	Name       string       `json:"name"`
	Schema     string       `json:"schema,omitempty"`
	Columns    []ColumnInfo `json:"columns"`
	RowCount   int64        `json:"row_count,omitempty"`
	PrimaryKey []string     `json:"primary_key,omitempty"`
	HasRelations bool       `json:"has_relations"`
}

// ColumnInfo describes a column.
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// CollectionInfo describes a MongoDB collection.
type CollectionInfo struct {
	Name     string      `json:"name"`
	Fields   []FieldInfo `json:"fields"`
	DocCount int64       `json:"doc_count,omitempty"`
}

// FieldInfo describes a MongoDB field.
type FieldInfo struct {
	Name   string   `json:"name"`
	Types  []string `json:"types"`
	IsArray bool    `json:"is_array,omitempty"`
}

// QueryResponse for execute_query result.
type QueryResponse struct {
	Columns       []string                 `json:"columns"`
	Rows          []map[string]interface{} `json:"rows"`
	RowCount      int                      `json:"row_count"`
	TotalCount    int64                    `json:"total_count,omitempty"`
	NextCursor    string                   `json:"next_cursor,omitempty"`
	HasMore       bool                     `json:"has_more"`
	ExecutionTime string                   `json:"execution_time"`
}

// StreamBatch represents a batch of streamed rows.
type StreamBatch struct {
	Columns  []string                 `json:"columns,omitempty"`
	Rows     []map[string]interface{} `json:"rows"`
	BatchNum int                      `json:"batch_num"`
	Done     bool                     `json:"done"`
}

// ExportResponse for export_query result.
type ExportResponse struct {
	FilePath  string `json:"file_path"`
	RowCount  int64  `json:"row_count"`
	FileSize  int64  `json:"file_size"`
	Format    string `json:"format"`
}

// MultiQueryResponse for multi_source_query result.
type MultiQueryResponse struct {
	Results  map[string]*QueryResponse `json:"results"`
	Merged   *QueryResponse            `json:"merged,omitempty"`
}

// NewError creates a new JSON-RPC error.
func NewError(code int, message string, data interface{}) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// NewResponse creates a successful JSON-RPC response.
func NewResponse(id interface{}, result interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse creates an error JSON-RPC response.
func NewErrorResponse(id interface{}, err *Error) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   err,
	}
}

// NewToolResult creates a text tool result.
func NewToolResult(text string) *ToolResult {
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: text}},
	}
}

// NewErrorToolResult creates an error tool result.
func NewErrorToolResult(errMsg string) *ToolResult {
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: errMsg}},
		IsError: true,
	}
}
