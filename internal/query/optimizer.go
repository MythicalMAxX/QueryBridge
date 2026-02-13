// Package query provides query planning and optimization.
package query

import (
	"fmt"
	"strings"
)

// PerformanceSuggestion represents a suggestion to improve query performance.
type PerformanceSuggestion struct {
	Type        string `json:"type"` // Index, Refactor, Schema
	Priority    string `json:"priority"` // High, Medium, Low
	Description string `json:"description"`
	Action      string `json:"action,omitempty"` // SQL command if applicable
}

// AnalyzePerformance analyzes a query plan and provides suggestions.
func AnalyzePerformance(dbType string, rawPlan string) []PerformanceSuggestion {
	var suggestions []PerformanceSuggestion

	// Rule-based analysis (Simplified for demonstration)
	planLower := strings.ToLower(rawPlan)

	if strings.Contains(planLower, "seq scan") || strings.Contains(planLower, "full table scan") {
		suggestions = append(suggestions, PerformanceSuggestion{
			Type:        "Index",
			Priority:    "High",
			Description: "Sequential scan detected on a table. Consider adding an index on the filtered columns.",
		})
	}

	if strings.Contains(planLower, "nested loop") && (strings.Contains(planLower, "cost=") || strings.Contains(planLower, "rows=")) {
		// Heuristic: Nested loops on large datasets are often slow
		suggestions = append(suggestions, PerformanceSuggestion{
			Type:        "Refactor",
			Priority:    "Medium",
			Description: "Nested loop join detected. Ensure join columns are indexed or consider using a hash join if possible.",
		})
	}

	if strings.Contains(planLower, "temporary file") || strings.Contains(planLower, "disk") {
		suggestions = append(suggestions, PerformanceSuggestion{
			Type:        "Configuration",
			Priority:    "Medium",
			Description: "Query is using disk for sorting/hashing. Increase work_mem (or equivalent) for this session.",
		})
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, PerformanceSuggestion{
			Type:        "Compliment",
			Priority:    "Low",
			Description: "Query plan looks efficient. No obvious bottlenecks found.",
		})
	}

	return suggestions
}

// FormatSuggestions returns a markdown-formatted string of suggestions.
func FormatSuggestions(suggestions []PerformanceSuggestion) string {
	var sb strings.Builder
	sb.WriteString("### Performance Optimization Suggestions\n\n")
	for _, s := range suggestions {
		sb.WriteString(fmt.Sprintf("- **[%s] (%s)**: %s\n", s.Type, s.Priority, s.Description))
		if s.Action != "" {
			sb.WriteString(fmt.Sprintf("  - *Action*: `%s`\n", s.Action))
		}
	}
	return sb.String()
}
