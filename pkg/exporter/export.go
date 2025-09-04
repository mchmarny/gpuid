package exporter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mchmarny/gpuid/pkg/exporter/console"
	"github.com/mchmarny/gpuid/pkg/gpu"
	corev1 "k8s.io/api/core/v1"
)

var (
	exporters = map[string]Exporter{
		"stdout": console.Export,
	}
)

// Exporter defines the function signature for exporting GPU serial number readings.
type Exporter func(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error

// Export handles exporting GPU serial numbers for a given pod using the specified exporter type.
// It validates inputs, logs the export process, and invokes the appropriate exporter function.
// Returns an error if validation fails or if the export operation encounters an issue.
func Export(ctx context.Context, log *slog.Logger, exporterType, cluster string, pod *corev1.Pod, serials []string) error {
	if strings.TrimSpace(exporterType) == "" {
		return fmt.Errorf("exporter type is required")
	}

	exporterFn, ok := exporters[exporterType]
	if !ok {
		return fmt.Errorf("unknown exporter type: %s", exporterType)
	}

	if strings.TrimSpace(cluster) == "" {
		return fmt.Errorf("cluster name is required")
	}

	if pod == nil {
		return fmt.Errorf("pod is nil")
	}
	if len(serials) == 0 {
		log.Info("no serial numbers found, skipping export", "ns", pod.Namespace, "pod", pod.Name)
		return nil
	}

	// Use default logger if none provided
	if log == nil {
		log = slog.Default()
	}

	log.Debug("exporting serial numbers",
		"ns", pod.Namespace,
		"pod", pod.Name,
		"exporter", exporterType,
		"serial_numbers", strings.Join(serials, ","),
	)

	records := make([]*gpu.SerialNumberReading, 0)
	for _, sn := range serials {
		records = append(records, &gpu.SerialNumberReading{
			Cluster:  cluster,
			Node:     pod.Spec.NodeName,
			Machine:  "",
			Source:   fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			GPU:      sn,
			ReadTime: time.Now().UTC(),
		})
	}

	if err := exporterFn(ctx, log, records); err != nil {
		return fmt.Errorf("exporter %s failed: %w", exporterType, err)
	}

	log.Debug("export completed successfully", "exporter", exporterType)
	return nil
}
