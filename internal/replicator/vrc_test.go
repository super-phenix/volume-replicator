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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8s_testing "k8s.io/client-go/testing"
)

func setupTestEnvironment() (*fake.Clientset, *dynamicfake.FakeDynamicClient, informers.SharedInformerFactory) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(VolumeReplicationClassesResource.GroupVersion().WithKind("VolumeReplicationClassList"), &unstructured.UnstructuredList{})

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8s.DynamicClientSet = dynamicClient

	client := fake.NewClientset()
	k8s.ClientSet = client

	informerFactory := informers.NewSharedInformerFactory(client, 0)
	NamespaceInformer = informerFactory.Core().V1().Namespaces()

	return client, dynamicClient, informerFactory
}

func clearNamespaceIndexer(t *testing.T) {
	indexer := NamespaceInformer.Informer().GetIndexer()
	for _, obj := range indexer.List() {
		err := indexer.Delete(obj)
		require.NoError(t, err)
	}
}

func TestGetVolumeReplicationClass(t *testing.T) {
	client, dynamicClient, _ := setupTestEnvironment()

	nsName := "test-namespace"
	vrcName := "test-vrc"
	selectorValue := "test-selector"
	stcName := "test-storage-class"
	groupName := "test-group"

	provisionerName := "test-provisioner"

	// Create a StorageClass
	stc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: stcName,
			Labels: map[string]string{
				constants.StorageClassGroup: groupName,
			},
		},
	}
	_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

	// Create a VRC that matches the selector
	vrc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
			"kind":       "VolumeReplicationClass",
			"metadata": map[string]interface{}{
				"name": "vrc-matched",
				"labels": map[string]interface{}{
					constants.StorageClassGroup:     groupName,
					constants.VrcSelectorAnnotation: selectorValue,
				},
			},
			"spec": map[string]interface{}{
				"provisioner": provisionerName,
			},
		},
	}
	_, _ = dynamicClient.Resource(VolumeReplicationClassesResource).Create(context.Background(), vrc, metav1.CreateOptions{})

	tests := []struct {
		name           string
		pvc            *corev1.PersistentVolumeClaim
		namespace      *corev1.Namespace
		expectedResult string
	}{
		{
			name: "VRC value in PVC",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: vrcName,
					},
				},
			},
			expectedResult: vrcName,
		},
		{
			name: "VRC value in Namespace",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: vrcName,
					},
				},
			},
			expectedResult: vrcName,
		},
		{
			name: "VRC selector in PVC",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation:        selectorValue,
						constants.StorageProvisionerAnnotation: provisionerName,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &stcName,
				},
			},
			expectedResult: "vrc-matched",
		},
		{
			name: "VRC selector in Namespace",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.StorageProvisionerAnnotation: provisionerName,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &stcName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation: selectorValue,
					},
				},
			},
			expectedResult: "vrc-matched",
		},
		{
			name: "VRC value takes priority over selector",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation:    vrcName,
						constants.VrcSelectorAnnotation: selectorValue,
					},
				},
			},
			expectedResult: vrcName,
		},
		{
			name: "None provided",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearNamespaceIndexer(t)

			if tt.namespace != nil {
				err := NamespaceInformer.Informer().GetIndexer().Add(tt.namespace)
				require.NoError(t, err)
			}

			result := getVolumeReplicationClass(tt.pvc)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestGetVolumeReplicationClassValue(t *testing.T) {
	setupTestEnvironment()

	// Initializing the informer's cache with some data
	nsName := "test-namespace"
	vrcName := "test-vrc"

	tests := []struct {
		name           string
		pvc            *corev1.PersistentVolumeClaim
		namespace      *corev1.Namespace
		expectedResult string
	}{
		{
			name: "VRC in PVC annotations",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: vrcName,
					},
				},
			},
			expectedResult: vrcName,
		},
		{
			name: "VRC in Namespace annotations",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: vrcName,
					},
				},
			},
			expectedResult: vrcName,
		},
		{
			name: "VRC in both - PVC priority",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: "pvc-vrc",
					},
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcValueAnnotation: "ns-vrc",
					},
				},
			},
			expectedResult: "pvc-vrc",
		},
		{
			name: "VRC missing in both",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			},
			expectedResult: "",
		},
		{
			name: "Namespace retrieval failure (not found)",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "non-existent",
				},
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearNamespaceIndexer(t)

			if tt.namespace != nil {
				err := NamespaceInformer.Informer().GetIndexer().Add(tt.namespace)
				require.NoError(t, err)
			}

			result := getVolumeReplicationClassValue(tt.pvc)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestGetVolumeReplicationClassSelector(t *testing.T) {
	setupTestEnvironment()

	nsName := "test-namespace"
	selectorValue := "test-selector"

	tests := []struct {
		name           string
		pvc            *corev1.PersistentVolumeClaim
		namespace      *corev1.Namespace
		expectedResult string
	}{
		{
			name: "Selector in PVC annotations",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation: selectorValue,
					},
				},
			},
			expectedResult: selectorValue,
		},
		{
			name: "Selector in Namespace annotations",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation: selectorValue,
					},
				},
			},
			expectedResult: selectorValue,
		},
		{
			name: "Selector in both - PVC priority",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation: "pvc-selector",
					},
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcSelectorAnnotation: "ns-selector",
					},
				},
			},
			expectedResult: "pvc-selector",
		},
		{
			name: "Selector missing in both",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: nsName,
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			},
			expectedResult: "",
		},
		{
			name: "Namespace retrieval failure (not found)",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "non-existent",
				},
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearNamespaceIndexer(t)

			if tt.namespace != nil {
				err := NamespaceInformer.Informer().GetIndexer().Add(tt.namespace)
				require.NoError(t, err)
			}

			result := getVolumeReplicationClassSelector(tt.pvc)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestGetVolumeReplicationClassFromSelector(t *testing.T) {
	client, dynamicClient, _ := setupTestEnvironment()

	stcName := "test-storage-class"
	groupName := "test-group"
	selectorValue := "test-selector"
	provisionerName := "test-provisioner"

	vrc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
			"kind":       "VolumeReplicationClass",
			"metadata": map[string]interface{}{
				"name": "vrc-matched",
				"labels": map[string]interface{}{
					constants.StorageClassGroup:     groupName,
					constants.VrcSelectorAnnotation: selectorValue,
				},
			},
			"spec": map[string]interface{}{
				"provisioner": provisionerName,
			},
		},
	}
	_, _ = dynamicClient.Resource(VolumeReplicationClassesResource).Create(context.Background(), vrc, metav1.CreateOptions{})

	stc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: stcName,
			Labels: map[string]string{
				constants.StorageClassGroup: groupName,
			},
		},
	}
	_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})

	t.Run("PVC without VrcSelectorAnnotation", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("PVC with annotation, StorageClass has group, match found", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation:        selectorValue,
					constants.StorageProvisionerAnnotation: provisionerName,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "vrc-matched", result)
	})

	t.Run("PVC with annotation, matching provisioner not found", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation:        selectorValue,
					constants.StorageProvisionerAnnotation: "other-provisioner",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("StorageClass has no group", func(t *testing.T) {
		stcNoGroup := "stc-no-group"
		stc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: stcNoGroup,
			},
		}
		_, _ = client.StorageV1().StorageClasses().Create(context.Background(), stc, metav1.CreateOptions{})
		defer func() {
			_ = client.StorageV1().StorageClasses().Delete(context.Background(), stcNoGroup, metav1.DeleteOptions{})
		}()

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation: selectorValue,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcNoGroup,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("No matching VRC found", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation: "no-match",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("Multiple matching VRCs found", func(t *testing.T) {
		vrc2 := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
				"kind":       "VolumeReplicationClass",
				"metadata": map[string]interface{}{
					"name": "vrc-matched-2",
					"labels": map[string]interface{}{
						constants.StorageClassGroup:     groupName,
						constants.VrcSelectorAnnotation: selectorValue,
					},
				},
				"spec": map[string]interface{}{
					"provisioner": provisionerName,
				},
			},
		}
		_, _ = dynamicClient.Resource(VolumeReplicationClassesResource).Create(context.Background(), vrc2, metav1.CreateOptions{})
		defer func() {
			_ = dynamicClient.Resource(VolumeReplicationClassesResource).Delete(context.Background(), "vrc-matched-2", metav1.DeleteOptions{})
		}()

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation:        selectorValue,
					constants.StorageProvisionerAnnotation: provisionerName,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &stcName,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("StorageClass retrieval error", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation: selectorValue,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &[]string{"non-existent"}[0],
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})

	t.Run("PVC has no StorageClassName", func(t *testing.T) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.VrcSelectorAnnotation: selectorValue,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: nil,
			},
		}
		result := getVolumeReplicationClassFromSelector(pvc)
		require.Equal(t, "", result)
	})
}

func TestFilterVrcFromSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(VolumeReplicationClassesResource.GroupVersion().WithKind("VolumeReplicationClassList"), &unstructured.UnstructuredList{})

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8s.DynamicClientSet = dynamicClient

	vrc1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
			"kind":       "VolumeReplicationClass",
			"metadata": map[string]interface{}{
				"name": "vrc-1",
				"labels": map[string]interface{}{
					constants.StorageClassGroup:     "group-1",
					constants.VrcSelectorAnnotation: "match",
				},
			},
			"spec": map[string]interface{}{
				"provisioner": "provisioner-1",
			},
		},
	}

	vrc2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", VolumeReplicationResource.Group, VolumeReplicationResource.Version),
			"kind":       "VolumeReplicationClass",
			"metadata": map[string]interface{}{
				"name": "vrc-2",
				"labels": map[string]interface{}{
					constants.StorageClassGroup:     "group-2",
					constants.VrcSelectorAnnotation: "no-match",
				},
			},
			"spec": map[string]interface{}{
				"provisioner": "provisioner-2",
			},
		},
	}

	_, _ = dynamicClient.Resource(VolumeReplicationClassesResource).Create(context.Background(), vrc1, metav1.CreateOptions{})
	_, _ = dynamicClient.Resource(VolumeReplicationClassesResource).Create(context.Background(), vrc2, metav1.CreateOptions{})

	t.Run("Match found with both labels and provisioner", func(t *testing.T) {
		list, err := filterVrcFromSelector("group-1", "match", "provisioner-1")
		require.NoError(t, err)
		require.Equal(t, []string{"vrc-1"}, list)
	})

	t.Run("No match found - wrong provisioner", func(t *testing.T) {
		list, err := filterVrcFromSelector("group-1", "match", "wrong-provisioner")
		require.NoError(t, err)
		require.Empty(t, list)
	})

	t.Run("No match found - wrong selector", func(t *testing.T) {
		list, err := filterVrcFromSelector("group-1", "no-match", "provisioner-1")
		require.NoError(t, err)
		require.Empty(t, list)
	})

	t.Run("Match found - empty pvcProvisioner", func(t *testing.T) {
		list, err := filterVrcFromSelector("group-1", "match", "")
		require.NoError(t, err)
		require.Equal(t, []string{"vrc-1"}, list)
	})

	t.Run("API error", func(t *testing.T) {
		// Prepend a reactor to inject an error
		dynamicClient.PrependReactor("list", "volumereplicationclasses", func(action k8s_testing.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf("injected list error")
		})
		defer func() { dynamicClient.ReactionChain = dynamicClient.ReactionChain[1:] }()

		list, err := filterVrcFromSelector("group-1", "match", "provisioner-1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "injected list error")
		require.Nil(t, list)
	})
}
