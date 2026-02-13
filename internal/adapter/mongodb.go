// Package adapter provides MongoDB database adapter implementation.
package adapter

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoAdapter implements the Adapter interface for MongoDB.
type MongoAdapter struct {
	name   string
	config MongoConfig
	client *mongo.Client
	db     *mongo.Database
}

// MongoConfig holds MongoDB connection configuration.
type MongoConfig struct {
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	AuthSource     string
	MaxConnections int
	ConnectTimeout int
}

// NewMongoAdapter creates a new MongoDB adapter.
func NewMongoAdapter(name string, config MongoConfig) *MongoAdapter {
	if config.MaxConnections <= 0 {
		config.MaxConnections = 10
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 10
	}
	if config.AuthSource == "" {
		config.AuthSource = "admin"
	}
	return &MongoAdapter{name: name, config: config}
}

// Connect establishes a connection to MongoDB.
func (a *MongoAdapter) Connect(ctx context.Context) error {
	uri := fmt.Sprintf("mongodb://%s:%d/%s?connectTimeoutMS=%d",
		a.config.Host, a.config.Port, a.config.Database, a.config.ConnectTimeout*1000)

	if a.config.User != "" {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?authSource=%s&connectTimeoutMS=%d",
			a.config.User, a.config.Password, a.config.Host, a.config.Port,
			a.config.Database, a.config.AuthSource, a.config.ConnectTimeout*1000)
	}

	clientOpts := options.Client().ApplyURI(uri).
		SetMaxPoolSize(uint64(a.config.MaxConnections)).SetMinPoolSize(1)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("connecting to mongodb: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("pinging mongodb: %w", err)
	}
	a.client = client
	a.db = client.Database(a.config.Database)
	return nil
}

func (a *MongoAdapter) Close() error {
	if a.client != nil {
		return a.client.Disconnect(context.Background())
	}
	return nil
}

func (a *MongoAdapter) Type() string { return "mongodb" }
func (a *MongoAdapter) Name() string { return a.name }
func (a *MongoAdapter) Ping(ctx context.Context) error { return a.client.Ping(ctx, nil) }

// DiscoverSchema infers schema from MongoDB collections.
func (a *MongoAdapter) DiscoverSchema(ctx context.Context) (*Schema, error) {
	schema := &Schema{
		Database: a.config.Database, Type: "mongodb",
		Collections: make(map[string]*Collection), DiscoveredAt: time.Now(),
	}

	collections, err := a.db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}

	for _, name := range collections {
		if strings.HasPrefix(name, "system.") {
			continue
		}
		coll, err := a.discoverCollection(ctx, name)
		if err != nil {
			return nil, err
		}
		schema.Collections[name] = coll
	}
	return schema, nil
}

