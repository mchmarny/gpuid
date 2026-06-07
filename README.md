[![main](https://github.com/mchmarny/gpuid/actions/workflows/main.yaml/badge.svg)](https://github.com/mchmarny/gpuid/actions/workflows/main.yaml)
[![release](https://github.com/mchmarny/gpuid/actions/workflows/release.yaml/badge.svg)](https://github.com/mchmarny/gpuid/actions/workflows/release.yaml)
![Issues](https://img.shields.io/github/issues/mchmarny/gpuid)
![PRs](https://img.shields.io/github/issues-pr/mchmarny/gpuid)
![Go Report Card](https://goreportcard.com/badge/github.com/mchmarny/gpuid)
[![codecov](https://codecov.io/gh/mchmarny/gpuid/branch/main/graph/badge.svg)](https://codecov.io/gh/mchmarny/gpuid)

# gpuid — GPU and chassis serial number exporter for Kubernetes

`gpuid` discovers the GPU and chassis serial numbers behind every GPU-capable node in your cluster, labels each node so workloads can target them, and streams a normalized record to a backend of your choice (stdout, HTTP, PostgreSQL, or S3-compatible object storage). It's a small, single-purpose controller designed to fill the inventory gap between the physical hardware and the ephemeral cloud VMs Kubernetes schedules onto it.

## Why

On managed Kubernetes (EKS, GKE, AKS) a "node" is a VM. The same physical chassis can serve many VMs over its lifetime, and a single GPU can move between machines after a host swap, an RMA, or a maintenance event. That makes a few things harder than they should be:

- **Correlating health and performance to physical hardware** — `nvidia-smi` issues, fabric flaps, and ECC anomalies are properties of a serial, not a node name.
- **Maintaining an audit trail** — knowing which GPU ran which workload at which time, even after the VM is gone.
- **Break-fix and RMA workflows** — quickly answering "where is this serial today?" and "what else has run on this chassis?"
- **Capacity and lifecycle reporting** in multi-tenant environments where namespaces own VMs but ops owns hardware.

`gpuid` makes the physical identifiers first-class: visible as node labels for scheduling and selectors, and persisted in your backend of choice for analytics.

## What it does

- Watches pods in a configurable namespace (default: the `nvidia-device-plugin` daemonset).
- Executes `nvidia-smi -q -x` inside each ready pod, parses the XML, and extracts the GPU and chassis (host) serial numbers.
- Patches the node those pods run on with `gpuid.github.com/*` labels using a strategic-merge patch (conflict-free with other label writers).
- Emits a structured record per (cluster, node, machine, chassis, GPU) to the configured exporter backend.
- Reports per-pod success/failure as Prometheus counters and exposes `/healthz`, `/readyz`, and `/metrics`.
- Runs **one or many** replicas — multi-replica deployments coordinate through a Kubernetes lease so only one replica reconciles at a time.

## Node labels

```shell
# H100 (no chassis serial reported by nvidia-smi):
gpuid.github.com/gpu-0=1652823054567
gpuid.github.com/gpu-1=1652823055642
gpuid.github.com/gpu-2=1652823055647
gpuid.github.com/gpu-3=1652823055931
gpuid.github.com/gpu-4=1652923033989
gpuid.github.com/gpu-5=1652923034028
gpuid.github.com/gpu-6=1652923034291
gpuid.github.com/gpu-7=1653023018213

# GB200:
gpuid.github.com/chassis=1821325191344
gpuid.github.com/chassis-count=1
gpuid.github.com/gpu-0=1761025346025
gpuid.github.com/gpu-1=1761125340419
```

> GB200 nodes ship 4 GPUs but only 2 unique serial numbers per chassis. The GPUs come in a dual-die package stitched together with NVLink-C2C on the same module.

When a node hosts multiple chassis, the chassis index is included in both the chassis and GPU labels (for example `gpuid.github.com/chassis-0`, `gpuid.github.com/chassis-0-gpu-0`).

## Exporters

| Type | Use case | Output |
|---|---|---|
| `stdout` (default) | Local dev, log aggregation, sidecar pattern | One structured `slog` record per reading |
| `http` | Pushing to an internal inventory service or webhook | JSON array POST with bearer-token auth |
| `postgres` | Long-lived inventory with ad-hoc queries | Batched `COPY FROM STDIN` into a single table |
| `s3` | Data lake / analytics ingest (Athena, Snowflake, DMS) | Headerless CSV, time-partitioned object keys |

### `stdout` — default

```yaml
env:
  - name: CLUSTER_NAME
    value: 'validation'
```

### `http`

Sends a JSON array of readings per pod, with optional bearer-token auth. Bodies are drained and connections reused.

```yaml
env:
  - name: EXPORTER_TYPE
    value: 'http'
  - name: CLUSTER_NAME
    value: 'validation'
  - name: HTTP_ENDPOINT
    value: 'https://api.example.com/gpu-data'
  - name: HTTP_TIMEOUT
    value: '30s'
  - name: HTTP_AUTH_TOKEN
    valueFrom:
      secretKeyRef:
        name: http-credentials
        key: token
```

### `postgres`

Writes via `COPY FROM STDIN` inside a transaction for high-throughput batch inserts. Schema bootstrap is **opt-in** via `POSTGRES_AUTO_MIGRATE=true` — most deployments should manage the schema out-of-band (DBA tooling / migrations) and grant the controller insert-only.

```yaml
env:
  - name: EXPORTER_TYPE
    value: 'postgres'
  - name: CLUSTER_NAME
    value: 'validation'
  - name: POSTGRES_PORT
    value: '5432'
  - name: POSTGRES_DB
    value: 'gpuid'
  - name: POSTGRES_TABLE
    value: 'serials'
  # Optional: let gpuid create the table + indexes on first run (dev only).
  - name: POSTGRES_AUTO_MIGRATE
    value: 'false'
  - name: POSTGRES_HOST
    valueFrom:
      secretKeyRef: { name: db-credentials, key: host }
  - name: POSTGRES_USER
    valueFrom:
      secretKeyRef: { name: db-credentials, key: username }
  - name: POSTGRES_PASSWORD
    valueFrom:
      secretKeyRef: { name: db-credentials, key: password }
```

### `s3`

Writes one **CSV** object per export batch into a time-partitioned key, suitable for Hive-style table layouts (Athena, Glue, DMS source).

```yaml
env:
  - name: EXPORTER_TYPE
    value: 's3'
  - name: CLUSTER_NAME
    value: 'validation'
  - name: NAMESPACE
    value: 'gpu-operator'
  - name: LABEL_SELECTOR
    value: 'app=nvidia-device-plugin-daemonset'
  - name: S3_BUCKET
    value: 'gpuids'
  - name: S3_PREFIX
    value: 'serial-numbers'
  - name: S3_REGION
    value: 'us-east-1'
  - name: S3_PARTITION_PATTERN
    value: 'year=%Y/month=%m/day=%d/hour=%H'
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef: { name: s3-credentials, key: AWS_ACCESS_KEY_ID }
  - name: AWS_SECRET_ACCESS_KEY
    valueFrom:
      secretKeyRef: { name: s3-credentials, key: AWS_SECRET_ACCESS_KEY }
```

Object layout:

```
s3://bucket-name/prefix/year=YYYY/month=MM/day=DD/hour=HH/YYYYMMDD-HHMMSS.mmm.csv
```

Columns are written in the fixed order `cluster,node,machine,source,chassis,gpu,time` (no header row, RFC3339 timestamps) so the file is DMS- and Glue-friendly.

## Deploy

### Download

Download the `gpuid` release artifacts (and the optional `policy` bundle) from [the latest release](https://github.com/mchmarny/gpuid/releases/latest).

### Apply

Pick the overlay that matches your exporter:

- `stdout` (default) — [deployments/gpuid/overlays/stdout/patch-deployment.yaml](deployments/gpuid/overlays/stdout/patch-deployment.yaml)
- `http` — [deployments/gpuid/overlays/http/patch-deployment.yaml](deployments/gpuid/overlays/http/patch-deployment.yaml)
- `postgres` — [deployments/gpuid/overlays/postgres/patch-deployment.yaml](deployments/gpuid/overlays/postgres/patch-deployment.yaml)
- `s3` — [deployments/gpuid/overlays/s3/patch-deployment.yaml](deployments/gpuid/overlays/s3/patch-deployment.yaml)

```shell
kubectl apply -k deployments/gpuid/overlays/stdout
```

### Verify

```shell
kubectl -n gpuid get pods -l app=gpuid
kubectl -n gpuid logs  -l app=gpuid --tail=-1
```

## High availability

The base deployment ships with `replicas: 2` and a soft `podAntiAffinity` for host spread. To prevent duplicate exports across replicas, `gpuid` uses [Kubernetes lease-based leader election](https://kubernetes.io/docs/concepts/architecture/leases/) (`coordination.k8s.io/Lease`). Followers serve `/healthz`, `/readyz`, and `/metrics` so probes succeed cluster-wide; only the leader runs the reconciliation workers. If the leader loses its lease, it exits non-zero and Kubernetes restarts it.

To disable leader election (single-replica deployments), set `LEADER_ELECTION=false`. To tune the lease, override:

| Variable | Default | Notes |
|---|---|---|
| `LEADER_ELECTION` | `false` | Set to `true` to enable; the base deployment sets it. |
| `LEASE_NAMESPACE` | `gpuid` | Must be the namespace `gpuid` runs in. |
| `LEASE_NAME` | `gpuid-leader` | One lease per controller deployment. |
| `LEASE_DURATION` | `15s` | Must satisfy `lease > renew > retry > 0`. |
| `LEASE_RENEW_DEADLINE` | `10s` | |
| `LEASE_RETRY_PERIOD` | `2s` | |

The RBAC bundle grants the namespaced `Role` needed for the lease automatically.

## Observability

### Logs

Structured JSON via `slog`, ready for log-aggregation pipelines. Filter with `jq`:

```shell
# Errors only:
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.level == "ERROR") | "\(.time) \(.msg) \(.err)"'

# Serial readings only:
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.msg == "gpu serial number reading")
           | "\(.chassis) \(.node) \(.machine) \(.gpu)"'
```

### Metrics

`gpuid` exposes Prometheus metrics on `:8080/metrics`:

- `gpuid_export_success_total{node, pod}` — successful exports.
- `gpuid_export_failure_total{node, pod}` — failed exports.

### Health

- `/healthz` — process liveness (always 200 once the HTTP server is up).
- `/readyz` — readiness; same surface as `/healthz` so probes don't compete with metrics rendering.
- `/metrics` — Prometheus exposition.

## Record schema

Every record carries the same set of fields across all backends:

| Field | Description |
|---|---|
| `cluster` | Kubernetes cluster identifier (`CLUSTER_NAME`) |
| `node` | Kubernetes node name where the GPU was observed |
| `machine` | Provider instance ID parsed from `spec.providerID` (or `na`) |
| `source` | `namespace/podname` of the pod that produced the reading |
| `chassis` | Chassis (host) serial; `unknown` if absent (e.g., H100) |
| `gpu` | GPU serial reported by `nvidia-smi` |
| `time` | Reading time in RFC3339 |

### HTTP body

```json
[
  {
    "cluster": "production-cluster",
    "node": "gpu-node-01",
    "machine": "i-1234567890abcdef0",
    "source": "gpu-operator/nvidia-device-plugin-abc123",
    "chassis": "1821325191344",
    "gpu": "1761025346025",
    "time": "2026-06-07T10:30:45Z"
  }
]
```

### PostgreSQL

When `POSTGRES_AUTO_MIGRATE=true`, `gpuid` creates this schema (idempotent):

```sql
CREATE TABLE serials (
    id BIGSERIAL PRIMARY KEY,
    cluster VARCHAR(255) NOT NULL,
    node VARCHAR(255) NOT NULL,
    machine VARCHAR(255) NOT NULL,
    source VARCHAR(255) NOT NULL,
    chassis VARCHAR(255) NOT NULL,
    gpu VARCHAR(255) NOT NULL,
    read_time TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(cluster, node, machine, source, chassis, gpu, read_time)
);

CREATE INDEX idx_serials_cluster    ON serials (cluster);
CREATE INDEX idx_serials_node       ON serials (node);
CREATE INDEX idx_serials_read_time  ON serials (read_time);
CREATE INDEX idx_serials_created_at ON serials (created_at);
```

Useful queries:

```sql
-- GPUs that have been used in more than one machine
SELECT gpu, COUNT(DISTINCT machine) AS machines_per_gpu
FROM serials GROUP BY gpu HAVING COUNT(DISTINCT machine) > 1
ORDER BY machines_per_gpu DESC;

-- GPUs that moved across clusters
SELECT gpu, COUNT(DISTINCT cluster) AS clusters_seen_in
FROM serials GROUP BY gpu HAVING COUNT(DISTINCT cluster) > 1
ORDER BY clusters_seen_in DESC;

-- Unique GPUs per day
SELECT DATE(read_time) AS day, COUNT(DISTINCT gpu) AS unique_gpus
FROM serials GROUP BY day ORDER BY day;
```

### Inspect labels in the cluster

```shell
kubectl get nodes -l nodeGroup=customer-gpu -o json \
| jq -r '
    [ .items[]
      | {chassis: (.metadata.labels["gpuid.github.com/chassis"] // "na")}
    ]
    | group_by(.chassis)
    | map({(.[0].chassis): length})
    | add
'
# {
#   "1821025191506": 9,
#   "1821225190819": 7,
#   "1821225192095": 9,
#   "1821325191344": 9
# }
```

### Cleanup

```shell
kubectl delete -k deployments/gpuid/overlays/stdout
```

## Configuration reference

Every flag is an environment variable read at startup. Typed values (durations, ints, floats, bools) fail-fast on parse errors instead of silently falling back to defaults.

| Variable | Default | Notes |
|---|---|---|
| `EXPORTER_TYPE` | `stdout` | `stdout`, `http`, `postgres`, `s3` |
| `CLUSTER_NAME` | `""` (required) | Used in every exported record |
| `NAMESPACE` | `gpu-operator` | Namespace to watch |
| `LABEL_SELECTOR` | `app=nvidia-device-plugin-daemonset` | Pod selector |
| `CONTAINER` | `nvidia-device-plugin` | Container to `exec nvidia-smi` in |
| `WORKERS` | `16` | Concurrent reconcilers (1–100) |
| `TIMEOUT` | `30s` | Per-pod processing budget |
| `RESYNC` | `0` | Informer resync; 0 = event-driven only |
| `QPS` | `50` | Kubernetes API client QPS |
| `BURST` | `100` | Kubernetes API client burst |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SERVER_PORT` | `8080` | Metrics + health server port |
| `LEADER_ELECTION` | `false` | See [High availability](#high-availability) |

See `pkg/runner/option.go` for the full list including the lease tuning knobs and per-exporter variables.

## Security and supply chain

`gpuid` images are built and attested with [SLSA](https://slsa.dev/) provenance. Containers run with:

- `runAsNonRoot: true`, `runAsUser: 65532`
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- `capabilities: drop [ALL]`
- `seccompProfile: RuntimeDefault`

### Verify the image

```shell
export IMAGE=ghcr.io/mchmarny/gpuid:latest
```

With the GitHub CLI:

```shell
gh attestation verify "oci://$IMAGE" \
  --repo mchmarny/gpuid \
  --predicate-type https://slsa.dev/provenance/v1 \
  --limit 1
```

With `cosign`:

```shell
cosign verify-attestation \
    --type https://slsa.dev/provenance/v1 \
    --certificate-github-workflow-repository 'mchmarny/gpuid' \
    --certificate-identity-regexp 'https://github.com/mchmarny/gpuid/*' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    $IMAGE
```

Manual verification: browse [the attestations page](https://github.com/mchmarny/gpuid/attestations) and match the subject digest against the image you deploy.

### Cluster-side admission policy

To enforce that only signed `gpuid` images can run in your cluster:

```shell
# 1. Install Sigstore Policy Controller (if you don't have it)
kubectl create namespace cosign-system
helm repo add sigstore https://sigstore.github.io/helm-charts
helm repo update
helm install policy-controller -n cosign-system sigstore/policy-controller

# 2. Opt the gpuid namespace into Sigstore validation
kubectl label namespace gpuid policy.sigstore.dev/include=true

# 3. Apply the bundled image policy
kubectl apply -f deployments/policy/slsa-attestation.yaml

# 4. Smoke-test
kubectl -n gpuid run test --image=$IMAGE
```

## Development

```shell
make qualify      # full quality gate: test + coverage + lint + scan
make test         # unit tests with race detector
make test-coverage  # tests + coverage threshold (45% baseline; raise as it improves)
make lint         # golangci-lint + yamllint
make scan         # grype vulnerability scan
make e2e          # KinD-based end-to-end test
make tools-check  # report installed-vs-pinned tool versions from .settings.yaml
```

Tool versions live in `.settings.yaml` (Renovate-managed) and the Go toolchain pin in `.go-version`. CI invokes the same reusable [`qualification.yaml`](.github/workflows/qualification.yaml) workflow for both PR validation and the release pipeline.

## Disclaimer

This is a personal project and does not represent my employer. While I do my best to keep things working, I take no responsibility for issues caused by this code.
