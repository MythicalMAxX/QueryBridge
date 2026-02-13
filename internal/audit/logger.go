// Package audit provides structured logging for all database operations.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// Entry represents a single audit log entry.
type Entry struct {
	Timestamp    time.Time              `json:"timestamp"`
	Level        string                 `json:"level"`
	Database     string                 `json:"database,omitempty"`
	Tool         string                 `json:"tool"`
	Query        map[string]interface{} `json:"query,omitempty"`
	RowCount     int                    `json:"row_count,omitempty"`
	Duration     int64                  `json:"duration_ms"`
	Error        string                 `json:"error,omitempty"`
	UserID       string                 `json:"user_id,omitempty"`
	RequestID    string                 `json:"request_id,omitempty"`
	Success      bool                   `json:"success"`
}

// Logger provides structured audit logging for database operations.
type Logger struct {
	mu      sync.Mutex
	encoder *json.Encoder
	output  io.Writer
	enabled bool
}

// Config holds audit logger configuration.
type Config struct {
	// Enabled controls whether audit logging is active
	Enabled bool `json:"enabled"`

	// Output specifies where to write logs: "stdout", "stderr", or a file path
	Output string `json:"output"`

	// Pretty enables indented JSON output (for debugging)
	Pretty bool `json:"pretty"`
}

// NewLogger creates a new audit logger with the given configuration.
func NewLogger(cfg Config) (*Logger, error) {
	if !cfg.Enabled {
		return &Logger{enabled: false}, nil
	}

	var output io.Writer
	switch cfg.Output {
	case "", "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening audit log file: %w", err)
		}
		output = f
	}

	encoder := json.NewEncoder(output)
	if cfg.Pretty {
		encoder.SetIndent("", "  ")
	}

	return &Logger{
		encoder: encoder,
		output:  output,
		enabled: true,
	}, nil
}

// DefaultLogger creates a logger that writes to stderr.
func DefaultLogger() *Logger {
	encoder := json.NewEncoder(os.Stderr)
	return &Logger{
		encoder: encoder,
		output:  os.Stderr,
		enabled: true,
	}
}

// DisabledLogger creates a logger that does nothing.
func DisabledLogger() *Logger {
	return &Logger{enabled: false}
}

// Log writes an audit entry.
func (l *Logger) Log(entry Entry) {
	if !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Level == "" {
		if entry.Error != "" {
			entry.Level = "error"
		} else {
			entry.Level = "info"
		}
	}

	if err := l.encoder.Encode(entry); err != nil {
		log.Printf("audit: failed to write entry: %v", err)
	}
}

// LogToolCall is a convenience method for logging tool invocations.
func (l *Logger) LogToolCall(tool, database string, args map[string]interface{}, rowCount int, duration time.Duration, err error) {
	entry := Entry{
		Tool:     tool,
		Database: database,
		Query:    args,
		RowCount: rowCount,
		Duration: duration.Milliseconds(),
		Success:  err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.Log(entry)
}

// LogQuery is a convenience method for logging query operations.
func (l *Logger) LogQuery(database, queryType string, table string, rowCount int, duration time.Duration, err error) {
	entry := Entry{
		Tool:     "execute_query",
		Database: database,
		Query: map[string]interface{}{
			"type":  queryType,
			"table": table,
		},
		RowCount: rowCount,
		Duration: duration.Milliseconds(),
		Success:  err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.Log(entry)
}

// LogExport logs export operations.
func (l *Logger) LogExport(database, format, filename string, rowCount int, duration time.Duration, err error) {
	entry := Entry{
		Tool:     "export_query",
		Database: database,
		Query: map[string]interface{}{
			"format":   format,
			"filename": filename,
		},
		RowCount: rowCount,
		Duration: duration.Milliseconds(),
		Success:  err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.Log(entry)
}

// LogDeniedAccess logs attempts to access denied fields.
func (l *Logger) LogDeniedAccess(database, table string, deniedFields []string) {
	entry := Entry{
		Level:    "warn",
		Tool:     "execute_query",
		Database: database,
		Query: map[string]interface{}{
			"table":         table,
			"denied_fields": deniedFields,
		},
		Success: false,
		Error:   "attempted to access denied fields",
	}
	l.Log(entry)
}

// LogReadOnlyViolation logs attempts to perform write operations in read-only mode.
func (l *Logger) LogReadOnlyViolation(database, operation string) {
	entry := Entry{
		Level:    "warn",
		Tool:     operation,
		Database: database,
		Success:  false,
		Error:    "write operation blocked by read-only mode",
	}
	l.Log(entry)
}

// Close closes the logger, flushing any buffered data.
func (l *Logger) Close() error {
	if closer, ok := l.output.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// IsEnabled returns whether audit logging is enabled.
func (l *Logger) IsEnabled() bool {
	return l.enabled
}
