package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mchmarny/gpuid/pkg/counter"
	"github.com/mchmarny/gpuid/pkg/logger"
	"github.com/mchmarny/gpuid/pkg/node"
	"github.com/mchmarny/gpuid/pkg/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
)

const (
	// serviceName is used in logging and metrics to identify this service
	serviceName = "gpuid"

	// exporterCloseTimeout bounds exporter flush during shutdown.
	exporterCloseTimeout = 10 * time.Second
)

// leaderState reports the leader-election lifecycle outcome to the caller of Run.
type leaderState int

const (
	leaderShutdown leaderState = iota // ctx canceled before/after leadership
	leaderLost                        // we held the lease and then lost it — fatal
)

// Run starts the pod execution controller with proper lifecycle management.
// Returns an exit code so main() can propagate it without calling os.Exit here.
func Run() int {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // Kubernetes pod termination
	)
	defer cancel()

	cmd, cmdErr := NewCommandFromEnvVars()
	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "failed to load configuration from environment: %v\n", cmdErr)
		return 2
	}

	log := logger.NewProductionLogger(logger.Config{
		Service:   serviceName,
		Level:     cmd.LogLevel,
		AddSource: true,
	})
	log.Info("configuration", "input", fmt.Sprintf("%+v", cmd))

	if vErr := cmd.Validate(); vErr != nil {
		log.Error("invalid command configuration", "err", vErr)
		return 2
	}

	if initErr := cmd.Init(ctx, log); initErr != nil {
		log.Error("failed to initialize command", "err", initErr)
		return 2
	}
	defer func() {
		// Use a fresh context with bounded budget — parent ctx is canceled by now.
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), exporterCloseTimeout)
		defer closeCancel()
		if cErr := cmd.exporter.Close(closeCtx); cErr != nil {
			log.Warn("failed to close exporter", "err", cErr)
		}
	}()

	cs, cfg, rcErr := buildRestConfig(cmd.Kubeconfig, cmd.QPS, cmd.Burst)
	if rcErr != nil {
		log.Error("failed to create kubernetes clientset", "err", rcErr)
		return 2
	}

	log.Debug("kubernetes client created", "qps", cfg.QPS, "burst", cfg.Burst)

	if _, lpErr := labels.Parse(cmd.PodLabelSelector); lpErr != nil {
		log.Error("invalid label selector", "selector", cmd.PodLabelSelector, "err", lpErr)
		return 2
	}

	// HTTP server runs on every replica so probes and metrics work even on followers.
	// Reconciliation goes through runController, which is gated on leader election
	// when enabled.
	srvErrCh := make(chan error, 1)
	srv := server.NewServer(server.WithLogger(log), server.WithPort(cmd.ServerPort))
	go func() {
		srvErrCh <- srv.Serve(ctx, map[string]http.Handler{
			"/metrics": counter.Handler(),
		})
	}()

	var reconcileErr error
	if cmd.LeaderElection {
		state, err := runWithLeaderElection(ctx, log, cs, cfg, cmd)
		reconcileErr = err
		if state == leaderLost {
			// Lost the lease unexpectedly — exit non-zero so Kubernetes restarts us.
			log.Error("leader election lease lost, exiting", "err", reconcileErr)
			return 1
		}
	} else {
		reconcileErr = runController(ctx, log, cs, cfg, cmd)
	}

	// Drain server. If both reconcile and server return errors prefer the reconcile
	// one, since it's the primary failure.
	select {
	case sErr := <-srvErrCh:
		if reconcileErr == nil && sErr != nil {
			log.Error("server terminated unexpectedly", "err", sErr)
			return 2
		}
	case <-time.After(exporterCloseTimeout):
		log.Warn("server did not shut down within budget")
	}

	if reconcileErr != nil {
		log.Error("controller failed", "err", reconcileErr)
		return 2
	}
	return 0
}

