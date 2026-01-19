package replicator

import (
	"context"
	"fmt"
	"regexp"

	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

var ExclusionRegex string

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
		klog.Infof("VolumeReplication %s has a dataSource mismatch with its parent", key)
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

	annotations := make(map[string]interface{})
	for k, v := range pvc.Annotations {
		annotations[k] = v
	}

	labels := make(map[string]interface{})
	for k, v := range getLabelsWithParent(pvc.Labels, pvc.Name) {
		labels[k] = v
	}

	volumeReplication.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
		"kind":       "VolumeReplication",
		"metadata": map[string]interface{}{
			"name":        pvc.Name,
			"namespace":   pvc.Namespace,
			"annotations": annotations,
			"labels":      labels,
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
	obj, exists, err := VolumeReplicationInformer.Informer().GetIndexer().GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(VolumeReplicationResource.GroupResource(), key)
	}
	return obj.(*unstructured.Unstructured), nil
}

// isParentLabelPresent returns whether a parent label is present on a VolumeReplication
func isParentLabelPresent(labels map[string]string) bool {
	return labels[constants.ParentLabel] != ""
}

// getLabelsWithParent returns a new map of labels for a VolumeReplication with its parent PVC embedded.
// It creates a copy of the input map to avoid side effects.
func getLabelsWithParent(pvcLabels map[string]string, parent string) map[string]string {
	res := make(map[string]string, len(pvcLabels)+1)
	for k, v := range pvcLabels {
		res[k] = v
	}
	res[constants.ParentLabel] = parent
	return res
}

// getStorageClassLabels returns the labels of a StorageClass
func getStorageClassLabels(pvc *corev1.PersistentVolumeClaim) (map[string]string, error) {
	// If the PVC doesn't have a storageClass, we can't do much more
	if pvc.Spec.StorageClassName == nil {
		return nil, nil
	}

	// Retrieve the StorageClass associated with this PVC
	stcGetter := k8s.ClientSet.StorageV1().StorageClasses()
	storageClass, err := stcGetter.Get(context.Background(), *pvc.Spec.StorageClassName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return storageClass.Labels, nil
}

// getStorageClassGroup returns the StorageClass group of a PVC
func getStorageClassGroup(pvc *corev1.PersistentVolumeClaim) (string, error) {
	// Retrieve the labels on the StorageClass of that PVC
	stcLabels, err := getStorageClassLabels(pvc)
	if err != nil {
		return "", err
	}

	// Retrieve the group of VolumeReplicationClasses associated with this StorageClass
	return stcLabels[constants.StorageClassGroup], nil
}

// getPvcProvisioner returns the dynamic provisioner used to provision a PVC
func getPvcProvisioner(pvc *corev1.PersistentVolumeClaim) string {
	// Try the well-known annotation first
	if pvc.Annotations[constants.StorageProvisionerAnnotation] != "" {
		return pvc.Annotations[constants.StorageProvisionerAnnotation]
	}

	// Fallback to the deprecated annotation
	return pvc.Annotations[constants.DeprecatedStorageProvisionerAnnotation]
}

// pvcNameMatchesExclusion returns whether a PVC has a name matching the exclusion regex
func pvcNameMatchesExclusion(pvc *corev1.PersistentVolumeClaim) bool {
	// If no regex is provided, return that it doesn't match
	// This is to avoid Go matching "" as "everything matches"
	if ExclusionRegex == "" {
		return false
	}

	// Match the user-provided regex
	match, err := regexp.MatchString(ExclusionRegex, pvc.Name)
	if err != nil {
		klog.Errorf("failed to parse exclusion regex: %s", err.Error())
		return false
	}

	return match
}
