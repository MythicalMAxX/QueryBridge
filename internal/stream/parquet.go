package stream

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
)

// ExportParquet exports streaming results to a Parquet file.
func (e *Exporter) ExportParquet(ctx context.Context, stream adapter.RowStream, filename string) (*ExportResult, error) {
	start := time.Now()

	if filename == "" {
		filename = fmt.Sprintf("export_%d.parquet", time.Now().UnixNano())
	}
	if filepath.Ext(filename) != ".parquet" {
		filename += ".parquet"
	}
	path := filepath.Join(e.outputDir, filename)

	// Collect all rows first to determine schema
	var allRows []adapter.Row
	var columns []string

	for stream.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		row := stream.Row()
		
		// Get columns from first row
		if len(columns) == 0 {
			for k := range row {
				columns = append(columns, k)
			}
		}
		
		// Make a copy of the row
		rowCopy := make(adapter.Row)
		for k, v := range row {
			rowCopy[k] = v
		}
		allRows = append(allRows, rowCopy)
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	if len(allRows) == 0 {
		return &ExportResult{
			FilePath: path,
			RowCount: 0,
			Format:   "parquet",
			Duration: time.Since(start),
		}, nil
	}

	// Convert to generic row format for parquet
	type GenericRow struct {
		Data map[string]interface{} `parquet:"data,json"`
	}

	// Create parquet writer
	rows := make([]GenericRow, len(allRows))
	for i, row := range allRows {
		rows[i] = GenericRow{Data: row}
	}

	// Write to parquet file
	if err := parquet.WriteFile(path, rows); err != nil {
		return nil, fmt.Errorf("writing parquet file: %w", err)
	}

	return &ExportResult{
		FilePath: path,
		RowCount: int64(len(allRows)),
		Format:   "parquet",
		Duration: time.Since(start),
	}, nil
}
