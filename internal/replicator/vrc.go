package replicator

import (
	"context"

	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

// getVolumeReplicationClass returns the VRC to use for a PVC.
// The VRC can be provided through annotations as a value or as a selector.
// The annotations can be placed on the PVC or on its namespace.
func getVolumeReplicationClass(pvc *corev1.PersistentVolumeClaim) string {
	// Retrieve the literal VRC provided on the PVC
	value := getVolumeReplicationClassValue(pvc)
	if value != "" {
		return value
	}

	// If no VRC value was provided, fallback to the selector
	return getVolumeReplicationClassFromSelector(pvc)
}

// getVolumeReplicationClassFromSelector finds a VolumeReplicationClass that matches the StorageClass group of a PVC
// and that matches the user-defined selector placed in the annotation of the PVC.
// This function is used to automatically infer the correct VRC to use based on a standard label
// placed on each VolumeReplication (e.g. "replication.superphenix.net/classSelector: daily" for VRCs
// that synchronize the data every day).
func getVolumeReplicationClassFromSelector(pvc *corev1.PersistentVolumeClaim) string {
	// If the selector is not provided, we cannot proceed with filtering
	selector := getVolumeReplicationClassSelector(pvc)
	if selector == "" {
		return ""
	}

	// Retrieve the StorageClass group of the PVC
	group, err := getStorageClassGroup(pvc)
	if err != nil {
		klog.Errorf("failed to get StorageClass group for PVC %s/%s: %s", pvc.Namespace, pvc.Name, err.Error())
		return ""
	}

	// Abort if no group is specified
	if group == "" {
		klog.Infof("no StorageClass group on PVC %s/%s", pvc.Namespace, pvc.Name)
		return ""
	}

	// Filter all VolumeReplicationClasses in the correct group and with the correct classSelector/provisioner
	volumeReplicationClasses, err := filterVrcFromSelector(group, selector, getPvcProvisioner(pvc))
	if err != nil {
		klog.Errorf("failed to filter VRCs for PVC %s/%s: %s", pvc.Namespace, pvc.Name, err.Error())
		return ""
	}

	// We expect to find exactly one VolumeReplicationClass
	if len(volumeReplicationClasses) != 1 {
		if len(volumeReplicationClasses) > 1 {
			klog.Errorf("found %d matching VRCs for PVC %s/%s, expected 1", len(volumeReplicationClasses), pvc.Namespace, pvc.Name)
		}
		return ""
	}

	return volumeReplicationClasses[0]
}

// getVolumeReplicationClassValue returns the VRC to use for a PVC.
// The VRC is specified through an annotation on the PVC or on its namespace.
// The annotation on the PVC has priority over the one of the namespace.
// If no VRC is found on either PVC or namespace, we return an empty string.
func getVolumeReplicationClassValue(pvc *corev1.PersistentVolumeClaim) string {
	return getAnnotationValue(pvc, constants.VrcValueAnnotation)
}

// getVolumeReplicationClassSelector returns the VRC selector to use for a PVC.
// The VRC selector is specified through an annotation on the PVC or on its namespace.
// The annotation on the PVC has priority over the one of the namespace.
// If no VRC selector is found on either PVC or namespace, we return an empty string.
func getVolumeReplicationClassSelector(pvc *corev1.PersistentVolumeClaim) string {
	return getAnnotationValue(pvc, constants.VrcSelectorAnnotation)
}

// getAnnotationValue returns the value of an annotation from a PVC or its namespace.
// The annotation on the PVC has priority over the one of the namespace.
func getAnnotationValue(pvc *corev1.PersistentVolumeClaim, annotation string) string {
	// If the PVC has the annotation specified, it has priority over the one of the namespace
	if value, ok := pvc.Annotations[annotation]; ok && value != "" {
		return value
	}

	// If the PVC doesn't have the annotation specified, fall back to the namespace
	namespace, err := NamespaceInformer.Lister().Get(pvc.Namespace)
	if err != nil {
		klog.Errorf("failed to retrieve parent namespace for PVC %s/%s: %s", pvc.Namespace, pvc.Name, err.Error())
		return ""
	}

	// If the namespace doesn't have the annotation, this will return an empty string
	return namespace.Annotations[annotation]
}

// filterVrcFromSelector returns a VolumeReplicationClass that is in a specific StorageClass Group
// and with a specific VolumeReplicationClass selector. It also filters for faulty provisioners.
// It is assumed that a VRC must have a provisioner identical to the provisioner of the PVC.
func filterVrcFromSelector(group, selector, pvcProvisioner string) ([]string, error) {
	// Filter only VRCs in the right StorageClass group and with the right selector
	vrcLister := k8s.DynamicClientSet.Resource(VolumeReplicationClassesResource)
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			constants.StorageClassGroup:     group,
			constants.VrcSelectorAnnotation: selector,
		},
	}

	// Retrieve the VRCs that match our labelSelector
	list, err := vrcLister.List(context.Background(), metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(labelSelector)})
	if err != nil {
		return nil, err
	}

	// Filter for VRCs that have the same provisioner as our PVC
	var classes []string
	for _, item := range list.Items {
		vrcProvisioner, _, _ := unstructured.NestedString(item.Object, "spec", "provisioner")
		// Allow the pvcProvisioner to be empty, as some CSI may not place it in any annotation.
		if vrcProvisioner == pvcProvisioner || pvcProvisioner == "" {
			classes = append(classes, item.GetName())
		} else {
			klog.V(2).Infof("discarded VRC %s as it doesn't have the same provisioner as the PVC, got %s, expected %s", item.GetName(), vrcProvisioner, pvcProvisioner)
		}
	}

	return classes, nil
}
