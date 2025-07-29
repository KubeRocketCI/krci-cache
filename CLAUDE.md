# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

KubeRocketCI Cache (krci-cache) is a high-performance HTTP caching server for CI/CD pipeline artifacts, built in Go with the Echo framework. It supports file uploads, tar.gz extraction, and secure file management with production-grade features like authentication, concurrent upload limiting, and observability.

## Key Components

- **main.go**: Application entry point with structured logging and graceful shutdown
- **uploader/uploader.go**: Core HTTP server with Echo framework, authentication, and file operations
- **uploader/untar.go**: Secure tar.gz extraction with size limits and path traversal protection
- **uploader/uploader_test.go** and **uploader/untar_test.go**: Comprehensive test suites

## Common Development Commands

### Building and Testing
```bash
# Build binary for current architecture
make build

# Build for all supported architectures (amd64, arm64)
make build-all

# Run tests with coverage
make test

# Generate HTML coverage report
make test-coverage

# Run tests in short mode
make test-short

# Format and vet code
make fmt
make vet
```

### Linting
```bash
# Run golangci-lint
make lint

# Run golangci-lint with auto-fix
make lint-fix
```

### Docker Operations
```bash
# Build Docker image
make docker-build

# Push Docker image
make docker-push
```

### E2E Testing
```bash
# Setup complete e2e environment
make e2e-setup-cluster

# Run e2e tests
make e2e-test

# Cleanup e2e resources
make e2e-cleanup
```

### Tool Management
```bash
# Install all development tools
make install-tools

# Clean build artifacts
make clean

# Clean tools cache
make clean-tools

# Complete cleanup
make clean-all
```

## Architecture

### Server Configuration
The server uses environment-based configuration with these key variables:
- `UPLOADER_HOST`, `UPLOADER_PORT`: Server binding
- `UPLOADER_DIRECTORY`: Upload directory (default: ./pub)
- `UPLOADER_UPLOAD_CREDENTIALS`: Basic auth (format: username:password)
- `UPLOADER_MAX_UPLOAD_SIZE`: Regular file size limit
- `UPLOADER_MAX_CONCURRENT_UPLOADS`: Concurrency limit
- `UPLOADER_REQUEST_TIMEOUT`: Request timeout

### Security Features
- Path traversal protection via `isPathSafe()` functions
- Basic HTTP authentication for protected endpoints
- Tar.gz extraction with size limits (2GB per file, 8GB total)
- Symlink and hard link prevention in archives
- Context-aware operations with cancellation support

### API Endpoints
- `GET /health`: Health check with JSON response
- `POST /upload`: File upload with explicit tar.gz extraction (requires `targz=true` form parameter)
- `DELETE /upload`: Single file deletion
- `DELETE /delete`: Bulk deletion of old files
- `HEAD /:path`: File metadata and caching headers
- `GET /*`: Static file serving

**Important**: Tar.gz extraction only occurs when explicitly requested with `targz=true` form parameter. Files with `.tar.gz` extension are stored as regular files unless this flag is set.

### Concurrency Model
- Semaphore-based upload limiting (`uploadSem` channel)
- Context-aware operations for cancellation
- Graceful shutdown with configurable timeout
- Structured JSON logging with request tracing

## Testing Strategy

### Unit Tests
- `uploader_test.go`: Server functionality, security, configuration
- `untar_test.go`: Archive extraction, security validations, edge cases
- Use testify/assert for assertions
- Test both success and failure scenarios

### E2E Tests
- Kubernetes-based testing with Chainsaw framework
- Kind cluster for local testing
- Tests deployment, health checks, and service accessibility
- Located in `e2e/` directory with automated setup

## Development Workflow

1. **Code Changes**: Make changes following existing patterns
2. **Format**: Run `make fmt vet` before committing
3. **Test**: Run `make test` to ensure all tests pass
4. **Lint**: Run `make lint` to check code quality
5. **E2E**: Run `make e2e-setup-cluster && make e2e-test` for integration testing
6. **Build**: Run `make build` to verify compilation

## Important Notes

- Always run `make lint` and `make test` before committing changes
- Security is critical - never bypass path traversal or size limit checks
- All file operations should be context-aware for proper cancellation
- Use structured logging with slog for observability
- Follow the existing error handling patterns with proper HTTP status codes
- When adding new endpoints, ensure proper authentication integration