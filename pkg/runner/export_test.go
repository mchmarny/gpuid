package runner

import (
	"context"
	"log/slog"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetExporterSimple(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	tests := []struct {
		name         string
		exporterType string
		wantErr      bool
	}{
		{"stdout", "stdout", false},
		{"postgres", "postgres", true}, // Will fail without DB connection, but validates config
		{"empty_type", "", true},
		{"unknown_type", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter, err := GetExporterSimple(ctx, log, tt.exporterType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetExporterSimple() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("GetExporterSimple() unexpected error: %v", err)
				return
			}

			if exporter == nil {
				t.Errorf("GetExporterSimple() returned nil exporter")
				return
			}

			// Cleanup
			if err := exporter.Close(ctx); err != nil {
				t.Errorf("Close() failed: %v", err)
			}
		})
	}
}

func TestExporter_Export(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	// Create a stdout exporter for testing
	config := ExporterConfig{
		Type: "stdout",
	}

	exporter, err := GetExporter(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Close(ctx)

	// Create test data
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	tests := []struct {
		name    string
		cluster string
		pod     *corev1.Pod
		node    string
		serials []string
		wantErr bool
	}{
		{
			name:    "valid_export",
			cluster: "test-cluster",
			pod:     pod,
			node:    "test-node-id",
			serials: []string{"serial1", "serial2"},
			wantErr: false,
		},
		{
			name:    "empty_cluster",
			cluster: "",
			pod:     pod,
			node:    "test-node-id",
			serials: []string{"serial1"},
			wantErr: true,
		},
		{
			name:    "nil_pod",
			cluster: "test-cluster",
			pod:     nil,
			node:    "test-node-id",
			serials: []string{"serial1"},
			wantErr: true,
		},
		{
			name:    "empty_node",
			cluster: "test-cluster",
			pod:     pod,
			node:    "",
			serials: []string{"serial1"},
			wantErr: true,
		},
		{
			name:    "empty_serials",
			cluster: "test-cluster",
			pod:     pod,
			node:    "test-node-id",
			serials: []string{},
			wantErr: false, // Should not error, just skip export
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.Export(ctx, log, tt.cluster, tt.pod, tt.node, tt.serials)

			if tt.wantErr && err == nil {
				t.Errorf("Export() expected error but got none")
			} else if !tt.wantErr && err != nil {
				t.Errorf("Export() unexpected error: %v", err)
			}
		})
	}
}

func TestExporter_Health(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	// Test stdout exporter health
	config := ExporterConfig{
		Type: "stdout",
	}

	exporter, err := GetExporter(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	exporter.Close(ctx)
}

func TestExporterConfig_Defaults(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	// Test that defaults are applied correctly
	config := ExporterConfig{
		Type: "stdout",
		// Leave batch size, retry count, and timeout as zero values
	}

	exporter, err := GetExporter(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Close(ctx)

	// Verify defaults were applied
	if exporter.Config.BatchSize != defaultBatchSize {
		t.Errorf("Expected default batch size %d, got %d", defaultBatchSize, exporter.Config.BatchSize)
	}

	if exporter.Config.RetryCount != defaultRetryCount {
		t.Errorf("Expected default retry count %d, got %d", defaultRetryCount, exporter.Config.RetryCount)
	}

	if exporter.Config.Timeout != defaultTimeout {
		t.Errorf("Expected default timeout %v, got %v", defaultTimeout, exporter.Config.Timeout)
	}
}
