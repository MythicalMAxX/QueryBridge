# Contributing to QueryBridge

Thank you for your interest in contributing to QueryBridge! We welcome contributions from the community to help make this MCP server even better.

## How to Contribute

### Reporting Bugs
- Search existing issues to see if the bug has already been reported.
- If not, open a new issue with a clear title and description.
- Include steps to reproduce the bug and details about your environment (OS, Go version, database type).

### Suggesting Enhancements
- Open a new issue to discuss your idea before starting work.
- Provide a clear use case for the enhancement.

### Pull Requests
1. Fork the repository.
2. Create a new branch for your feature or bugfix: `git checkout -b feature/awesome-feature`
3. Make your changes and ensure they follow the project's code style.
4. Add tests for your changes.
5. Run existing tests to ensure no regressions: `go test ./...`
6. Commit your changes with clear, descriptive commit messages.
7. Push to your fork and submit a pull request.

## Development Setup

### Prerequisites
- [Go](https://go.dev/dl/) (version 1.24.0 or later)
- [Docker](https://www.docker.com/products/docker-desktop/) (for running database containers)

### Getting Started
1. Clone the repository:
   ```bash
   git clone https://github.com/MythicalMAxX/QueryBridge.git
   cd QueryBridge/QueryBridge
   ```
2. Start the test databases:
   ```bash
   cd ../docker
   docker-compose up -d
   ```
3. Run the tests:
   ```bash
   cd ../QueryBridge
   go test ./...
   ```

## Code Style
- Follow standard Go formatting (`go fmt`).
- Use descriptive variable and function names.
- Document public APIs with comments.

## License
By contributing, you agree that your contributions will be licensed under the MIT License.
