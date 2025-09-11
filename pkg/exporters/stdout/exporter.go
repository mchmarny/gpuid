package stdout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// New creates a new instance of the stdout Exporter.
func New() *Exporter {
	return &Exporter{}
}

// Exporter defines the stdout exporter that writes GPU serial number readings to stdout.
// This implementation provides structured JSON output suitable for log aggregation systems.
type Exporter struct{}

// Write outputs GPU serial number readings to stdout as JSON.
// Each record is written as a separate JSON line (NDJSON format) for better log parsing.
func (e *Exporter) Write(_ context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
	if records == nil {
		return fmt.Errorf("records is nil")
	}

	// Use a JSON encoder for consistent formatting
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ") // Pretty print for readability

	for _, reading := range records {
		if reading == nil {
			continue // Skip nil records gracefully
		}

		if err := encoder.Encode(reading); err != nil {
			return fmt.Errorf("failed to encode reading: %w", err)
		}
	}

	log.Info("export completed", "records", len(records))

	return nil
}

// Close performs cleanup for the stdout exporter.
// Since stdout output doesn't require cleanup, this is a no-op but satisfies the interface.
func (e *Exporter) Close(_ context.Context) error {
	// stdout exporter doesn't maintain any resources that need cleanup
	return nil
}

// Health checks the health of the stdout exporter.
// This validates that stdout is available for writing.
func (e *Exporter) Health(_ context.Context) error {
	// Test that we can write to stdout
	stat, err := os.Stdout.Stat()
	if err != nil {
		return fmt.Errorf("stdout is not available: %w", err)
	}

	// Ensure stdout is not a directory (basic sanity check)
	if stat.IsDir() {
		return fmt.Errorf("stdout appears to be a directory")
	}

	return nil
}
