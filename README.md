[![License](https://img.shields.io/github/license/KubeRocketCI/krci-cache)](/LICENSE)

# KubeRocketCI Cache (krci-cache)

A lightweight caching component for KubeRocketCI pipeline artifacts, built in Go with Echo framework.
Simplified for memory efficiency and stability in resource-constrained environments.

**Note**: This is a simplified version optimized for memory efficiency. Some advanced features have been removed to resolve OOM issues in Kubernetes environments with limited memory (512MB).

## Table of Contents

- [Install](#install)
- [Configuration](#configuration)
  - [Basic Configuration](#basic-configuration)
  - [Production Example](#production-example)
- [Core Features](#core-features)
  - [Memory-Optimized Architecture](#memory-optimized-architecture)
  - [Security Features](#security-features)
- [Limitations](#limitations)
  - [Tar.gz Archive Limits](#targz-archive-limits)
  - [Removed Features](#removed-features)
- [Usage](#usage)
  - [API Endpoints](#api-endpoints)
  - [Upload](#upload)
  - [Response Format](#response-format)
- [Container Deployment](#container-deployment)
  - [Build](#build)
  - [Simplified Deployment](#simplified-deployment)
- [Setup](#setup)
  - [Run directly](#run-directly)
  - [Run with authentication](#run-with-authentication)
- [API](#api)
  - [Upload File](#upload-file)
  - [Delete File](#delete-file)
  - [Delete Old Files](#delete-old-files)
- [Use Cases](#use-cases)
- [LICENSE](#license)

## Install

```shell
go install github.com/KubeRocketCI/krci-cache
```

### Configuration

Configuration is done via environment variables:

#### Basic Configuration

- **UPLOADER_HOST** -- hostname to bind to (default: localhost)
- **UPLOADER_PORT** -- port to bind to (default: 8080)
- **UPLOADER_DIRECTORY** -- Directory where to upload (default: ./pub)
- **UPLOADER_UPLOAD_CREDENTIALS** -- Protect upload/delete endpoints with username:password (e.g: `username:password`)

#### Production Example

```shell
export UPLOADER_HOST="0.0.0.0"
export UPLOADER_PORT="8080"
export UPLOADER_DIRECTORY="/var/cache/artifacts"
export UPLOADER_UPLOAD_CREDENTIALS="cache-user:secure-password"
```

The service should be deployed behind proper authentication and access controls.
Do not expose this directly to the internet without protection.

## Core Features

### Memory-Optimized Architecture

- **Simplified Design**: Reduced complexity to minimize memory footprint
- **Essential Middleware Only**: Only recovery and basic logging middleware
- **Direct File Operations**: Streamlined upload/download without complex buffering
- **Built-in Tar.gz Limits**: File extraction with safety limits (2GB per file, 8GB total)

### Security Features

- **Path Traversal Protection**: Prevents uploads outside designated directory
- **Basic Authentication**: Optional username/password protection for sensitive endpoints
- **Tar.gz Safety**: Built-in protection against zip bombs and malicious archives
- **Directory Isolation**: All operations confined to the configured upload directory

## Limitations

The service has been simplified for memory efficiency, with some trade-offs in functionality:

### Tar.gz Archive Limits

- **Individual File Size**: Maximum 2GB per file within tar.gz archives
- **Archive Total Size**: Maximum 8GB total uncompressed size for tar.gz uploads
- **Regular File Uploads**: No configurable size limits (limited by available disk space)
- **Security**: Built-in protection against zip bombs, path traversal, and malicious archives

### Features

- Basic file upload/download
- Tar.gz extraction with built-in size limits
- Basic authentication (`UPLOADER_UPLOAD_CREDENTIALS`)
- Health check endpoint (`/health`)
- File deletion (single and batch by age)
- Path traversal protection
- Static file serving

## Usage

### API Endpoints

#### Health Check

- **method**: GET
- **path**: */health*
- **description**: Health check endpoint for load balancers and monitoring
- **response**: JSON with status, timestamp, and version

```shell
curl http://localhost:8080/health
```

#### Upload

The service accepts HTTP form fields:

- **file**: The file stream of the upload
- **path**: The target path for the file
- **targz**: Set to extract tar.gz archives automatically on the filesystem

#### Response Format

All endpoints return JSON responses for better integration:

```json
{
  "message": "File has been uploaded to example.txt",
  "path": "example.txt",
  "size": 1024
}
```

## Container Deployment

The application is containerized using a multi-architecture approach with pre-built binaries.
The container runs as a non-root user for security.

### Build

Use the provided Dockerfile which supports both amd64 and arm64 architectures:

```shell
docker build --build-arg TARGETARCH=amd64 -t krci-cache .
```

### Simplified Deployment

```yaml
# docker-compose.yml example
version: '3.8'
services:
  krci-cache:
    image: krci-cache:latest
    ports:
      - "8080:8080"
    environment:
      - UPLOADER_HOST=0.0.0.0
      - UPLOADER_PORT=8080
      - UPLOADER_DIRECTORY=/var/cache/artifacts
      - UPLOADER_UPLOAD_CREDENTIALS=cache-user:secure-password
    volumes:
      - ./cache:/var/cache/artifacts
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

## Setup

### Run directly

You can run the service directly or use containerization. The default upload credentials are `username:password` for the `/upload` and `/delete` endpoints.

For production deployment, set proper credentials via the `UPLOADER_UPLOAD_CREDENTIALS` environment variable.

### Run with authentication

Set up authentication credentials:

```shell
export UPLOADER_UPLOAD_CREDENTIALS="username:password"
./krci-cache
```

Test the deployment:

```shell
echo "HELLO WORLD" > /tmp/hello.txt
curl -u username:password -F path=hello-upload.txt -X POST -F file=@/tmp/hello.txt http://localhost:8080/upload
curl http://localhost:8080/hello-upload.txt
```

### API

#### Upload File

- **method**: POST
- **path**: */upload*
- **arguments**:
  - **path**: Target path for the file (relative to upload directory, directory traversal prevented)
  - **file**: File post data (no size limits, limited by available disk space)
  - **targz**: Boolean flag to extract tar.gz files on filesystem (tar.gz uploads subject to built-in size limits: max 2GB per file, 8GB total)

- **examples**:

```shell
# Regular file upload (no size limits)
curl -u username:password -F path=hello-upload.txt -X POST -F file=@/tmp/hello.txt http://localhost:8080/upload
```

```shell
# Large file upload (limited by disk space)
curl -u username:password -F path=large-database.sql -X POST -F file=@/path/to/large-database.sql http://localhost:8080/upload
```

```shell
# Extract tar.gz automatically (max 2GB per file, 8GB total uncompressed)
tar czf - /path/to/directory|curl -u username:password -F path=hello-upload.txt -F targz=true -X POST -F file=@- http://localhost:8080/upload
```

### Delete File

- **method**: DELETE
- **path**: */upload*
- **arguments**:
  - **path**: Path to delete

- **example**:

```shell
curl -u username:password -F path=hello-upload.txt -X DELETE http://localhost:8080/upload
```

### Delete Old Files

- **method**: DELETE
- **path**: */delete*
- **arguments**:
  - **path**: Directory path to clean up
  - **days**: Delete files older than X days
  - **recursive**: Recursively delete in subdirectories (defaults to `false`)

- **example**:

```shell
curl -u username:password -F path=/path/to/directory -F days=1 -F recursive=true -X DELETE http://localhost:8080/delete
```

## Use Cases

### 1. Simple CI/CD Artifact Storage

Store and manage build artifacts with basic cleanup functionality.

**Upload build artifacts with extraction:**

```bash
# Upload and extract build artifacts to versioned directory
curl -u cache-user:secure-password \
  -X POST \
  -F "file=@app-v1.2.3.tar.gz" \
  -F "targz=true" \
  -F "path=builds/v1.2.3/" \
  http://cache-server:8080/upload
```

**Download specific artifacts:**

```bash
# Access extracted binary
curl http://cache-server:8080/builds/v1.2.3/bin/application

# Check if artifact exists
curl -I http://cache-server:8080/builds/v1.2.3/config.json
```

**Cleanup old build artifacts:**

```bash
# Remove builds older than 30 days
curl -u cache-user:secure-password \
  -X DELETE \
  "http://cache-server:8080/delete?path=builds&days=30&recursive=true"
```

### 2. Basic File Storage

Simple file upload and retrieval for small to medium-scale applications.

**Upload files:**

```bash
# Upload application logs
curl -u cache-user:secure-password \
  -X POST \
  -F "file=@application.log" \
  -F "path=logs/application-2024-01-15.log" \
  http://cache-server:8080/upload
```

**Basic retention:**

```bash
# Remove old files after 7 days
curl -u cache-user:secure-password \
  -X DELETE \
  "http://cache-server:8080/delete?path=logs&days=7&recursive=true"
```

### 3. Static Content Hosting

Host simple static websites and documentation.

**Deploy documentation:**

```bash
# Upload and extract documentation site
tar czf docs.tar.gz -C /path/to/docs .
curl -u cache-user:secure-password \
  -X POST \
  -F "file=@docs.tar.gz" \
  -F "targz=true" \
  -F "path=docs/v2.1/" \
  http://cache-server:8080/upload
```

**Access content:**

```bash
# Browse documentation
curl http://cache-server:8080/docs/v2.1/index.html
```

## [LICENSE](LICENSE)

[Apache 2.0](LICENSE)
