package node

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Info holds parsed information about a Kubernetes node's cloud provider and instance identifier.
type Info struct {
	Provider   string
	Identifier string
	Raw        string
}

// GetNodeProviderID retrieves and parses the provider ID of a given Kubernetes node.
// It returns a Info struct containing the cloud provider, instance identifier, and raw provider ID.
// Supports AWS, GCP, Azure and BareMetal. Returns an error if node cannot be fetched or if provider is unrecognized.
func GetNodeProviderID(ctx context.Context, log *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, node string) (*Info, error) {
	n, err := Get(ctx, log, cs, cfg, node)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	if n == nil {
		return nil, fmt.Errorf("node is nil")
	}

	providerID := n.Spec.ProviderID

	if providerID == "" {
		return nil, fmt.Errorf("node providerID is empty")
	}

	log.Debug("processing node", "providerID", providerID)

	return parseNodeInfo(log, providerID)
}

func parseNodeInfo(log *slog.Logger, providerID string) (*Info, error) {
	if providerID == "" {
		return nil, fmt.Errorf("node providerID is empty")
	}

	log.Debug("processing node", "providerID", providerID)

	parts := strings.Split(providerID, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid providerID format: %s", providerID)
	}

	info := &Info{
		Provider: strings.ToLower(parts[0]),
		Raw:      providerID,
	}

	provider := strings.ToLower(parts[0])
	details := strings.TrimPrefix(providerID, provider+"://")

	log.Debug("parsed providerID", "provider", provider, "details", details)

	switch provider {
	case "aws":
		// aws:///us-east-1-dfw-2a/i-09718a2c72afb281c
		subParts := strings.Split(details, "/")
		if len(subParts) != 3 {
			return nil, fmt.Errorf("invalid AWS providerID format: %s, parts: %d", providerID, len(subParts))
		}
		info.Identifier = subParts[len(subParts)-1]
	case "gce":
		// gce://project-id/us-central1-a/instance-name
		subParts := strings.Split(details, "/")
		if len(subParts) != 3 {
			return nil, fmt.Errorf("invalid GCP providerID format: %s, parts: %d", providerID, len(subParts))
		}
		info.Identifier = subParts[len(subParts)-1]
	case "azure":
		// azure:///subscriptions/subscription-id/resourceGroups/rg-name/providers/Microsoft.Compute/virtualMachines/vm-name
		subParts := strings.Split(details, "/")
		if len(subParts) < 9 {
			return nil, fmt.Errorf("invalid Azure providerID format: %s, parts: %d", providerID, len(subParts))
		}
		info.Identifier = subParts[len(subParts)-1]
	case "baremetal":
		// baremetal:///node-name
		subParts := strings.Split(details, "/")
		if len(subParts) != 2 {
			return nil, fmt.Errorf("invalid BareMetal providerID format: %s, parts: %d", providerID, len(subParts))
		}
		info.Identifier = subParts[len(subParts)-1]
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	return info, nil
}

// Get fetches the Kubernetes node object by name using the provided clientset and config.
// It returns an error if the node name is empty, clientset or config is nil, or if the node cannot be fetched.
func Get(ctx context.Context, log *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, node string) (*corev1.Node, error) {
	if strings.TrimSpace(node) == "" {
		return nil, fmt.Errorf("node name is required")
	}

	if cs == nil {
		return nil, fmt.Errorf("kubernetes clientset is nil")
	}

	if cfg == nil {
		return nil, fmt.Errorf("kubernetes rest config is nil")
	}

	if log == nil {
		log = slog.Default()
	}

	log.Debug("fetching node information", "node", node)

	// Fetch the node information
	n, err := cs.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	return n, nil
}
