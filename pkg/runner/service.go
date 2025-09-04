package runner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mchmarny/gpuid/pkg/counter"
	"github.com/mchmarny/gpuid/pkg/logger"
	"github.com/mchmarny/gpuid/pkg/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	// serviceName is used in logging and metrics to identify this service
	// TODO: migrate to k8s annotations/labels for processing state or use controller-runtime
	serviceName = "gpuid"
)

// Run starts the pod execution controller with proper lifecycle management.
// This is the main entry point that orchestrates all controller components
// following Kubernetes controller-runtime patterns for robustness.
func Run() {
	// Set up signal handling for graceful shutdown - essential for cloud-native apps
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // Kubernetes pod termination
	)
	defer cancel()

	cmd := NewCommandFromEnvVars()
	if cmd == nil {
		die("failed to create command configuration", nil)
	}

	// Initialize structured logging with the configured level
	logger := logger.NewProductionLogger(logger.Config{
		Service:   serviceName,
		Level:     cmd.LogLevel,
		AddSource: true,
	})

	// Log command configuration for transparency
	logger.Info("configuration", "input", fmt.Sprintf("%+v", cmd))

	if err := cmd.Validate(); err != nil {
		die("invalid command configuration: %v", logger, err)
	}

	// Create Kubernetes clientset with retry logic built into client
	cs, cfg, err := buildRestConfig(cmd.Kubeconfig, cmd.QPS, cmd.Burst)
	if err != nil {
		die("failed to create kubernetes clientset: %v", logger, err)
	}
	if cs == nil || cfg == nil {
		die("kubernetes clientset or config is nil", logger)
	}

	logger.Debug("kubernetes client created", "qps", cfg.QPS, "burst", cfg.Burst)

	// Validate label selector syntax early to fail fast
	_, err = labels.Parse(cmd.PodLabelSelector)
	if err != nil {
		die("invalid label selector %q: %v", logger, cmd.PodLabelSelector, err)
	}

	// Run the controller with proper error handling
	if err := runController(ctx, logger, cs, cfg, cmd); err != nil {
		die("controller failed: %v", logger, err)
	}
}

// runController encapsulates the main controller logic with proper error handling.
// Separating this allows for better testing and error management.
func runController(ctx context.Context, logger *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, cmd *Command) error {
	if logger == nil {
		return fmt.Errorf("logger is nil")
	}
	if cs == nil {
		return fmt.Errorf("kubernetes clientset is nil")
	}
	if cfg == nil {
		return fmt.Errorf("kubernetes config is nil")
	}
	if cmd == nil {
		return fmt.Errorf("command configuration is nil")
	}

	logger.Info("starting controller...")

	// Create ListWatch for pod informer with proper error handling
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.LabelSelector = cmd.PodLabelSelector
			return cs.CoreV1().Pods(cmd.Namespace).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = cmd.PodLabelSelector
			return cs.CoreV1().Pods(cmd.Namespace).Watch(ctx, opts)
		},
	}

	// Create shared informer with proper indexing for efficient lookups
	informer := cache.NewSharedIndexInformer(
		lw,
		&corev1.Pod{},
		cmd.Resync, // Use configured resync period
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Create rate-limiting queue to handle pod events
	// This prevents overwhelming the system during mass pod events
	// Using NewTypedRateLimitingQueue instead of deprecated NewRateLimitingQueue
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer q.ShutDown()

	// Helper function to enqueue pods that are ready for command execution
	enqueueIfReady := func(obj any) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			logger.Warn("received non-pod object in event handler")
			return
		}

		if !podReady(pod) {
			logger.Debug("skipping non-ready pod", "pod", pod.Name, "phase", pod.Status.Phase)
			return
		}

		key, err := cache.MetaNamespaceKeyFunc(pod)
		if err != nil {
			logger.Warn("failed to generate cache key", "pod", pod.Name, "err", err)
			return
		}

		logger.Debug("enqueueing ready pod", "pod", pod.Name, "key", key)
		q.Add(key)
	}

	// Register event handlers for pod lifecycle events
	var err error
	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueIfReady,
		UpdateFunc: func(_, newObj any) {
			// Only process update events, not every reconciliation
			enqueueIfReady(newObj)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	// Start the informer and wait for cache sync
	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start HTTP server for metrics and health checks
	logger.Info("starting server")
	s := server.NewServer(server.WithLogger(logger), server.WithPort(cmd.ServerPort))
	go s.Serve(ctx, map[string]http.Handler{
		"/metrics": counter.Handler(),
	})

	logger.Info("starting kubernetes informer")
	go informer.Run(stopCh)

	// Wait for cache to sync before starting workers
	// This ensures we have a consistent view of the cluster state
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return fmt.Errorf("failed to wait for informer cache sync")
	}

	// Start worker goroutines to process the queue
	logger.Info("cache synced, starting workers")
	for i := 0; i < cmd.Workers; i++ {
		logger.Debug("starting worker", "worker_id", i)
		go do(ctx, logger.With("worker_id", i), cs, cfg, informer.GetIndexer(), q, cmd)
	}

	<-ctx.Done() // Wait for shutdown signal
	logger.Info("shutdown signal received, draining work queue")
	q.ShutDownWithDrain() // Graceful shutdown: wait for queue to drain
	logger.Info("controller shutdown complete")

	return nil
}

// die terminates the program with an error message.
// This follows the Go convention for CLI programs that need to exit with errors.
func die(f string, l *slog.Logger, a ...any) {
	if l == nil {
		fmt.Fprintf(os.Stderr, f+"\n", a...)
	} else {
		l.Error(fmt.Sprintf(f, a...))
	}
	os.Exit(2)
}
