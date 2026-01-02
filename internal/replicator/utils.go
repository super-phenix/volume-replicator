package replicator

import (
	"context"
	"fmt"
	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// isVolumeReplicationCorrect verifies if the definition of a VolumeReplication conforms to its originating PVC
func isVolumeReplicationCorrect(pvc *corev1.PersistentVolumeClaim, vr *unstructured.Unstructured) bool {
	key := fmt.Sprintf("%s/%s", vr.GetNamespace(), vr.GetName())

	// Check that the VRC correspond to the one inherited from the PVC
	replicationClass, _, _ := unstructured.NestedString(vr.Object, "spec", "volumeReplicationClass")
	if getVolumeReplicationClass(pvc) != replicationClass {
		klog.Infof("VolumeReplication %s has a replication class mismatch with its parent (got %s)", key, replicationClass)
		return false
	}

	// Check that the dataSource points to the PVC
	dataSource, _, _ := unstructured.NestedNullCoercingStringMap(vr.Object, "spec", "dataSource")
	if dataSource["apiGroup"] != "v1" || dataSource["kind"] != "PersistentVolumeClaim" || dataSource["name"] != pvc.Name {
		klog.Infof("VolumeReplication %s has a datasource mismatch with its parent", key)
		return false
	}

	return true
}

// cleanupVolumeReplication deletes the VolumeReplication associated with a PVC
func cleanupVolumeReplication(name, namespace string) {
	vrNsClientSet := k8s.DynamicClientSet.Resource(VolumeReplicationResource).Namespace(namespace)

	// Try to delete the VR, dismiss any error if it simply never existed in the first place
	err := vrNsClientSet.Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("couldn't delete VolumeReplication for PVC %s/%s", namespace, name)
	}
}

// getPersistentVolumeClaim returns a PersistentVolumeClaim from its key
func getPersistentVolumeClaim(key string) (*corev1.PersistentVolumeClaim, error) {
	pvc, exists, err := PvcInformer.Informer().GetIndexer().GetByKey(key)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to retrieve PVC %s: %s", key, err.Error())
	}

	if !exists {
		return nil, nil
	}

	return pvc.(*corev1.PersistentVolumeClaim), nil
}

// createVolumeReplication creates the corresponding VolumeReplication for a given PVC.
// The VolumeReplication inherits the same name and metadata (labels, annotations) as the PVC.
func createVolumeReplication(pvc *corev1.PersistentVolumeClaim) error {
	// Create an unstructured VolumeReplication with the same name and same metadata as the PVC
	volumeReplication := &unstructured.Unstructured{}
	volumeReplication.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
		"kind":       "VolumeReplication",
		"metadata": map[string]interface{}{
			"name":        pvc.Name,
			"namespace":   pvc.Namespace,
			"annotations": pvc.Annotations,
			"labels":      getLabelsWithParent(pvc.Labels, pvc.Name),
		},
		"spec": map[string]interface{}{
			"volumeReplicationClass": getVolumeReplicationClass(pvc),
			"replicationState":       "primary",
			"dataSource": map[string]interface{}{
				"apiGroup": "v1",
				"kind":     "PersistentVolumeClaim",
				"name":     pvc.Name,
			},
		},
	})

	// Create the VolumeReplication in the same namespace where the PVC is
	resourceInterface := k8s.DynamicClientSet.Resource(VolumeReplicationResource).Namespace(pvc.Namespace)
	_, err := resourceInterface.Create(context.Background(), volumeReplication, metav1.CreateOptions{})
	return err
}

// getVolumeReplication returns the VolumeReplication associated with a PVC
func getVolumeReplication(key string) (*unstructured.Unstructured, error) {
	ns, name, _ := cache.SplitMetaNamespaceKey(key)
	vrNsClientSet := k8s.DynamicClientSet.Resource(VolumeReplicationResource).Namespace(ns)
	return vrNsClientSet.Get(context.Background(), name, metav1.GetOptions{})
}

// getVolumeReplicationClass returns the VRC to use for a PVC
// The VRC is specified through an annotation on the PVC or on its namespace
// The annotation on the PVC has priority over the one of the namespace
// If no VRC is found on either PVC or namespace, we return an empty string
func getVolumeReplicationClass(pvc *corev1.PersistentVolumeClaim) string {
	// If the PVC has a VRC specified, it has priority over the one of the namespace
	if pvc.Annotations[constants.VrcAnnotation] != "" {
		return pvc.Annotations[constants.VrcAnnotation]
	}

	// If the PVC doesn't have a VRC specified, fall back to the namespace
	namespace, err := NamespaceInformer.Lister().Get(pvc.Namespace)
	if err != nil {
		klog.Errorf("failed to retrieve parent namespace for pvc %s/%s: %s", pvc.Namespace, pvc.Name, err.Error())
		return ""
	}

	// If the namespace doesn't have VRC, this will return an empty string
	return namespace.Annotations[constants.VrcAnnotation]
}

// isParentLabelPresent returns whether a parent label is present on a VolumeReplication
func isParentLabelPresent(labels map[string]string) bool {
	return labels[constants.VrParentLabel] != ""
}

// getLabelsWithParent returns labels for a VolumeReplication with its parent PVC embedded
func getLabelsWithParent(labels map[string]string, parent string) map[string]string {
	labels[constants.VrParentLabel] = parent
	return labels
}
