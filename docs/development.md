# Development Guide

Guide for developers contributing to LoKey.

## Table of Contents

- [Getting Started](#getting-started)
- [Project Structure](#project-structure)
- [Building from Source](#building-from-source)
- [Code Quality](#code-quality)
- [Testing](#testing)
- [Using Task](#using-task)
- [Contributing](#contributing)

## Getting Started

### Prerequisites

**Required:**
- Go 1.24 or later
- Docker and Docker Compose
- Git

**Optional:**
- [Task](https://taskfile.dev/) - Task runner (recommended)
- [golangci-lint](https://golangci-lint.run/) - Linter
- [swag](https://github.com/swaggo/swag) - Swagger documentation generator

**Quick Install:**
```
bash
# Go (macOS)
brew install go

# Task
brew install go-task

# Development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/swaggo/swag/cmd/swag@latest
```
### Clone and Build
```
bash
# Clone repository
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey

# Download dependencies
go mod download

# Build all services
task build
```
### IDE Setup

**VS Code** - Install extensions:
- Go (golang.go)
- Docker (ms-azuretools.vscode-docker)

**GoLand/IntelliJ** - Works out of the box with Go plugin.

### Development Workflow

**Using Docker Compose (Recommended):**
```
bash
# Start all services
docker compose up -d

# View logs
docker compose logs -f

# Restart after changes
docker compose restart api

# Stop services
docker compose down
```
**Running Locally:**

```bash
# Terminal 1 - Controller
cd cmd/controller && go run main.go

# Terminal 2 - Fortuna
cd cmd/fortuna && go run main.go

# Terminal 3 - API
cd cmd/api && go run main.go

# Terminal 4 - Test
curl http://localhost:8080/api/v1/health
```

## Project Structure

```
.
├── cmd/                          # Service entry points
│   ├── api/main.go              # API service
│   ├── controller/main.go       # Controller service
│   └── fortuna/main.go          # Fortuna service
│
├── pkg/                          # Shared packages
│   ├── api/                     # REST API implementation
│   │   ├── server.go
│   │   ├── polling.go
│   │   └── docs/                # Generated Swagger docs
│   ├── atecc608a/               # Hardware interface
│   │   └── controller.go
│   ├── database/                # Data persistence
│   │   ├── interface.go
│   │   ├── bolt.go
│   │   └── factory.go
│   └── fortuna/                 # Cryptographic PRNG
│       └── fortuna.go
│
├── docs/                         # Documentation
├── .github/workflows/           # CI/CD pipelines
├── docker-compose.yaml          # Development compose
├── Taskfile.yaml                # Task definitions
└── go.mod
```


### Key Packages

- **`pkg/api`** - REST API, request handling, background polling
- **`pkg/atecc608a`** - I2C communication with ATECC608A chip
- **`pkg/database`** - BoltDB implementation, queue management
- **`pkg/fortuna`** - Fortuna algorithm, AES-256 generation

## Building from Source

### Build All Services

```shell script
# Using Task
task build

# Or manually
mkdir -p bin
go build -o bin/controller ./cmd/controller
go build -o bin/api ./cmd/api
go build -o bin/fortuna ./cmd/fortuna
```


### Build Individual Services

```shell script
task build_controller
task build_api
task build_fortuna
```


### Cross-Compilation

```shell script
# Raspberry Pi 4/5 (ARM64)
GOOS=linux GOARCH=arm64 go build -o bin/controller-arm64 ./cmd/controller

# Raspberry Pi 2/3 (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 go build -o bin/controller-armv7 ./cmd/controller

# AMD64
GOOS=linux GOARCH=amd64 go build -o bin/controller-amd64 ./cmd/controller
```


### Docker Build

```shell script
# Development images
docker compose build

# Production images (optimized)
docker build -t lokey-api:latest -f cmd/api/Dockerfile.action .

# Multi-architecture (for Raspberry Pi)
task build_images_and_registry
```


## Code Quality

### Formatting

```shell script
# Format all code
task fmt

# Or manually
gofmt -s -w .
goimports -w .
```


### Linting

```shell script
# Run linter
task lint

# Or manually
golangci-lint run

# Auto-fix issues
golangci-lint run --fix
```


### Generate Swagger Docs

```shell script
# Generate API documentation
task generate_swagger

# Or manually
cd pkg/api
swag init --parseDependency --parseInternal \
  --generalInfo server.go \
  --dir ./ \
  --output ./docs
```


## Testing

### Running Tests

```shell script
# Run all tests with summary
task test

# Run tests for specific package
go test ./pkg/api

# Run specific test
go test ./pkg/api -run TestServer_GetRandomData

# Run with coverage
go test ./pkg/api -cover

# Run with race detection
go test ./pkg/api -race
```

The `task test` command provides clean output showing only failures, errors, and a summary with test statistics.

For comprehensive testing documentation, see **[Testing Guide](tests.md)**.

## Using Task

Task is the recommended build tool. View available tasks:

```shell script
task --list
```


### Common Tasks

```shell script
task build                      # Build all services
task fmt                        # Format code
task lint                       # Run linter
task test                       # Run tests with summary
task generate_swagger           # Generate API docs
task dev_up                     # Start development environment
task dev_down                   # Stop development environment
task dev_logs                   # View logs
task build_images_and_registry  # Build for Raspberry Pi
task all                        # Format, lint, test, and build
```


## Contributing

### Workflow

1. **Fork and clone:**

```shell script
git clone https://github.com/yourusername/Lokey.git
cd Lokey
git remote add upstream https://github.com/LokeyTRaaS/Lokey.git
```


2. **Create feature branch:**

```shell script
git checkout -b feature/my-feature
```


3. **Make changes:**

```shell script
# Edit files
vim pkg/api/server.go

# Format and lint
task fmt
task lint
```


4. **Commit with conventional messages:**

```shell script
git commit -m "feat: add new feature"
```


**Commit prefixes:**
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation
- `refactor:` - Code refactoring
- `chore:` - Maintenance
- `ci:` - CI/CD changes

5. **Push and create PR:**

```shell script
git push origin feature/my-feature
# Create pull request on GitHub
```


### Code Review Checklist

- [ ] Code follows Go best practices
- [ ] No linter warnings
- [ ] Tests added/updated for new features
- [ ] All tests pass (`task test`)
- [ ] Documentation updated
- [ ] Swagger annotations added (for API changes)
- [ ] Backwards compatible

### Debugging Tips

**View Docker logs:**
```shell script
docker compose logs -f api
```


**Execute shell in container:**
```shell script
docker compose exec api sh
```


**Common issues:**

- **"i2c: no such file"** - Check device mapping in `docker-compose.yaml`
- **"database locked"** - Only one API instance can run at a time
- **"no data available"** - Wait for polling cycle, check controller health

## Release Process

### Creating a Release

1. **Update version and CHANGELOG**

2. **Create git tag:**

```shell script
git tag -a v1.1.0 -m "Release v1.1.0"
git push origin v1.1.0
```


3. **GitHub Actions automatically:**
    - Builds multi-arch binaries
    - Creates Docker images
    - Generates documentation
    - Creates GitHub release

### Version Numbering

LoKey uses [Semantic Versioning](https://semver.org/):
- **MAJOR**: Breaking changes
- **MINOR**: New features (backwards compatible)
- **PATCH**: Bug fixes

## Resources

### Documentation
- [API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)
- [Architecture Guide](architecture.md)
- [Deployment Guide](deployment.md)
- [Testing Guide](tests.md)

### Go Resources
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### Tools
- [Task](https://taskfile.dev/)
- [golangci-lint](https://golangci-lint.run/)
- [Swag](https://github.com/swaggo/swag)

### Community
- [GitHub Issues](https://github.com/LokeyTRaaS/Lokey/issues)
- [GitHub Discussions](https://github.com/LokeyTRaaS/Lokey/discussions)

## Next Steps

- **[Quickstart](quickstart.md)** - Run LoKey locally
- **[Architecture](architecture.md)** - Understand the system
- **[API Examples](api-examples.md)** - Learn the API
- **[Testing Guide](tests.md)** - Run and write tests
- **[Deployment](deployment.md)** - Deploy to production

Thank you for contributing to LoKey! 🎲