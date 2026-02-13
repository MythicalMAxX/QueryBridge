// Package auth provides RBAC (Role-Based Access Control) for QueryBridge.
package auth

// Permission represents an action that can be performed.
type Permission string

const (
	// Database permissions
	PermListDatabases   Permission = "databases:list"
	PermDescribeTables  Permission = "databases:describe"
	
	// Query permissions
	PermExecuteQuery    Permission = "query:execute"
	PermStreamQuery     Permission = "query:stream"
	PermExportQuery     Permission = "query:export"
	PermExplainQuery    Permission = "query:explain"
	PermCrossDBJoin     Permission = "query:crossjoin"
	
	// Template permissions
	PermListTemplates   Permission = "templates:list"
	PermExecuteTemplate Permission = "templates:execute"
	PermSaveTemplate    Permission = "templates:save"
	PermDeleteTemplate  Permission = "templates:delete"
	
	// Schedule permissions
	PermListSchedules   Permission = "schedules:list"
	PermCreateSchedule  Permission = "schedules:create"
	PermDeleteSchedule  Permission = "schedules:delete"
	
	// Cache permissions
	PermViewCache       Permission = "cache:view"
	PermClearCache      Permission = "cache:clear"
	
	// Admin permissions
	PermManageUsers     Permission = "admin:users"
	PermViewMetrics     Permission = "admin:metrics"
	PermViewHistory     Permission = "history:view"
	PermClearHistory    Permission = "history:clear"
)

// RolePermissions maps roles to their allowed permissions.
var RolePermissions = map[Role][]Permission{
	RoleAdmin: {
		// All permissions
		PermListDatabases, PermDescribeTables,
		PermExecuteQuery, PermStreamQuery, PermExportQuery, PermExplainQuery, PermCrossDBJoin,
		PermListTemplates, PermExecuteTemplate, PermSaveTemplate, PermDeleteTemplate,
		PermListSchedules, PermCreateSchedule, PermDeleteSchedule,
		PermViewCache, PermClearCache,
		PermManageUsers, PermViewMetrics, PermViewHistory, PermClearHistory,
	},
	RoleAnalyst: {
		// Query and data management, no admin
		PermListDatabases, PermDescribeTables,
		PermExecuteQuery, PermStreamQuery, PermExportQuery, PermExplainQuery, PermCrossDBJoin,
		PermListTemplates, PermExecuteTemplate, PermSaveTemplate,
		PermListSchedules, PermCreateSchedule,
		PermViewCache,
		PermViewHistory,
	},
	RoleViewer: {
		// Read-only access
		PermListDatabases, PermDescribeTables,
		PermExecuteQuery, PermExplainQuery,
		PermListTemplates, PermExecuteTemplate,
		PermListSchedules,
		PermViewHistory,
	},
}

// ToolPermissions maps MCP tool names to required permissions.
var ToolPermissions = map[string]Permission{
	"describe_databases": PermListDatabases,
	"describe_tables":    PermDescribeTables,
	"execute_query":      PermExecuteQuery,
	"stream_query":       PermStreamQuery,
	"export_query":       PermExportQuery,
	"explain_query":      PermExplainQuery,
	"multi_source_query": PermExecuteQuery,
	"cross_db_join":      PermCrossDBJoin,
	"list_templates":     PermListTemplates,
	"execute_template":   PermExecuteTemplate,
	"save_template":      PermSaveTemplate,
	"schedule_query":     PermCreateSchedule,
	"list_schedules":     PermListSchedules,
	"cancel_schedule":    PermDeleteSchedule,
	"cache_stats":        PermViewCache,
	"clear_cache":        PermClearCache,
}

// APIRoutePermissions maps API routes to required permissions.
var APIRoutePermissions = map[string]Permission{
	"GET /api/databases":           PermListDatabases,
	"GET /api/databases/*/tables":  PermDescribeTables,
	"POST /api/query":              PermExecuteQuery,
	"POST /api/stream":             PermStreamQuery,
	"POST /api/export":             PermExportQuery,
	"GET /api/history":             PermViewHistory,
	"DELETE /api/history":          PermClearHistory,
	"POST /api/explain":            PermExplainQuery,
	"GET /api/users":               PermManageUsers,
	"POST /api/users":              PermManageUsers,
	"DELETE /api/users/*":          PermManageUsers,
	"GET /metrics":                 PermViewMetrics,
}

// HasPermission checks if a role has a specific permission.
func HasPermission(role Role, permission Permission) bool {
	permissions, ok := RolePermissions[role]
	if !ok {
		return false
	}

	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// GetPermissionsForRole returns all permissions for a role.
func GetPermissionsForRole(role Role) []Permission {
	return RolePermissions[role]
}

// CanAccessTool checks if a role can access a specific MCP tool.
func CanAccessTool(role Role, toolName string) bool {
	permission, ok := ToolPermissions[toolName]
	if !ok {
		// Unknown tools require admin
		return role == RoleAdmin
	}
	return HasPermission(role, permission)
}

// CanAccessRoute checks if a role can access a specific API route.
func CanAccessRoute(role Role, route string) bool {
	permission, ok := APIRoutePermissions[route]
	if !ok {
		// Unknown routes require admin
		return role == RoleAdmin
	}
	return HasPermission(role, permission)
}
