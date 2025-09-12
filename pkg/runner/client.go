package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientTimeoutDefault = 30 * time.Second
)

// buildRestConfig constructs a Kubernetes REST config with proper error context and logging.
// Prioritizes in-cluster config with gracefully falls back to kubeconfig files.
func buildRestConfig(kubeconfig string, qps float32, burst int) (*kubernetes.Clientset, *rest.Config, error) {
	var config *rest.Config
	var err error

	// Try in-cluster configuration first. Handle service account tokens and cluster CA certs.
	if config, err = rest.InClusterConfig(); err != nil {
		// Fall back to kubeconfig for development and debugging
		if kubeconfig == "" {
			kubeconfig = defaultKubeconfig()
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build rest config from kubeconfig %q: %w", kubeconfig, err)
		}
	}

	// Configure rate limiting to prevent overwhelming the API server
	config.QPS = qps     // controls sustained request rate
	config.Burst = burst // allows short spikes
	config.Timeout = clientTimeoutDefault

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, config, nil
}

// defaultKubeconfig returns the default kubeconfig path using standard Kubernetes conventions.
// This follows the kubectl precedence: KUBECONFIG env var, then ~/.kube/config.
func defaultKubeconfig() string {
	// Respect KUBECONFIG environment variable (supports multiple files separated by colons)
	if p := os.Getenv("KUBECONFIG"); p != "" {
		return p
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// In some container environments, UserHomeDir() might fail
		// Return empty string which will cause buildRestConfig to try in-cluster first
		return ""
	}

	kubeconfigPath := filepath.Join(home, ".kube", "config")

	// Check if the kubeconfig file actually exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return ""
	}

	return kubeconfigPath
}
