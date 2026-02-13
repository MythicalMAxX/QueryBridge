// Package querybridge provides a public API for integrating the MCP server
// as a library within other applications.
package querybridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/audit"
	"github.com/MythicalMAxX/QueryBridge/internal/config"
	"github.com/MythicalMAxX/QueryBridge/internal/mcp"
	"github.com/MythicalMAxX/QueryBridge/internal/schema"
)

// Bridge provides direct access to MCP server functionality without subprocess.
// It is the main entry point for library integration.
type Bridge struct {
	server   *mcp.Server
	adapters map[string]adapter.Adapter
	registry *schema.Registry
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// NewBridge creates a new QueryBridge with embedded MCP server.
// configPath should point to a valid config.json file.
func NewBridge(configPath string) (*Bridge, error) {
	// Load configuration
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	return NewBridgeWithConfig(cfg)
}

// NewBridgeWithConfig creates a new QueryBridge using a Config struct directly.
func NewBridgeWithConfig(cfg *config.Config) (*Bridge, error) {
	// Create schema registry
	registry := schema.NewRegistry(cfg.DenyFields)

	// Create and connect adapters
	adapters := make(map[string]adapter.Adapter)
	ctx := context.Background()

	for _, dbCfg := range cfg.Databases {
		a, err := createDatabaseAdapter(dbCfg)
		if err != nil {
			return nil, fmt.Errorf("creating adapter for %s: %w", dbCfg.Name, err)
		}

		if err := a.Connect(ctx); err != nil {
			return nil, fmt.Errorf("connecting to %s: %w", dbCfg.Name, err)
		}

		// Discover schema
		s, err := a.DiscoverSchema(ctx)
		if err != nil {
			return nil, fmt.Errorf("discovering schema for %s: %w", dbCfg.Name, err)
		}
		s.Database = dbCfg.Name
		registry.Register(s)

		adapters[dbCfg.Name] = a
		log.Printf("Connected to %s (%s)", dbCfg.Name, dbCfg.Type)
	}

	// Create audit logger
	auditor, err := audit.NewLogger(audit.Config{
		Enabled: cfg.Server.Audit.Enabled,
		Output:  cfg.Server.Audit.Output,
		Pretty:  cfg.Server.Audit.Pretty,
	})
	if err != nil {
		return nil, fmt.Errorf("creating audit logger: %w", err)
	}

	// Create MCP server
	server := mcp.NewServer(registry, adapters, cfg.Server.ExportDirectory, cfg.Server.ReadOnly, auditor)

	log.Println("QueryBridge initialized successfully")

	return &Bridge{
		server:   server,
		adapters: adapters,
		registry: registry,
	}, nil
}

// createDatabaseAdapter creates the appropriate adapter for the database type.
func createDatabaseAdapter(cfg config.DatabaseConfig) (adapter.Adapter, error) {
	switch cfg.Type {
	case "postgres":
		return adapter.NewPostgresAdapter(cfg.Name, adapter.PostgresConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Database:       cfg.Database,
			User:           cfg.User,
			Password:       cfg.Password,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "mysql":
		return adapter.NewMySQLAdapter(cfg.Name, adapter.MySQLConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Database:       cfg.Database,
			User:           cfg.User,
			Password:       cfg.Password,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "sqlite":
		// For SQLite, use Database field as the file path
		return adapter.NewSQLiteAdapter(cfg.Name, adapter.SQLiteConfig{
			Path:           cfg.Database,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "redis":
		return adapter.NewRedisAdapter(cfg.Name, adapter.RedisConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Password:       cfg.Password,
			Database:       0, // Redis DB number
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "elasticsearch":
		return adapter.NewElasticsearchAdapter(cfg.Name, adapter.ElasticsearchConfig{
			Host:     cfg.Host,
			Port:     cfg.Port,
			User:     cfg.User,
			Password: cfg.Password,
		}), nil

	case "sqlserver":
		return adapter.NewSQLServerAdapter(cfg.Name, adapter.SQLServerConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Database:       cfg.Database,
			User:           cfg.User,
			Password:       cfg.Password,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "clickhouse":
		return adapter.NewClickHouseAdapter(cfg.Name, adapter.ClickHouseConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Database:       cfg.Database,
			User:           cfg.User,
			Password:       cfg.Password,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "mongodb":
		return adapter.NewMongoAdapter(cfg.Name, adapter.MongoConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Database:       cfg.Database,
			User:           cfg.User,
			Password:       cfg.Password,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "cassandra":
		hosts := []string{cfg.Host}
		return adapter.NewCassandraAdapter(cfg.Name, adapter.CassandraConfig{
			Hosts:    hosts,
			Port:     cfg.Port,
			Keyspace: cfg.Database,
			User:     cfg.User,
			Password: cfg.Password,
		}), nil

	case "couchdb":
		return adapter.NewCouchDBAdapter(cfg.Name, adapter.CouchDBConfig{
			Host:     cfg.Host,
			Port:     cfg.Port,
			Database: cfg.Database,
			User:     cfg.User,
			Password: cfg.Password,
		}), nil

	case "neo4j":
		uri := fmt.Sprintf("bolt://%s:%d", cfg.Host, cfg.Port)
		if cfg.Port == 0 {
			uri = fmt.Sprintf("bolt://%s:7687", cfg.Host)
		}
		return adapter.NewNeo4jAdapter(cfg.Name, adapter.Neo4jConfig{
			URI:      uri,
			User:     cfg.User,
			Password: cfg.Password,
			Database: cfg.Database,
		}), nil

	case "influxdb":
		url := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
		if cfg.Port == 0 {
			url = fmt.Sprintf("http://%s:8086", cfg.Host)
		}
		return adapter.NewInfluxDBAdapter(cfg.Name, adapter.InfluxDBConfig{
			URL:    url,
			Token:  cfg.Password, // Use password field for token
			Org:    cfg.User,     // Use user field for org
			Bucket: cfg.Database,
		}), nil

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}
}

// ListTools returns all available MCP tools.
func (b *Bridge) ListTools() []Tool {
	tools := mcp.GetTools()
	result := make([]Tool, len(tools))
	for i, t := range tools {
		schemaBytes, _ := json.Marshal(t.InputSchema)
		result[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schemaBytes,
		}
	}
	return result
}

// CallTool invokes an MCP tool and returns the result as a JSON string.
func (b *Bridge) CallTool(name string, args map[string]interface{}) (string, error) {
	ctx := context.Background()
	return b.server.CallToolDirect(ctx, name, args)
}

// CallToolWithContext invokes an MCP tool with a custom context.
func (b *Bridge) CallToolWithContext(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	return b.server.CallToolDirect(ctx, name, args)
}

// Close closes all database connections.
func (b *Bridge) Close() error {
	for name, a := range b.adapters {
		log.Printf("Closing connection to %s", name)
		a.Close()
	}
	return nil
}

// DatabaseCount returns the number of connected databases.
func (b *Bridge) DatabaseCount() int {
	return len(b.adapters)
}

// DatabaseNames returns the names of all connected databases.
func (b *Bridge) DatabaseNames() []string {
	names := make([]string, 0, len(b.adapters))
	for name := range b.adapters {
		names = append(names, name)
	}
	return names
}
