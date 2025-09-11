[![pipeline status](https://github.com/mchmarny/gpuid/badges/main/pipeline.svg)](https://github.com/mchmarny/gpuid/-/commits/main) [![coverage report](https://github.com/mchmarny/gpuid/badges/main/coverage.svg)](https://github.com/mchmarny/gpuid/-/commits/main)

# GPU Serial Number Exporter (gpuid)

A Kubernetes-native service that monitors GPU-enabled pods and exports GPU serial numbers to various backends for tracking, monitoring, and analytics. The service watches for pods with NVIDIA device plugin daemonsets, executes `nvidia-smi` to extract GPU serial numbers, and exports structured data to configurable destinations.

## Why

Kubernetes nodes in managed services (e.g. AWS EKS, GCP GKE) are ephemeral VMs that can run on different physical hosts over time. When managing GPU workloads, it's crucial to:

- Track GPU health and utilization across physical hardware
- Correlate GPU performance issues with specific hardware units
- Maintain audit trails for GPU resource allocation
- Monitor GPU lifecycle in multi-tenant environments

The `gpuid` service provides a lightweight, scalable solution for GPU inventory management in Kubernetes clusters.

## Features

- **Configurable Backends**: Support for stdout, PostgreSQL DB, and Amazon S3 bucket
- **Scale Ready**: Connection pooling, retry logic, health checks
- **Structured Logging**: JSON-formatted logs with contextual information
- **Emitting Metrics**: Prometheus-compatible metrics for monitoring
- **Supply Chain Secure**: SLSA attestation and Sigstore validation support 

## Available Exporters

### Stdout Exporter

**Type**: `stdout`
**Purpose**: Development and debugging, outputs JSON to stdout
**Configuration**: No additional environment variables required

```yaml
env:
  - name: EXPORTER_TYPE
    value: 'stdout'
```

### PostgreSQL Exporter

**Type**: `postgres`
**Purpose**: Database storage with full ACID compliance
**Features**: Connection pooling, automatic schema management, batch processing

```yaml
env:
  - name: EXPORTER_TYPE
    value: 'postgres'
  - name: POSTGRES_HOST
    value: 'postgresql.database.svc.cluster.local'
  - name: POSTGRES_PORT
    value: '5432'
  - name: POSTGRES_DB
    value: 'gpu_monitoring'
  - name: POSTGRES_USER
    valueFrom:
      secretKeyRef:
        name: postgresql-credentials
        key: username
  - name: POSTGRES_PASSWORD
    valueFrom:
      secretKeyRef:
        name: postgresql-credentials
        key: password
  - name: POSTGRES_SSLMODE
    value: 'require'
  - name: POSTGRES_TABLE
    value: 'gpu_serial_readings'
```

### Amazon S3 Exporter

**Type**: `s3`
**Purpose**: Cloud storage with time-based partitioning
**Features**: Automatic partitioning, batch uploads, configurable prefixes

```yaml
env:
  - name: EXPORTER_TYPE
    value: 's3'
  - name: S3_BUCKET
    value: 'gpu-monitoring-data'
  - name: S3_PREFIX
    value: 'gpu-serials'
  - name: S3_REGION
    value: 'us-west-2'
  - name: S3_PARTITION_PATTERN
    value: 'year=%Y/month=%m/day=%d/hour=%H'
```

## Usage

### Deployment

1. **Configure the deployment** by updating `deployment/overlays/prod/patch-deployment.yaml`:

```yaml
# Core service configuration
env:
  - name: CLUSTER_NAME
    value: 'production-cluster'  # Unique cluster identifier
  - name: NAMESPACE
    value: 'gpu-operator'        # Target namespace for GPU pods
  - name: LABEL_SELECTOR
    value: 'app=nvidia-device-plugin-daemonset'  # Pod selector
  
  # Exporter configuration (choose one)
  - name: EXPORTER_TYPE
    value: 'postgres'  # Options: stdout, postgres, s3
  
  # Batch processing configuration
  - name: BATCH_SIZE
    value: '100'       # Records per batch (optional, default: 10)
  - name: RETRY_COUNT
    value: '3'         # Retry attempts (optional, default: 2)
  - name: TIMEOUT
    value: '30'        # Timeout in seconds (optional, default: 30)
```

2. **Apply the configuration**:

```shell
kubectl apply -k deployment/overlays/prod
```

3. **Verify deployment**:

```shell
kubectl -n gpuid get pods -l app=gpuid
kubectl -n gpuid logs -l app=gpuid --tail=10
```

### Monitoring and Observability

**Structured Logging**: `gpuid` emits JSON-formatted logs with contextual information:

```shell
kubectl -n gpuid logs -l app=gpuid -f
```

**Filter logs with jq** for specific information:

```shell
# Show GPU serial exports
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.msg == "exported GPU serials") | "\(.cluster) \(.node) \(.gpu_count) GPUs"'

# Show health check results  
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.msg == "exporter health check") | "\(.time) \(.exporter_type) \(.status)"'

# Show error events
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -r 'select(.level == "ERROR") | "\(.time) \(.msg) \(.error)"'
```

**Sample JSON Output**:
```json
{
  "time": "2025-09-10T10:30:45Z",
  "level": "INFO",
  "msg": "exported GPU serials",
  "cluster": "production-cluster", 
  "node": "gpu-node-01",
  "machine": "i-1234567890abcdef0",
  "source": "gpu-operator/nvidia-device-plugin-abc123",
  "gpu_count": 4,
  "exporter_type": "postgres",
  "duration": "145ms"
}
```

### Cleanup

```shell
kubectl delete -k deployment/overlays/prod
```

## Metrics and Monitoring

The `gpuid` service exposes Prometheus-compatible metrics on `:8080/metrics`:

- `gpuid_export_success_total{exporter_type, cluster}`: Successful export operations
- `gpuid_export_failure_total{exporter_type, cluster, error_type}`: Failed export operations  
- `gpuid_export_duration_seconds{exporter_type, cluster}`: Export operation duration
- `gpuid_gpu_count_total{cluster, node}`: Total GPU count by node
- `gpuid_health_check_success{exporter_type}`: Exporter health check status

**Sample Prometheus Queries**:

```promql
# Export success rate by exporter type
rate(gpuid_export_success_total[5m]) / rate(gpuid_export_total[5m])

# GPU discovery rate across the cluster  
rate(gpuid_gpu_count_total[5m])

# Export operation P95 latency
histogram_quantile(0.95, gpuid_export_duration_seconds)
```

## Data Schema

GPU serial number readings are exported in a consistent schema across all backends:

```json
{
  "cluster": "production-cluster",
  "node": "gpu-node-01", 
  "machine": "i-1234567890abcdef0",
  "source": "gpu-operator/nvidia-device-plugin-abc123",
  "gpu": "GPU-A100-1234567890",
  "read_time": "2025-09-10T10:30:45Z"
}
```

**Field Descriptions**:

- `cluster`: Kubernetes cluster identifier
- `node`: Kubernetes node name where GPU was discovered
- `machine`: Cloud instance ID or physical machine identifier  
- `source`: Namespace/pod name that provided the GPU information
- `gpu`: GPU serial number from nvidia-smi
- `read_time`: Timestamp when the reading was taken (RFC3339 format)

### PostgreSQL Schema

When using the PostgreSQL exporter, data is stored in the following table structure:

```sql
CREATE TABLE gpu_serial_readings (
    id BIGSERIAL PRIMARY KEY,
    cluster VARCHAR(255) NOT NULL,
    node VARCHAR(255) NOT NULL, 
    machine VARCHAR(255) NOT NULL,
    source VARCHAR(255) NOT NULL,
    gpu VARCHAR(255) NOT NULL,
    read_time TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(cluster, node, machine, source, gpu, read_time)
);

-- Optimized indexes for common query patterns
CREATE INDEX idx_gpu_serial_readings_cluster ON gpu_serial_readings (cluster);
CREATE INDEX idx_gpu_serial_readings_node ON gpu_serial_readings (node);
CREATE INDEX idx_gpu_serial_readings_read_time ON gpu_serial_readings (read_time);
CREATE INDEX idx_gpu_serial_readings_created_at ON gpu_serial_readings (created_at);
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

### Container Image Attestation

The `gpuid` container images are built with SLSA (Supply-chain Levels for Software Artifacts) attestation for enhanced security. You can verify the image integrity using Sigstore tools.

#### Manual Verification 

> Update the image digest to the version you end up using.

```shell
export IMAGE=ghcr.io/mchmarny/gpuid:v0.5.0@sha256:345638126a65cff794a59c620badcd02cdbc100d45f7745b4b42e32a803ff645

cosign verify-attestation \
    --output json \
    --type slsaprovenance \
    --certificate-identity-regexp 'https://github.com/.*/.*/.github/workflows/.*' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    $IMAGE 
```

#### In-Cluster Policy Enforcement

To ensure only verified images are deployed in your cluster:

1. **Enable Sigstore policy validation**:

```shell
kubectl label namespace gpuid policy.sigstore.dev/include=true
```

2. **Apply the image policy**:

```shell
kubectl apply -f deployment/policy/slsa-attestation.yaml
```

3. **Test the admission policy**:

```shell
kubectl -n gpuid run test --image=$IMAGE
```

4. **Install Sigstore Policy Controller** (if not already installed):

```shell
kubectl create namespace cosign-system
helm repo add sigstore https://sigstore.github.io/helm-charts
helm repo update
helm install policy-controller -n cosign-system sigstore/policy-controller
```

## Disclaimer

This is my personal project and it does not represent my employer. While I do my best to ensure that everything works, I take no responsibility for issues caused by this code.