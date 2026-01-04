package replicator

import (
	"context"
	"fmt"
	"testing"

	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8s_testing "k8s.io/client-go/testing"
)

func TestCreateVolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8s.DynamicClientSet = dynamicClient

	client := fake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	NamespaceInformer = informerFactory.Core().V1().Namespaces()

	nsName := "test-namespace"
	pvcName := "test-pvc"
	vrcName := "test-vrc"

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: nsName,
			Annotations: map[string]string{
				constants.VrcValueAnnotation: vrcName,
				"other-annotation":           "value",
			},
			Labels: map[string]string{
				"other-label": "value",
			},
		},
	}

	t.Run("Successful creation", func(t *testing.T) {
		err := createVolumeReplication(pvc)
		require.NoError(t, err)

		// Verify creation
		vr, err := dynamicClient.Resource(VolumeReplicationResource).Namespace(nsName).Get(context.Background(), pvcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.NotNil(t, vr)

		// Check metadata
		require.Equal(t, pvcName, vr.GetName())
		require.Equal(t, nsName, vr.GetNamespace())
		require.Equal(t, vrcName, vr.GetAnnotations()[constants.VrcValueAnnotation])
		require.Equal(t, "value", vr.GetAnnotations()["other-annotation"])
		require.Equal(t, "value", vr.GetLabels()["other-label"])
		require.Equal(t, pvcName, vr.GetLabels()[constants.VrParentLabel])

		// Check spec
		spec, ok := vr.Object["spec"].(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, vrcName, spec["volumeReplicationClass"])
		require.Equal(t, "primary", spec["replicationState"])

		dataSource, ok := spec["dataSource"].(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, "v1", dataSource["apiGroup"])
		require.Equal(t, "PersistentVolumeClaim", dataSource["kind"])
		require.Equal(t, pvcName, dataSource["name"])
	})

	t.Run("Creation failure", func(t *testing.T) {
		// Set up a reactor to inject an error
		dynamicClient.PrependReactor("create", "volumereplications", func(action k8s_testing.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf("injected error")
		})
		defer func() { dynamicClient.ReactionChain = dynamicClient.ReactionChain[1:] }()

		err := createVolumeReplication(pvc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "injected error")
	})
}

func TestGetPersistentVolumeClaim(t *testing.T) {
	client := fake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	PvcInformer = informerFactory.Core().V1().PersistentVolumeClaims()

	nsName := "test-namespace"
	pvcName := "test-pvc"
	key := fmt.Sprintf("%s/%s", nsName, pvcName)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: nsName,
		},
	}

	t.Run("PVC exists", func(t *testing.T) {
		indexer := PvcInformer.Informer().GetIndexer()
		err := indexer.Add(pvc)
		require.NoError(t, err)
		defer indexer.Delete(pvc)

		result, err := getPersistentVolumeClaim(key)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, pvcName, result.Name)
		require.Equal(t, nsName, result.Namespace)
	})

	t.Run("PVC does not exist", func(t *testing.T) {
		result, err := getPersistentVolumeClaim("non-existent/pvc")
		require.NoError(t, err)
		require.Nil(t, result)
	})
}

func TestIsParentLabelPresent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		result bool
	}{
		{
			name: "not present",
			labels: map[string]string{
				"a": "b",
			},
			result: false,
		},
		{
			name: "present",
			labels: map[string]string{
				"a":                     "b",
				"c":                     "d",
				constants.VrParentLabel: "test",
			},
			result: true,
		},
		{
			name:   "nil labels",
			labels: nil,
			result: false,
		},
		{
			name: "empty value",
			labels: map[string]string{
				constants.VrParentLabel: "",
			},
			result: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isParentLabelPresent(tt.labels)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestGetStorageClassLabels(t *testing.T) {
	client := fake.NewClientset()
	k8s.ClientSet = client

	stcName := "test-storage-class"
	labels := map[string]string{"foo": "bar"}

	t.Run("PVC has no StorageClassName", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: nil,
			},
		}
		result, err := getStorageClassLabels(pvc)
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("StorageClass exists and has labels", func(t *testing.T) {
		stc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:   stcName,
				Labels: labels,
			},
		}
		_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassLabels(pvc)
		require.NoError(t, err)
		require.Equal(t, labels, result)

		_ = client.StorageV1().StorageClasses().Delete(context.Background(), stcName, metav1.DeleteOptions{})
	})

	t.Run("StorageClass exists and has no labels", func(t *testing.T) {
		stc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: stcName,
			},
		}
		_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassLabels(pvc)
		require.NoError(t, err)
		require.Nil(t, result)

		_ = client.StorageV1().StorageClasses().Delete(context.Background(), stcName, metav1.DeleteOptions{})
	})

	t.Run("StorageClass does not exist", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassLabels(pvc)
		require.Error(t, err)
		require.True(t, errors.IsNotFound(err))
		require.Nil(t, result)
	})
}

