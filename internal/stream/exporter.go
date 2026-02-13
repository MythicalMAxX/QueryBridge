// Package stream provides streaming and export functionality.
package stream

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
)

// Exporter handles exporting query results to files.
type Exporter struct {
	outputDir string
}

// NewExporter creates a new exporter.
func NewExporter(outputDir string) *Exporter {
	os.MkdirAll(outputDir, 0755)
	return &Exporter{outputDir: outputDir}
}

// ExportResult holds export operation results.
type ExportResult struct {
	FilePath  string
	RowCount  int64
	FileSize  int64
	Format    string
	Duration  time.Duration
}

// ExportCSV exports streaming results to a CSV file.
func (e *Exporter) ExportCSV(ctx context.Context, stream adapter.RowStream, filename string) (*ExportResult, error) {
	start := time.Now()

	if filename == "" {
		filename = fmt.Sprintf("export_%d.csv", time.Now().UnixNano())
	}
	path := filepath.Join(e.outputDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	var columns []string
	var rowCount int64
	headerWritten := false

	for stream.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		row := stream.Row()

		// Write header on first row
		if !headerWritten {
			for k := range row {
				columns = append(columns, k)
			}
			if err := writer.Write(columns); err != nil {
				return nil, err
			}
			headerWritten = true
		}

		// Write row
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = fmt.Sprintf("%v", row[col])
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
		rowCount++
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	writer.Flush()
	info, _ := file.Stat()

	return &ExportResult{
		FilePath: path,
		RowCount: rowCount,
		FileSize: info.Size(),
		Format:   "csv",
		Duration: time.Since(start),
	}, nil
}

// ExportJSONL exports streaming results to a JSONL file.
func (e *Exporter) ExportJSONL(ctx context.Context, stream adapter.RowStream, filename string) (*ExportResult, error) {
	start := time.Now()

	if filename == "" {
		filename = fmt.Sprintf("export_%d.jsonl", time.Now().UnixNano())
	}
	path := filepath.Join(e.outputDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	var rowCount int64

	for stream.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := encoder.Encode(stream.Row()); err != nil {
			return nil, err
		}
		rowCount++
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	info, _ := file.Stat()

	return &ExportResult{
		FilePath: path,
		RowCount: rowCount,
		FileSize: info.Size(),
		Format:   "jsonl",
		Duration: time.Since(start),
	}, nil
}

// BatchStreamer wraps a RowStream to provide batched results.
type BatchStreamer struct {
	stream    adapter.RowStream
	batchSize int
	columns   []string
}

// NewBatchStreamer creates a new batch streamer.
func NewBatchStreamer(stream adapter.RowStream, batchSize int) *BatchStreamer {
	if batchSize <= 0 {
		batchSize = 1000
	}
	return &BatchStreamer{stream: stream, batchSize: batchSize}
}

// NextBatch returns the next batch of rows.
func (b *BatchStreamer) NextBatch() ([]adapter.Row, bool, error) {
	var rows []adapter.Row

	for i := 0; i < b.batchSize && b.stream.Next(); i++ {
		rows = append(rows, b.stream.Row())
	}

	if err := b.stream.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(rows) == b.batchSize
	return rows, hasMore, nil
}

// Close closes the underlying stream.
func (b *BatchStreamer) Close() error {
	return b.stream.Close()
}
