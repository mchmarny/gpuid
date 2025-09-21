package runner

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/mchmarny/gpuid/pkg/counter"
	"github.com/mchmarny/gpuid/pkg/gpu"
	"github.com/mchmarny/gpuid/pkg/node"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

var (
	// Metrics for monitoring command execution outcomes.
	counterSuccess = counter.New("gpuid_export_success_total", "Total number of successful export executions", "node", "pod")
	counterErr     = counter.New("gpuid_export_failure_total", "Total number of failed export executions", "node", "pod")
)

// do processes items from the work queue in a loop until the context is canceled.
func do(
	ctx context.Context,
	log *slog.Logger,
	cs *kubernetes.Clientset,
	cfg *rest.Config,
	indexer cache.Indexer,
	q workqueue.TypedRateLimitingInterface[string],
	cmd *Command,
) {

	log.Debug("worker started")
	defer log.Debug("worker stopped")

	for {
		// Get next item from queue with proper shutdown handling
		item, shutdown := q.Get()
		if shutdown {
			log.Debug("work queue shut down, stopping worker")
			return
		}

		// Process the item in a closure to ensure proper cleanup
		func(key string) {
			// Mark the item as done when we finish processing
			defer q.Done(key)

			// Parse namespace and name from the cache key
			_, _, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				log.Warn("invalid cache key format", "key", key, "err", err)
				q.Forget(key)
				return
			}

			// Retrieve the pod object from the cache
			o, exists, err := indexer.GetByKey(key)
			if err != nil {
				log.Warn("failed to get pod from cache", "key", key, "err", err)
				q.Forget(key)
				return
			}

			if !exists {
				// Pod was deleted; this is normal during pod lifecycle
				log.Debug("pod no longer exists in cache", "key", key)
				q.Forget(key)
				return
			}

			// Type assert to Pod object
			pod, ok := o.(*corev1.Pod)
			if !ok {
				log.Warn("cache object is not a Pod", "key", key, "type", fmt.Sprintf("%T", o))
				q.Forget(key)
				return
			}

			// Process the pod
			if err := processPod(ctx, log, cs, cfg, pod, cmd); err != nil {
				log.Warn("failed to process pod", "pod", pod.Name, "err", err)
			}

			// Always forget the item to prevent infinite retries
			q.Forget(key)
		}(item)
	}
}

// processPod handles the execution of a command in a single pod.
// This function encapsulates all the logic for command execution, including
// readiness checks, deduplication, and cleanup operations.
func processPod(
	ctx context.Context,
	log *slog.Logger,
	cs *kubernetes.Clientset,
	cfg *rest.Config,
	pod *corev1.Pod,
	cmd *Command,
) error {
	// In case pod transitioned states between enqueueing and processing
	if !podReady(pod) {
		log.Debug("pod not ready at processing time", "pod", pod.Name, "phase", pod.Status.Phase)
		return nil
	}

	// Ensure we only process each pod UID once to prevent duplicate exports
	if processed.Has(string(pod.UID)) {
		log.Debug("pod already processed", "pod", pod.Name, "uid", pod.UID)
		return nil
	}

	// Add jitter to prevent thundering herd problems when many pods become ready simultaneously
	jitterMs := rand.Intn(200) //nolint:gosec // G404: Non-crypto use case for jitter timing
	select {
	case <-time.After(time.Duration(jitterMs) * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Create per-pod timeout context to prevent hanging executions
	pctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
	defer cancel()

	// Mark this pod as processed regardless of success/failure to prevent endless retries
	processed.Add(string(pod.UID))

	log.Debug("processing pod",
		"pod", pod.Name,
		"uid", pod.UID,
		"node", pod.Spec.NodeName,
	)

	// Get GPU serial numbers from the pod
	serials, err := gpu.GetSerialNumbers(pctx, log, cs, cfg, pod, cmd.Container)
	if err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Error("failed to get GPU serial numbers",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		return fmt.Errorf("error obtaining GPU serial numbers: %w", err)
	}

	// Skip pods without serial numbers
	if len(serials) == 0 {
		log.Debug("no GPU serial numbers found, skipping export", "pod", pod.Name, "uid", pod.UID, "node", pod.Spec.NodeName)
		return nil
	}

	// Update gpu and chassis labels on the pod underlining node
	labeler := node.NewLabelUpdater(cs)
	if err = node.EnsureLabels(pctx, log, labeler, pod.Spec.NodeName, serials); err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Error("failed to ensure node labels",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		return fmt.Errorf("failed to ensure node labels: %w", err)
	}

	// Retrieve node provider ID for export metadata
	nodeInfo, err := node.GetNodeProviderID(pctx, log, cs, cfg, pod.Spec.NodeName)
	if err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Warn("failed to get node provider ID",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		return fmt.Errorf("failed to get node provider ID: %w", err)
	}

	if nodeInfo.Identifier == "" {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Warn("node provider ID is empty",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"provider", nodeInfo.Raw,
		)
	}

	// Export the serial numbers using the specified exporter
	if err := cmd.exporter.Export(pctx, log, cmd.Cluster, pod, nodeInfo.Identifier, serials); err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Error("failed to export GPU serial numbers",
			"exporter", cmd.ExporterType,
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"provider", nodeInfo.Raw,
			"err", err,
		)
		return fmt.Errorf("failed to export GPU serial numbers: %w", err)
	}

	// Increment success metric
	counterSuccess.Increment(pod.Spec.NodeName, pod.Name)

	// Success case
	log.Debug("pod processed successfully",
		"exporter", cmd.ExporterType,
		"pod", pod.Name,
		"uid", pod.UID,
		"node", pod.Spec.NodeName,
	)

	return nil
}

// podReady checks if a pod is ready to execute commands.
// A pod is considered ready when it's in Running phase and all containers are ready.
func podReady(p *corev1.Pod) bool {
	// Pod must be in Running phase
	if p.Status.Phase != corev1.PodRunning {
		return false
	}

	// Ensure all declared containers are present in status
	// This prevents racing with container creation
	if len(p.Status.ContainerStatuses) < len(p.Spec.Containers) {
		return false
	}

	// All containers must be ready
	// This ensures we don't execute commands before containers finish starting
	for _, status := range p.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	return true
}
