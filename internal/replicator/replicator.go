package replicator

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type Controller struct {
	pvcQueue workqueue.TypedRateLimitingInterface[string]
}

func NewController() *Controller {
	return &Controller{
		pvcQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
}

// Run begins watching and syncing.
func (c *Controller) Run(ctx context.Context, workers int) {
	defer runtime.HandleCrashWithContext(ctx)

	// Let the workers stop when we are done
	defer c.pvcQueue.ShutDown()
	klog.Info("Starting replication controller")

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	klog.Info("Stopping replication controller")
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := c.pvcQueue.Get()
	if quit {
		return false
	}

	reconcileVolumeReplication(key)

	defer c.pvcQueue.Done(key)
	return true
}

// Reconcile:
// - if the PVC doesn't exist anymore, delete the corresponding VolumeReplication (if it exists)
//
// - if the VolumeReplication exists
//   - check if the PVC has a matching VolumeReplicationClass
//   - and if it doesn't, delete the VolumeReplication
//   - check if the definition of the VolumeReplication is correct
//   - and if it doesn't, delete it, and it will be re-created on the next sync
//
// - if the VolumeReplication doesn't exist
//   - and if a corresponding VolumeReplicationClass exists, create the VolumeReplication
func reconcileVolumeReplication(key string) {
	klog.Infof("reconciling VolumeReplication for PVC %s", key)
	namespace, name, _ := cache.SplitMetaNamespaceKey(key)

	// Retrieve the PVC that we might need to replicate (or that shouldn't be replicated anymore)
	pvc, err := getPersistentVolumeClaim(key)
	if err != nil {
		klog.Error(err)
		return
	}

	// Retrieve the VolumeReplication that corresponds to the PVC (it has the same name)
	volumeReplication, err := getVolumeReplication(key)
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("couldn't get VolumeReplication for pvc %s: %s", key, err.Error())
		return
	}

	// If the VR exists, and it isn't owned by our controller, do not proceed further
	if volumeReplication != nil && !isParentLabelPresent(volumeReplication.GetLabels()) {
		klog.Infof("VolumeReplication %s isn't owned by us, skipping", key)
		return
	}

	// The PVC got deleted, delete the VolumeReplication associated with it
	if pvc == nil || pvc.DeletionTimestamp != nil {
		klog.Infof("deleting VolumeReplication %s as its PVC doesn't exist anymore", key)
		cleanupVolumeReplication(name, namespace)
		return
	}

	// Retrieve the VRC that should apply to this PVC
	replicationClass := getVolumeReplicationClass(pvc)
	if replicationClass != "" {
		klog.Infof("found VolumeReplicationClass %s for PVC %s", replicationClass, key)
	}

	// The VolumeReplication exists, we need to check:
	//  - if the PVC still has a matching VolumeReplicationClass
	//    - and if it doesn't, we need to delete the VolumeReplication
	//  - if the definition of the VolumeReplication is correct
	//    - and if it isn't, we need to delete the VolumeReplication
	if volumeReplication != nil {
		vrcExists := replicationClass != ""
		vrCorrect := isVolumeReplicationCorrect(pvc, volumeReplication)

		if !vrcExists || !vrCorrect {
			klog.Infof("deleting VolumeReplication %s as it doesn't conform anymore, vrcExists(%t), vrCorrect(%t)", key, vrcExists, vrCorrect)
			cleanupVolumeReplication(name, namespace)
			return
		}
	}

	// No volume replication object was found for this PVC, we need to create it
	if volumeReplication == nil && replicationClass != "" {
		klog.Infof("creating VolumeReplication for PVC %s", key)
		if err = createVolumeReplication(pvc); err != nil {
			klog.Errorf("failed to create VolumeReplication for PVC %s: %s", key, err.Error())
		}
	}
}
