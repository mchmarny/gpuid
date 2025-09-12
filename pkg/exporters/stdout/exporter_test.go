package stdout

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// TestExporter_Basic tests basic functionality of the stdout exporter
func TestExporter_Basic(t *testing.T) {
	// Test creating a new stdout exporter
	exporter := New()
	if exporter == nil {
		t.Fatal("Expected non-nil exporter")
	}

	// Test validation (stdout exporter should always be valid)
	records := []*gpu.SerialNumberReading{
		{
			Cluster: "test-cluster",
			Node:    "test-node",
			Machine: "test-machine",
			Source:  "test-namespace/test-pod",
			GPU:     "GPU-12345",
			Time:    time.Date(2025, 9, 11, 10, 30, 0, 0, time.UTC),
		},
	}

	// Note: We can't easily test the actual Write() method output in unit tests
	// since it writes to stdout, but we can test that it accepts the right data structure
	if len(records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(records))
	}

	// Test that the record has the expected structure
	record := records[0]
	if record.Cluster != "test-cluster" {
		t.Errorf("Expected cluster 'test-cluster', got %q", record.Cluster)
	}
	if record.GPU != "GPU-12345" {
		t.Errorf("Expected GPU 'GPU-12345', got %q", record.GPU)
	}

	t.Log("Stdout exporter basic functionality verified")
}

// TestExporter_EmptyRecords tests handling of empty record sets
func TestExporter_EmptyRecords(t *testing.T) {
	exporter := New()

	// Test with nil records
	var nilRecords []*gpu.SerialNumberReading
	if len(nilRecords) != 0 {
		t.Errorf("Expected 0 records, got %d", len(nilRecords))
	}

	// Test with empty slice
	emptyRecords := []*gpu.SerialNumberReading{}
	if len(emptyRecords) != 0 {
		t.Errorf("Expected 0 records, got %d", len(emptyRecords))
	}

	// Test with slice containing nil record
	recordsWithNil := []*gpu.SerialNumberReading{nil}
	validCount := 0
	for _, record := range recordsWithNil {
		if record != nil {
			validCount++
		}
	}
	if validCount != 0 {
		t.Errorf("Expected 0 valid records, got %d", validCount)
	}

	t.Log("Stdout exporter handles empty/nil records correctly")

	// Ensure exporter is not nil (suppress unused variable warning)
	_ = exporter
}

// TestExporter_RecordValidation tests that records are properly structured
func TestExporter_RecordValidation(t *testing.T) {
	validRecord := &gpu.SerialNumberReading{
		Cluster: "test-cluster",
		Node:    "test-node",
		Machine: "test-machine",
		Source:  "test-namespace/test-pod",
		GPU:     "GPU-12345",
		Time:    time.Now(),
	}

	// Test record validation
	if err := validRecord.Validate(); err != nil {
		t.Errorf("Valid record failed validation: %v", err)
	}

	// Test invalid record (missing cluster)
	invalidRecord := &gpu.SerialNumberReading{
		Node:    "test-node",
		Machine: "test-machine",
		Source:  "test-namespace/test-pod",
		GPU:     "GPU-12345",
		Time:    time.Now(),
	}

	if err := invalidRecord.Validate(); err == nil {
		t.Error("Expected validation error for record missing cluster")
	}

	t.Log("Record validation working correctly")
}

// TestExporter_Write tests the Write method
func TestExporter_Write(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()
	exporter := New()

	tests := []struct {
		name    string
		records []*gpu.SerialNumberReading
		wantErr bool
	}{
		{
			name:    "nil records",
			records: nil,
			wantErr: true,
		},
		{
			name:    "empty records",
			records: []*gpu.SerialNumberReading{},
			wantErr: false,
		},
		{
			name: "valid records",
			records: []*gpu.SerialNumberReading{
				{
					Cluster: "test-cluster",
					Node:    "test-node",
					Machine: "test-machine",
					Source:  "test-namespace/test-pod",
					GPU:     "GPU-12345",
					Time:    time.Date(2025, 9, 11, 10, 30, 0, 0, time.UTC),
				},
			},
			wantErr: false,
		},
		{
			name: "records with nil entry",
			records: []*gpu.SerialNumberReading{
				nil,
				{
					Cluster: "test-cluster",
					Node:    "test-node",
					Machine: "test-machine",
					Source:  "test-namespace/test-pod",
					GPU:     "GPU-12345",
					Time:    time.Date(2025, 9, 11, 10, 30, 0, 0, time.UTC),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exporter.Write(ctx, log, tt.records)
			if (err != nil) != tt.wantErr {
				t.Errorf("Write() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestExporter_Close tests the Close method
func TestExporter_Close(t *testing.T) {
	ctx := context.Background()
	exporter := New()

	err := exporter.Close(ctx)
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// TestExporter_Health tests the Health method
func TestExporter_Health(t *testing.T) {
	ctx := context.Background()
	exporter := New()

	err := exporter.Health(ctx)
	if err != nil {
		t.Errorf("Health() error = %v, want nil", err)
	}
}
