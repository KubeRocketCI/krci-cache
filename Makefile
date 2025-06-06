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
CHAINSAW_VERSION ?= v0.2.12
KIND_VERSION ?= v0.29.0
KUSTOMIZE_VERSION ?= v5.6.0

# Kind cluster configuration
KUBE_VERSION ?= 1.32
KIND_CONFIG ?= e2e/kind-$(KUBE_VERSION).yaml
KIND_CLUSTER_NAME ?= krci-cache-e2e

# Set default shell following EDP pattern
SHELL=/bin/bash -o pipefail -o errexit

.PHONY: all fmt vet lint lint-fix dev test test-coverage build docker-build docker-push clean
.PHONY: install-tools e2e-setup-cluster e2e-test e2e-cleanup

all: build

# Tool binaries
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
CHAINSAW ?= $(LOCALBIN)/chainsaw
KIND ?= $(LOCALBIN)/kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize

.PHONY: install-tools
install-tools: golangci-lint chainsaw kind kustomize ## Download all tools locally if necessary

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

##@ E2E Testing

.PHONY: chainsaw
chainsaw: $(CHAINSAW) ## Download chainsaw locally if necessary
$(CHAINSAW): $(LOCALBIN)
	$(call go-install-tool,$(CHAINSAW),github.com/kyverno/chainsaw,$(CHAINSAW_VERSION))

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create kind cluster for e2e testing
	@if [ ! -f "$(KIND_CONFIG)" ]; then \
		echo "Kind configuration file not found: $(KIND_CONFIG)"; \
		exit 1; \
	fi
	@if $(KIND) get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER_NAME)$$"; then \
		echo "Cluster $(KIND_CLUSTER_NAME) already exists"; \
	else \
		$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config=$(KIND_CONFIG) --wait=300s; \
	fi
	@kubectl cluster-info --context kind-$(KIND_CLUSTER_NAME)

.PHONY: kind-load-images
kind-load-images: kind docker-build ## Load images into kind cluster
	@docker tag $(IMG) $(BIN_NAME):latest
	@$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)
	@$(KIND) load docker-image $(BIN_NAME):latest --name $(KIND_CLUSTER_NAME)

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete kind cluster
	@$(KIND) delete cluster --name $(KIND_CLUSTER_NAME) 2>/dev/null || true

.PHONY: e2e-setup-cluster
e2e-setup-cluster: kind-create-cluster kind-load-images ## Setup kind cluster and load images for e2e testing

.PHONY: e2e-test
e2e-test: chainsaw ## Run e2e tests against local kind cluster
	@cd e2e && $(CHAINSAW) test ./chainsaw/tests --config ./chainsaw/config/chainsaw.yaml

.PHONY: e2e-cleanup
e2e-cleanup: kind-delete-cluster ## Cleanup e2e test resources
	@rm -rf e2e/test-results || true

help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
