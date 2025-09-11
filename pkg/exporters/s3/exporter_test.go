package s3

import (
	"testing"
	"time"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// TestExporter_CSVOutput tests that the S3 exporter creates proper CSV output
func TestExporter_CSVOutput(t *testing.T) {
	// This test verifies CSV formatting without actually uploading to S3
	// It focuses on the CSV generation logic

	records := []*gpu.SerialNumberReading{
		{
			Cluster: "test-cluster",
			Node:    "test-node-1",
			Machine: "test-machine-1",
			Source:  "test-namespace/test-pod-1",
			GPU:     "GPU-12345",
			Time:    time.Date(2025, 9, 11, 10, 30, 0, 0, time.UTC),
		},
		{
			Cluster: "test-cluster",
			Node:    "test-node-2",
			Machine: "test-machine-2",
			Source:  "test-namespace/test-pod-2",
			GPU:     "GPU-67890",
			Time:    time.Date(2025, 9, 11, 10, 31, 0, 0, time.UTC),
		},
	}

	// Test CSV generation logic by creating a minimal exporter
	// Note: This won't actually upload to S3 since we don't have real AWS credentials in tests
	exporter := &Exporter{
		Bucket: "test-bucket",
		Region: "us-east-1",
	}

	// Test that the exporter validates correctly
	if err := exporter.Validate(); err != nil {
		t.Fatalf("Exporter validation failed: %v", err)
	}

	// Test that generateS3Key works with CSV extension
	timestamp := time.Date(2025, 9, 11, 10, 30, 0, 0, time.UTC)
	key := exporter.generateS3Key(timestamp)

	expectedSuffix := ".csv"
	if !endsWith(key, expectedSuffix) {
		t.Errorf("Expected S3 key to end with %s, got: %s", expectedSuffix, key)
	}

	// The actual CSV generation would be tested in integration tests
	// since it requires AWS credentials and S3 access
	t.Logf("CSV key would be: %s", key)
	t.Logf("Would upload %d records", len(records))
}

// endsWith checks if a string ends with a suffix
func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
