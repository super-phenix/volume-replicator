# Volume Replicator

The **Volume Replicator** is a Kubernetes controller designed to automate the lifecycle of `VolumeReplication` resources. It monitors `PersistentVolumeClaim` (PVC) resources and automatically creates, updates, or deletes corresponding `VolumeReplication` objects based on annotations.

`VolumeReplications` are a CRD from the [CSI-Addons project](https://github.com/csi-addons/kubernetes-csi-addons).
This controller only makes it easier to automatically generate `VolumeReplication` objects when creating `PersistentVolumeClaims` and does not handle
the actual replication itself.

## Features

- **Automated Lifecycle**: Automatically creates `VolumeReplication` objects for PVCs with the appropriate annotation.
- **Inheritance**: Can inherit the `VolumeReplicationClass` from the PVC or from the PVC's namespace (if not specified on the PVC).
- **Exclusion by Name**: Supports excluding PVCs from replication using a global regular expression.
- **VRC Selector**: Supports selecting a `VolumeReplicationClass` using a selector, allowing for more dynamic configuration based on `StorageClass` groups.
- **Cleanup**: Automatically deletes `VolumeReplication` resources when their parent PVC is deleted or when the replication annotation is removed.
- **Leader Election**: Supports high availability with leader election to ensure only one instance is active at a time.
- **Metadata Propagation**: Labels and annotations from the PVC are propagated to the generated `VolumeReplication` resource.

## Usage

To enable replication for a PVC, you need to specify a `VolumeReplicationClass` or a selector using an annotation.
The `VolumeReplicationClass` must be compatible with the `StorageClass` of your PVC.

Once the annotation is detected on either the PVC or its namespace, the corresponding `VolumeReplication` object is created.

> [!NOTE]
> It is important that your CSI is compatible with the CSI-Addons project and can handle replicating the data in your volumes to another cluster.
> Check the documentation of your CSI for more information and ensure the CSI-Addons CRDs are installed on your  cluster.

### Using a specific VolumeReplicationClass

Add the following annotation to your `PersistentVolumeClaim`:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
  annotations:
    replication.superphenix.net/class: "my-replication-class"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

### Using a VolumeReplicationClass Selector

Alternatively, you can use a selector to let the controller choose the appropriate `VolumeReplicationClass`.
This is useful when you have multiple `StorageClasses` and you want to apply a common replication policy (e.g., "daily") across them.

For example, if you have two different CSIs, you might have two different `StorageClass`.
If you wish to implement a daily replication policy, you will have to create two different `VolumeReplicationClass` objects (one for each `StorageClass`).
These two VRCs will do exactly the same thing but have different credentials for each backend.

Using the `replication.superphenix.net/class` annotation, users would need to know which `VolumeReplicationClass` to use for which `StorageClass`.
Using selectors, a simple `daily` label can be added to the PVC to make it replicated, without knowledge of the real `VolumeReplicationClass` name.

The controller will automatically infer the `VolumeReplicationClass` linked to the `StorageClass` of the PVC based on the `storageClassGroup` label appended to both of them.
The user can then specify what replication policy they want applied to their PVC using an additional annotation on the PVC.

#### 1. Label your StorageClass

Add the `replication.superphenix.net/storageClassGroup` label to your `StorageClass`:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast-storage
  labels:
    replication.superphenix.net/storageClassGroup: "ceph"
provisioner: ceph
```

#### 2. Label your VolumeReplicationClasses

Label your `VolumeReplicationClass` objects with both the group and the selector:

```yaml
apiVersion: replication.storage.openshift.io/v1alpha1
kind: VolumeReplicationClass
metadata:
  name: production-ssd-daily
  labels:
    replication.superphenix.net/storageClassGroup: "ceph"
    replication.superphenix.net/classSelector: "daily"
spec:
  provisioner: ceph
  parameters:
    ...
```

The controller now knows that PVCs provisioned using the "fast-storage" `StorageClass` can be replicated using the "production-ssd-daily" `VolumeReplicationClass`.
It is possible to link multiple `StorageClass` and `VolumeReplicationClass` objects together using the same selector.

The controller also knows that the `production-ssd-daily` `VolumeReplicationClass` can be selected using the `daily` selector.

Note that the `provisioner` of the `VolumeReplicationClass` must match the `provisioner` of the `StorageClass` it is linked to.

#### 3. Annotate your PVC or Namespace

Add the `replication.superphenix.net/classSelector` annotation to your PVC or its Namespace:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
  annotations:
    replication.superphenix.net/classSelector: "daily"
spec:
  storageClassName: fast-storage
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

The controller will look for a `VolumeReplicationClass` that matches both the `storageClassGroup` of the PVC's `StorageClass` and the `classSelector` specified in the annotation.

### Annotating a Namespace

If multiple PVCs in a namespace should use the same `VolumeReplicationClass` (or selector), you can annotate the namespace instead:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
  annotations:
    replication.superphenix.net/class: "my-replication-class"
    # OR
    # replication.superphenix.net/classSelector: "daily"
```

The annotation on the PVC takes precedence over the annotation on the namespace.

If the annotation is modified, the `VolumeReplication` is updated accordingly with the new `VolumeReplicationClass`.

> [!CAUTION]
> VolumeReplication objects are mostly immutable, which means that the controller will delete and recreate them when they need to be updated.
> Depending on your CSI, that means the replicated data can be lost during deletion and re-created shortly after.
> This may cause unnecessary replication traffic and a potential risk of data loss on the replication cluster.

If the annotation is deleted on both the PVC and the namespace, the VolumeReplication is deleted.

### Excluding PVCs from replication

It is possible to exclude some PVCs from being replicated, even if they have the correct annotations (or their namespace has them).
This is done by providing a regular expression to the controller using the `--exclusion-regex` flag or the `EXCLUSION_REGEX` environment variable.

Any PVC whose name matches the regular expression will be ignored by the controller.
For example, if you set `EXCLUSION_REGEX` to `^prime-.*`, all PVCs starting with `prime-` will be excluded from replication.

This feature is useful to avoid unnecessary replications of temporary PVCs (for example, for "prime" PVCs created by the [Container Data Importer](https://github.com/kubevirt/containerized-data-importer)).

> [!NOTE]
> If the regular expression is empty, no PVC will be excluded (unless it doesn't have the appropriate annotations).

## Configuration

The controller can be configured using command-line flags or environment variables:

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--kubeconfig` | - | - | Path to a kubeconfig file. If not provided, it assumes in-cluster configuration. |
| `--namespace` | `NAMESPACE` | - | **Required**. The namespace where the controller is deployed (used for leader election). |
| `--exclusion-regex` | `EXCLUSION_REGEX` | - | Optional regular expression to exclude PVCs from replication by name. |

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