func (a *MongoAdapter) discoverCollection(ctx context.Context, name string) (*Collection, error) {
	coll := a.db.Collection(name)
	count, _ := coll.EstimatedDocumentCount(ctx)

	cursor, err := coll.Aggregate(ctx, mongo.Pipeline{{{Key: "$sample", Value: bson.M{"size": 100}}}})
	if err != nil {
		cursor, err = coll.Find(ctx, bson.M{}, options.Find().SetLimit(100))
		if err != nil {
			return nil, err
		}
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	cursor.All(ctx, &docs)

	return &Collection{Name: name, Fields: inferSchemaFromDocs(docs), SampleSize: len(docs), DocCount: count}, nil
}

func inferSchemaFromDocs(docs []bson.M) []Field {
	fieldInfo := make(map[string]*fieldStats)
	for _, doc := range docs {
		analyzeDoc("", doc, fieldInfo)
	}
	var fields []Field
	for name, stats := range fieldInfo {
		if !strings.Contains(name, ".") {
			fields = append(fields, stats.toField(name))
		}
	}
	return fields
}

type fieldStats struct {
	types    map[string]int
	nullable bool
	isArray  bool
}

func analyzeDoc(prefix string, doc bson.M, stats map[string]*fieldStats) {
	for key, value := range doc {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if stats[path] == nil {
			stats[path] = &fieldStats{types: make(map[string]int)}
		}
		fs := stats[path]
		if value == nil {
			fs.nullable = true
			continue
		}
		fs.types[inferBSONType(value)]++
		if m, ok := value.(bson.M); ok {
			analyzeDoc(path, m, stats)
		}
		if _, ok := value.(bson.A); ok {
			fs.isArray = true
		}
	}
}

func inferBSONType(value interface{}) string {
	switch value.(type) {
	case primitive.ObjectID:
		return "objectId"
	case string:
		return "string"
	case int, int32, int64:
		return "int"
	case float32, float64:
		return "float"
	case bool:
		return "bool"
	case primitive.DateTime, time.Time:
		return "datetime"
	case bson.M, bson.D:
		return "object"
	case bson.A:
		return "array"
	default:
		return reflect.TypeOf(value).String()
	}
}

func (fs *fieldStats) toField(name string) Field {
	types := make([]string, 0, len(fs.types))
	for t := range fs.types {
		types = append(types, t)
	}
	return Field{Name: name, Types: types, Nullable: fs.nullable, IsArray: fs.isArray}
}

// Execute runs a MongoDB query.
func (a *MongoAdapter) Execute(ctx context.Context, query *Query) (*Result, error) {
	start := time.Now()
	coll := a.db.Collection(query.Table)
	filter, opts := a.buildQuery(query)

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	cursor.All(ctx, &docs)

	result := &Result{Columns: query.Columns, Rows: make([]Row, 0, len(docs))}
	for _, doc := range docs {
		result.Rows = append(result.Rows, a.docToRow(doc, query.Columns))
	}
	result.RowCount = len(result.Rows)
	result.ExecutionTime = time.Since(start)
	result.HasMore = query.Limit > 0 && len(result.Rows) == query.Limit
	return result, nil
}

// Stream returns a streaming cursor.
func (a *MongoAdapter) Stream(ctx context.Context, query *Query) (RowStream, error) {
	coll := a.db.Collection(query.Table)
	filter, opts := a.buildQuery(query)
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	return &mongoRowStream{cursor: cursor, columns: query.Columns, adapter: a}, nil
}

func (a *MongoAdapter) buildQuery(q *Query) (bson.M, *options.FindOptions) {
	filter := bson.M{}
	opts := options.Find()

	for _, f := range q.Filters {
		op := map[string]string{"ne": "$ne", "gt": "$gt", "gte": "$gte", "lt": "$lt", "lte": "$lte", "in": "$in"}[f.Operator]
		if op == "" {
			filter[f.Column] = f.Value
		} else {
			filter[f.Column] = bson.M{op: f.Value}
		}
	}

	if len(q.Columns) > 0 {
		proj := bson.M{}
		for _, c := range q.Columns {
			proj[c] = 1
		}
		opts.SetProjection(proj)
	}

	if len(q.OrderBy) > 0 {
		sort := bson.D{}
		for _, ob := range q.OrderBy {
			dir := 1
			if strings.ToLower(ob.Direction) == "desc" {
				dir = -1
			}
			sort = append(sort, bson.E{Key: ob.Column, Value: dir})
		}
		opts.SetSort(sort)
	}

	if q.Limit > 0 {
		opts.SetLimit(int64(q.Limit))
	}
	if q.Offset > 0 {
		opts.SetSkip(int64(q.Offset))
	}
	return filter, opts
}

func (a *MongoAdapter) docToRow(doc bson.M, columns []string) Row {
	row := make(Row)
	if len(columns) == 0 {
		for k, v := range doc {
			row[k] = a.convertValue(v)
		}
	} else {
		for _, c := range columns {
			if v, ok := doc[c]; ok {
				row[c] = a.convertValue(v)
			}
		}
	}
	return row
}

func (a *MongoAdapter) convertValue(v interface{}) interface{} {
	switch val := v.(type) {
	case primitive.ObjectID:
		return val.Hex()
	case primitive.DateTime:
		return val.Time().Format(time.RFC3339)
	case bson.A:
		arr := make([]interface{}, len(val))
		for i, e := range val {
			arr[i] = a.convertValue(e)
		}
		return arr
	case bson.M:
		m := make(map[string]interface{})
		for k, v2 := range val {
			m[k] = a.convertValue(v2)
		}
		return m
	default:
		return v
	}
}

type mongoRowStream struct {
	cursor  *mongo.Cursor
	columns []string
	current Row
	err     error
	adapter *MongoAdapter
}

func (s *mongoRowStream) Next() bool {
	if !s.cursor.Next(context.Background()) {
		return false
	}
	var doc bson.M
	if err := s.cursor.Decode(&doc); err != nil {
		s.err = err
		return false
	}
	s.current = s.adapter.docToRow(doc, s.columns)
	return true
}

func (s *mongoRowStream) Row() Row   { return s.current }
func (s *mongoRowStream) Err() error { return s.cursor.Err() }
func (s *mongoRowStream) Close() error { return s.cursor.Close(context.Background()) }
