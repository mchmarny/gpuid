# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Overview

**gpuid** — GPU and Chassis Serial Number Exporter for Kubernetes. Monitors pods on GPU-accelerated nodes, labels nodes with chassis/GPU serial numbers, and exports serial data to pluggable backends.

**Tech Stack:** Go 1.25, Kubernetes client-go, Prometheus metrics, Kustomize deployments

## Architecture

```
cmd/gpuid/main.go          → Entry point (calls runner.Run())
cmd/smifaker/main.go       → nvidia-smi faker for testing
pkg/runner/                 → Core service: config, worker loop, export orchestration
pkg/gpu/                    → GPU detection via nvidia-smi parsing
pkg/node/                   → Kubernetes node labeling
pkg/server/                 → HTTP server (health, metrics)
pkg/counter/                → Prometheus metrics
pkg/logger/                 → Structured logging (slog)
pkg/exporters/
  ├── stdout/               → Default exporter (dev/debug)
  ├── http/                 → HTTP POST exporter
  ├── postgres/             → PostgreSQL batch insert exporter
  └── s3/                   → S3-compatible object exporter
deployments/
  ├── gpuid/base/           → Base Kustomize manifests
  ├── gpuid/overlays/       → Per-exporter overlays (stdout, http, postgres, s3)
  ├── smi-faker/            → Test faker deployment
  └── policy/               → SLSA/SBOM attestation policies
```

## Commands

```bash
make ci          # Full qualification: test + lint + scan
make test        # Unit tests with -race and coverage
make lint        # golangci-lint + yamllint
make scan        # go vet + grype vulnerability scan
make tidy        # Format + update deps
make upgrade     # Upgrade all dependencies

# Single test
go test -v ./pkg/gpu/... -run TestSpecificFunction

# Integration (KinD)
make e2e         # Destroy + create cluster + run e2e
make up          # Create KinD cluster
make down        # Delete KinD cluster
make faker       # Build nvidia-smi faker image

# Release
make bump-patch  # Bump patch version
make release     # goreleaser release
```

## Key Patterns

- **Functional options** for configuration (`pkg/runner/option.go`)
- **Interface-based exporters** with pluggable backends
- **Table-driven tests** for multiple test cases
- **slog** for structured logging
- **Prometheus** metrics via `pkg/counter`

## Non-Negotiable Rules

1. **Read before writing** — Never modify code you haven't read
2. **Tests must pass** — `make test` with race detector; never skip tests
3. **Run `make ci` before commits** — Fix ALL lint/test/scan failures before proceeding
4. **Use project patterns** — Learn existing code before inventing new approaches
5. **3-strike rule** — After 3 failed fix attempts, stop and reassess

## Git Configuration

- Commit to `main` branch (not `master`)
- Do use `-S` to cryptographically sign the commit
- Do NOT add `Co-Authored-By` lines (organization policy)
- Do not sign-off commits (no `-s` flag)

## Key Files

| File | Purpose |
|------|---------|
| `.golangci.yaml` | Linter configuration |
| `.grype.yaml` | Vulnerability scanner config |
| `.codecov.yaml` | Code coverage config |
| `kind.yaml` | KinD cluster config for e2e |
| `.goreleaser.yaml` | Release configuration |
| `.github/workflows/main.yaml` | CI workflow |
| `.github/workflows/release.yaml` | Release workflow |
