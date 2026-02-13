// Package api provides REST API endpoints for QueryBridge.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/history"
	"github.com/MythicalMAxX/QueryBridge/pkg/querybridge"
)

// Server provides HTTP REST API access to QueryBridge functionality.
type Server struct {
	bridge       *querybridge.Bridge
	history      *history.Store
	router       *http.ServeMux
	httpServer   *http.Server
	exportDir    string
	corsOrigins  []string
	staticDir    string
}

// Config holds API server configuration.
type Config struct {
	Port        int      `json:"port"`
	CORSOrigins []string `json:"cors_origins"`
	ExportDir   string   `json:"export_dir"`
	HistoryPath string   `json:"history_path"`
	StaticDir   string   `json:"static_dir"`
}

// DefaultConfig returns the default API configuration.
func DefaultConfig() Config {
	return Config{
		Port:        8080,
		CORSOrigins: []string{"*"},
		ExportDir:   "./exports",
		HistoryPath: "./history.db",
		StaticDir:   "./web",
	}
}

// NewServer creates a new REST API server.
func NewServer(bridge *querybridge.Bridge, cfg Config) (*Server, error) {
	// Create history store
	historyStore, err := history.NewStore(cfg.HistoryPath)
	if err != nil {
		return nil, fmt.Errorf("creating history store: %w", err)
	}

	// Ensure export directory exists
	if err := os.MkdirAll(cfg.ExportDir, 0755); err != nil {
		return nil, fmt.Errorf("creating export directory: %w", err)
	}

	s := &Server{
		bridge:      bridge,
		history:     historyStore,
		router:      http.NewServeMux(),
		exportDir:   cfg.ExportDir,
		corsOrigins: cfg.CORSOrigins,
		staticDir:   cfg.StaticDir,
	}

	s.setupRoutes()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.withMiddleware(s.router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s, nil
}

// setupRoutes configures all API routes.
func (s *Server) setupRoutes() {
	// API endpoints
	s.router.HandleFunc("GET /api/databases", s.handleListDatabases)
	s.router.HandleFunc("GET /api/databases/{name}/tables", s.handleDescribeTables)
	s.router.HandleFunc("POST /api/query", s.handleExecuteQuery)
	s.router.HandleFunc("POST /api/stream", s.handleStreamQuery)
	s.router.HandleFunc("POST /api/export", s.handleExportQuery)
	s.router.HandleFunc("GET /api/exports/{filename}", s.handleDownloadExport)
	s.router.HandleFunc("GET /api/history", s.handleGetHistory)
	s.router.HandleFunc("DELETE /api/history", s.handleClearHistory)
	s.router.HandleFunc("POST /api/explain", s.handleExplainQuery)

	// Serve static files for Web UI (catch-all for non-API routes)
	s.router.HandleFunc("/", s.handleStaticFiles)
}

// withMiddleware wraps the router with middleware.
func (s *Server) withMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// CORS headers
		origin := r.Header.Get("Origin")
		if s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		
		// Recover from panics
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC: %v", err)
				s.writeError(wrapped, http.StatusInternalServerError, "Internal server error")
			}
		}()

		handler.ServeHTTP(wrapped, r)

		// Log request
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.status, time.Since(start))
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// isAllowedOrigin checks if the origin is in the allowed list.
func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range s.corsOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	log.Printf("REST API server starting on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down REST API server...")
	if err := s.history.Close(); err != nil {
		log.Printf("Error closing history store: %v", err)
	}
	return s.httpServer.Shutdown(ctx)
}

// handleStaticFiles serves static files for the Web UI.
func (s *Server) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	filePath := filepath.Join(s.staticDir, filepath.Clean(path))
	
	// Security: ensure we're still within static dir
	if !strings.HasPrefix(filePath, filepath.Clean(s.staticDir)) {
		http.NotFound(w, r)
		return
	}

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// Try serving index.html for SPA routing
		filePath = filepath.Join(s.staticDir, "index.html")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
	} else if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	} else if info.IsDir() {
		filePath = filepath.Join(filePath, "index.html")
	}

	// Set content type based on extension
	ext := filepath.Ext(filePath)
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	}

	http.ServeFile(w, r, filePath)
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]interface{}{
		"error":   true,
		"message": message,
	})
}

// APIResponse is a standard API response wrapper.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}
