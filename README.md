# Volume Replicator

The **Volume Replicator** is a Kubernetes controller designed to automate the lifecycle of `VolumeReplication` resources. It monitors `PersistentVolumeClaim` (PVC) resources and automatically creates, updates, or deletes corresponding `VolumeReplication` objects based on annotations.

`VolumeReplications` are a CRD from the [CSI-Addons project](https://github.com/csi-addons/kubernetes-csi-addons).
This controller only makes it easier to automatically generate `VolumeReplication` objects when creating `PersistentVolumeClaims` and does not handle
the actual replication itself.

## Features

- **Automated Lifecycle**: Automatically creates `VolumeReplication` objects for PVCs with the appropriate annotation.
- **Inheritance**: Can inherit the `VolumeReplicationClass` from the PVC or from the PVC's namespace (if not specified on the PVC).
- **Cleanup**: Automatically deletes `VolumeReplication` resources when their parent PVC is deleted or when the replication annotation is removed.
- **Leader Election**: Supports high availability with leader election to ensure only one instance is active at a time.
- **Metadata Propagation**: Labels and annotations from the PVC are propagated to the generated `VolumeReplication` resource.

## Usage

To enable replication for a PVC, you need to specify a `VolumeReplicationClass` using an annotation.
The `VolumeReplicationClass` must be pre-existing and compatible with the `StorageClass` of your PVC.

Once the annotation is detected on either the PVC or its namespace, the corresponding `VolumeReplication` object is created.

> [!NOTE]
> It is important that your CSI is compatible with the CSI-Addons project and can handle replicating the data in your volumes to another cluster.
> Check the documentation of your CSI for more information and ensure the CSI-Addons CRDs are installed on your  cluster.

### Annotating a PVC

Add the following annotation to your `PersistentVolumeClaim`:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
  annotations:
    replication.superphenix.net/class: "my-replication-class"
spec:
  # ... PVC spec ...
```

### Annotating a Namespace

If multiple PVCs in a namespace should use the same `VolumeReplicationClass`, you can annotate the namespace instead:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
  annotations:
    replication.superphenix.net/class: "my-replication-class"
```

The annotation on the PVC takes precedence over the annotation on the namespace.

If the annotation is modified, the `VolumeReplication` is updated accordingly with the new `VolumeReplicationClass`.

> [!CAUTION]
> VolumeReplication objects are mostly immutable, which means that the controller will delete and recreate them when they need to be updated.
> Depending on your CSI, that means the replicated data can be lost during deletion and re-created shortly after.
> This may cause unnecessary replication traffic and a potential risk of data loss on the replication cluster.

If the annotation is deleted on both the PVC and the namespace, the VolumeReplication is deleted.

## Configuration

The controller can be configured using command-line flags or environment variables:

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--kubeconfig` | - | - | Path to a kubeconfig file. If not provided, it assumes in-cluster configuration. |
| `--namespace` | `NAMESPACE` | - | **Required**. The namespace where the controller is deployed (used for leader election). |

Standard `klog` flags are also supported for logging configuration.

## Build

### Prerequisites

- Go 1.22+
- Docker (optional, for containerized builds)

### Building the binary

```bash
go build -o volume-replicator cmd/main.go
```

### Building the Docker image

```bash
docker build -t volume-replicator:latest .
```

## Deployment

### Helm Chart

A Helm chart is provided in the `charts/volume-replicator` directory.

```bash
helm install volume-replicator ./charts/volume-replicator -n volume-replicator --create-namespace
```

Ensure you configure the `namespace` correctly in your values or via `--set`.

## Development

### Running Tests

To run the unit tests:

```bash
go test -v ./...
```

### Local Development

You can run the controller locally pointing to your current Kubernetes context:

```bash
export NAMESPACE=default
go run cmd/main.go --kubeconfig ~/.kube/config --namespace $NAMESPACE
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
