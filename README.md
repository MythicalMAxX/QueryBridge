# QueryBridge 🌉

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/MythicalMAxX/QueryBridge?filename=QueryBridge/go.mod)](https://go.dev/)
[![MCP Compatible](https://img.shields.io/badge/MCP-Compatible-blue.svg)](https://modelcontextprotocol.io/)

**QueryBridge** is a production-grade MCP (Model Context Protocol) server written in Go, designed to give AI agents secure, natural language access to a wide variety of SQL and NoSQL databases.

It bridges the gap between Large Language Models (LLMs) and your data infrastructure, providing a unified interface for schema discovery, querying, and streaming data.

---

## 🌐 Live Demo

Experience QueryBridge in action: **[QueryBridge Demo](https://qb-demo-17ep.onrender.com/)**

---

## 🚀 Key Features

- **Unified Database Interface**: Query SQL and NoSQL databases using the same MCP tools.
- **Auto-Schema Discovery**: Automatically detects tables, columns, relationships, and collection structures.
- **Production-Ready Security**: Built-in field denylist to prevent sensitive data (passwords, tokens, SSNs) from ever reaching the LLM.
- **High Performance**: Native Go implementation with row-by-row streaming for large datasets.
- **Multi-Format Export**: Export results directly to CSV or JSONL.
- **Extensible Architecture**: Easily add new database adapters.

---

## 🔌 Connection Methods

QueryBridge can be integrated into your workflow through multiple interfaces:

- **MCP (Model Context Protocol)**:
  - **stdio**: Connect directly to AI clients like Claude Desktop by running the binary.
  - **SSE (Experimental)**: Support for remote MCP connections via Server-Sent Events.
- **REST API**:
  - Exposes standard endpoints for schema discovery and querying.
  - Ideal for integration with custom web frontends or non-MCP agents.
- **Embedded Library**:
  - The core logic is available in `pkg/querybridge` for direct integration into other Go projects (like the accompanying chatbot).

---

- **Audit Logging**: Comprehensive auditing of all data access queries.
- **Metrics**: Built-in metrics support for observability.
- **Schema Management**: In-memory registry for fast schema access and discovery.
- **Security**: Field denylist for sensitive data protection.

---

## 🗄️ Supported Databases

QueryBridge supports a vast range of data sources out of the box:

| Type | Databases |
| :--- | :--- |
| **Relational (SQL)** | PostgreSQL, MySQL, SQLite, SQL Server, ClickHouse, InfluxDB |
| **NoSQL / Document** | MongoDB, CouchDB, Elasticsearch |
| **Graph / Key-Value** | Neo4j, Redis, Cassandra |
| **File Formats** | CSV, Excel (via adapters) |

---

## 🛠️ Quick Start

### 1. Prerequisites
- [Go 1.24.0+](https://go.dev/dl/)
- Docker (for running local test databases)

### 2. Setup Databases
```bash
# Navigate to the docker directory
cd ../docker
docker-compose up -d
```

### 3. Build and Run
```bash
# Navigate to the QueryBridge core
cd ../QueryBridge
go mod tidy
go build -o querybridge.exe .
./querybridge.exe
```

---

## ⚙️ Configuration

Create a `config.json` in the `QueryBridge` directory to define your connections:

```json
{
  "databases": [
    {
      "name": "production_pg",
      "type": "postgres",
      "host": "localhost",
      "port": 5432,
      "database": "app_db",
      "user": "querybridge",
      "password": "secure_password",
      "max_connections": 20,
      "options": {
        "sslmode": "disable"
      }
    },
    {
      "name": "analytics_mongo",
      "type": "mongodb",
      "host": "localhost",
      "port": 27017,
      "database": "logs",
      "max_connections": 10
    }
  ],
  "deny_fields": ["password", "token", "secret", "ssn", "credit_card", "api_key"],
  "server": {
    "transport": "stdio",
    "address": ":8080",
    "query_timeout": 30,
    "stream_batch_size": 1000,
    "export_directory": "./exports",
    "readonly": true,
    "enable_metrics": true,
    "audit": {
      "enabled": true,
      "output": "audit.log",
      "pretty": true
    }
  }
}
```

---

## 🤖 MCP Tools

QueryBridge exposes the following tools to MCP-compatible clients (like Claude Desktop):

| Tool | Action |
| :--- | :--- |
| `describe_databases` | List all configured data sources. |
| `describe_tables` | Show schema details for a specific database. |
| `execute_query` | Run a standard query and get JSON results. |
| `stream_query` | Efficiently fetch large datasets via streaming. |
| `export_query` | Run a query and save the output to CSV/JSONL. |
| `multi_source_query` | Combine data from different databases in one request. |

---

## 📂 Project Structure

```text
QueryBridge/
├── cmd/                # Entry points for the application
├── internal/
│   ├── adapter/        # Individual database implementations
│   ├── config/         # Configuration logic
│   ├── mcp/            # MCP server protocol handling
│   ├── query/          # Query parser and planner
│   ├── schema/         # Schema discovery and registry
│   └── stream/         # Streaming and export logic
├── pkg/                # Public packages for library use
└── exports/            # Default directory for query exports
```

---

## 🤝 Contributing

We welcome contributions! Please see our [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to get involved.

---

## 🛡️ Security

Security is a core principle of QueryBridge. We implement strict field filtering and promote a read-only-first approach. For more details, see [SECURITY.md](SECURITY.md).

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
