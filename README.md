![OnPush](https://github.com/mchmarny/gpuid/actions/workflows/main.yaml/badge.svg)
![OnTag](https://github.com/mchmarny/gpuid/actions/workflows/release.yaml/badge.svg)
![Issues](https://img.shields.io/github/issues/mchmarny/gpuid)
![PRs](https://img.shields.io/github/issues-pr/mchmarny/gpuid)
![Go Report Card](https://goreportcard.com/badge/github.com/mchmarny/gpuid)
[![codecov](https://codecov.io/gh/mchmarny/gpuid/branch/main/graph/badge.svg)](https://codecov.io/gh/mchmarny/gpuid)

# GPU Serial Number Exporter (gpuid)

Monitor pods on GPU-accelerated node in Kubernetes cluster and update nodes with chassis and GPU labels serial numbers. Supports serial number export to various state backends for tracking, monitoring, and analyses.

## Why

GPU accelerated Kubernetes nodes in operator managed services (e.g. EKS in AWS or GKE in GCP) are ephemeral VMs that can run on top of physical hosts which change over time. Multiple VPs over time may run on a single physical host, so to ensure break-fix context of these nodes it's crucial to:

- Track GPU health and utilization across physical hardware
- Correlate GPU performance issues with specific hardware units
- Maintain audit trails for GPU resource allocation
- Monitor GPU lifecycle in multi-tenant environments

`gpuid` provides a lightweight, scalable solution for GPU inventory management in Kubernetes clusters.

## Features

- Node labels with the GPU and chassis serial numbers
- HTTP, PostgreSQL DB, and S3 exporters
- Connection pooling, retry logic, health checks
- Structured logging with contextual information
- Prometheus-compatible observability metrics for monitoring
- SLSA build attestation and Sigstore attestation validation

## Available Exporters

**gpuid** supports multiple data export backends:

* **StdOut**: Development and debugging (default)
* **HTTP**: POSTs to HTTP endpoints
* **PostgreSQL**: Batch inserts into PostgreSQL database
* **S3**: Puts CSV object into S3-compatible bucket

### Stdout Exporter

**Type**: `stdout` (default)
**Purpose**: Development and debugging, outputs JSON to stdout
**Configuration**: No additional environment variables required

```yaml
env:
  - name: CLUSTER_NAME
    value: 'validation'
```

### HTTP Exporter

**Type**: `http`
**Purpose**: Send GPU data to HTTP endpoints via POST requests
**Features**: Bearer token authentication, configurable timeouts, automatic retries

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


### PostgreSQL Exporter

**Type**: `postgres`
**Purpose**: Database storage with full ACID compliance
**Features**: Connection pooling, automatic schema management, batch processing

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
  - name: POSTGRES_HOST
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: host
  - name: POSTGRES_USER
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: username
  - name: POSTGRES_PASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: password
```

### Amazon S3 Exporter

**Type**: `s3`
**Purpose**: Cloud storage with time-based partitioning
**Features**: Automatic partitioning, batch uploads, configurable prefixes

```yaml
env:
  - name: EXPORTER_TYPE
    value: 's3'
  - name: CLUSTER_NAME
    value: 'validation'
  # GPU Serial Number Provider
  - name: NAMESPACE
    value: 'gpu-operator'
  - name: LABEL_SELECTOR
    value: 'app=nvidia-device-plugin-daemonset'
  # S3 Exporter Configuration
  - name: S3_BUCKET
    value: 'gpuids'
  - name: S3_PREFIX
    value: 'serial-numbers'
  - name: S3_REGION
    value: 'us-east-1'
  - name: S3_PARTITION_PATTERN
    value: 'year=%Y/month=%m/day=%d/hour=%H'
  # AWS Credentials from Kubernetes Secret
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: s3-credentials
        key: AWS_ACCESS_KEY_ID
  - name: AWS_SECRET_ACCESS_KEY
    valueFrom:
      secretKeyRef:
        name: s3-credentials
        key: AWS_SECRET_ACCESS_KEY
```

## Usage

### Deployment

1. **Configure the deployment** by updating the specific overlay that corresponds to your backend type:

* http - [deployments/gpuid/overlays/http/patch-deployment.yaml](deployments/gpuid/overlays/http/patch-deployment.yaml)   
* postgres - [deployments/gpuid/overlays/postgres/patch-deployment.yaml](deployments/gpuid/overlays/postgres/patch-deployment.yaml)
* s3 - [deployments/gpuid/overlays/s3/patch-deployment.yaml](deployments/gpuid/overlays/s3/patch-deployment.yaml)
* stdout - [deployments/gpuid/overlays/stdout/patch-deployment.yaml](deployments/gpuid/overlays/stdout/patch-deployment.yaml)

1. **Apply the configuration**

For example, to deploy with S3 backend:

```shell
kubectl apply -k deployments/gpuid/overlays/s3
```

1. **Verify deployment**

Make sure the exporter pod is running:

```shell
kubectl -n gpuid get pods -l app=gpuid
```

And review its logs: 

```shell
kubectl -n gpuid logs -l app=gpuid --tail=-1
```

### Monitoring and Observability

`gpuid` emits structured logs in JSON format with contextual information:

Since these logs are in JSON, you can filter them with `jq` for specific information, for example, error events:

```shell
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.level == "ERROR") | "\(.time) \(.msg) \(.error)"'
```

### Cleanup

```shell
kubectl delete -k deployments/gpuid/overlays/s3
```

## Metrics and Monitoring

The `gpuid` service exposes Prometheus-compatible metrics on the `:8080/metrics` endpoint:

- `gpuid_export_success_total{exporter_type, node, pod}`: Successful export operations
- `gpuid_export_failure_total{exporter_type, node, pod, error_type}`: Failed export operations

## Exported Data Schema

GPU serial number readings are exported in a consistent schema across all backends:

- `cluster`: Kubernetes cluster identifier where the GPUs were observed
- `node`: Kubernetes node name where GPU was discovered
- `machine`: VM instance ID or physical machine identifier  
- `source`: Namespace/Pod name that provided the GPU information
- `gpu`: GPU serial number from nvidia-smi
- `read_time`: Timestamp when the reading was taken (RFC3339 format)

### HTTP Post Content

When using HTTP exporter, the content includes the JSON serialized record: 

```json
{
  "cluster": "production-cluster",
  "node": "gpu-node-01", 
  "machine": "i-1234567890abcdef0",
  "source": "gpu-operator/nvidia-device-plugin-abc123",
  "gpu": "1234567890",
  "time": "2025-09-10T10:30:45Z"
}
```

### PostgreSQL Schema

When using the PostgreSQL exporter, data is stored in the following table structure:

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

-- Optimized indexes for common query patterns
CREATE INDEX idx_serials_cluster ON serials (cluster);
CREATE INDEX idx_serials_node ON serials (node);
CREATE INDEX idx_serials_read_time ON serials (read_time);
CREATE INDEX idx_serials_created_at ON serials (created_at);
```

Few queries: 

GPUs which have been used in more than 1 machine:

```sql
SELECT 
    gpu, 
    COUNT(DISTINCT machine) AS machines_per_gpu
FROM serials
GROUP BY gpu
HAVING COUNT(DISTINCT machine) > 1
ORDER BY gpu;
```

GPUs that moved across clusters:

```sql
SELECT 
    gpu,
    COUNT(DISTINCT cluster) AS clusters_seen_in
FROM serials
GROUP BY gpu
HAVING COUNT(DISTINCT cluster) > 1
ORDER BY clusters_seen_in DESC;
```

Number of GPUs per day: 

```sql
SELECT 
    DATE(read_time) AS day,
    COUNT(DISTINCT gpu) AS unique_gpus
FROM serials
GROUP BY day
ORDER BY day;
```

### S3 Object Structure

The S3 exporter organizes data with time-based partitioning:

```
s3://bucket-name/prefix/
├── year=2025/month=09/day=10/hour=10/
│   ├── cluster=prod/node=gpu-node-01/20250910-103045-uuid.json
│   └── cluster=prod/node=gpu-node-02/20250910-103112-uuid.json
└── year=2025/month=09/day=10/hour=11/
    └── cluster=prod/node=gpu-node-01/20250910-110215-uuid.json
```

## Security and Validation

The `gpuid` container images are built with SLSA (Supply-chain Levels for Software Artifacts).

### Manual Verification 

Navigate to https://github.com/mchmarny/gpuid/attestations and pick the version you want to verify. The subject digest at the bottom should match the digest of the image you are deploying.

### Using CLIs

> Update the image digest to the version you end up using.

```shell
export IMAGE=ghcr.io/mchmarny/gpuid:v0.6.0@sha256:307ae8fe9303fae95e345ab2cad5022835b497aa82e9bd714ae94f7286657c4d
```

#### GitHub CLI

To verify the attestation on this image using GitHub CLI: 

```shell
gh attestation verify "oci://$IMAGE" \
  --repo mchmarny/gpuid \
  --predicate-type https://slsa.dev/provenance/v1 \
  --limit 1
```

#### Cosign CLI

```shell
cosign verify-attestation \
    --type https://slsa.dev/provenance/v1 \
    --certificate-github-workflow-repository 'mchmarny/gpuid' \
    --certificate-identity-regexp 'https://github.com/mchmarny/gpuid/*' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    $IMAGE
```

### In-Cluster Policy Enforcement

To ensure only verified images are deployed in your cluster:

1. Install Sigstore Policy Controller (if not already installed):

```shell
kubectl create namespace cosign-system
helm repo add sigstore https://sigstore.github.io/helm-charts
helm repo update
helm install policy-controller -n cosign-system sigstore/policy-controller
```

2. Enable Sigstore policy validation:

```shell
kubectl label namespace gpuid policy.sigstore.dev/include=true
```

3. Apply the image policy:

```shell
kubectl apply -f deployments/policy/slsa-attestation.yaml
```

4. Test the admission policy:

```shell
kubectl -n gpuid run test --image=$IMAGE
```

## Disclaimer

This is my personal project and it does not represent my employer. While I do my best to ensure that everything works, I take no responsibility for issues caused by this code.