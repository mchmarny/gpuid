# =============================================================================
# Makefile for the gpuid project.
#
# Conventions (mirrors github.com/nvidia/aicr):
#   * .settings.yaml owns all tool versions and quality thresholds.
#   * .go-version owns the Go toolchain — read by Makefile and CI.
#   * Targets ending in -check are CI-friendly (read-only, no mutation).
#   * `make qualify` is the full quality gate; `make ci` is an alias.
# =============================================================================

APP_NAME           := gpuid
BRANCH             := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
COMMIT             := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
VERSION            ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
CONFIG_FILE        ?= kind.yaml

# Go toolchain version (single source of truth: .go-version).
GO_VERSION         := $(shell cat .go-version 2>/dev/null)
export GOTOOLCHAIN  = go$(GO_VERSION)

# Tool versions installed locally; "not installed" surface in `make info` /
# `make tools-check` makes drift from .settings.yaml immediately visible.
GOLINT_VERSION     := $(shell golangci-lint --version 2>/dev/null | awk '{print $$4}' | sed 's/golangci-lint version //' || echo "not installed")
KO_VERSION         := $(shell ko version 2>/dev/null || echo "not installed")
KUBECTL_VERSION    := $(shell kubectl version --client 2>/dev/null | grep Client | awk '{print $$3}' | sed 's/v//' || echo "not installed")

YAML_FILES         := $(shell find . -type f \( -iname "*.yml" -o -iname "*.yaml" \) ! -path "./example/*" ! -path "./.git/*")

# Quality thresholds from .settings.yaml, with conservative fallbacks so the
# Makefile keeps working in environments without yq (CI installs yq early).
COVERAGE_THRESHOLD ?= $(shell yq -r '.quality.coverage_threshold' .settings.yaml 2>/dev/null)
ifeq ($(COVERAGE_THRESHOLD),)
COVERAGE_THRESHOLD := 70
endif
LINT_TIMEOUT       ?= $(shell yq -r '.quality.lint_timeout' .settings.yaml 2>/dev/null)
ifeq ($(LINT_TIMEOUT),)
LINT_TIMEOUT       := 5m
endif
TEST_TIMEOUT       ?= $(shell yq -r '.quality.test_timeout' .settings.yaml 2>/dev/null)
ifeq ($(TEST_TIMEOUT),)
TEST_TIMEOUT       := 5m
endif

GO111MODULE        := on
GO_ENV             := GO111MODULE=$(GO111MODULE) CGO_ENABLED=0
# CGO is needed for the race detector under `make test`.
GO_TEST_ENV        := GO111MODULE=$(GO111MODULE) CGO_ENABLED=1

# Default target: print project info; `make help` lists targets.
all: info

# =============================================================================
# Project info
# =============================================================================

.PHONY: info
info: ## Prints the current project info
	@echo "app:            $(APP_NAME)"
	@echo "version:        $(VERSION)"
	@echo "branch:         $(BRANCH)"
	@echo "commit:         $(COMMIT)"
	@echo "go:             $(GO_VERSION)"
	@echo "ko:             $(KO_VERSION)"
	@echo "kubectl:        $(KUBECTL_VERSION)"
	@echo "linter:         $(GOLINT_VERSION)"
	@echo "coverage gate:  $(COVERAGE_THRESHOLD)%"
	@echo ""
	@echo "Run 'make help' to see available commands"

# =============================================================================
# Tools
# =============================================================================

.PHONY: tools-check
tools-check: ## Reports installed-vs-expected tool versions from .settings.yaml
	@bash tools/check-tools

# =============================================================================
# Formatting & dependencies
# =============================================================================

.PHONY: tidy
tidy: ## Formats code and updates Go module dependencies
	@set -e; \
	$(GO_ENV) go fmt ./...; \
	$(GO_ENV) go mod tidy

.PHONY: fmt-check
fmt-check: ## Verifies code is gofmt-clean (CI-friendly, no modifications)
	@out="$$(gofmt -l . 2>/dev/null)"; \
	if [ -n "$$out" ]; then \
		echo "Code is not formatted. Run 'make tidy' to fix:"; \
		echo "$$out"; \
		exit 1; \
	fi; \
	echo "Code formatting check passed"

.PHONY: upgrade
upgrade: ## Upgrades all Go dependencies to latest minor/patch
	@set -e; \
	$(GO_ENV) go get -u ./...; \
	$(GO_ENV) go mod tidy

# =============================================================================
# Linting
# =============================================================================

.PHONY: lint
lint: lint-go lint-yaml ## Lints the entire project (Go + YAML)
	@echo "Completed Go and YAML lints"

.PHONY: lint-go
lint-go: ## Lints Go files with go vet + golangci-lint
	@set -e; \
	echo "Running go vet"; \
	$(GO_ENV) go vet ./...; \
	echo "Running golangci-lint"; \
	$(GO_ENV) golangci-lint -c .golangci.yaml run --timeout=$(LINT_TIMEOUT)

.PHONY: lint-yaml
lint-yaml: ## Lints YAML files with yamllint
	@if [ -n "$(YAML_FILES)" ]; then \
		yamllint -c .yamllint $(YAML_FILES); \
	else \
		echo "No YAML files found to lint."; \
	fi

