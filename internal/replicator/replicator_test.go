package replicator

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/super-phenix/volume-replicator/internal/constants"
	"github.com/super-phenix/volume-replicator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8s_testing "k8s.io/client-go/testing"
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

	// Add namespace to informer
	_ = NamespaceInformer.Informer().GetIndexer().Add(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName},
	})

	vr := &unstructured.Unstructured{}
	vr.SetUnstructuredContent(map[string]any{
		"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
		"kind":       "VolumeReplication",
		"metadata": map[string]any{
			"name":      pvcName,
			"namespace": nsName,
			"labels": map[string]any{
				constants.ParentLabel: pvcName,
			},
		},
		"spec": map[string]any{
			"volumeReplicationClass": vrcName,
			"replicationState":       "primary",
			"dataSource": map[string]any{
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
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
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
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
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
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
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
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
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
				created := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "create" && action.GetResource().Resource == "volumereplications"
				})
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
		{
			name: "PVC paused -> do not create VR",
			setup: func() {
				pausedPvc := pvc.DeepCopy()
				pausedPvc.Annotations[constants.PauseAnnotation] = "true"
				err := PvcInformer.Informer().GetIndexer().Add(pausedPvc)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				for _, action := range actions {
					require.NotEqual(t, "create", action.GetVerb())
					require.NotEqual(t, "delete", action.GetVerb())
				}
			},
		},
		{
			name: "PVC paused, PVC being deleted -> delete VR anyway",
			setup: func() {
				pausedPvc := pvc.DeepCopy()
				pausedPvc.Annotations[constants.PauseAnnotation] = "true"
				now := metav1.Now()
				pausedPvc.DeletionTimestamp = &now
				err := PvcInformer.Informer().GetIndexer().Add(pausedPvc)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
				require.True(t, deleted, "VR should have been deleted despite PVC pause")
			},
		},
		{
			name: "Namespace paused, PVC missing -> delete VR",
			setup: func() {
				err := NamespaceInformer.Informer().GetIndexer().Add(&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nsName,
						Annotations: map[string]string{constants.PauseAnnotation: "true"},
					},
				})
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
				require.True(t, deleted, "VR should have been deleted despite namespace pause")
			},
		},
		{
			name: "Namespace paused, PVC being deleted (no PVC-level pause) -> delete VR",
			setup: func() {
				err := NamespaceInformer.Informer().GetIndexer().Add(&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nsName,
						Annotations: map[string]string{constants.PauseAnnotation: "true"},
					},
				})
				require.NoError(t, err)
				pvcBeingDeleted := pvc.DeepCopy()
				now := metav1.Now()
				pvcBeingDeleted.DeletionTimestamp = &now
				err = PvcInformer.Informer().GetIndexer().Add(pvcBeingDeleted)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete" && action.GetResource().Resource == "volumereplications"
				})
				require.True(t, deleted, "VR should have been deleted despite namespace pause")
			},
		},
		{
			name: "Namespace paused, PVC present without VR -> do not create VR",
			setup: func() {
				err := NamespaceInformer.Informer().GetIndexer().Add(&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nsName,
						Annotations: map[string]string{constants.PauseAnnotation: "true"},
					},
				})
				require.NoError(t, err)
				err = PvcInformer.Informer().GetIndexer().Add(pvc)
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
			name: "PVC pause=false overrides namespace pause=true -> create VR",
			setup: func() {
				err := NamespaceInformer.Informer().GetIndexer().Add(&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nsName,
						Annotations: map[string]string{constants.PauseAnnotation: "true"},
					},
				})
				require.NoError(t, err)
				unpausedPvc := pvc.DeepCopy()
				unpausedPvc.Annotations[constants.PauseAnnotation] = "false"
				err = PvcInformer.Informer().GetIndexer().Add(unpausedPvc)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				created := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "create" && action.GetResource().Resource == "volumereplications"
				})
				require.True(t, created, "VR should have been created")
			},
		},
		{
			name: "VR exists, replicationState mismatch -> update VR",
			setup: func() {
				pvcSecondary := pvc.DeepCopy()
				pvcSecondary.Annotations[constants.ReplicationStateAnnotation] = "secondary"
				err := PvcInformer.Informer().GetIndexer().Add(pvcSecondary)
				require.NoError(t, err)
				err = VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
				require.NoError(t, err)
			},
			verify: func(t *testing.T) {
				actions := dynamicClient.Actions()
				updated := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					if action.GetVerb() != "update" {
						return false
					}
					updateAction := action.(k8s_testing.UpdateAction)
					obj := updateAction.GetObject().(*unstructured.Unstructured)
					state, _, _ := unstructured.NestedString(obj.Object, "spec", "replicationState")
					return state == "secondary"
				})
				require.True(t, updated, "VR should have been updated with new replication state")

				deleted := slices.ContainsFunc(actions, func(action k8s_testing.Action) bool {
					return action.GetVerb() == "delete"
				})
				require.False(t, deleted, "VR should not have been deleted")
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
			for _, obj := range NamespaceInformer.Informer().GetIndexer().List() {
				_ = NamespaceInformer.Informer().GetIndexer().Delete(obj)
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
