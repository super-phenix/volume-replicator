package k8s

import (
	"fmt"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	ClientSet        *kubernetes.Clientset
	DynamicClientSet *dynamic.DynamicClient
)

// Load loads the Kubernetes configuration and creates all the informers and clients
func Load(path string) error {
	if err := loadConfiguration(path); err != nil {
		return fmt.Errorf("failed to load kubernetes configuration: %w", err)
	}

	return nil
}

// loadConfiguration loads a Kubernetes connection configuration
// Path indicates the path to a kubeconfig, if none is provided we fall back to extracting
// the configuration with the assumption we are running inside a pod in the cluster.
func loadConfiguration(path string) error {
	if path != "" {
		return loadOutOfCluster(path)
	}

	return loadInCluster()
}

// loadOutOfCluster retrieves the cluster configuration from a kubeconfig file
func loadOutOfCluster(kubeconfigPath string) error {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to retrieve in-cluster configuration: %w", err)
	}

	return craftClients(config)
}

// loadInCluster retrieves the cluster configuration in the pod
func loadInCluster() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to retrieve in-cluster configuration: %w", err)
	}

	return craftClients(config)
}

// craftClients converts a Kubernetes REST configuration into usable clients
func craftClients(config *rest.Config) (err error) {
	ClientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create ClientSet: %w", err)
	}

	DynamicClientSet, err = dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create DynamicClientSet: %w", err)
	}

	return nil
}
