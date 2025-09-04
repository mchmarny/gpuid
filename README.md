[![pipeline status](https://github.com/mchmarny/gpuid/badges/main/pipeline.svg)](https://github.com/mchmarny/gpuid/-/commits/main) [![coverage report](https://github.com/mchmarny/gpuid/badges/main/coverage.svg)](https://github.com/mchmarny/gpuid/-/commits/main)

# gpu serial number exporter (aka gpuid)

Watches for pod with device plugin daemonset based on namespace and label selectors. When new one appears, it executes `nvidia-smi` command to extract unique list of GPU serial numbers and export them to one of the defined exporters.

## why 

Kubernetes nodes used in managed services offerings like AWS EKS or GCP GKE are VMs. These VMs can be running on any number of physical hosts. When reasoning about a GPU node, it may be required to understand the "health" of the GPU, and that GPU can be used by multiple VMs over time. 

## usage

### deploy

Update the `deployment/overlays/prod` overlay. The key parts are: 

```yaml
env:
  - name: CLUSTER_NAME
    value: 'prod'  # unique name used as source of serial numbers in the exported data 
  - name: NAMESPACE
    value: 'gpu-operator'  # namespace for pod selector
  - name: LABEL_SELECTOR
    value: 'app=nvidia-device-plugin-daemonset'  # select for pod with SMI
  - name: EXPORTER_TYPE
    value: 'stdout'  # where to export the data (s3, postgress, etc.)
```

When done, apply the configuration to your cluster

```shell
kubectl apply -k deployment/overlays/prod
```

### monitor

`gpuid` emits structured logging:

```shell
kubectl -n gpuid logs -l app=gpuid -f
```

You can also use `jq` to output the key bits:

```shell
kubectl -n gpuid logs -l app=gpuid --tail=-1 \
  | jq -s -r '.[] | select(.stdout != null) | "\(.node) \(.msg)"'
```

### delete 

```shell
kubectl delete -k deployment/overlays/prod
```

## Observe

The `gpuid` service emits following metrics: 

- `gpuid_export_success_total`: Number of successful export operations.
- `gpuid_export_failure_total`: Number of failed export operations.

## Validation (optional)

The image produced by this repo comes with SLSA attestation which verifies that node role setter image was built in this repo. You can either verify that manually via [Sigstore](https://docs.sigstore.dev/about/overview/)  CLI or in the cluster suing [Sigstore](https://docs.sigstore.dev/about/overview/) policy controller.

### Manual 

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

### In Cluster

To to ensure the image used in the node role setter was built in this repo, you can enroll that one namespace (default: `node-labeler`):

```shell
kubectl label namespace gpuid policy.sigstore.dev/include=true
```

And then add ClusterImagePolicy:

```shell
kubectl apply -f deployment/policy/slsa-attestation.yaml
```

And then test admission policy: 

```shell
 kubectl -n gpuid run test --image=$IMAGE
```

If you don't already have [Sigstore](https://docs.sigstore.dev/about/overview/) policy controller, you add it into your cluster:

```shell
kubectl create namespace cosign-system
helm repo add sigstore https://sigstore.github.io/helm-charts
helm repo update
helm install policy-controller -n cosign-system sigstore/policy-controller
```

## Disclaimer

This is my personal project and it does not represent my employer. While I do my best to ensure that everything works, I take no responsibility for issues caused by this code.