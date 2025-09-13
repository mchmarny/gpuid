# Contributing to GPU Serial Number Exporter (gpuid)

Thank you for your interest in contributing to the `gpuid` project! This document provides guidelines and information for contributors.

## Getting Started

Before contributing, please:

1. **Read the Documentation**: Familiarize yourself with the [README](README.md) and project structure
2. **Check Existing Issues**: Look for existing issues or discussions related to your contribution
3. **Open an Issue**: For new features or significant changes, create an issue to discuss the approach
4. **Follow the Code of Conduct**: Please follow our [Code of Conduct](CODE-OF-CONDUCT.md)

## Contribution Workflow

### 1. Fork and Clone
```shell
# Fork the repository on GitHub
git clone https://github.com/yourusername/gpuid.git
cd gpuid
```

### 2. Create a Feature Branch
```shell
git checkout -b feature/your-feature-name
```

### 3. Make Changes
- Follow the coding standards outlined below
- Write tests for new functionality
- Update documentation as needed

### 4. Commit Your Changes
Use [Conventional Commits](https://www.conventionalcommits.org/) format:
```shell
git commit -m "feat: add new postgres connection pooling"
git commit -m "fix: resolve memory leak in GPU discovery"
git commit -m "docs: update exporter configuration examples"
```

### 5. Create Pull Request
- Push your branch to your fork
- Create a pull request with a clear description
- Link any related issues

## Development Environment Setup

### Prerequisites
- Go 1.25 or later
- Docker and Docker Compose (for local testing)
- kubectl (for Kubernetes integration testing)
- Access to a Kubernetes cluster (optional, for integration testing)

### Local Development Setup

1. **Install Dependencies**:
```shell
go mod download
```

2. **Verify Installation**:
```shell
# Run unit tests
make test

# Run linting
make lint

# Build the binary
make build
```

3. **Local Testing with Different Exporters**:

**Stdout Exporter** (default):
```shell
export CLUSTER_NAME="local-dev"
export NAMESPACE="default" 
export LABEL_SELECTOR="app=test"
export EXPORTER_TYPE="stdout"
./gpuid
```

**PostgreSQL Exporter** (requires local PostgreSQL):
```shell
# Start PostgreSQL with Docker
docker run --name postgres-test -e POSTGRES_PASSWORD=test -p 5432:5432 -d postgres:15

# Configure environment
export POSTGRES_HOST="localhost"
export POSTGRES_PORT="5432"
export POSTGRES_DB="postgres"
export POSTGRES_USER="postgres"
export POSTGRES_PASSWORD="test"
export POSTGRES_SSLMODE="disable"
export EXPORTER_TYPE="postgres"
./gpuid
```

### Development Tools

The `Makefile` provides essential development commands:

```shell
# View all available targets
make help

# Development workflow
make test     # Run unit tests
make lint     # Run code linting  
make build    # Build binary
make clean    # Clean build artifacts

# Quality assurance (run before submitting PR)
make quality  # Run all tests, linting, and security checks

# Container operations
make image    # Build container image
make push     # Push to registry

# Release management (maintainers only)
make bump-patch  # v1.2.3 → v1.2.4
make bump-minor  # v1.2.3 → v1.3.0  
make bump-major  # v1.2.3 → v2.0.0
```

### Code Quality Standards

Before submitting a pull request:

1. **Run Quality Checks**:
```shell
make quality
```

2. **Verify Tests Pass**:
```shell
go test -v ./...
```

3. **Check Test Coverage**:
```shell
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

4. **Format Code**:
```shell
go fmt ./...
gofumpt -w .
```

## Project Architecture

### Design Principles

- **12-Factor App**: Configuration via environment variables
- **Interface-Driven**: Clean abstractions for extensibility
- **Cloud-Native**: Kubernetes-first design with proper RBAC
- **Observability**: Structured logging and Prometheus metrics
- **Security**: SLSA attestation and minimal container images
- **Reliability**: Health checks, retries, and graceful degradation

## Adding New Exporters

The exporter system is designed for easy extension. Here's how to add a new exporter:

### 1. Create the Exporter Package

Create `pkg/exporters/yourexporter/exporter.go`:

```go
package yourexporter

import (
    "context"
    "log/slog"
    "github.com/mchmarny/gpuid/pkg/gpu"
)

// Environment variable constants
const (
    EnvYourExporterConfig = "YOUR_EXPORTER_CONFIG"
    // Add other required env vars
)

type Exporter struct {
    // Your configuration fields
}

func New() (*Exporter, error) {
    // Load configuration from environment variables
    // Initialize any required connections/clients
    return &Exporter{}, nil
}

func (e *Exporter) Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
    // Implement data export logic
    return nil
}

