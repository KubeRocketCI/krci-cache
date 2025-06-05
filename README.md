[![License](https://img.shields.io/github/license/KubeRocketCI/krci-cache)](/LICENSE)

# KubeRocketCI Cache (krci-cache)

A caching component for KubeRocketCI pipeline artifacts, built in Go with Echo framework.
Designed to be deployed in containers with proper authentication and access controls.

## Install

```shell
go install github.com/KubeRocketCI/krci-cache
```

### Configuration

Configuration is done via environment variables:

- **UPLOADER_HOST** -- hostname to bind to (default: localhost)
- **UPLOADER_PORT** -- port to bind to (default: 8080)
- **UPLOADER_DIRECTORY** -- Directory where to upload (default: ./pub)
- **UPLOADER_UPLOAD_CREDENTIALS** -- Protect upload/delete endpoints with username:password (e.g: `username:password`)

The service should be deployed behind proper authentication and access controls.
Do not expose this directly to the internet without protection.

## Usage

The service accepts HTTP form fields:

- **file**: The file stream of the upload
- **path**: The target path for the file
- **targz**: Set to extract tar.gz archives automatically on the filesystem

## Container Deployment

The application is containerized using a multi-architecture approach with pre-built binaries.
The container runs as a non-root user for security.

### Build

Use the provided Dockerfile which supports both amd64 and arm64 architectures:

```shell
docker build --build-arg TARGETARCH=amd64 -t krci-cache .
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

#### Upload

- **method**: POST
- **path**: */upload*
- **arguments**:
  - **path**: Target path for the file (relative to upload directory, directory traversal prevented)
  - **file**: File post data
  - **targz**: Boolean flag to extract tar.gz files on filesystem

- **examples**:

```shell
curl -u username:password -F path=hello-upload.txt -X POST -F file=@/tmp/hello.txt http://localhost:8080/upload
```

```shell
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

## [LICENSE](LICENSE)

[Apache 2.0](LICENSE)
