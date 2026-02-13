// Package metrics provides Prometheus metrics for QueryBridge.
package metrics

import (
	"net/http"
	"strconv"
	"time"
)

// Metrics holds all Prometheus metrics for QueryBridge.
type Metrics struct {
	// Counters
	requestsTotal      map[string]int64
	queriesTotal       map[string]int64
	cacheHitsTotal     int64
	cacheMissesTotal   int64
	errorsTotal        map[string]int64

	// Gauges
	activeConnections  map[string]int
	cacheSize          int

	// Histograms (simplified as buckets)
	queryDurations     []float64
}

// New creates a new Metrics instance.
func New() *Metrics {
	return &Metrics{
		requestsTotal:     make(map[string]int64),
		queriesTotal:      make(map[string]int64),
		errorsTotal:       make(map[string]int64),
		activeConnections: make(map[string]int),
		queryDurations:    make([]float64, 0, 1000),
	}
}

// IncRequests increments the request counter for a method.
func (m *Metrics) IncRequests(method, path string, status int) {
	key := method + " " + path + " " + strconv.Itoa(status)
	m.requestsTotal[key]++
}

// IncQueries increments the query counter for a database.
func (m *Metrics) IncQueries(database, tool string, success bool) {
	key := database + ":" + tool
	if success {
		m.queriesTotal[key+"_success"]++
	} else {
		m.queriesTotal[key+"_error"]++
	}
}

// IncCacheHit increments the cache hit counter.
func (m *Metrics) IncCacheHit() {
	m.cacheHitsTotal++
}

// IncCacheMiss increments the cache miss counter.
func (m *Metrics) IncCacheMiss() {
	m.cacheMissesTotal++
}

// IncErrors increments the error counter for an error type.
func (m *Metrics) IncErrors(errorType string) {
	m.errorsTotal[errorType]++
}

// SetActiveConnections sets the number of active connections for a database.
func (m *Metrics) SetActiveConnections(database string, count int) {
	m.activeConnections[database] = count
}

// SetCacheSize sets the current cache size.
func (m *Metrics) SetCacheSize(size int) {
	m.cacheSize = size
}

// ObserveQueryDuration records a query duration.
func (m *Metrics) ObserveQueryDuration(duration time.Duration) {
	// Keep last 1000 durations
	if len(m.queryDurations) >= 1000 {
		m.queryDurations = m.queryDurations[1:]
	}
	m.queryDurations = append(m.queryDurations, duration.Seconds())
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		// Requests total
		for key, count := range m.requestsTotal {
			w.Write([]byte("querybridge_http_requests_total{endpoint=\"" + key + "\"} " + strconv.FormatInt(count, 10) + "\n"))
		}

		// Queries total
		for key, count := range m.queriesTotal {
			w.Write([]byte("querybridge_queries_total{key=\"" + key + "\"} " + strconv.FormatInt(count, 10) + "\n"))
		}

		// Cache metrics
		w.Write([]byte("querybridge_cache_hits_total " + strconv.FormatInt(m.cacheHitsTotal, 10) + "\n"))
		w.Write([]byte("querybridge_cache_misses_total " + strconv.FormatInt(m.cacheMissesTotal, 10) + "\n"))
		w.Write([]byte("querybridge_cache_size " + strconv.Itoa(m.cacheSize) + "\n"))

		// Active connections
		for db, count := range m.activeConnections {
			w.Write([]byte("querybridge_active_connections{database=\"" + db + "\"} " + strconv.Itoa(count) + "\n"))
		}

		// Errors
		for errType, count := range m.errorsTotal {
			w.Write([]byte("querybridge_errors_total{type=\"" + errType + "\"} " + strconv.FormatInt(count, 10) + "\n"))
		}

		// Query duration histogram (simplified)
		if len(m.queryDurations) > 0 {
			var sum float64
			for _, d := range m.queryDurations {
				sum += d
			}
			avg := sum / float64(len(m.queryDurations))
			w.Write([]byte("querybridge_query_duration_seconds_sum " + strconv.FormatFloat(sum, 'f', 6, 64) + "\n"))
			w.Write([]byte("querybridge_query_duration_seconds_count " + strconv.Itoa(len(m.queryDurations)) + "\n"))
			w.Write([]byte("querybridge_query_duration_seconds_avg " + strconv.FormatFloat(avg, 'f', 6, 64) + "\n"))
		}
	}
}

// Middleware returns HTTP middleware that records request metrics.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Record metrics
		duration := time.Since(start)
		m.IncRequests(r.Method, r.URL.Path, wrapped.status)
		m.ObserveQueryDuration(duration)
	})
}

// statusWriter wraps http.ResponseWriter to capture status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// GetStats returns current metrics as a map.
func (m *Metrics) GetStats() map[string]interface{} {
	totalRequests := int64(0)
	for _, count := range m.requestsTotal {
		totalRequests += count
	}

	totalQueries := int64(0)
	for _, count := range m.queriesTotal {
		totalQueries += count
	}

	hitRate := 0.0
	total := m.cacheHitsTotal + m.cacheMissesTotal
	if total > 0 {
		hitRate = float64(m.cacheHitsTotal) / float64(total) * 100
	}

	avgDuration := 0.0
	if len(m.queryDurations) > 0 {
		var sum float64
		for _, d := range m.queryDurations {
			sum += d
		}
		avgDuration = sum / float64(len(m.queryDurations)) * 1000 // ms
	}

	return map[string]interface{}{
		"total_requests":       totalRequests,
		"total_queries":        totalQueries,
		"cache_hits":           m.cacheHitsTotal,
		"cache_misses":         m.cacheMissesTotal,
		"cache_hit_rate":       hitRate,
		"cache_size":           m.cacheSize,
		"avg_query_duration_ms": avgDuration,
		"active_connections":   m.activeConnections,
	}
}
