package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/history"
)

// DatabaseInfo contains information about a connected database.
type DatabaseInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Tables int    `json:"tables,omitempty"`
}

// handleListDatabases returns all connected databases.
func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	result, err := s.bridge.CallTool("describe_databases", nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse the result and return as proper JSON
	var data interface{}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		// Return raw string if not valid JSON
		s.writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Data:    result,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

// handleDescribeTables returns tables for a specific database.
func (s *Server) handleDescribeTables(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("name")
	if dbName == "" {
		s.writeError(w, http.StatusBadRequest, "database name is required")
		return
	}

	result, err := s.bridge.CallTool("describe_tables", map[string]interface{}{
		"database": dbName,
	})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var data interface{}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		s.writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Data:    result,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

// QueryRequest represents a query execution request.
type QueryRequest struct {
	Database     string                   `json:"database"`
	Table        string                   `json:"table,omitempty"`
	Columns      []string                 `json:"columns,omitempty"`
	Filters      []map[string]interface{} `json:"filters,omitempty"`
	OrderBy      []map[string]interface{} `json:"order_by,omitempty"`
	GroupBy      []string                 `json:"group_by,omitempty"`
	Aggregations []map[string]interface{} `json:"aggregations,omitempty"`
	Joins        []map[string]interface{} `json:"joins,omitempty"`
	Limit        int                      `json:"limit,omitempty"`
	Offset       int                      `json:"offset,omitempty"`
	Raw          string                   `json:"raw,omitempty"`
}

// handleExecuteQuery executes a query and returns results.
func (s *Server) handleExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Database == "" {
		s.writeError(w, http.StatusBadRequest, "database is required")
		return
	}

	// Build args for MCP tool - query params need to be nested
	queryParams := make(map[string]interface{})
	if req.Table != "" {
		queryParams["table"] = req.Table
	}
	if len(req.Columns) > 0 {
		queryParams["columns"] = req.Columns
	}
	if len(req.Filters) > 0 {
		queryParams["filters"] = req.Filters
	}
	if len(req.OrderBy) > 0 {
		queryParams["order_by"] = req.OrderBy
	}
	if len(req.GroupBy) > 0 {
		queryParams["group_by"] = req.GroupBy
	}
	if len(req.Aggregations) > 0 {
		queryParams["aggregations"] = req.Aggregations
	}
	if len(req.Joins) > 0 {
		queryParams["joins"] = req.Joins
	}
	if req.Limit > 0 {
		queryParams["limit"] = req.Limit
	}
	if req.Offset > 0 {
		queryParams["offset"] = req.Offset
	}
	if req.Raw != "" {
		queryParams["raw"] = req.Raw
	}

	args := map[string]interface{}{
		"database": req.Database,
		"query":    queryParams,
	}

	start := time.Now()
	result, err := s.bridge.CallTool("execute_query", args)
	duration := time.Since(start)

	// Record in history
	queryStr := req.Raw
	if queryStr == "" {
		queryStr = fmt.Sprintf("SELECT FROM %s", req.Table)
	}
	
	historyEntry := history.Entry{
		Timestamp: time.Now(),
		Database:  req.Database,
		Query:     queryStr,
		Tool:      "execute_query",
		Duration:  duration.Milliseconds(),
		Success:   err == nil,
	}

	if err != nil {
		historyEntry.Error = err.Error()
		s.history.Record(historyEntry)
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Try to parse result to get row count
	var resultData struct {
		Rows     []interface{} `json:"rows"`
		RowCount int           `json:"row_count"`
	}
	if json.Unmarshal([]byte(result), &resultData) == nil {
		if resultData.RowCount > 0 {
			historyEntry.RowCount = resultData.RowCount
		} else {
			historyEntry.RowCount = len(resultData.Rows)
		}
	}
	s.history.Record(historyEntry)

	var data interface{}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		s.writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Data:    result,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

// handleStreamQuery streams query results using Server-Sent Events.
func (s *Server) handleStreamQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Database == "" {
		s.writeError(w, http.StatusBadRequest, "database is required")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Build args
	args := map[string]interface{}{
		"database": req.Database,
	}
	if req.Table != "" {
		args["table"] = req.Table
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Raw != "" {
		args["raw"] = req.Raw
	}

	result, err := s.bridge.CallTool("stream_query", args)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Parse and stream batches
	var streamData struct {
		Batches []interface{} `json:"batches"`
		Rows    []interface{} `json:"rows"`
	}
	if json.Unmarshal([]byte(result), &streamData) == nil {
		if len(streamData.Batches) > 0 {
			for i, batch := range streamData.Batches {
				data, _ := json.Marshal(batch)
				fmt.Fprintf(w, "event: batch\ndata: %s\n\n", data)
				flusher.Flush()
				if i < len(streamData.Batches)-1 {
					time.Sleep(10 * time.Millisecond)
				}
			}
		} else if len(streamData.Rows) > 0 {
			data, _ := json.Marshal(streamData.Rows)
			fmt.Fprintf(w, "event: data\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "event: done\ndata: complete\n\n")
	flusher.Flush()
}

// ExportRequest represents an export request.
type ExportRequest struct {
	Database string `json:"database"`
	Table    string `json:"table,omitempty"`
	Raw      string `json:"raw,omitempty"`
	Format   string `json:"format"` // csv, jsonl, excel, parquet
	Filename string `json:"filename,omitempty"`
}

// handleExportQuery exports query results to a file.
func (s *Server) handleExportQuery(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Database == "" {
		s.writeError(w, http.StatusBadRequest, "database is required")
		return
	}
	if req.Format == "" {
		req.Format = "csv"
	}

	// Validate format
	validFormats := map[string]bool{"csv": true, "jsonl": true, "excel": true, "parquet": true}
	if !validFormats[strings.ToLower(req.Format)] {
		s.writeError(w, http.StatusBadRequest, "invalid format, use: csv, jsonl, excel, or parquet")
		return
	}

	args := map[string]interface{}{
		"database": req.Database,
		"format":   req.Format,
	}
	if req.Table != "" {
		args["table"] = req.Table
	}
	if req.Raw != "" {
		args["raw"] = req.Raw
	}
	if req.Filename != "" {
		args["filename"] = req.Filename
	}

	start := time.Now()
	result, err := s.bridge.CallTool("export_query", args)
	duration := time.Since(start)

	// Record in history
	historyEntry := history.Entry{
		Timestamp: time.Now(),
		Database:  req.Database,
		Query:     fmt.Sprintf("EXPORT %s TO %s", req.Table, req.Format),
		Tool:      "export_query",
		Duration:  duration.Milliseconds(),
		Success:   err == nil,
	}

	if err != nil {
		historyEntry.Error = err.Error()
		s.history.Record(historyEntry)
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse result to get filename and add download URL
	var exportResult map[string]interface{}
	if json.Unmarshal([]byte(result), &exportResult) == nil {
		if filename, ok := exportResult["filename"].(string); ok {
			exportResult["download_url"] = "/api/exports/" + filepath.Base(filename)
		}
		if rowCount, ok := exportResult["row_count"].(float64); ok {
			historyEntry.RowCount = int(rowCount)
		}
	}
	s.history.Record(historyEntry)

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    exportResult,
	})
}

// handleDownloadExport serves an exported file for download.
func (s *Server) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if filename == "" {
		s.writeError(w, http.StatusBadRequest, "filename is required")
		return
	}

	// Security: sanitize filename
	filename = filepath.Base(filename)
	filePath := filepath.Join(s.exportDir, filename)

	// Verify file exists and is within export directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	absExportDir, _ := filepath.Abs(s.exportDir)
	if !strings.HasPrefix(absPath, absExportDir) {
		s.writeError(w, http.StatusForbidden, "access denied")
		return
	}

	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		s.writeError(w, http.StatusNotFound, "file not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()

	// Set appropriate content type
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".csv":
		w.Header().Set("Content-Type", "text/csv")
	case ".jsonl":
		w.Header().Set("Content-Type", "application/x-ndjson")
	case ".xlsx":
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	case ".parquet":
		w.Header().Set("Content-Type", "application/octet-stream")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	
	io.Copy(w, file)
}

// handleGetHistory returns query history.
func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	opts := history.ListOptions{
		Limit: 100,
	}

	// Parse query params
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}
	if db := r.URL.Query().Get("database"); db != "" {
		opts.Database = db
	}
	if tool := r.URL.Query().Get("tool"); tool != "" {
		opts.Tool = tool
	}
	if successStr := r.URL.Query().Get("success"); successStr != "" {
		success := successStr == "true"
		opts.Success = &success
	}

	entries, total, err := s.history.List(opts)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"entries": entries,
			"total":   total,
			"limit":   opts.Limit,
			"offset":  opts.Offset,
		},
	})
}

// handleClearHistory clears all query history.
func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.history.Clear(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    "history cleared",
	})
}

// ExplainRequest represents a query explain request.
type ExplainRequest struct {
	Database string `json:"database"`
	Table    string `json:"table,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

// handleExplainQuery returns the execution plan for a query.
func (s *Server) handleExplainQuery(w http.ResponseWriter, r *http.Request) {
	var req ExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Database == "" {
		s.writeError(w, http.StatusBadRequest, "database is required")
		return
	}

	args := map[string]interface{}{
		"database": req.Database,
	}
	if req.Table != "" {
		args["table"] = req.Table
	}
	if req.Raw != "" {
		args["raw"] = req.Raw
	}

	result, err := s.bridge.CallTool("explain_query", args)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var data interface{}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		s.writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Data:    result,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}
