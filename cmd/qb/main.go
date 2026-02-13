// Package main provides a CLI tool for QueryBridge database operations.
// Usage:
//
//	qb databases                                  - List connected databases
//	qb tables <database>                          - Show tables in database
//	qb query <database> <table>                   - Query a table
//	qb query <database> --raw "SELECT * FROM t"   - Run raw SQL
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MythicalMAxX/QueryBridge/pkg/querybridge"
)

const (
	formatJSON  = "json"
	formatCSV   = "csv"
	formatTable = "table"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Get config path
	configPath := os.Getenv("QB_CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}

	// Initialize bridge
	bridge, err := querybridge.NewBridge(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer bridge.Close()

	command := os.Args[1]
	switch command {
	case "databases", "db":
		cmdDatabases(bridge)
	case "tables", "t":
		cmdTables(bridge, os.Args[2:])
	case "query", "q":
		cmdQuery(bridge, os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Println("qb version 1.0.0")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`QueryBridge CLI - Query databases from the command line

Usage:
  qb <command> [arguments] [options]

Commands:
  databases, db              List all connected databases
  tables, t <database>       Show tables in a database
  query, q <database> <table> [options]
                             Query a table
  help                       Show this help message
  version                    Show version

Query Options:
  --limit <n>                Limit results (default: 10)
  --format <type>            Output format: json, csv, table (default: json)
  --output <file>            Write output to file
  --raw "<sql>"              Execute raw SQL query

Examples:
  qb databases
  qb tables postgres
  qb query postgres users --limit 5
  qb query postgres orders --format csv --output orders.csv
  qb query postgres --raw "SELECT * FROM users WHERE active = true"

Environment Variables:
  QB_CONFIG                  Path to config.json (default: config.json)`)
}

func cmdDatabases(bridge *querybridge.Bridge) {
	result, err := bridge.CallTool("describe_databases", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(result)
}

func cmdTables(bridge *querybridge.Bridge, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Error: database name required\n")
		fmt.Fprintf(os.Stderr, "Usage: qb tables <database>\n")
		os.Exit(1)
	}

	database := args[0]
	result, err := bridge.CallTool("describe_tables", map[string]interface{}{
		"database": database,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(result)
}

func cmdQuery(bridge *querybridge.Bridge, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Error: database name required\n")
		fmt.Fprintf(os.Stderr, "Usage: qb query <database> <table> [options]\n")
		os.Exit(1)
	}

	database := args[0]
	var table string
	var rawSQL string
	limit := 10
	format := formatJSON
	output := ""

	// Parse arguments
	i := 1
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--limit" && i+1 < len(args):
			i++
			fmt.Sscanf(args[i], "%d", &limit)
		case arg == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case arg == "--output" && i+1 < len(args):
			i++
			output = args[i]
		case arg == "--raw" && i+1 < len(args):
			i++
			rawSQL = args[i]
		case !strings.HasPrefix(arg, "--") && table == "":
			table = arg
		}
		i++
	}

	// Build query
	var queryArgs map[string]interface{}
	if rawSQL != "" {
		queryArgs = map[string]interface{}{
			"database": database,
			"query": map[string]interface{}{
				"raw": rawSQL,
			},
		}
	} else {
		if table == "" {
			fmt.Fprintf(os.Stderr, "Error: table name or --raw query required\n")
			os.Exit(1)
		}
		queryArgs = map[string]interface{}{
			"database": database,
			"query": map[string]interface{}{
				"table": table,
				"limit": limit,
			},
		}
	}

	result, err := bridge.CallTool("execute_query", queryArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Format output
	formattedOutput := formatOutput(result, format)

	// Write to file or stdout
	if output != "" {
		if err := os.WriteFile(output, []byte(formattedOutput), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Results written to %s\n", output)
	} else {
		fmt.Println(formattedOutput)
	}
}

func formatOutput(jsonResult, format string) string {
	if format == formatJSON {
		return jsonResult
	}

	// Parse JSON result
	var response struct {
		Columns  []string                 `json:"columns"`
		Rows     []map[string]interface{} `json:"rows"`
		RowCount int                      `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(jsonResult), &response); err != nil {
		return jsonResult // Return raw if parsing fails
	}

	switch format {
	case formatCSV:
		return formatAsCSV(response.Columns, response.Rows)
	case formatTable:
		return formatAsTable(response.Columns, response.Rows)
	default:
		return jsonResult
	}
}

func formatAsCSV(columns []string, rows []map[string]interface{}) string {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	// Write header
	writer.Write(columns)

	// Write rows
	for _, row := range rows {
		record := make([]string, len(columns))
		for i, col := range columns {
			if val, ok := row[col]; ok {
				record[i] = fmt.Sprintf("%v", val)
			}
		}
		writer.Write(record)
	}
	writer.Flush()
	return sb.String()
}

func formatAsTable(columns []string, rows []map[string]interface{}) string {
	if len(columns) == 0 {
		return "No data"
	}

	// Calculate column widths
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, col := range columns {
			if val, ok := row[col]; ok {
				strVal := fmt.Sprintf("%v", val)
				if len(strVal) > widths[i] {
					widths[i] = len(strVal)
				}
			}
		}
	}

	// Cap widths at 40 characters
	for i := range widths {
		if widths[i] > 40 {
			widths[i] = 40
		}
	}

	var sb strings.Builder

	// Header
	sb.WriteString("| ")
	for i, col := range columns {
		sb.WriteString(padRight(col, widths[i]))
		if i < len(columns)-1 {
			sb.WriteString(" | ")
		}
	}
	sb.WriteString(" |\n")

	// Separator
	sb.WriteString("|")
	for i := range columns {
		sb.WriteString(strings.Repeat("-", widths[i]+2))
		sb.WriteString("|")
	}
	sb.WriteString("\n")

	// Rows
	for _, row := range rows {
		sb.WriteString("| ")
		for i, col := range columns {
			val := ""
			if v, ok := row[col]; ok {
				val = fmt.Sprintf("%v", v)
			}
			sb.WriteString(padRight(truncate(val, widths[i]), widths[i]))
			if i < len(columns)-1 {
				sb.WriteString(" | ")
			}
		}
		sb.WriteString(" |\n")
	}

	sb.WriteString(fmt.Sprintf("\n(%d rows)\n", len(rows)))
	return sb.String()
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
