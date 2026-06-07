package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// Environment variable names for configuration
	EnvVarExporterType     = "EXPORTER_TYPE"
	EnvVarClusterName      = "CLUSTER_NAME"
	EnvVarNamespace        = "NAMESPACE"
	EnvVarPodLabelSelector = "LABEL_SELECTOR"
	EnvVarContainer        = "CONTAINER"
	EnvVarWorkers          = "WORKERS"
	EnvVarTimeout          = "TIMEOUT"
	EnvVarResync           = "RESYNC"
	EnvVarQPS              = "QPS"
	EnvVarBurst            = "BURST"
	EnvVarKubeconfig       = "KUBECONFIG"
	EnvVarLogLevel         = "LOG_LEVEL"
	EnvVarServerPort       = "SERVER_PORT"
	EnvVarLeaderElection   = "LEADER_ELECTION"
	EnvVarLeaseNamespace   = "LEASE_NAMESPACE"
	EnvVarLeaseName        = "LEASE_NAME"
	EnvVarLeaseDuration    = "LEASE_DURATION"
	EnvVarLeaseRenew       = "LEASE_RENEW_DEADLINE"
	EnvVarLeaseRetry       = "LEASE_RETRY_PERIOD"
	EnvVarPodName          = "POD_NAME"
	EnvVarPodNamespace     = "POD_NAMESPACE"

	// Default values for configuration parameters
	DefaultExporterType     = ExporterTypeStdout // Simple stdout output for easy debugging
	DefaultClusterName      = ""
	DefaultNamespace        = "gpu-operator"
	DefaultPodLabelSelector = "app=nvidia-device-plugin-daemonset"
	DefaultContainer        = "nvidia-device-plugin"
	DefaultWorkers          = 16               // Balanced concurrency for most workloads
	DefaultTimeout          = 30 * time.Second // Reasonable for most commands
	DefaultResync           = 0                // Disable periodic resync by default (event-driven only)
	DefaultQPS              = 50               // Conservative API server rate limiting
	DefaultBurst            = 100              // Allow short bursts while maintaining average QPS
	DefaultKubeconfig       = ""               // Use standard kubeconfig resolution
	DefaultLogLevel         = "info"           // Balanced verbosity
	DefaultServerPort       = 8080             // Default port for metrics and health server

	// Leader election defaults — disabled by default to preserve single-replica behavior.
	DefaultLeaderElection = false
	DefaultLeaseNamespace = "gpuid"
	DefaultLeaseName      = "gpuid-leader"
	// Conservative defaults from controller-runtime; lease must be renewed before it
	// expires or another replica is allowed to take over.
	DefaultLeaseDuration = 15 * time.Second
	DefaultLeaseRenew    = 10 * time.Second
	DefaultLeaseRetry    = 2 * time.Second
)

var (
	// Error definitions follow Go conventions and provide clear context for debugging
	ErrInvalidExporter   = fmt.Errorf("invalid exporter type")
	ErrNoClusterName     = fmt.Errorf("cluster name must be specified")
	ErrInvalidWorkers    = fmt.Errorf("workers must be > 0")
	ErrInvalidTimeout    = fmt.Errorf("timeout must be > 0")
	ErrInvalidQPS        = fmt.Errorf("qps must be > 0")
	ErrInvalidBurst      = fmt.Errorf("burst must be > 0")
	ErrNoLabelSelector   = fmt.Errorf("label selector must be specified")
	ErrNoContainer       = fmt.Errorf("container must be specified")
	ErrInvalidResync     = fmt.Errorf("resync period must be >= 0 (0 disables periodic resync)")
	ErrInvalidServerPort = fmt.Errorf("server port must be a valid integer between 1000 and 65535")
	ErrInvalidLease      = fmt.Errorf("lease duration > renew deadline > retry period must hold")
)

