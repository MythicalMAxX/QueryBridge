// Package adapter provides Redis database adapter implementation.
package adapter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter implements the Adapter interface for Redis.
type RedisAdapter struct {
	name   string
	config RedisConfig
	client *redis.Client
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Host           string
	Port           int
	Password       string
	Database       int // Redis DB number (0-15)
	MaxConnections int
}

// NewRedisAdapter creates a new Redis adapter.
func NewRedisAdapter(name string, config RedisConfig) *RedisAdapter {
	if config.Port == 0 {
		config.Port = 6379
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 10
	}
	return &RedisAdapter{
		name:   name,
		config: config,
	}
}

// Connect establishes a connection to Redis.
func (a *RedisAdapter) Connect(ctx context.Context) error {
	a.client = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", a.config.Host, a.config.Port),
		Password:     a.config.Password,
		DB:           a.config.Database,
		PoolSize:     a.config.MaxConnections,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})

	if err := a.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("pinging redis: %w", err)
	}

	return nil
}

// Close closes the Redis connection.
func (a *RedisAdapter) Close() error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}

// Type returns the database type identifier.
func (a *RedisAdapter) Type() string {
	return "redis"
}

// Name returns the unique name of this adapter.
func (a *RedisAdapter) Name() string {
	return a.name
}

// Ping checks if Redis is reachable.
func (a *RedisAdapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx).Err()
}

// DiscoverSchema retrieves Redis key patterns as a pseudo-schema.
func (a *RedisAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database:     a.name,
		Type:         "redis",
		Collections:  make(map[string]*Collection),
		DiscoveredAt: time.Now(),
	}

	// Get key type distribution by sampling keys
	keyTypes := make(map[string]int64)
	var cursor uint64
	sampleCount := 0
	maxSamples := 1000

	for sampleCount < maxSamples {
		keys, nextCursor, err := a.client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			break
		}

		for _, key := range keys {
			keyType, err := a.client.Type(ctx, key).Result()
			if err == nil {
				keyTypes[keyType]++
				sampleCount++
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Create pseudo-collections for each key type
	for keyType, count := range keyTypes {
		schema.Collections[keyType] = &Collection{
			Name:       keyType,
			DocCount:   count,
			SampleSize: int(count),
			Fields: []Field{
				{Name: "key", Types: []string{"string"}},
				{Name: "value", Types: []string{keyType}},
			},
		}
	}

	// Get total key count
	dbSize, _ := a.client.DBSize(ctx).Result()
	schema.Collections["_keys"] = &Collection{
		Name:     "_keys",
		DocCount: dbSize,
	}

	return schema, nil
}

// Execute runs a Redis query.
func (a *RedisAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()

	// Parse Redis-specific query
	if query.Raw != "" {
		return a.executeRawCommand(ctx, query.Raw, start)
	}

	// Standard query: treat table as key pattern
	pattern := query.Table
	if pattern == "" {
		pattern = "*"
	}

	limit := query.Limit
	if limit == 0 {
		limit = 100
	}

	var resultRows []Row
	var cursor uint64
	count := 0

	for count < limit {
		keys, nextCursor, err := a.client.Scan(ctx, cursor, pattern, int64(limit-count)).Result()
		if err != nil {
			return nil, fmt.Errorf("scanning keys: %w", err)
		}

		for _, key := range keys {
			if count >= limit {
				break
			}

			row := make(Row)
			row["key"] = key

			// Get key type and value
			keyType, _ := a.client.Type(ctx, key).Result()
			row["type"] = keyType

			switch keyType {
			case "string":
				val, _ := a.client.Get(ctx, key).Result()
				row["value"] = val
			case "list":
				val, _ := a.client.LRange(ctx, key, 0, 9).Result()
				row["value"] = strings.Join(val, ", ")
				row["length"], _ = a.client.LLen(ctx, key).Result()
			case "set":
				val, _ := a.client.SMembers(ctx, key).Result()
				if len(val) > 10 {
					val = val[:10]
				}
				row["value"] = strings.Join(val, ", ")
				row["cardinality"], _ = a.client.SCard(ctx, key).Result()
			case "zset":
				val, _ := a.client.ZRange(ctx, key, 0, 9).Result()
				row["value"] = strings.Join(val, ", ")
				row["cardinality"], _ = a.client.ZCard(ctx, key).Result()
			case "hash":
				val, _ := a.client.HGetAll(ctx, key).Result()
				row["value"] = fmt.Sprintf("%v", val)
				row["fields"], _ = a.client.HLen(ctx, key).Result()
			}

			ttl, _ := a.client.TTL(ctx, key).Result()
			if ttl > 0 {
				row["ttl"] = ttl.Seconds()
			} else if ttl == -1 {
				row["ttl"] = "no expiry"
			}

			resultRows = append(resultRows, row)
			count++
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return &Result{
		Columns:       []string{"key", "type", "value", "ttl"},
		Rows:          resultRows,
		RowCount:      len(resultRows),
		ExecutionTime: time.Since(start),
	}, nil
}

func (a *RedisAdapter) executeRawCommand(ctx context.Context, command string, start time.Time) (*Result, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	args := make([]interface{}, len(parts))
	for i, p := range parts {
		args[i] = p
	}

	result, err := a.client.Do(ctx, args...).Result()
	if err != nil {
		return nil, err
	}

	row := make(Row)
	row["result"] = fmt.Sprintf("%v", result)

	return &Result{
		Columns:       []string{"result"},
		Rows:          []Row{row},
		RowCount:      1,
		ExecutionTime: time.Since(start),
	}, nil
}

// Stream returns a streaming result set (uses Execute internally for Redis).
func (a *RedisAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	result, err := a.Execute(ctx, query)
	if err != nil {
		return nil, err
	}

	return &redisRowStream{
		rows:    result.Rows,
		columns: result.Columns,
		index:   -1,
	}, nil
}

// redisRowStream implements RowStream for Redis.
type redisRowStream struct {
	rows    []Row
	columns []string
	index   int
	current Row
}

func (s *redisRowStream) Next() bool {
	s.index++
	if s.index >= len(s.rows) {
		return false
	}
	s.current = s.rows[s.index]
	return true
}

func (s *redisRowStream) Row() Row {
	return s.current
}

func (s *redisRowStream) Err() error {
	return nil
}

func (s *redisRowStream) Close() error {
	return nil
}

// Helper to parse port from string
func parseRedisPort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 6379
	}
	return port
}
