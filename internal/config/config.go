// Package config provides configuration management for the MCP server.
// It supports loading configuration from JSON files and environment variables.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config represents the main configuration structure for the MCP server.
type Config struct {
	// Databases contains connection configurations for all data sources
	Databases []DatabaseConfig `json:"databases"`

	// DenyFields lists field names that should never be exposed or queried
	// Examples: password, token, secret, ssn, credit_card
	DenyFields []string `json:"deny_fields"`

	// Server contains MCP server settings
	Server ServerConfig `json:"server"`
}

// DatabaseConfig represents connection settings for a single database.
type DatabaseConfig struct {
	// Name is a unique identifier for this database connection
	Name string `json:"name"`

	// Type specifies the database type: postgres, mysql, mongodb
	Type string `json:"type"`

	// Host is the database server hostname or IP
	Host string `json:"host"`

	// Port is the database server port
	Port int `json:"port"`

	// Database is the database/schema name to connect to
	Database string `json:"database"`

	// User is the authentication username
	User string `json:"user"`

	// Password is the authentication password
	Password string `json:"password"`

	// MaxConnections sets the connection pool size (default: 10)
	MaxConnections int `json:"max_connections"`

	// Options contains database-specific connection options
	Options map[string]string `json:"options"`
}

// ServerConfig contains MCP server operational settings.
type ServerConfig struct {
	// Transport specifies the MCP transport type: stdio or http
	Transport string `json:"transport"`

	// Address is the HTTP server address (only used when Transport is "http")
	Address string `json:"address"`

	// QueryTimeout is the maximum query execution time in seconds
	QueryTimeout int `json:"query_timeout"`

	// StreamBatchSize is the number of rows per streaming batch
	StreamBatchSize int `json:"stream_batch_size"`

	// ExportDirectory is the path for exported files (CSV, JSONL)
	ExportDirectory string `json:"export_directory"`

	// EnableMetrics enables Prometheus metrics endpoint
	EnableMetrics bool `json:"enable_metrics"`

	// ReadOnly when true prevents all write operations (INSERT, UPDATE, DELETE, etc.)
	ReadOnly bool `json:"readonly"`

	// Audit contains audit logging configuration
	Audit AuditConfig `json:"audit"`
}

// AuditConfig contains audit logging settings.
type AuditConfig struct {
	// Enabled controls whether audit logging is active
	Enabled bool `json:"enabled"`

	// Output specifies where to write logs: "stdout", "stderr", or a file path
	Output string `json:"output"`

	// Pretty enables indented JSON output (for debugging)
	Pretty bool `json:"pretty"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Databases: []DatabaseConfig{},
		DenyFields: []string{
			"password", "passwd", "pwd",
			"token", "access_token", "refresh_token", "api_key", "apikey",
			"secret", "secret_key", "private_key",
			"ssn", "social_security",
			"credit_card", "card_number", "cvv", "ccv",
			"pin", "otp",
		},
		Server: ServerConfig{
			Transport:       "stdio",
			Address:         ":8080",
			QueryTimeout:    30,
			StreamBatchSize: 1000,
			ExportDirectory: "./exports",
			EnableMetrics:   false,
		},
	}
}

// LoadFromFile loads configuration from a JSON file.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Apply environment variable overrides
	config.applyEnvOverrides()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// LoadFromEnv loads configuration entirely from environment variables.
func LoadFromEnv() (*Config, error) {
	config := DefaultConfig()
	config.applyEnvOverrides()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// applyEnvOverrides applies environment variable overrides to the configuration.
// Environment variables use the prefix QB_ (QueryBridge).
func (c *Config) applyEnvOverrides() {
	if transport := os.Getenv("QB_TRANSPORT"); transport != "" {
		c.Server.Transport = transport
	}
	if addr := os.Getenv("QB_ADDRESS"); addr != "" {
		c.Server.Address = addr
	}
	if denyFields := os.Getenv("QB_DENY_FIELDS"); denyFields != "" {
		c.DenyFields = append(c.DenyFields, strings.Split(denyFields, ",")...)
	}
	if exportDir := os.Getenv("QB_EXPORT_DIRECTORY"); exportDir != "" {
		c.Server.ExportDirectory = exportDir
	}
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Server.Transport != "stdio" && c.Server.Transport != "http" {
		return fmt.Errorf("transport must be 'stdio' or 'http', got '%s'", c.Server.Transport)
	}

	for i, db := range c.Databases {
		if db.Name == "" {
			return fmt.Errorf("database %d: name is required", i)
		}
		if db.Type != "postgres" && db.Type != "mysql" && db.Type != "mongodb" {
			return fmt.Errorf("database %s: type must be 'postgres', 'mysql', or 'mongodb'", db.Name)
		}
		if db.Host == "" {
			return fmt.Errorf("database %s: host is required", db.Name)
		}
		if db.Port <= 0 {
			return fmt.Errorf("database %s: port must be positive", db.Name)
		}
	}

	return nil
}

// GetDenyFieldSet returns a set of denied field names for O(1) lookup.
func (c *Config) GetDenyFieldSet() map[string]bool {
	set := make(map[string]bool, len(c.DenyFields))
	for _, field := range c.DenyFields {
		// Normalize to lowercase for case-insensitive matching
		set[strings.ToLower(field)] = true
	}
	return set
}

// SaveToFile writes the configuration to a JSON file.
func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