// Command encapsulates all configuration for the pod execution controller.
// The structure is designed to be immutable after validation, which prevents
// race conditions in concurrent environments and makes the behavior predictable.
type Command struct {
	ExporterType     string        // Exporter type (e.g., "stdout", "postgress", etc.)
	Cluster          string        // Cluster name for metrics labeling
	Namespace        string        // Kubernetes namespace to watch
	PodLabelSelector string        // Label selector for pod filtering
	Container        string        // Container name within the pod (empty = first container)
	Workers          int           // Number of concurrent workers
	Timeout          time.Duration // Per-command execution timeout
	Resync           time.Duration // Informer resync period (0 = no periodic resync)
	QPS              float32       // Kubernetes API client QPS limit
	Burst            int           // Kubernetes API client burst limit
	Kubeconfig       string        // Path to kubeconfig file
	LogLevel         string        // Logging verbosity level
	ServerPort       int           // Port for metrics and health server

	// Leader election configuration. When LeaderElection is true, only the holder
	// of the LeaseNamespace/LeaseName lease runs the reconciliation workers; the
	// metrics/health server runs on every replica so probes succeed.
	LeaderElection bool
	LeaseNamespace string
	LeaseName      string
	LeaseDuration  time.Duration
	LeaseRenew     time.Duration
	LeaseRetry     time.Duration
	PodName        string // Identity for the lease (from downward API)
	PodNamespace   string // Namespace housing the lease

	exporter *Exporter
}

func (c *Command) Init(ctx context.Context, log *slog.Logger) error {
	if c.ExporterType == "" {
		return ErrInvalidExporter
	}

	exp, err := GetExporter(ctx, log, ExporterConfig{Type: c.ExporterType})
	if err != nil {
		return fmt.Errorf("failed to get exporter: %w", err)
	}
	c.exporter = exp
	return nil
}

// Validate performs comprehensive validation of the command configuration.
// This validation is crucial in distributed systems where invalid config
// can cause cascading failures or resource exhaustion.
func (c *Command) Validate() error {
	if strings.TrimSpace(c.ExporterType) == "" {
		return ErrInvalidExporter
	}

	if strings.TrimSpace(c.Cluster) == "" {
		return ErrNoClusterName
	}

	if strings.TrimSpace(c.Container) == "" {
		return ErrNoContainer
	}

	if strings.TrimSpace(c.Namespace) == "" {
		return fmt.Errorf("namespace must be specified")
	}

	if strings.TrimSpace(c.PodLabelSelector) == "" {
		return ErrNoLabelSelector
	}

	// Ensure worker count is positive to avoid deadlocks
	if c.Workers <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidWorkers, c.Workers)
	}

	// Prevent excessive worker counts that could overwhelm the API server
	if c.Workers > 100 {
		return fmt.Errorf("workers should not exceed 100 to prevent API server overload: got %d", c.Workers)
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("%w: got %v", ErrInvalidTimeout, c.Timeout)
	}

	// Prevent timeouts that are too long and could cause resource leaks
	if c.Timeout > 10*time.Minute {
		return fmt.Errorf("timeout should not exceed 10 minutes to prevent resource leaks: got %v", c.Timeout)
	}

	if c.QPS <= 0 {
		return fmt.Errorf("%w: got %f", ErrInvalidQPS, c.QPS)
	}

	if c.Burst <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidBurst, c.Burst)
	}

	// Validate that burst is reasonable relative to QPS
	if float32(c.Burst) < c.QPS {
		return fmt.Errorf("burst (%d) should be >= QPS (%f) for proper rate limiting", c.Burst, c.QPS)
	}

	if c.Resync < 0 {
		return fmt.Errorf("%w: got %v", ErrInvalidResync, c.Resync)
	}

	if c.ServerPort < 1000 || c.ServerPort > 65535 {
		return ErrInvalidServerPort
	}

	if c.LeaderElection {
		if strings.TrimSpace(c.LeaseName) == "" {
			return fmt.Errorf("lease name must be specified when leader election is enabled")
		}
		if strings.TrimSpace(c.LeaseNamespace) == "" {
			return fmt.Errorf("lease namespace must be specified when leader election is enabled")
		}
		if strings.TrimSpace(c.PodName) == "" {
			return fmt.Errorf("%s must be set (downward API) when leader election is enabled", EnvVarPodName)
		}
		// client-go invariant: lease > renew > retry, each strictly greater.
		if c.LeaseDuration <= c.LeaseRenew || c.LeaseRenew <= c.LeaseRetry || c.LeaseRetry <= 0 {
			return fmt.Errorf("%w: got lease=%v renew=%v retry=%v", ErrInvalidLease, c.LeaseDuration, c.LeaseRenew, c.LeaseRetry)
		}
	}

	return nil
}

type Option func(*Command)