func (e *Exporter) Close(ctx context.Context) error {
    // Cleanup any resources
    return nil
}

func (e *Exporter) Health(ctx context.Context) error {
    // Implement health check logic
    return nil
}
```

### 2. Register in Factory

Add your exporter to `pkg/runner/export.go`:

```go
import "github.com/mchmarny/gpuid/pkg/exporters/yourexporter"

func GetExporter(ctx context.Context, log *slog.Logger, config ExporterConfig) (ExporterBackend, error) {
    switch strings.ToLower(config.Type) {
    case "yourexporter":
        return yourexporter.New()
    // ... existing cases
    }
}
```

### 3. Add Tests

Create `pkg/exporters/yourexporter/exporter_test.go`:

```go
package yourexporter

import (
    "context"
    "testing"
    "github.com/mchmarny/gpuid/pkg/gpu"
)

func TestExporter_Write(t *testing.T) {
    // Test implementation
}

func TestExporter_Health(t *testing.T) {
    // Test health check
}
```

### 4. Update Documentation

- Add configuration example to README.md
- Document environment variables
- Add deployment examples if needed

## Testing Guidelines

### Unit Tests
- Test all public functions
- Use table-driven tests for multiple scenarios
- Mock external dependencies
- Aim for >80% code coverage

### Integration Tests
- Test exporter implementations with real backends (when possible)
- Use testcontainers for database testing
- Test Kubernetes integration with kind/minikube

### Example Test Structure:
```go
func TestExporter_Write(t *testing.T) {
    tests := []struct {
        name    string
        records []*gpu.SerialNumberReading
        wantErr bool
    }{
        {
            name: "valid records",
            records: []*gpu.SerialNumberReading{
                {Cluster: "test", GPU: "test-gpu"},
            },
            wantErr: false,
        },
        // Add more test cases
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```
## Local Development

```shell
# Install dependencies
go mod download

# Run tests
go test ./...

# Build binary
go build -o gpuid ./cmd/gpuid

# Run locally (requires kubectl context)
./gpuid
```

## Adding New Exporters

1. Create a new package in `pkg/exporters/newexporter/`
2. Implement the `ExporterBackend` interface:
   ```go
   type ExporterBackend interface {
       Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error
       Close(ctx context.Context) error  
       Health(ctx context.Context) error
   }
   ```
3. Add the exporter to the factory function in `pkg/runner/export.go`
4. Update documentation and examples


## Code Style and Standards

### Go Code Standards
- Follow effective Go guidelines
- Use `gofmt` and `gofumpt` for formatting
- Follow Go naming conventions
- Add comments for exported functions and types
- Use structured logging with `slog`

### Environment Variables
- Use UPPER_CASE with underscores
- Group related variables with common prefixes
- Provide sensible defaults where possible
- Document all environment variables

### Error Handling
- Always handle errors explicitly
- Use wrapped errors with context: `fmt.Errorf("operation failed: %w", err)`
- Log errors with appropriate context
- Fail fast for configuration errors

### Logging Standards
```go
// Good: Structured logging with context
log.Info("export completed",
    "endpoint", e.Endpoint,
    "records", len(records),
    "size_bytes", len(data),
    "status", resp.StatusCode)

// Avoid: Unstructured logging
log.Printf("Exported %d serials from %s", len(serials), node)
```

## Pull Request Guidelines

### Before Submitting
- [ ] Run `make quality` and ensure all checks pass
- [ ] Add or update tests for your changes
- [ ] Update documentation if needed
- [ ] Test with different exporter configurations
- [ ] Verify Kubernetes manifests if changed

### PR Description Template
```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass (if applicable)
- [ ] Manual testing completed

## Checklist
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] Tests added/updated
```

## Release Process

The project uses automated releases triggered by git tags:

### For Maintainers

1. **Prepare Release**:
```shell
# Update version and changelog
make bump-patch  # or bump-minor, bump-major
```

2. **Verify CI Pipeline**: 
The GitHub Actions pipeline will:
- Run quality checks and tests
- Build multi-architecture container images
- Generate SLSA attestation
- Create GitHub release with artifacts
- Update container registries

3. **Validate Release**:
```shell
# Verify image attestation
cosign verify-attestation ghcr.io/mchmarny/gpuid:v1.x.x
```

Thank you for contributing to the GPU Serial Number Exporter project! 
