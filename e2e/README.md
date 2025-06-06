# E2E Testing with Chainsaw

This directory contains end-to-end tests for krci-cache using [Chainsaw](https://github.com/kyverno/chainsaw), a declarative Kubernetes-native testing framework.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) for container management
- [Make](https://www.gnu.org/software/make/) for build automation
- [Go](https://golang.org/doc/install) for building tools (if not using pre-built binaries)

**Note**: All other tools (kind, kubectl, chainsaw) are automatically installed via Makefile targets.

## Quick Start

1. **Setup test cluster:**

   ```bash
   make e2e-setup-cluster
   ```

2. **Run all e2e tests:**

   ```bash
   make e2e-test
   ```

3. **Cleanup:**

   ```bash
   make e2e-cleanup
   ```

## Directory Structure

```
e2e/
├── chainsaw/
│   ├── tests/                    # Test scenarios
│   │   └── basic-operations/     # Basic functionality tests
│   ├── manifests/               # Kubernetes manifests with kustomize
│   │   └── base/               # Base kustomize configuration
│   └── config/                 # Chainsaw configuration
├── kind-1.32.yaml              # Kind cluster configuration
└── README.md                   # This file
```

## Test Scenarios

### Basic Operations

Currently implemented test scenarios include:

- **Health endpoint validation** - Verifies the `/health` endpoint responds correctly
- **Deployment readiness** - Ensures krci-cache pods start and become ready
- **Service accessibility** - Tests that the service is properly exposed
- **Authentication configuration** - Validates basic auth is properly configured

**Future test scenarios** (to be implemented):

- File upload/download functionality
- Archive extraction capabilities
- Concurrent upload handling
- Path traversal protection
- Persistent volume functionality

## Running Specific Test Suites

```bash
# Install chainsaw if not already installed
make chainsaw

# Run only basic operations tests
cd e2e && ../bin/chainsaw test ./chainsaw/tests/basic-operations --config ./chainsaw/config/chainsaw.yaml

# Run all tests (currently only basic-operations)
make e2e-test

# Run with verbose output
cd e2e && ../bin/chainsaw test ./chainsaw/tests --config ./chainsaw/config/chainsaw.yaml -v 3
```

## Local Development

### Setup Kind Cluster

```bash
# Install tools automatically and create cluster
make e2e-setup-cluster

# Or run individual steps
make kind-create-cluster
make kind-load-images
```

### Test Development

1. Create new test files in the `tests/basic-operations/` directory
2. Follow the existing test structure and naming conventions: `*-test.yaml`
3. Test locally before committing: `make e2e-test`
4. Update documentation when adding new test scenarios

## CI/CD Integration

Tests are automatically executed in GitHub Actions on:

- Pull requests to main branch affecting relevant files
- Manual workflow dispatch with optional test suite selection

## Troubleshooting

### Common Issues

1. **Kind cluster not ready:**

   ```bash
   # Check if cluster exists and is accessible
   ./bin/kubectl cluster-info --context kind-krci-cache-e2e

   # Recreate cluster if needed
   make e2e-cleanup && make e2e-setup-cluster
   ```

2. **Image not loaded:**

   ```bash
   # Check images in kind cluster
   docker exec -it krci-cache-e2e-control-plane crictl images | grep krci-cache

   # Reload images if missing
   make kind-load-images
   ```

3. **Test timeouts or failures:**

   ```bash
   # Check pod status
   ./bin/kubectl get pods -n krci-cache-e2e

   # Check pod logs
   ./bin/kubectl logs -n krci-cache-e2e deployment/krci-cache

   # Check events
   ./bin/kubectl get events -n krci-cache-e2e --sort-by='.lastTimestamp'
   ```

### Debug Mode

```bash
# Run tests with debug information and extended cleanup delay
cd e2e && ../bin/chainsaw test ./chainsaw/tests --config ./chainsaw/config/chainsaw.yaml --cleanup-delay=60s -v 3

# Run tests without cleanup (for debugging)
cd e2e && ../bin/chainsaw test ./chainsaw/tests --config ./chainsaw/config/chainsaw.yaml --skip-delete
```

## Tool Management

The project uses a Makefile-based approach for managing tools and e2e testing:

### Available Make Targets

- `make kind` - Install kind locally if needed
- `make chainsaw` - Install chainsaw locally if needed
- `make kubectl` - Install kubectl locally if needed
- `make kind-create-cluster` - Create kind cluster for e2e testing
- `make kind-load-images` - Load Docker images into kind cluster
- `make kind-delete-cluster` - Delete kind cluster
- `make e2e-setup-cluster` - Complete setup: create cluster and load images
- `make e2e-test` - Run e2e tests against kind cluster
- `make e2e-cleanup` - Cleanup e2e test resources

All tools are installed to `./bin/` directory and versioned via Makefile variables.

## Complete E2E Testing Workflow

### Quick Workflow (Recommended)

```bash
# One command to setup everything and run tests
make e2e-setup-cluster && make e2e-test

# Or run with cleanup
make e2e-setup-cluster && make e2e-test && make e2e-cleanup
```

### Step-by-step Workflow

```bash
# 1. Setup cluster and load images
make e2e-setup-cluster

# 2. Run tests
make e2e-test

# 3. Clean up when done
make e2e-cleanup
```

### Manual Steps (for debugging)

```bash
# 1. Install tools (happens automatically when needed)
make kind kubectl chainsaw

# 2. Create cluster
make kind-create-cluster

# 3. Build and load images
make docker-build
make kind-load-images

# 4. Run tests
make e2e-test

# 5. Clean up
make e2e-cleanup
```

## Contributing

When adding new tests:

1. Follow the existing directory structure
2. Use descriptive test names
3. Include proper cleanup steps
4. Update documentation as needed
5. Test both success and failure scenarios
