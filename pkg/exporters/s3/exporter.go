package s3

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mchmarny/gpuid/pkg/gpu"
)

// Environment variable names for S3 configuration
const (
	EnvS3Bucket           = "S3_BUCKET"
	EnvS3Prefix           = "S3_PREFIX"
	EnvS3Region           = "S3_REGION"
	EnvS3PartitionPattern = "S3_PARTITION_PATTERN"
)

// Exporter implements the ExporterBackend interface for Amazon S3.
// This exporter supports time-based partitioning and efficient batch uploads
// suitable for high-throughput distributed GPU monitoring systems.
type Exporter struct {
	Bucket           string `json:"bucket" yaml:"bucket"`
	Prefix           string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Region           string `json:"region,omitempty" yaml:"region,omitempty"`
	PartitionPattern string `json:"partition_pattern,omitempty" yaml:"partition_pattern,omitempty"`

	client *s3.Client
}

// Validate ensures the S3 configuration is valid for operations.
func (e *Exporter) Validate() error {
	if strings.TrimSpace(e.Bucket) == "" {
		return fmt.Errorf("S3 bucket name is required")
	}

	if strings.TrimSpace(e.Region) == "" {
		return fmt.Errorf("AWS region is required")
	}

	return nil
}

// New creates a new S3 exporter with configuration loaded from environment variables.
// This follows the 12-factor app methodology for containerized deployments.
// Required environment variables:
//   - S3_BUCKET: S3 bucket name for storing GPU serial data
//   - S3_REGION: AWS region (defaults to us-east-1 if not set)
//
// Optional environment variables:
//   - S3_PREFIX: Object key prefix for organizing data
//   - S3_PARTITION_PATTERN: Custom partitioning pattern (defaults to year=%Y/month=%m/day=%d/hour=%H)
func New(ctx context.Context) (*Exporter, error) {
	exp := &Exporter{
		Bucket:           getEnv(EnvS3Bucket, ""),
		Prefix:           getEnv(EnvS3Prefix, ""),
		Region:           getEnv(EnvS3Region, "us-east-1"), // Default AWS region
		PartitionPattern: getEnv(EnvS3PartitionPattern, ""),
	}

	if err := exp.Validate(); err != nil {
		return nil, fmt.Errorf("S3 configuration validation failed: %w", err)
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(exp.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	exp.client = s3.NewFromConfig(cfg)

	return exp, nil
}

// Write uploads GPU serial number readings to S3 with time-based partitioning.
// Records are batched and uploaded as headerless CSV for DMS compatibility and efficient processing.
func (e *Exporter) Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
	if len(records) == 0 {
		return nil
	}

	// Create time-partitioned S3 key for efficient data organization
	timestamp := time.Now().UTC()
	key := e.generateS3Key(timestamp)

	// Serialize records to CSV format for efficient processing
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)

	for _, record := range records {
		if record == nil {
			continue
		}

		if err := writer.Write(record.Slice()); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	// Flush the writer to ensure all data is written to buffer
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV writer: %w", err)
	}

	// Upload to S3
	if _, err := e.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(e.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buffer.Bytes()),
		ContentType: aws.String("text/csv"),
		Metadata: map[string]string{
			"source":       "gpuid",
			"record_count": fmt.Sprintf("%d", len(records)),
			"timestamp":    timestamp.Format(time.RFC3339),
			"format":       "csv",
			"columns":      "cluster,node,machine,source,gpu,time", // Column order for DMS mapping
		},
	}); err != nil {
		return fmt.Errorf("failed to upload records to S3: %w", err)
	}

	log.Info("export completed",
		"bucket", e.Bucket,
		"key", key,
		"records", len(records),
		"size_bytes", buffer.Len())

	return nil
}

// Close performs cleanup of S3 client resources.
func (e *Exporter) Close(_ context.Context) error {
	return nil
}

// Health performs a health check by attempting to access the configured S3 bucket.
func (e *Exporter) Health(ctx context.Context) error {
	if _, err := e.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(e.Bucket),
	}); err != nil {
		return fmt.Errorf("failed to access S3 bucket %s: %w", e.Bucket, err)
	}

	return nil
}

// generateS3Key creates a time-partitioned S3 key for efficient data organization.
// Default pattern: prefix/year=YYYY/month=MM/day=DD/hour=HH/timestamp.csv
func (e *Exporter) generateS3Key(timestamp time.Time) string {
	pattern := e.PartitionPattern
	if pattern == "" {
		pattern = "year=%Y/month=%m/day=%d/hour=%H"
	}

	// Replace time format placeholders
	pattern = strings.ReplaceAll(pattern, "%Y", fmt.Sprintf("%04d", timestamp.Year()))
	pattern = strings.ReplaceAll(pattern, "%m", fmt.Sprintf("%02d", timestamp.Month()))
	pattern = strings.ReplaceAll(pattern, "%d", fmt.Sprintf("%02d", timestamp.Day()))
	pattern = strings.ReplaceAll(pattern, "%H", fmt.Sprintf("%02d", timestamp.Hour()))

	filename := fmt.Sprintf("%s.csv", timestamp.Format("20060102-150405-000"))

	if e.Prefix != "" {
		return fmt.Sprintf("%s/%s/%s", e.Prefix, pattern, filename)
	}

	return fmt.Sprintf("%s/%s", pattern, filename)
}

// getEnv retrieves an environment variable value with a fallback default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
