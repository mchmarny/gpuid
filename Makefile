# =======================================================================================
# Project configuration
# =======================================================================================

APP_NAME           := gpuid
BRANCH             := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT             := $(shell git rev-parse HEAD)
CONFIG_FILE        ?= kind.yaml
GO_VERSION	       := $(shell go version | awk '{print $$3}' | sed 's/go//')
GOLINT_VERSION     := $(shell golangci-lint --version | awk '{print $$4}' | sed 's/golangci-lint version //')
GORELEASER_VERSION := $(shell goreleaser --version | grep GitVersion | awk '{print $$2}')
KO_VERSION         := $(shell ko version)
KUBECTL_VERSION    := $(shell kubectl version --client | grep Client | awk '{print $$3}' | sed 's/v//')
YAML_FILES         := $(shell find . -type f \( -iname "*.yml" -o -iname "*.yaml" \) ! -path "./example/*")

# =======================================================================================
# Default target
# =======================================================================================

all: info

.PHONY: info
info: ## Prints the current project info
	@echo "app:            $(APP_NAME)"
	@echo "branch:         $(BRANCH)"
	@echo "commit:         $(COMMIT)"
	@echo "go:             $(GO_VERSION)"
	@echo "goreleaser:     $(GORELEASER_VERSION)"
	@echo "ko:             $(KO_VERSION)"
	@echo "kubectl:        $(KUBECTL_VERSION)"
	@echo "linter:         $(GOLINT_VERSION)"
	@echo ""
	@echo "Run 'make help' to see available commands"

# =======================================================================================
# Repo setup 
# =======================================================================================

.PHONY: init
init: ## Sets up the repository for development
	tools/init

# =======================================================================================
# Local dev loop
# =======================================================================================

GO111MODULE        := on
GO_ENV             := GO111MODULE=$(GO111MODULE) CGO_ENABLED=0
# Environment for Go test commands (CGO needed for race detector)
GO_TEST_ENV        := GO111MODULE=$(GO111MODULE) CGO_ENABLED=1

.PHONY: tidy
tidy: ## Updates Go modules all dependencies
	@set -e; \
	$(GO_ENV) go fmt ./...; \
	$(GO_ENV) go mod tidy

.PHONY: upgrade
upgrade: ## Upgrades all dependencies
	@set -e; \
	$(GO_ENV) go get -u ./...; \
	$(GO_ENV) go mod tidy

.PHONY: lint
lint: lint-go lint-yaml ## Lints the entire project
	@echo "Completed Go and YAML lints"

.PHONY: lint-go
lint-go: ## Lints the Go files
	@set -e; \
	echo "Running golangci-lint"; \
	$(GO_ENV) golangci-lint -c .golangci.yaml run

.PHONY: lint-yaml
lint-yaml: ## Lints YAML files
	@if [ -n "$(YAML_FILES)" ]; then \
		yamllint -c .yamllint $(YAML_FILES); \
	else \
		echo "No YAML files found to lint."; \
	fi

.PHONY: test
test: ## Runs unit tests
	@set -e; \
	echo "Running tests"; \
	$(GO_TEST_ENV) go test -count=1 -race -ldflags="-s -w" -covermode=atomic -coverprofile=coverage.out ./...; \
	echo "Test coverage"; \
	$(GO_ENV) go tool cover -func=coverage.out

.PHONY: benchmark
benchmark: ## Run benchmarks
	@set -e; \
	@echo "Running benchmarks..."; \
	$(GO_ENV) go test ./pkg/... -bench=. -benchmem

.PHONY: vet
vet: ## Run go vet
	@echo "Running go vet..."
	$(GO_ENV) go vet ./...

.PHONY: scan
scan: ## Scans for source vulnerabilities
	@set -e; \
	echo "Doing static analysis"; \
	$(GO_ENV) go vet ./...; \
	echo "Running vulnerability scan"; \
	grype dir:. --config .grype.yaml --fail-on high --quiet	

.PHONY: ci
ci: test lint scan ## Qualifies the current codebase (test, lint, scan)
	@echo "Codebase qualification completed"

.PHONY: clean
clean: ## Cleans temp directories
	@set -e; \
	$(GO_ENV) go clean -modcache; \
	$(GO_ENV) go clean ./...; \
	rm -rf ./bin; \
	$(GO_ENV) go get -u ./...; \
	$(GO_ENV) go mod tidy; \
	echo "Cleaned temp directories"

# =======================================================================================
# Tagging
# =======================================================================================

.PHONY: bump-major
bump-major: ## Bumps major version (1.2.3 → 2.0.0)
	tools/bump major

.PHONY: bump-minor
bump-minor: ## Bumps minor version (1.2.3 → 1.3.0)
	tools/bump minor

.PHONY: bump-patch
bump-patch: ## Bumps patch version (1.2.3 → 1.2.4)
	tools/bump patch

.PHONY: release
release: ## Runs the release process
	@set -e; \
	goreleaser release --clean --config .goreleaser.yaml --fail-fast --timeout 10m0s

# =======================================================================================
# Integration testing with KinD
# =======================================================================================

.PHONY: faker
faker: ## Create NV SMI faker container image
	tools/faker

.PHONY: up
up: ## Create a Kubernetes cluster with KinD
	kind create cluster --name $(APP_NAME) --config $(CONFIG_FILE) --wait 5m

.PHONY: down
down: ## Delete a Kubernetes cluster with KinD
	kind delete cluster --name $(APP_NAME)

.PHONY: e2e
e2e: down up ## Run end-to-end integration tests
	@echo "Running integration tests..."; \
	bash tools/e2e || exit 1;

# =======================================================================================
# Help command
# =======================================================================================

.PHONY: help
help: ## Displays available commands
	@echo "Available make targets:"; \
	grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk \
		'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'