func WithExporterType(exporter string) Option {
	return func(c *Command) {
		c.ExporterType = exporter
	}
}
func WithClusterName(cluster string) Option {
	return func(c *Command) {
		c.Cluster = cluster
	}
}
func WithNamespace(ns string) Option {
	return func(c *Command) {
		c.Namespace = ns
	}
}
func WithPodLabelSelector(labelSel string) Option {
	return func(c *Command) {
		c.PodLabelSelector = labelSel
	}
}
func WithContainer(container string) Option {
	return func(c *Command) {
		c.Container = container
	}
}
func WithWorkers(workers int) Option {
	return func(c *Command) {
		c.Workers = workers
	}
}
func WithTimeout(timeout time.Duration) Option {
	return func(c *Command) {
		c.Timeout = timeout
	}
}
func WithResync(resync time.Duration) Option {
	return func(c *Command) {
		c.Resync = resync
	}
}
func WithQPS(qps float32) Option {
	return func(c *Command) {
		c.QPS = qps
	}
}
func WithBurst(burst int) Option {
	return func(c *Command) {
		c.Burst = burst
	}
}

func WithKubeconfig(kubeconfig string) Option {
	return func(c *Command) {
		c.Kubeconfig = kubeconfig
	}
}

func WithLogLevel(level string) Option {
	return func(c *Command) {
		c.LogLevel = level
	}
}

func WithServerPort(port int) Option {
	return func(c *Command) {
		c.ServerPort = port
	}
}

func WithLeaderElection(enabled bool) Option {
	return func(c *Command) {
		c.LeaderElection = enabled
	}
}

func WithLeaseNamespace(ns string) Option {
	return func(c *Command) {
		c.LeaseNamespace = ns
	}
}

func WithLeaseName(name string) Option {
	return func(c *Command) {
		c.LeaseName = name
	}
}

func WithLeaseDuration(d time.Duration) Option {
	return func(c *Command) {
		c.LeaseDuration = d
	}
}

func WithLeaseRenew(d time.Duration) Option {
	return func(c *Command) {
		c.LeaseRenew = d
	}
}

func WithLeaseRetry(d time.Duration) Option {
	return func(c *Command) {
		c.LeaseRetry = d
	}
}

func WithPodName(name string) Option {
	return func(c *Command) {
		c.PodName = name
	}
}

func WithPodNamespace(ns string) Option {
	return func(c *Command) {
		c.PodNamespace = ns
	}
}

// NewCommand creates a Command with production-ready defaults.
// The defaults are chosen based on common Kubernetes controller patterns
// and have been battle-tested in high-throughput environments.
func NewCommand(opts ...Option) *Command {
	cmd := &Command{
		ExporterType:     DefaultExporterType,
		Cluster:          DefaultClusterName,
		Namespace:        DefaultNamespace,
		PodLabelSelector: DefaultPodLabelSelector,
		Container:        DefaultContainer,
		Workers:          DefaultWorkers,
		Timeout:          DefaultTimeout,
		Resync:           DefaultResync,
		QPS:              DefaultQPS,
		Burst:            DefaultBurst,
		Kubeconfig:       DefaultKubeconfig,
		LogLevel:         DefaultLogLevel,
		ServerPort:       DefaultServerPort,
		LeaderElection:   DefaultLeaderElection,
		LeaseNamespace:   DefaultLeaseNamespace,
		LeaseName:        DefaultLeaseName,
		LeaseDuration:    DefaultLeaseDuration,
		LeaseRenew:       DefaultLeaseRenew,
		LeaseRetry:       DefaultLeaseRetry,
	}

	// Apply all options in order - this pattern allows for composable configuration
	for _, opt := range opts {
		opt(cmd)
	}

	return cmd
}

func ListEnvVars() []string {
	return []string{
		EnvVarExporterType,
		EnvVarClusterName,
		EnvVarNamespace,
		EnvVarPodLabelSelector,
		EnvVarContainer,
		EnvVarWorkers,
		EnvVarTimeout,
		EnvVarResync,
		EnvVarQPS,
		EnvVarBurst,
		EnvVarKubeconfig,
		EnvVarLogLevel,
		EnvVarServerPort,
		EnvVarLeaderElection,
		EnvVarLeaseNamespace,
		EnvVarLeaseName,
		EnvVarLeaseDuration,
		EnvVarLeaseRenew,
		EnvVarLeaseRetry,
		EnvVarPodName,
		EnvVarPodNamespace,
	}
}

