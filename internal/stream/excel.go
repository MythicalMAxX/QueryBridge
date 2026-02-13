package stream

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/xuri/excelize/v2"
)

// ExportExcel exports streaming results to an Excel file.
func (e *Exporter) ExportExcel(ctx context.Context, stream adapter.RowStream, filename string) (*ExportResult, error) {
	start := time.Now()

	if filename == "" {
		filename = fmt.Sprintf("export_%d.xlsx", time.Now().UnixNano())
	}
	if filepath.Ext(filename) != ".xlsx" {
		filename += ".xlsx"
	}
	path := filepath.Join(e.outputDir, filename)

	// Create a new Excel file
	f := excelize.NewFile()
	defer f.Close()

	// Create a sheet
	sheetName := "Sheet1"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("creating sheet: %w", err)
	}
	f.SetActiveSheet(index)

	var columns []string
	var rowCount int64
	headerWritten := false
	currentRow := 1

	// Define header style
	headerStyle, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#4F81BD"}, Pattern: 1},
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "#000000", Style: 2},
		},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	if err != nil {
		headerStyle = 0 // Use default if style creation fails
	}

	for stream.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		row := stream.Row()

		// Write header on first row
		if !headerWritten {
			colIndex := 1
			for k := range row {
				columns = append(columns, k)
				cell, _ := excelize.CoordinatesToCellName(colIndex, currentRow)
				f.SetCellValue(sheetName, cell, k)
				if headerStyle != 0 {
					f.SetCellStyle(sheetName, cell, cell, headerStyle)
				}
				colIndex++
			}
			headerWritten = true
			currentRow++
		}

		// Write data row
		for i, col := range columns {
			cell, _ := excelize.CoordinatesToCellName(i+1, currentRow)
			value := row[col]
			
			// Handle different types
			switch v := value.(type) {
			case nil:
				f.SetCellValue(sheetName, cell, "")
			case time.Time:
				f.SetCellValue(sheetName, cell, v.Format(time.RFC3339))
			default:
				f.SetCellValue(sheetName, cell, v)
			}
		}
		currentRow++
		rowCount++
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	// Auto-fit column widths (approximate)
	for i, col := range columns {
		colLetter, _ := excelize.ColumnNumberToName(i + 1)
		width := float64(len(col) + 5)
		if width < 10 {
			width = 10
		}
		if width > 50 {
			width = 50
		}
		f.SetColWidth(sheetName, colLetter, colLetter, width)
	}

	// Save the file
	if err := f.SaveAs(path); err != nil {
		return nil, fmt.Errorf("saving excel file: %w", err)
	}

	return &ExportResult{
		FilePath: path,
		RowCount: rowCount,
		Format:   "excel",
		Duration: time.Since(start),
	}, nil
}
