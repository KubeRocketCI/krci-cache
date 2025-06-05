PROJECT_NAME := "krci-cache"
PKG := "github.com/KubeRocketCI/krci-cache"
PKG_LIST := $(shell go list ${PKG}/...)
GO_FILES := $(shell find . -name '*.go' | grep -v _test.go)

# Build configuration following EDP pattern
CURRENT_DIR=$(shell pwd)
DIST_DIR=${CURRENT_DIR}/dist
BIN_NAME=krci-cache
HOST_OS?=$(shell go env GOOS)
HOST_ARCH?=$(shell go env GOARCH)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Docker configuration
IMG ?= $(BIN_NAME):$(VERSION)

# Build flags
override LDFLAGS += \
	-X main.version=${VERSION} \
	-X main.buildDate=${BUILD_DATE} \
	-X main.gitCommit=${GIT_COMMIT}

override GCFLAGS +=all=-trimpath=${CURRENT_DIR}

# Local binary directory
LOCALBIN ?= $(CURRENT_DIR)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Ensure dist directory exists
$(DIST_DIR):
	mkdir -p $(DIST_DIR)

# Tool Versions
GOLANGCI_LINT_VERSION ?= v1.64.7

# Set default shell following EDP pattern
SHELL=/bin/bash -o pipefail -o errexit

.PHONY: all fmt vet lint lint-fix dev test test-coverage build docker-build docker-push clean

all: build

# Tool binaries
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet  
vet: fmt ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run -v ./...

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint with --fix
	$(GOLANGCI_LINT) run -v --fix ./...

.PHONY: dev
dev: ## Run development server with live reload
	@echo "Starting development server with live reload..."
	@command -v reflex >/dev/null 2>&1 || { \
		echo "Installing reflex for live reload..."; \
		go install github.com/cespare/reflex@latest; \
	}
	@cd uploader && \
		reflex -r '\.go$$' -s -- sh -c 'echo "Reloading..." && go run ../main.go'

.PHONY: test
test: fmt vet ## Run tests
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage report generated: coverage.out"

.PHONY: test-coverage
test-coverage: test ## Run tests and generate coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "HTML coverage report generated: coverage.html"
	@echo "Text coverage summary:"
	@go tool cover -func=coverage.out

.PHONY: test-short
test-short: ## Run tests in short mode
	go test -short -v ./...

build: $(DIST_DIR) ## Build the binary file following EDP pattern
	CGO_ENABLED=0 GOOS=${HOST_OS} GOARCH=${HOST_ARCH} go build -v \
		-ldflags '${LDFLAGS}' \
		-gcflags '${GCFLAGS}' \
		-o ${DIST_DIR}/${BIN_NAME}-${HOST_ARCH} \
		$(PKG)

.PHONY: build-all
build-all: $(DIST_DIR) ## Build binaries for all supported architectures
	$(MAKE) build HOST_ARCH=amd64
	$(MAKE) build HOST_ARCH=arm64

docker-build: build ## Build Docker image
	docker build --build-arg TARGETARCH=${HOST_ARCH} -t ${IMG} .

docker-push: ## Push Docker image
	docker push ${IMG}

clean: ## Remove previous build artifacts
	@rm -rf ${DIST_DIR}
	@rm -f coverage.*

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed  
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