// NewCommandFromEnvVars creates a Command by reading configuration from environment variables.
// Parse failures on typed env vars (int/float/duration) return an error rather than silently
// falling back to defaults — a typo in production should fail fast, not run misconfigured.
func NewCommandFromEnvVars() (*Command, error) {
	workers, err := getEnvAsInt(EnvVarWorkers, DefaultWorkers)
	if err != nil {
		return nil, err
	}
	timeout, err := getEnvAsDuration(EnvVarTimeout, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	resync, err := getEnvAsDuration(EnvVarResync, DefaultResync)
	if err != nil {
		return nil, err
	}
	qps, err := getEnvAsFloat32(EnvVarQPS, DefaultQPS)
	if err != nil {
		return nil, err
	}
	burst, err := getEnvAsInt(EnvVarBurst, DefaultBurst)
	if err != nil {
		return nil, err
	}
	port, err := getEnvAsInt(EnvVarServerPort, DefaultServerPort)
	if err != nil {
		return nil, err
	}
	leaderElection, err := getEnvAsBool(EnvVarLeaderElection, DefaultLeaderElection)
	if err != nil {
		return nil, err
	}
	leaseDuration, err := getEnvAsDuration(EnvVarLeaseDuration, DefaultLeaseDuration)
	if err != nil {
		return nil, err
	}
	leaseRenew, err := getEnvAsDuration(EnvVarLeaseRenew, DefaultLeaseRenew)
	if err != nil {
		return nil, err
	}
	leaseRetry, err := getEnvAsDuration(EnvVarLeaseRetry, DefaultLeaseRetry)
	if err != nil {
		return nil, err
	}
	return NewCommand(
		WithExporterType(getEnv(EnvVarExporterType, DefaultExporterType)),
		WithClusterName(getEnv(EnvVarClusterName, DefaultClusterName)),
		WithNamespace(getEnv(EnvVarNamespace, DefaultNamespace)),
		WithPodLabelSelector(getEnv(EnvVarPodLabelSelector, DefaultPodLabelSelector)),
		WithContainer(getEnv(EnvVarContainer, DefaultContainer)),
		WithWorkers(workers),
		WithTimeout(timeout),
		WithResync(resync),
		WithQPS(qps),
		WithBurst(burst),
		WithKubeconfig(getEnv(EnvVarKubeconfig, DefaultKubeconfig)),
		WithLogLevel(getEnv(EnvVarLogLevel, DefaultLogLevel)),
		WithServerPort(port),
		WithLeaderElection(leaderElection),
		WithLeaseNamespace(getEnv(EnvVarLeaseNamespace, DefaultLeaseNamespace)),
		WithLeaseName(getEnv(EnvVarLeaseName, DefaultLeaseName)),
		WithLeaseDuration(leaseDuration),
		WithLeaseRenew(leaseRenew),
		WithLeaseRetry(leaseRetry),
		WithPodName(getEnv(EnvVarPodName, "")),
		WithPodNamespace(getEnv(EnvVarPodNamespace, "")),
	), nil
}

func getEnvAsBool(name string, defaultVal bool) (bool, error) {
	valStr := getEnv(name, "")
	if valStr == "" {
		return defaultVal, nil
	}
	val, err := strconv.ParseBool(valStr)
	if err != nil {
		return false, fmt.Errorf("invalid boolean for %s=%q: %w", name, valStr, err)
	}
	return val, nil
}

func LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func getEnv(name, defaultVal string) string {
	val, exists := LookupEnv(name)
	if !exists {
		return defaultVal
	}
	return val
}

func getEnvAsDuration(name string, defaultVal time.Duration) (time.Duration, error) {
	valStr := getEnv(name, "")
	if valStr == "" {
		return defaultVal, nil
	}
	val, err := time.ParseDuration(valStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for %s=%q: %w", name, valStr, err)
	}
	return val, nil
}

func getEnvAsInt(name string, defaultVal int) (int, error) {
	valStr := getEnv(name, "")
	if valStr == "" {
		return defaultVal, nil
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("invalid integer for %s=%q: %w", name, valStr, err)
	}
	return val, nil
}

func getEnvAsFloat32(name string, defaultVal float32) (float32, error) {
	valStr := getEnv(name, "")
	if valStr == "" {
		return defaultVal, nil
	}
	val, err := strconv.ParseFloat(valStr, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid float for %s=%q: %w", name, valStr, err)
	}
	return float32(val), nil
}
