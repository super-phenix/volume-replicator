package replicator

import (
	"context"
	"fmt"
	"testing"

	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/skalanetworks/volume-replicator/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
				constants.VrcAnnotation: vrcName,
				"other-annotation":      "value",
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
		require.Equal(t, vrcName, vr.GetAnnotations()[constants.VrcAnnotation])
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

func TestGetVolumeReplicationClass(t *testing.T) {
	client := fake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	NamespaceInformer = informerFactory.Core().V1().Namespaces()

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
						constants.VrcAnnotation: vrcName,
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
						constants.VrcAnnotation: vrcName,
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
						constants.VrcAnnotation: "pvc-vrc",
					},
				},
			},
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					Annotations: map[string]string{
						constants.VrcAnnotation: "ns-vrc",
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
			// Clear cache and add namespace if provided
			// We use the indexer directly to avoid issues with starting the informer factory
			indexer := NamespaceInformer.Informer().GetIndexer()
			for _, obj := range indexer.List() {
				err := indexer.Delete(obj)
				require.NoError(t, err)
			}

			if tt.namespace != nil {
				err := indexer.Add(tt.namespace)
				require.NoError(t, err)
			}

			result := getVolumeReplicationClass(tt.pvc)
			require.Equal(t, tt.expectedResult, result)
		})
	}
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
				constants.VrcAnnotation: vrcName,
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