func TestGetStorageClassGroup(t *testing.T) {
	client := fake.NewClientset()
	k8s.ClientSet = client

	stcName := "test-storage-class"
	groupName := "test-group"

	t.Run("PVC has no StorageClassName", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: nil,
			},
		}
		result, err := getStorageClassGroup(pvc)
		require.NoError(t, err)
		require.Equal(t, "", result)
	})

	t.Run("StorageClass has no group label", func(t *testing.T) {
		stc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: stcName,
			},
		}
		_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassGroup(pvc)
		require.NoError(t, err)
		require.Equal(t, "", result)

		_ = client.StorageV1().StorageClasses().Delete(context.Background(), stcName, metav1.DeleteOptions{})
	})

	t.Run("StorageClass has group label", func(t *testing.T) {
		stc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: stcName,
				Labels: map[string]string{
					constants.VrStorageClassGroup: groupName,
				},
			},
		}
		_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassGroup(pvc)
		require.NoError(t, err)
		require.Equal(t, groupName, result)

		_ = client.StorageV1().StorageClasses().Delete(context.Background(), stcName, metav1.DeleteOptions{})
	})

	t.Run("StorageClass does not exist", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result, err := getStorageClassGroup(pvc)
		require.Error(t, err)
		require.True(t, errors.IsNotFound(err))
		require.Equal(t, "", result)
	})
}

func TestGetLabelsWithParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent string
		labels map[string]string
		result map[string]string
	}{
		{
			name:   "empty labels",
			parent: "test",
			labels: map[string]string{},
			result: map[string]string{
				constants.VrParentLabel: "test",
			},
		},
		{
			name:   "some labels",
			parent: "test",
			labels: map[string]string{
				"a": "b",
				"c": "d",
			},
			result: map[string]string{
				constants.VrParentLabel: "test",
				"a":                     "b",
				"c":                     "d",
			},
		},
		{
			name:   "nil labels",
			parent: "test",
			labels: nil,
			result: map[string]string{
				constants.VrParentLabel: "test",
			},
		},
		{
			name:   "label already present",
			parent: "new-test",
			labels: map[string]string{
				constants.VrParentLabel: "old-test",
				"a":                     "b",
			},
			result: map[string]string{
				constants.VrParentLabel: "new-test",
				"a":                     "b",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLabels := make(map[string]string)
			if tt.labels != nil {
				for k, v := range tt.labels {
					originalLabels[k] = v
				}
			} else {
				originalLabels = nil
			}

			result := getLabelsWithParent(tt.labels, tt.parent)
			require.Equal(t, tt.result, result)

			// Verify that the original map was not modified
			require.Equal(t, originalLabels, tt.labels)
		})
	}
}

func TestCleanupVolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8s.DynamicClientSet = dynamicClient

	nsName := "test-namespace"
	vrName := "test-vr"

	t.Run("Successful deletion", func(t *testing.T) {
		// Create a VR first so it can be deleted
		vr := &unstructured.Unstructured{}
		vr.SetGroupVersionKind(VolumeReplicationResource.GroupVersion().WithKind("VolumeReplication"))
		vr.SetName(vrName)
		vr.SetNamespace(nsName)

		_, err := dynamicClient.Resource(VolumeReplicationResource).Namespace(nsName).Create(context.Background(), vr, metav1.CreateOptions{})
		require.NoError(t, err)

		cleanupVolumeReplication(vrName, nsName)

		// Verify deletion
		_, err = dynamicClient.Resource(VolumeReplicationResource).Namespace(nsName).Get(context.Background(), vrName, metav1.GetOptions{})
		require.Error(t, err)
		require.True(t, errors.IsNotFound(err))
	})

	t.Run("Deletion when resource is not found", func(t *testing.T) {
		// Should not panic or return error (it logs it, but we can't easily check logs here without more setup)
		cleanupVolumeReplication("non-existent", nsName)
	})

	t.Run("Deletion failure", func(t *testing.T) {
		// Set up a reactor to inject an error
		dynamicClient.PrependReactor("delete", "volumereplications", func(action k8s_testing.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf("injected delete error")
		})
		defer func() { dynamicClient.ReactionChain = dynamicClient.ReactionChain[1:] }()

		// Should handle error gracefully (logs it)
		cleanupVolumeReplication(vrName, nsName)
	})
}

func TestGetVolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	VolumeReplicationInformer = dynamicInformerFactory.ForResource(VolumeReplicationResource)

	ns := "test-ns"
	name := "test-vr"
	key := ns + "/" + name

	vr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "replication.storage.openshift.io/v1alpha1",
			"kind":       "VolumeReplication",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
		},
	}

	t.Run("VR exists", func(t *testing.T) {
		err := VolumeReplicationInformer.Informer().GetIndexer().Add(vr)
		require.NoError(t, err)

		result, err := getVolumeReplication(key)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, name, result.GetName())
		require.Equal(t, ns, result.GetNamespace())
	})

	t.Run("VR does not exist", func(t *testing.T) {
		result, err := getVolumeReplication("non-existent/vr")
		require.Error(t, err)
		require.Nil(t, result)
		require.True(t, errors.IsNotFound(err))
	})
}

func TestIsVolumeReplicationCorrect(t *testing.T) {
	client := fake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	NamespaceInformer = informerFactory.Core().V1().Namespaces()

	nsName := "test-namespace"
	pvcName := "test-pvc"
	vrcName := "test-vrc"

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: nsName,
			Annotations: map[string]string{
				constants.VrcValueAnnotation: vrcName,
			},
		},
	}

	tests := []struct {
		name     string
		vr       *unstructured.Unstructured
		expected bool
	}{
		{
			name: "All fields match",
			vr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      pvcName,
						"namespace": nsName,
					},
					"spec": map[string]interface{}{
						"volumeReplicationClass": vrcName,
						"dataSource": map[string]interface{}{
							"apiGroup": "v1",
							"kind":     "PersistentVolumeClaim",
							"name":     pvcName,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "volumeReplicationClass mismatch",
			vr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      pvcName,
						"namespace": nsName,
					},
					"spec": map[string]interface{}{
						"volumeReplicationClass": "wrong-vrc",
						"dataSource": map[string]interface{}{
							"apiGroup": "v1",
							"kind":     "PersistentVolumeClaim",
							"name":     pvcName,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "dataSource apiGroup mismatch",
			vr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      pvcName,
						"namespace": nsName,
					},
					"spec": map[string]interface{}{
						"volumeReplicationClass": vrcName,
						"dataSource": map[string]interface{}{
							"apiGroup": "wrong-group",
							"kind":     "PersistentVolumeClaim",
							"name":     pvcName,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "dataSource kind mismatch",
			vr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      pvcName,
						"namespace": nsName,
					},
					"spec": map[string]interface{}{
						"volumeReplicationClass": vrcName,
						"dataSource": map[string]interface{}{
							"apiGroup": "v1",
							"kind":     "WrongKind",
							"name":     pvcName,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "dataSource name mismatch",
			vr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      pvcName,
						"namespace": nsName,
					},
					"spec": map[string]interface{}{
						"volumeReplicationClass": vrcName,
						"dataSource": map[string]interface{}{
							"apiGroup": "v1",
							"kind":     "PersistentVolumeClaim",
							"name":     "wrong-pvc-name",
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVolumeReplicationCorrect(pvc, tt.vr)
			require.Equal(t, tt.expected, result)
		})
	}
}
