package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
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

// transientError marks an error as eligible for rate-limited retry through the workqueue.
type transientError struct{ err error }

func (t *transientError) Error() string { return t.err.Error() }
func (t *transientError) Unwrap() error { return t.err }

func transient(err error) error { return &transientError{err: err} }

func isTransient(err error) bool {
	var te *transientError
	return errors.As(err, &te)
}

// do processes items from the work queue in a loop until the context is canceled.
func do(
	ctx context.Context,
	log *slog.Logger,
	cs *kubernetes.Clientset,
	cfg *rest.Config,
	indexer cache.Indexer,
	q workqueue.TypedRateLimitingInterface[string],
	labeler node.Updater,
	cmd *Command,
) {

	log.Debug("worker started")
	defer log.Debug("worker stopped")

	for {
		item, shutdown := q.Get()
		if shutdown {
			log.Debug("work queue shut down, stopping worker")
			return
		}

		func(key string) {
			defer q.Done(key)

			if _, _, err := cache.SplitMetaNamespaceKey(key); err != nil {
				log.Warn("invalid cache key format", "key", key, "err", err)
				q.Forget(key)
				return
			}

			o, exists, err := indexer.GetByKey(key)
			if err != nil {
				log.Warn("failed to get pod from cache", "key", key, "err", err)
				q.Forget(key)
				return
			}

			if !exists {
				// Pod deleted; normal lifecycle.
				log.Debug("pod no longer exists in cache", "key", key)
				q.Forget(key)
				return
			}

			pod, ok := o.(*corev1.Pod)
			if !ok {
				log.Warn("cache object is not a Pod", "key", key, "type", fmt.Sprintf("%T", o))
				q.Forget(key)
				return
			}

			if err := processPod(ctx, log, cs, cfg, labeler, pod, cmd); err != nil {
				log.Warn("failed to process pod", "pod", pod.Name, "err", err)
				if isTransient(err) {
					q.AddRateLimited(key)
					return
				}
			}

			q.Forget(key)
		}(item)
	}
}

// processPod handles the execution of a command in a single pod.
func processPod(
	ctx context.Context,
	log *slog.Logger,
	cs *kubernetes.Clientset,
	cfg *rest.Config,
	labeler node.Updater,
	pod *corev1.Pod,
	cmd *Command,
) error {
	// In case pod transitioned states between enqueueing and processing
	if !podReady(pod) {
		log.Debug("pod not ready at processing time", "pod", pod.Name, "phase", pod.Status.Phase)
		return nil
	}

	// Ensure we only process each pod UID once to prevent duplicate exports.
	if processed.Has(string(pod.UID)) {
		log.Debug("pod already processed", "pod", pod.Name, "uid", pod.UID)
		return nil
	}

	// Add jitter to prevent thundering herd when many pods become ready simultaneously.
	jitter := time.Duration(rand.IntN(200)) * time.Millisecond //nolint:gosec // G404: non-crypto jitter
	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Per-pod timeout context bounds the whole pod processing budget.
	pctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
	defer cancel()

	log.Debug("processing pod",
		"pod", pod.Name,
		"uid", pod.UID,
		"node", pod.Spec.NodeName,
	)

	serials, err := gpu.GetSerialNumbers(pctx, log, cs, cfg, pod, cmd.Container)
	if err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Error("failed to get GPU serial numbers",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		// Pod-exec failures are commonly transient (network, pod restart).
		return transient(fmt.Errorf("error obtaining GPU serial numbers: %w", err))
	}

	if len(serials) == 0 {
		log.Debug("no GPU serial numbers found, skipping export", "pod", pod.Name, "uid", pod.UID, "node", pod.Spec.NodeName)
		// Cache this UID so we don't keep retrying pods that report no GPUs.
		processed.Add(string(pod.UID))
		return nil
	}

	if err = node.EnsureLabels(pctx, log, labeler, pod.Spec.NodeName, serials); err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Error("failed to ensure node labels",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		return transient(fmt.Errorf("failed to ensure node labels: %w", err))
	}

	nodeInfo, err := node.GetNodeProviderID(pctx, log, cs, pod.Spec.NodeName)
	if err != nil {
		counterErr.Increment(pod.Spec.NodeName, pod.Name)
		log.Warn("failed to get node provider ID",
			"pod", pod.Name,
			"uid", pod.UID,
			"node", pod.Spec.NodeName,
			"err", err,
		)
		return transient(fmt.Errorf("failed to get node provider ID: %w", err))
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
		return transient(fmt.Errorf("failed to export GPU serial numbers: %w", err))
	}

	counterSuccess.Increment(pod.Spec.NodeName, pod.Name)
	processed.Add(string(pod.UID))

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
	if p.Status.Phase != corev1.PodRunning {
		return false
	}

	// Ensure all declared containers are present in status to prevent racing with container creation.
	if len(p.Status.ContainerStatuses) < len(p.Spec.Containers) {
		return false
	}

	for _, status := range p.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	return true
}
