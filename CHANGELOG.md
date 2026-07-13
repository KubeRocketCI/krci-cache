<a name="unreleased"></a>
## [Unreleased]


<a name="v0.3.0"></a>
## [v0.3.0] - 2026-07-13
### Bug Fixes

- update rsync pin to 3.4.3-r0 to unblock Docker build

### Documentation

- streamline CLAUDE.md

### CI

- automate changelog, release notes, and commit validation
- bump actions/checkout from 6 to 7

### Dependencies

- bump golang.org/x/sys in the go-stdlib group
- bump golang.org/x/net from 0.54.0 to 0.55.0
- bump golang.org/x/crypto from 0.45.0 to 0.52.0
- bump golang.org/x/sys in the go-stdlib group
- bump golang.org/x/sys in the go-stdlib group
- bump golang.org/x/sys in the go-stdlib group


<a name="v0.2.0"></a>
## v0.2.0 - 2026-05-21
### Features

- honor request context in publish operations and optimize tar extraction
- stream multipart uploads to eliminate disk write amplification
- implement atomic publish with last-writer-wins concurrency safety
- add production-ready high-performance features for pipeline cache

### Bug Fixes

- prevent OOM by limiting multipart form memory to 32MB

### Performance Improvements

- optimize memory usage in tar.gz extraction and HTTP server

### Code Refactoring

- consolidate uploader server configuration and handlers
- Simplify code to ensure lower memory usage

### Testing

- Add checks for e2e tests
- Tune e2e tests
- Add e2e simple tests

### Documentation

- Update README by adding use-cases

### CI

- implement branch-sha and semver-based image tagging for releases
- bump docker/build-push-action from 6 to 7
- bump docker/setup-qemu-action from 3 to 4
- bump docker/metadata-action from 5 to 6
- bump docker/setup-buildx-action from 3 to 4
- bump docker/login-action from 3 to 4
- bump actions/upload-artifact from 6 to 7
- bump actions/upload-artifact from 5 to 6
- bump actions/checkout from 5 to 6
- bump actions/upload-artifact from 4 to 5
- bump github/codeql-action from 3 to 4
- bump actions/setup-go from 5 to 6
- bump actions/checkout from 4 to 5

### Dependencies

- bump golang.org/x/crypto from 0.38.0 to 0.45.0
- bump github.com/stretchr/testify from 1.11.0 to 1.11.1
- bump github.com/stretchr/testify from 1.10.0 to 1.11.0

### Routine

- Update alpine version to 3.23
- Update rsync package version in Dockerfile
- update base image version in Dockerfile
- Setup KubeRocketAI ([#25](https://github.com/KubeRocketCI/krci-cache/issues/25))
- Implement semver tag versioning ([#20](https://github.com/KubeRocketCI/krci-cache/issues/20))
- Align Dockerfile args for multi-arch build support ([#18](https://github.com/KubeRocketCI/krci-cache/issues/18))
- Add CODEOWNERS
- Add cache for pipelines
- Use kubectl exec instead of spinning pod each time in e2e
- Switch to alpine image
- Add security advisory
- Add community attributes
- Update untar approach
- Update golang version
- Update build pipeline
- Update README.md file
- Simplify PR workflow
- Fix build pipeline
- Update build flow to support multi-arch
- Add sonarqube configuration
- Update project structure
- update golangci.yml configuration
- update dependencies


[Unreleased]: https://github.com/KubeRocketCI/krci-cache/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/KubeRocketCI/krci-cache/compare/v0.2.0...v0.3.0