# =============================================================================
# Testing
# =============================================================================

.PHONY: test
test: ## Runs unit tests with race detector + coverage
	@set -e; \
	echo "Running tests"; \
	$(GO_TEST_ENV) go test -count=1 -race -timeout=$(TEST_TIMEOUT) -covermode=atomic -coverprofile=coverage.out ./...; \
	echo "Test coverage:"; \
	$(GO_ENV) go tool cover -func=coverage.out | tail -1

.PHONY: test-coverage
test-coverage: test ## Runs tests and enforces coverage_threshold from .settings.yaml
	@coverage=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Coverage: $$coverage% (threshold: $(COVERAGE_THRESHOLD)%)"; \
	awk -v cov=$$coverage -v thr=$(COVERAGE_THRESHOLD) 'BEGIN { if (cov+0 < thr+0) exit 1 }' || { \
		echo "ERROR: coverage $$coverage% is below threshold $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	}; \
	echo "Coverage check passed"

.PHONY: benchmark
benchmark: ## Runs benchmarks
	@set -e; \
	echo "Running benchmarks..."; \
	$(GO_ENV) go test ./pkg/... -bench=. -benchmem

.PHONY: vet
vet: ## Runs go vet
	@echo "Running go vet..."
	$(GO_ENV) go vet ./...

# =============================================================================
# Security & qualification
# =============================================================================

.PHONY: scan
scan: ## Scans for source vulnerabilities (go vet + grype)
	@set -e; \
	echo "Doing static analysis"; \
	$(GO_ENV) go vet ./...; \
	echo "Running vulnerability scan"; \
	grype dir:. --config .grype.yaml --fail-on high --quiet

.PHONY: qualify
qualify: test-coverage lint scan ## Full quality gate (test+coverage, lint, scan)
	@echo "Codebase qualification completed"

# `make ci` retained as an alias for backward compatibility.
.PHONY: ci
ci: qualify ## Alias for `make qualify`

# =============================================================================
# Cleanup
# =============================================================================

.PHONY: clean
clean: ## Cleans project build artifacts (does NOT touch the global module cache)
	@set -e; \
	$(GO_ENV) go clean ./...; \
	rm -rf ./bin coverage.out; \
	echo "Cleaned project build artifacts"

# =============================================================================
# Release tagging
# =============================================================================

.PHONY: bump-major
bump-major: ## Bumps major version (1.2.3 -> 2.0.0)
	tools/bump major

.PHONY: bump-minor
bump-minor: ## Bumps minor version (1.2.3 -> 1.3.0)
	tools/bump minor

.PHONY: bump-patch
bump-patch: ## Bumps patch version (1.2.3 -> 1.2.4)
	tools/bump patch

# =============================================================================
# KinD integration testing
# =============================================================================

.PHONY: faker
faker: ## Creates the nvidia-smi faker container image
	tools/faker

.PHONY: up
up: ## Creates a Kubernetes cluster with KinD
	kind create cluster --name $(APP_NAME) --config $(CONFIG_FILE) --wait 5m

.PHONY: down
down: ## Deletes the Kubernetes cluster with KinD
	kind delete cluster --name $(APP_NAME)

.PHONY: e2e
e2e: down up ## Runs end-to-end integration tests
	@echo "Running integration tests..."; \
	bash tools/e2e || exit 1;

# =============================================================================
# Help
# =============================================================================

.PHONY: help
help: ## Displays available commands grouped by section
	@echo ""
	@echo "\033[1m=== Quality & Testing ===\033[0m"
	@echo "  make qualify        Full qualification (test-coverage + lint + scan)"
	@echo "  make test           Unit tests with race detector"
	@echo "  make test-coverage  Tests with coverage threshold enforcement"
	@echo "  make lint           Lint Go and YAML"
	@echo "  make scan           Vulnerability scan with grype"
	@echo "  make benchmark      Run benchmarks"
	@echo ""
	@echo "\033[1m=== Local Development ===\033[0m"
	@echo "  make tidy           Format code and tidy go.mod"
	@echo "  make fmt-check      Check formatting without mutating (CI-friendly)"
	@echo "  make upgrade        Upgrade dependencies"
	@echo "  make up / down      Create / delete local KinD cluster"
	@echo "  make e2e            End-to-end integration tests"
	@echo "  make faker          Build the nvidia-smi faker image"
	@echo ""
	@echo "\033[1m=== Release ===\033[0m"
	@echo "  make bump-patch     Tag patch (1.2.3 -> 1.2.4)"
	@echo "  make bump-minor     Tag minor (1.2.3 -> 1.3.0)"
	@echo "  make bump-major     Tag major (1.2.3 -> 2.0.0)"
	@echo ""
	@echo "\033[1m=== Tools ===\033[0m"
	@echo "  make info           Show project + tool versions"
	@echo "  make tools-check    Compare installed tool versions against .settings.yaml"
	@echo ""
	@echo "\033[1m=== Cleanup ===\033[0m"
	@echo "  make clean          Remove project build artifacts"
	@echo ""

.PHONY: help-targets
help-targets: ## Displays every public target (flat list)
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk \
		'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' | sort
