package replicator

import (
	"fmt"
	"testing"

	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReconcileVolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8s.DynamicClientSet = dynamicClient

	client := fake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	PvcInformer = informerFactory.Core().V1().PersistentVolumeClaims()
	NamespaceInformer = informerFactory.Core().V1().Namespaces()

	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	VolumeReplicationInformer = dynamicInformerFactory.ForResource(VolumeReplicationResource)

	nsName := "test-namespace"
	pvcName := "test-pvc"
	vrcName := "test-vrc"
	key := fmt.Sprintf("%s/%s", nsName, pvcName)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: nsName,
			Annotations: map[string]string{
				constants.VrcValueAnnotation: vrcName,
			},
		},
	}

	vr := &unstructured.Unstructured{}
	vr.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
		"kind":       "VolumeReplication",
		"metadata": map[string]interface{}{
			"name":      pvcName,
			"namespace": nsName,
			"labels": map[string]interface{}{
				constants.VrParentLabel: pvcName,
			},
		},
		"spec": map[string]interface{}{
			"volumeReplicationClass": vrcName,
			"replicationState":       "primary",
			"dataSource": map[string]interface{}{
				"apiGroup": "v1",
				"kind":     "PersistentVolumeClaim",
				"name":     pvcName,
			},
		},
	})

	tests := []struct {
		name   string
		setup  func()
		verify func(t *testing.T)
	}{
		{
			name: "PVC missing -> delete VR",
			setup: func() {
				err := VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := false
				for _, action := range actions {
					if action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications" {
						deleted = true
						break
					}
				}
				require.True(t, deleted, "VR should have been deleted")
			},
		},
		{
			name: "PVC being deleted -> delete VR",
			setup: func() {
				pvcWithDeletion := pvc.DeepCopy()
				now := metav1.Now()
				pvcWithDeletion.DeletionTimestamp = &now
				err := PvcInformer.Informer().GetIndexer().Add(pvcWithDeletion)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := false
				for _, action := range actions {
					if action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications" {
						deleted = true
						break
					}
				}
				require.True(t, deleted, "VR should have been deleted")
			},
		},
		{
			name: "VR not owned by us -> do nothing",
			setup: func() {
				err := PvcInformer.Informer().GetIndexer().Add(pvc)
				require.NoError(t, err)
				vrNotOwned := vr.DeepCopy()
				vrNotOwned.SetLabels(nil)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vrNotOwned)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				for _, action := range actions {
					require.NotEqual(t, "delete", action.GetVerb())
					require.NotEqual(t, "create", action.GetVerb())
				}
			},
		},
		{
			name: "VR exists, VRC missing -> delete VR",
			setup: func() {
				pvcNoVrc := pvc.DeepCopy()
				pvcNoVrc.Annotations = nil
				err := PvcInformer.Informer().GetIndexer().Add(pvcNoVrc)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := false
				for _, action := range actions {
					if action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications" {
						deleted = true
						break
					}
				}
				require.True(t, deleted, "VR should have been deleted")
			},
		},
		{
			name: "VR exists, VR incorrect -> delete VR",
			setup: func() {
				err := PvcInformer.Informer().GetIndexer().Add(pvc)
				require.NoError(t, err)
				vrIncorrect := vr.DeepCopy()
				_ = unstructured.SetNestedField(vrIncorrect.Object, "wrong-vrc", "spec", "volumeReplicationClass")
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vrIncorrect)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := false
				for _, action := range actions {
					if action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications" {
						deleted = true
						break
					}
				}
				require.True(t, deleted, "VR should have been deleted")
			},
		},
		{
			name: "VR missing, VRC present -> create VR",
			setup: func() {
				err := PvcInformer.Informer().GetIndexer().Add(pvc)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				created := false
				for _, action := range actions {
					if action.GetVerb() == "create" && action.GetResource().Resource == "volumereplications" {
						created = true
						break
					}
				}
				require.True(t, created, "VR should have been created")
			},
		},
		{
			name: "VR missing, VRC missing -> do nothing",
			setup: func() {
				pvcNoVrc := pvc.DeepCopy()
				pvcNoVrc.Annotations = nil
				err := PvcInformer.Informer().GetIndexer().Add(pvcNoVrc)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				for _, action := range actions {
					require.NotEqual(t, "create", action.GetVerb())
				}
			},
		},
		{
			name: "VR exists and correct -> do nothing",
			setup: func() {
				err := PvcInformer.Informer().GetIndexer().Add(pvc)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				for _, action := range actions {
					require.NotEqual(t, "delete", action.GetVerb())
					require.NotEqual(t, "create", action.GetVerb())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear indexers
			for _, obj := range PvcInformer.Informer().GetIndexer().List() {
				_ = PvcInformer.Informer().GetIndexer().Delete(obj)
			}
			for _, obj := range VolumeReplicationInformer.Informer().GetIndexer().List() {
				_ = VolumeReplicationInformer.Informer().GetIndexer().Delete(obj)
			}
			dynamicClient.ClearActions()

			if tt.setup != nil {
				tt.setup()
			}

			reconcileVolumeReplication(key)

			if tt.verify != nil {
				tt.verify(t)
			}
		})
	}
}
