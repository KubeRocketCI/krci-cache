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
- `UPLOADER_HOST`, `UPLOADER_PORT`: Server binding (defaults: `localhost`, `8080`)
- `UPLOADER_DIRECTORY`: Upload directory (default: `./pub`)
- `UPLOADER_UPLOAD_CREDENTIALS`: Basic auth (format: `username:password`); when unset, all endpoints are anonymous
- `UPLOADER_MAX_UPLOAD_SIZE`: Maximum request body size enforced by `middleware.BodyLimit` (default: `8GB`; accepts suffixes like `100M`, `2G`)
- `UPLOADER_SHUTDOWN_TIMEOUT`: Maximum time to wait for in-flight requests during graceful shutdown (default: `10m`; Go `time.ParseDuration` syntax)

### Security Features
- Path traversal protection on every mutating endpoint: target must lie strictly inside the upload directory (sibling-prefix escapes like `/data` vs `/data-evil` are rejected — see `TestUploadSiblingPrefixTraversal`)
- Basic HTTP authentication for protected endpoints; only `POST /upload`, `DELETE /upload`, `DELETE /delete` require credentials
- Tar.gz extraction with streaming size limits (`MaxFileSize` 2GB per file, `MaxTotalSize` 8GB total) enforced via `trackingWriter`
- Symlink and hard link entries in archives are rejected outright
- Request body size limit via `middleware.BodyLimit` (`UPLOADER_MAX_UPLOAD_SIZE`)

### API Endpoints
- `GET /health`: Health check with JSON response (always anonymous)
- `POST /upload`: File upload with explicit tar.gz extraction (requires `targz=true` form parameter)
- `DELETE /upload`: Single file deletion
- `DELETE /delete`: Bulk deletion of files older than `days`
- `HEAD /:path`: File metadata and caching headers (`Last-Modified`)
- `GET /*`: Static file serving via `http.FileServer` (range/conditional GET/sendfile-backed on Linux)

**Important**: Tar.gz extraction only occurs when `targz=true` is sent as a form parameter. Files whose names end in `.tar.gz` are stored as regular files unless this flag is set.

### Concurrency Model
The request path holds no shared mutable state and uses no application-level locks (no `sync.Mutex`, no semaphore, no in-memory index). Concurrency is governed at the kernel level:
- `http.Server.ReadHeaderTimeout` (10s) and `IdleTimeout` (120s) defend against slow clients without truncating legitimate large transfers. `ReadTimeout`/`WriteTimeout` are deliberately left at zero so they cannot abort multi-GB uploads/downloads.
- `middleware.BodyLimit` (`UPLOADER_MAX_UPLOAD_SIZE`) enforces a hard cap on request body size with one integer comparison per request.
- Graceful shutdown is triggered by `SIGINT`/`SIGTERM`; the server stops accepting new connections and waits up to `UPLOADER_SHUTDOWN_TIMEOUT` for in-flight requests to drain.
- Static downloads use `http.FileServer` and benefit from `sendfile(2)` zero-copy on Linux; no userspace involvement in the byte loop.

Future phases will add atomic publish via temp-file-plus-rename (single-file uploads) and directory-rename (tar uploads) to extend integrity guarantees to concurrent writers on the same path. These additions remain lock-free.

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