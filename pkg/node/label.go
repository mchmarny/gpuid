package node

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/mchmarny/gpuid/pkg/gpu"
	"k8s.io/client-go/kubernetes"
)

const (
	// Label namespace and prefixes
	labelNS            = "gpuid.github.com"
	labelChassisPrefix = "chassis"
	labelChassisCount  = "chassis-count"
	labelGPUPrefix     = "gpu"

	// Retry configuration in case of large number of GPU nodes being added all at once
	maxRetries      = 5
	retryBackoff    = 2 * time.Second
	maxRetryBackoff = 45 * time.Second

	notSetDefault = "na"
)

// labelValueRegex matches valid Kubernetes label values
// Valid regex: (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?
var labelValueRegex = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9\-_.]*[A-Za-z0-9])?$`)

// sanitizeLabelValue converts any string into a valid Kubernetes label value
// Invalid characters are replaced with underscores, and N/A is converted to "unknown"
func sanitizeLabelValue(value string) string {
	if value == "" {
		return notSetDefault
	}

	// Handle common "N/A" case from nvidia-smi output
	if value == "N/A" {
		return notSetDefault
	}

	// Replace invalid characters with hyphens
	sanitized := regexp.MustCompile(`[^A-Za-z0-9\-_.]`).ReplaceAllString(value, "-")

	// Ensure it starts and ends with alphanumeric character
	// Trim leading/trailing non-alphanumeric characters
	sanitized = regexp.MustCompile(`^[^A-Za-z0-9]+`).ReplaceAllString(sanitized, "")
	sanitized = regexp.MustCompile(`[^A-Za-z0-9]+$`).ReplaceAllString(sanitized, "")

	// If empty after sanitization, use a default
	if sanitized == "" {
		return notSetDefault
	}

	// Validate the result
	if !labelValueRegex.MatchString(sanitized) {
		return notSetDefault
	}

	return strings.TrimSpace(sanitized)
}

// Updater interface for testability
type Updater interface {
	GetNode(ctx context.Context, name string) (*corev1.Node, error)
	UpdateNode(ctx context.Context, node *corev1.Node) (*corev1.Node, error)
}

// Labeler implements Updater
type Labeler struct {
	client kubernetes.Interface
}

// NewLabelUpdater creates a new Labeler instance
func NewLabelUpdater(client kubernetes.Interface) Updater {
	return &Labeler{client: client}
}

func (l *Labeler) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	return l.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
}

func (l *Labeler) UpdateNode(ctx context.Context, node *corev1.Node) (*corev1.Node, error) {
	return l.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
}

// EnsureLabels is the testable version that accepts an interface
func EnsureLabels(ctx context.Context, log *slog.Logger, labeler Updater, nodeName string, serials []*gpu.Serials) error {
	desiredLabels := calculateGPULabels(log, serials)

	return wait.ExponentialBackoff(wait.Backoff{
		Duration: retryBackoff,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    maxRetries,
		Cap:      maxRetryBackoff,
	}, func() (bool, error) {
		success, err := attemptLabelUpdate(ctx, log, labeler, nodeName, desiredLabels)
		if err != nil {
			// Don't retry permission errors - they won't resolve with retries
			if errors.IsForbidden(err) {
				log.Error("insufficient permissions to update node labels - check RBAC configuration",
					"node", nodeName, "error", err)
				return false, err // Return the error to stop retrying
			}
			// Don't retry validation errors - they indicate a bug in label value generation
			if errors.IsInvalid(err) {
				log.Error("invalid label values - this indicates a bug in label sanitization",
					"node", nodeName, "error", err)
				return false, err // Return the error to stop retrying
			}
			log.Warn("failed to update node labels, retrying", "node", nodeName, "error", err)
			return false, nil
		}
		return success, nil
	})
}

// attemptLabelUpdate performs a single attempt to update node labels
func attemptLabelUpdate(ctx context.Context, log *slog.Logger, labeler Updater, nodeName string, desiredLabels map[string]string) (bool, error) {
	node, err := labeler.GetNode(ctx, nodeName)
	if err != nil {
		return false, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	if node == nil {
		return false, fmt.Errorf("node %s is nil", nodeName)
	}

	// Check if labels need updating to avoid unnecessary API calls
	currentLabels := node.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}

	if !needsLabelUpdate(currentLabels, desiredLabels) {
		log.Debug("node labels already up to date", "node", nodeName)
		return true, nil
	}

	// Create a copy of labels to avoid mutating the original
	updatedLabels := make(map[string]string)
	for k, v := range currentLabels {
		updatedLabels[k] = v
	}

	// Clear existing GPU labels and apply new ones
	clearLabels(updatedLabels)
	applyLabels(updatedLabels, desiredLabels)

	// Update the node
	node.SetLabels(updatedLabels)
	_, err = labeler.UpdateNode(ctx, node)
	if err != nil {
		return false, err
	}

	log.Info("successfully updated node labels", "node", nodeName, "gpu_labels", len(desiredLabels))
	return true, nil
}

// calculateGPULabels pre-calculates all GPU labels to minimize work in retry loop
func calculateGPULabels(log *slog.Logger, serials []*gpu.Serials) map[string]string {
	labels := make(map[string]string)
	if len(serials) == 0 {
		return labels
	}

	log.Debug("calculating GPU labels", "count", len(serials))

	// Sort serials by chassis name for predictable order
	sortedSerials := make([]*gpu.Serials, len(serials))
	copy(sortedSerials, serials)

	// Sort by chassis name, placing nils at the end for consistent indexes
	sort.Slice(sortedSerials, func(i, j int) bool {
		if sortedSerials[i] == nil {
			return false
		}
		if sortedSerials[j] == nil {
			return true
		}
		return sortedSerials[i].Chassis < sortedSerials[j].Chassis
	})

	chassisCount := 0

	// Generate labels
	for i, s := range sortedSerials {
		if s == nil {
			continue
		}

		// increment chassis count only for non-nil entries
		chassisCount++

		// parse and sanitize chassis serial number
		chassisSerial := sanitizeLabelValue(s.Chassis)

		// set chassis label if serial number is present
		if chassisSerial != notSetDefault {
			chassisLabel := fmt.Sprintf("%s/%s-%d", labelNS, labelChassisPrefix, i)
			labels[chassisLabel] = chassisSerial
		}

		// sort GPU serials for predictable order
		gpuSerials := make([]string, len(s.GPU))
		copy(gpuSerials, s.GPU)
		sort.Strings(gpuSerials)

		// GPU labels
		for j, g := range gpuSerials {
			if g == "" {
				continue
			}

			// GPU label - no chassis prefix by default (h100)
			gpuLabel := fmt.Sprintf("%s/%s-%d", labelNS, labelGPUPrefix, j)
			if chassisSerial != notSetDefault {
				// if chassis has a serial number, then include its index in the GPU label
				gpuLabel = fmt.Sprintf("%s/%s-%d-%s-%d", labelNS, labelChassisPrefix, i, labelGPUPrefix, j)
			}

			labels[gpuLabel] = sanitizeLabelValue(g)
		}
	}

	// Chassis count
	chassisCountLabel := fmt.Sprintf("%s/%s", labelNS, labelChassisCount)
	labels[chassisCountLabel] = fmt.Sprintf("%d", chassisCount)

	return labels
}

// needsLabelUpdate checks if the current labels differ from desired labels
func needsLabelUpdate(current, desired map[string]string) bool {
	// Check if any GPU labels need to be removed
	for k := range current {
		if strings.HasPrefix(k, labelNS) {
			if _, exists := desired[k]; !exists {
				return true
			}
		}
	}

	// Check if any desired labels are missing or different
	for k, v := range desired {
		if current[k] != v {
			return true
		}
	}

	return false
}

// clearGPULabels removes all existing GPU-related labels
func clearLabels(labels map[string]string) {
	for k := range labels {
		if strings.HasPrefix(k, labelNS) {
			delete(labels, k)
		}
	}
}

// applyGPULabels adds the desired GPU labels
func applyLabels(labels, desiredLabels map[string]string) {
	for k, v := range desiredLabels {
		labels[k] = v
	}
}
