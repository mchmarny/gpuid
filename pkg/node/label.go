package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
var (
	labelValueRegex   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9\-_.]*[A-Za-z0-9])?$`)
	invalidCharsRegex = regexp.MustCompile(`[^A-Za-z0-9\-_.]`)
	leadingJunkRegex  = regexp.MustCompile(`^[^A-Za-z0-9]+`)
	trailingJunkRegex = regexp.MustCompile(`[^A-Za-z0-9]+$`)
)

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
	sanitized := invalidCharsRegex.ReplaceAllString(value, "-")

	// Ensure it starts and ends with alphanumeric character
	// Trim leading/trailing non-alphanumeric characters
	sanitized = leadingJunkRegex.ReplaceAllString(sanitized, "")
	sanitized = trailingJunkRegex.ReplaceAllString(sanitized, "")

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

// Updater is the contract the labeler depends on. Reads the current node and applies
// a label patch so multiple controllers writing different labels never conflict.
type Updater interface {
	GetNode(ctx context.Context, name string) (*corev1.Node, error)
	PatchNodeLabels(ctx context.Context, name string, patch []byte) error
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

func (l *Labeler) PatchNodeLabels(ctx context.Context, name string, patch []byte) error {
	_, err := l.client.CoreV1().Nodes().Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
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

// attemptLabelUpdate performs a single attempt to update node labels via a
// strategic merge patch. Patch is conflict-free with other label writers and
// avoids the Get/mutate/Update race entirely.
func attemptLabelUpdate(ctx context.Context, log *slog.Logger, labeler Updater, nodeName string, desiredLabels map[string]string) (bool, error) {
	node, err := labeler.GetNode(ctx, nodeName)
	if err != nil {
		return false, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	if node == nil {
		return false, fmt.Errorf("node %s is nil", nodeName)
	}

	currentLabels := node.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}

	if !needsLabelUpdate(currentLabels, desiredLabels) {
		log.Debug("node labels already up to date", "node", nodeName)
		return true, nil
	}

	// Build patch: set every desired label, and explicitly null any stale
	// gpuid-prefixed label that is no longer desired (strategic merge null = delete).
	patchLabels := make(map[string]any, len(desiredLabels))
	for k, v := range desiredLabels {
		patchLabels[k] = v
	}
	for k := range currentLabels {
		if !strings.HasPrefix(k, labelNS) {
			continue
		}
		if _, keep := desiredLabels[k]; !keep {
			patchLabels[k] = nil
		}
	}

	patch := map[string]any{
		"metadata": map[string]any{
			"labels": patchLabels,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return false, fmt.Errorf("failed to marshal label patch: %w", err)
	}

	if err := labeler.PatchNodeLabels(ctx, nodeName, patchBytes); err != nil {
		return false, err
	}

	log.Info("successfully patched node labels", "node", nodeName, "gpu_labels", len(desiredLabels))
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
	multipleChassis := len(sortedSerials) > 1

	if multipleChassis {
		log.Debug("multiple chassis detected, GPU labels will include chassis index", "chassis_count", len(sortedSerials))
	}

	// Generate labels
	for i, s := range sortedSerials {
		if s == nil {
			continue
		}

		// parse and sanitize chassis serial number
		chassisSerial := sanitizeLabelValue(s.Chassis)

		// set chassis label if serial number is present
		if chassisSerial != notSetDefault {
			chassisLabel := fmt.Sprintf("%s/%s", labelNS, labelChassisPrefix)
			if multipleChassis {
				chassisLabel = fmt.Sprintf("%s/%s-%d", labelNS, labelChassisPrefix, i)
			}
			labels[chassisLabel] = chassisSerial

			// increment chassis count only for non-nil entries
			chassisCount++
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
			if multipleChassis && chassisSerial != notSetDefault {
				// if chassis has a serial number, then include its index in the GPU label
				gpuLabel = fmt.Sprintf("%s/%s-%d-%s-%d", labelNS, labelChassisPrefix, i, labelGPUPrefix, j)
			}

			labels[gpuLabel] = sanitizeLabelValue(g)
		}
	}

	// Chassis count, only if there is at least one chassis with a serial number
	if chassisCount > 0 {
		chassisCountLabel := fmt.Sprintf("%s/%s", labelNS, labelChassisCount)
		labels[chassisCountLabel] = fmt.Sprintf("%d", chassisCount)
	}

	return labels
}

// needsLabelUpdate checks if the current labels differ from desired labels.
func needsLabelUpdate(current, desired map[string]string) bool {
	// Any gpuid label that is no longer desired must be removed.
	for k := range current {
		if strings.HasPrefix(k, labelNS) {
			if _, exists := desired[k]; !exists {
				return true
			}
		}
	}

	// Any desired label that is missing or has a different value must be applied.
	for k, v := range desired {
		if current[k] != v {
			return true
		}
	}

	return false
}
