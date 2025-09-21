package runner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mchmarny/gpuid/pkg/exporters/http"
	"github.com/mchmarny/gpuid/pkg/exporters/postgres"
	"github.com/mchmarny/gpuid/pkg/exporters/s3"
	"github.com/mchmarny/gpuid/pkg/exporters/stdout"
	"github.com/mchmarny/gpuid/pkg/gpu"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultBatchSize  = 10
	defaultRetryCount = 2
	defaultTimeout    = 30 * time.Second
)

// ExporterConfig holds configuration for initializing exporters.
// Individual exporters load their specific configuration from environment variables,
type ExporterConfig struct {
	Type       string        `json:"type" yaml:"type"`
	BatchSize  int           `json:"batch_size,omitempty" yaml:"batch_size,omitempty"`
	RetryCount int           `json:"retry_count,omitempty" yaml:"retry_count,omitempty"`
	Timeout    time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// ExporterBackend defines the interface that all exporter implementations must satisfy.
type ExporterBackend interface {
	// Write exports the provided GPU serial number readings.
	Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error

	// Close performs cleanup of any resources held by the exporter.
	Close(ctx context.Context) error

	// Health performs a health check of the exporter's dependencies.
	Health(ctx context.Context) error
}

// Exporter wraps an ExporterBackend with metadata and provides a high-level interface
// for exporting GPU serial number data in distributed systems.
type Exporter struct {
	Type   string         `json:"type"`
	Config ExporterConfig `json:"config"`

	backend ExporterBackend
}

// GetExporter initializes an exporter based on the provided configuration.
func GetExporter(ctx context.Context, log *slog.Logger, config ExporterConfig) (*Exporter, error) {
	if strings.TrimSpace(config.Type) == "" {
		config.Type = "stdout" // Default to stdout if not specified
	}

	// Apply defaults for optional configuration
	if config.BatchSize <= 0 {
		config.BatchSize = defaultBatchSize
	}
	if config.RetryCount <= 0 {
		config.RetryCount = defaultRetryCount
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	log.Debug("initializing exporter",
		"type", config.Type,
		"batch_size", config.BatchSize,
		"retry_count", config.RetryCount,
		"timeout", config.Timeout)

	e := &Exporter{
		Type:   config.Type,
		Config: config,
	}

	var err error
	switch config.Type {
	case "stdout":
		e.backend = stdout.New()
	case "s3":
		e.backend, err = s3.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize S3 exporter: %w", err)
		}
	case "postgres":
		e.backend, err = postgres.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize PostgreSQL exporter: %w", err)
		}
	case "http":
		e.backend, err = http.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize HTTP exporter: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown exporter type: %s", config.Type)
	}

	// Validate the backend is properly initialized
	if e.backend == nil {
		return nil, fmt.Errorf("failed to initialize backend for type: %s", config.Type)
	}

	// Perform health check during initialization
	if err = e.backend.Health(ctx); err != nil {
		return nil, fmt.Errorf("exporter health check failed: %w", err)
	}

	return e, nil
}

// GetExporterSimple provides a backwards-compatible constructor for simple use cases.
func GetExporterSimple(ctx context.Context, log *slog.Logger, exporterType string) (*Exporter, error) {
	config := ExporterConfig{
		Type: exporterType,
	}
	return GetExporter(ctx, log, config)
}

// Export handles exporting GPU serial numbers for a given pod using the configured exporter.
func (e *Exporter) Export(ctx context.Context, log *slog.Logger, cluster string, pod *corev1.Pod, node string, serials []*gpu.Serials) error {
	if len(serials) == 0 {
		return nil
	}

	if e == nil || e.backend == nil {
		return fmt.Errorf("exporter not initialized")
	}

	if strings.TrimSpace(cluster) == "" {
		return fmt.Errorf("cluster name is required")
	}

	if pod == nil {
		return fmt.Errorf("pod is nil")
	}

	if strings.TrimSpace(node) == "" {
		return fmt.Errorf("node name required")
	}

	// Use default logger if none provided
	if log == nil {
		log = slog.Default()
	}

	log.Debug("exporting serial numbers",
		"ns", pod.Namespace,
		"pod", pod.Name,
		"node", node,
		"cluster", cluster,
		"serial_numbers", len(serials),
		"exporter_type", e.Type)

	// Transform serials into structured readings with proper provenance
	records := make([]*gpu.SerialNumberReading, 0)

	for _, cn := range serials {
		if cn == nil || cn.Chassis == "" {
			log.Warn("skipping empty or nil chassis serial number", "chassis", cn)
			continue
		}

		// Create a record for each GPU serial number associated with the chassis
		for _, sn := range cn.GPU {
			record := &gpu.SerialNumberReading{
				Cluster: cluster,
				Node:    pod.Spec.NodeName,
				Machine: node,
				Source:  fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
				Chassis: cn.Chassis,
				GPU:     sn,
				Time:    time.Now().UTC(),
			}

			// Validate record before adding to batch
			if err := record.Validate(); err != nil {
				log.Warn("skipping invalid record", "error", err, "serial", sn)
				continue
			}

			records = append(records, record)
		}
	}

	if len(records) == 0 {
		log.Info("no valid records to export after validation", "ns", pod.Namespace, "pod", pod.Name)
		return nil
	}

	// Delegate to backend with proper error context
	if err := e.backend.Write(ctx, log, records); err != nil {
		return fmt.Errorf("exporter backend failed (type=%s): %w", e.Type, err)
	}

	log.Debug("export completed successfully",
		"exporter_type", e.Type,
		"records_exported", len(records))

	return nil
}

// Close performs cleanup of the exporter's resources.
func (e *Exporter) Close(ctx context.Context) error {
	if e == nil || e.backend == nil {
		return nil
	}

	return e.backend.Close(ctx)
}

// Health performs a health check of the exporter's backend.
func (e *Exporter) Health(ctx context.Context) error {
	if e == nil || e.backend == nil {
		return fmt.Errorf("exporter is not initialized")
	}

	return e.backend.Health(ctx)
}
