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

// buildRestConfig constructs a Kubernetes REST config with proper error context and logging.
// The function prioritizes in-cluster config (when running as a pod) but gracefully falls back
// to kubeconfig files. This dual-mode approach is essential for controllers that need to work
// both in development (out-of-cluster) and production (in-cluster) environments.
func buildRestConfig(kubeconfig string, qps float32, burst int) (*kubernetes.Clientset, *rest.Config, error) {
	var config *rest.Config
	var err error

	// Try in-cluster configuration first - this is the preferred method in production
	// as it automatically handles service account tokens and cluster CA certificates
	if config, err = rest.InClusterConfig(); err == nil {
		// Configure rate limiting to prevent overwhelming the API server
		// QPS controls sustained request rate, Burst allows short spikes
		config.QPS, config.Burst = qps, burst

		// Set reasonable timeouts to prevent hanging connections in distributed environments
		config.Timeout = 30 * time.Second
	} else {
		// Fall back to kubeconfig - essential for development and debugging
		if kubeconfig == "" {
			kubeconfig = defaultKubeconfig()
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build rest config from kubeconfig %q: %w", kubeconfig, err)
		}

		// Apply the same rate limiting and timeout settings
		config.QPS, config.Burst = qps, burst
		config.Timeout = 30 * time.Second
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, config, nil
}

// defaultKubeconfig returns the default kubeconfig path using standard Kubernetes conventions.
// This follows the kubectl precedence: KUBECONFIG env var, then ~/.kube/config.
// The function gracefully handles missing home directories and missing config files (common in containerized environments).
func defaultKubeconfig() string {
	// Respect KUBECONFIG environment variable (supports multiple files separated by colons)
	if p := os.Getenv("KUBECONFIG"); p != "" {
		return p
	}

	// Fall back to standard location
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// In some container environments, UserHomeDir() might fail
		// Return empty string which will cause buildRestConfig to try in-cluster first
		return ""
	}

	kubeconfigPath := filepath.Join(home, ".kube", "config")

	// Check if the kubeconfig file actually exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		// If the file doesn't exist, return empty string to trigger in-cluster config fallback
		return ""
	}

	return kubeconfigPath
}
