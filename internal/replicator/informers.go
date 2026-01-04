package replicator

import (
	"context"
	"time"

	"github.com/skalanetworks/volume-replicator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	resync                   = 30 * time.Minute
	volumeReplicationGroup   = "replication.storage.openshift.io"
	volumeReplicationVersion = "v1alpha1"
)

var (
	NamespaceInformer         v1.NamespaceInformer
	PvcInformer               v1.PersistentVolumeClaimInformer
	VolumeReplicationInformer informers.GenericInformer

	VolumeReplicationResource = schema.GroupVersionResource{
		Group:    volumeReplicationGroup,
		Version:  volumeReplicationVersion,
		Resource: "volumereplications",
	}

	VolumeReplicationClassesResource = schema.GroupVersionResource{
		Group:    volumeReplicationGroup,
		Version:  volumeReplicationVersion,
		Resource: "volumereplicationclasses",
	}
)

func (c *Controller) LoadInformers(ctx context.Context) {
	informerFactory := informers.NewSharedInformerFactory(k8s.ClientSet, resync)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(k8s.DynamicClientSet, resync)

	c.createNamespaceInformer(informerFactory)
	c.createPvcInformer(informerFactory)
	c.createVolumeReplicationInformer(dynamicInformerFactory)

	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	dynamicInformerFactory.Start(ctx.Done())
	dynamicInformerFactory.WaitForCacheSync(ctx.Done())
}

func (c *Controller) createNamespaceInformer(factory informers.SharedInformerFactory) {
	NamespaceInformer = factory.Core().V1().Namespaces()
	NamespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			c.namespaceUpdate(oldObj.(*corev1.Namespace), newObj.(*corev1.Namespace))
		},
	})
}

func (c *Controller) createPvcInformer(factory informers.SharedInformerFactory) {
	PvcInformer = factory.Core().V1().PersistentVolumeClaims()
	PvcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			c.pvcUpdate(obj.(*corev1.PersistentVolumeClaim))
		},
		UpdateFunc: func(_, newObj any) {
			c.pvcUpdate(newObj.(*corev1.PersistentVolumeClaim))
		},
		DeleteFunc: func(obj interface{}) {
			c.pvcUpdate(obj.(*corev1.PersistentVolumeClaim))
		},
	})
}

func (c *Controller) createVolumeReplicationInformer(factory dynamicinformer.DynamicSharedInformerFactory) {
	VolumeReplicationInformer = factory.ForResource(VolumeReplicationResource)
	VolumeReplicationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			c.volumeReplicationCreateOrDelete(obj.(*unstructured.Unstructured))
		},
		UpdateFunc: func(oldObj, newObj any) {
			c.volumeReplicationUpdate(oldObj.(*unstructured.Unstructured), newObj.(*unstructured.Unstructured))
		},
		DeleteFunc: func(obj any) {
			c.volumeReplicationCreateOrDelete(obj.(*unstructured.Unstructured))
		},
	})
}
