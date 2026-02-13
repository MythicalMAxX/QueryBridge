// QueryBridge MCP Server - Main entry point
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MythicalMAxX/QueryBridge/internal/adapter"
	"github.com/MythicalMAxX/QueryBridge/internal/audit"
	"github.com/MythicalMAxX/QueryBridge/internal/config"
	"github.com/MythicalMAxX/QueryBridge/internal/mcp"
	"github.com/MythicalMAxX/QueryBridge/internal/metadata"
	"github.com/MythicalMAxX/QueryBridge/internal/schema"
)

func main() {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Create metadata store
	metaPath := cfg.Server.ExportDirectory + "/metadata.db"
	metaStore, err := metadata.NewStore(metaPath)
	if err != nil {
		log.Fatalf("Failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Create schema registry
	registry := schema.NewRegistry(cfg.DenyFields, metaStore)

	// Create adapters and connect
	adapters := make(map[string]adapter.Adapter)
	for _, dbCfg := range cfg.Databases {
		a, err := createAdapter(dbCfg)
		if err != nil {
			log.Fatalf("Failed to create adapter for %s: %v", dbCfg.Name, err)
		}

		if err := a.Connect(ctx); err != nil {
			log.Fatalf("Failed to connect to %s: %v", dbCfg.Name, err)
		}
		defer a.Close()

		// Discover schema
		s, err := a.DiscoverSchema(ctx)
		if err != nil {
			log.Fatalf("Failed to discover schema for %s: %v", dbCfg.Name, err)
		}
		// Override the database name with the adapter name for consistent lookups
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
		log.Fatalf("Failed to create audit logger: %v", err)
	}

	// Create and run MCP server
	server := mcp.NewServer(registry, adapters, cfg.Server.ExportDirectory, cfg.Server.ReadOnly, auditor)

	log.Println("QueryBridge MCP Server started")
	if err := server.ServeStdio(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("Server error: %v", err)
	}
}

func loadConfig() (*config.Config, error) {
	// Try config file first
	configPath := os.Getenv("QB_CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}

	if _, err := os.Stat(configPath); err == nil {
		return config.LoadFromFile(configPath)
	}

	// Fall back to environment variables
	return config.LoadFromEnv()
}

func createAdapter(cfg config.DatabaseConfig) (adapter.Adapter, error) {
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
		return adapter.NewSQLiteAdapter(cfg.Name, adapter.SQLiteConfig{
			Path:           cfg.Database,
			MaxConnections: cfg.MaxConnections,
		}), nil

	case "redis":
		return adapter.NewRedisAdapter(cfg.Name, adapter.RedisConfig{
			Host:           cfg.Host,
			Port:           cfg.Port,
			Password:       cfg.Password,
			Database:       0,
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
			Token:  cfg.Password,
			Org:    cfg.User,
			Bucket: cfg.Database,
		}), nil

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}
}