// runWithLeaderElection blocks until ctx is canceled or the lease is lost.
// While leading, it invokes runController with a context that is canceled when
// either ctx is canceled OR leadership is lost.
func runWithLeaderElection(ctx context.Context, log *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, cmd *Command) (leaderState, error) {
	// Identity must be unique per replica. PodName + a per-process uuid ensures we
	// can't accidentally collide with a stale lease record from a pod with the
	// same name in a previous incarnation.
	identity := fmt.Sprintf("%s_%s", cmd.PodName, uuid.New().String())

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cmd.LeaseName,
			Namespace: cmd.LeaseNamespace,
		},
		Client: cs.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: identity,
		},
	}

	// runErr is written by OnStartedLeading; runDone signals completion.
	var (
		runErr  error
		runDone = make(chan struct{})
	)

	var state leaderState

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cmd.LeaseDuration,
		RenewDeadline:   cmd.LeaseRenew,
		RetryPeriod:     cmd.LeaseRetry,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaseCtx context.Context) {
				log.Info("became leader, starting controller", "identity", identity)
				runErr = runController(leaseCtx, log, cs, cfg, cmd)
				close(runDone)
			},
			OnStoppedLeading: func() {
				log.Warn("stopped leading", "identity", identity)
				// If ctx is still live we lost the lease; that's fatal because we
				// have no idempotent way to know how far the new leader has progressed.
				if ctx.Err() == nil {
					state = leaderLost
				}
			},
			OnNewLeader: func(current string) {
				if current == identity {
					return
				}
				log.Info("observed new leader", "leader", current)
			},
		},
		Name: cmd.LeaseName,
	})
	if err != nil {
		return leaderShutdown, fmt.Errorf("failed to create leader elector: %w", err)
	}

	// Run blocks until ctx is canceled OR we lose the lease.
	le.Run(ctx)

	// If runController was started (we led at some point), wait for it to finish.
	select {
	case <-runDone:
	default:
		// We never led; nothing to wait for.
	}

	return state, runErr
}

// runController encapsulates the main controller logic with proper error handling.
// Separating this allows for better testing and error management.
func runController(ctx context.Context, log *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, cmd *Command) error {
	if log == nil {
		return errors.New("logger is nil")
	}
	if cs == nil {
		return errors.New("kubernetes clientset is nil")
	}
	if cfg == nil {
		return errors.New("kubernetes config is nil")
	}
	if cmd == nil {
		return errors.New("command configuration is nil")
	}

	log.Info("starting controller...")

	// Build the labeler once — it is stateless and safe to share across workers.
	labeler := node.NewLabelUpdater(cs)

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

	informer := cache.NewSharedIndexInformer(
		lw,
		&corev1.Pod{},
		cmd.Resync,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Rate-limiting queue to handle pod events without overwhelming the API server.
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer q.ShutDown()

	enqueueIfReady := func(obj any) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			log.Warn("received non-pod object in event handler")
			return
		}

		if !podReady(pod) {
			log.Debug("skipping non-ready pod", "pod", pod.Name, "phase", pod.Status.Phase)
			return
		}

		key, err := cache.MetaNamespaceKeyFunc(pod)
		if err != nil {
			log.Warn("failed to generate cache key", "pod", pod.Name, "err", err)
			return
		}

		log.Debug("enqueueing ready pod", "pod", pod.Name, "key", key)
		q.Add(key)
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueIfReady,
		UpdateFunc: func(_, newObj any) {
			enqueueIfReady(newObj)
		},
	}); err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	stopCh := make(chan struct{})

	var wg sync.WaitGroup

	log.Info("starting kubernetes informer")
	wg.Go(func() {
		informer.Run(stopCh)
	})

	// Wait for cache to sync before starting workers so we have a consistent view.
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		close(stopCh)
		wg.Wait()
		return errors.New("failed to wait for informer cache sync")
	}

	log.Info("cache synced, starting workers")
	for i := range cmd.Workers {
		log.Debug("starting worker", "worker_id", i)
		wg.Go(func() {
			do(ctx, log.With("worker_id", i), cs, cfg, informer.GetIndexer(), q, labeler, cmd)
		})
	}

	<-ctx.Done()
	log.Info("shutdown signal received, draining work queue")
	q.ShutDownWithDrain()
	close(stopCh)

	wg.Wait()
	log.Info("controller shutdown complete")

	return nil
}
