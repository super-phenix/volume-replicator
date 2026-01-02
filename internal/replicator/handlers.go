package replicator

import (
	"fmt"
	"github.com/skalanetworks/volume-replicator/internal/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"reflect"
)

// namespaceUpdate is called whenever an update is detected on a namespace
// We check if the volumeReplicationClass annotation has changed, and if it has,
// we propagate the update to every PVC inside the namespace
func (c *Controller) namespaceUpdate(oldNs, newNs *corev1.Namespace) {
	// Don't continue if the class hasn't changed or if the annotation wasn't deleted
	if oldNs.Annotations[constants.VrcAnnotation] == newNs.Annotations[constants.VrcAnnotation] {
		return
	}

	// If the annotation has changed, we grab every PVC inside the namespace to propagate the update
	klog.Infof("detected volumeReplicationClass update for namespace %s", newNs.Name)
	pvcs, err := PvcInformer.Lister().PersistentVolumeClaims(newNs.Name).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pvcs in namespace %s: %s", newNs.Namespace, err.Error())
		return
	}

	// Propagate the update to every PVC in the namespace
	for _, pvc := range pvcs {
		key, err := cache.MetaNamespaceKeyFunc(pvc)
		if err != nil {
			klog.Errorf("failed to get key for pvc %s/%s: %s", pvc.Namespace, pvc.Namespace, err.Error())
		}

		c.pvcQueue.Add(key)
	}
}

// pvcUpdate is called whenever a PVC is created or updated
func (c *Controller) pvcUpdate(pvc *corev1.PersistentVolumeClaim) {
	key, err := cache.MetaNamespaceKeyFunc(pvc)
	if err != nil {
		klog.Errorf("failed to get key for pvc %s/%s: %s", pvc.Namespace, pvc.Namespace, err.Error())
	}

	klog.Infof("detected PVC update for %s", key)
	c.pvcQueue.Add(key)
}

// volumeReplicationCreateOrDelete is called whenever a VolumeReplication is created or deleted
func (c *Controller) volumeReplicationCreateOrDelete(volumeReplication *unstructured.Unstructured) {
	key := fmt.Sprintf("%s/%s", volumeReplication.GetNamespace(), volumeReplication.GetName())
	klog.Infof("detected VolumeReplication creation or deletion for %s", key)
	c.pvcQueue.Add(key)
}

// volumeReplicationUpdate is called whenever a VolumeReplication is updated
func (c *Controller) volumeReplicationUpdate(oldVr, newVr *unstructured.Unstructured) {
	key := fmt.Sprintf("%s/%s", newVr.GetNamespace(), newVr.GetName())
	klog.Infof("detected VolumeReplication update for %s", key)

	// Don't handle VolumeReplications that aren't controlled by us
	if !isParentLabelPresent(newVr.GetLabels()) {
		klog.Infof("ignoring update to VolumeReplication %s as it isn't controlled by us", key)
		return
	}

	// Skip updates if nothing happened to the specs
	if reflect.DeepEqual(oldVr.Object["spec"], newVr.Object["spec"]) {
		return
	}

	c.pvcQueue.Add(key)
}
