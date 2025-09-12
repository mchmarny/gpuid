package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// Environment variable names for HTTP configuration
const (
	EnvHTTPEndpoint  = "HTTP_ENDPOINT"
	EnvHTTPTimeout   = "HTTP_TIMEOUT"
	EnvHTTPAuthToken = "HTTP_AUTH_TOKEN" // #nosec G101 - this is an environment variable name, not a credential
)

// Default configuration values
const (
	DefaultTimeout = 30 * time.Second
)

// Exporter implements the ExporterBackend interface for HTTP streaming.
// This exporter provides simple HTTP POST functionality for sending GPU data
// to HTTP endpoints with JSON serialization.
type Exporter struct {
	Endpoint  string        `json:"endpoint" yaml:"endpoint"`
	Timeout   time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	AuthToken string        `json:"auth_token,omitempty" yaml:"auth_token,omitempty"`

	client *http.Client
}

// Validate ensures the HTTP configuration is valid for operations.
func (e *Exporter) Validate() error {
	if strings.TrimSpace(e.Endpoint) == "" {
		return fmt.Errorf("HTTP endpoint URL is required")
	}

	if !strings.HasPrefix(e.Endpoint, "http://") && !strings.HasPrefix(e.Endpoint, "https://") {
		return fmt.Errorf("HTTP endpoint must be a valid HTTP/HTTPS URL")
	}

	if e.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", e.Timeout)
	}

	return nil
}

// New creates a new HTTP exporter with configuration loaded from environment variables.
// This follows the 12-factor app methodology for containerized deployments.
// Required environment variables:
//   - HTTP_ENDPOINT: HTTP endpoint URL for sending GPU serial data
//
// Optional environment variables:
//   - HTTP_TIMEOUT: Request timeout in seconds - defaults to 30
//   - HTTP_AUTH_TOKEN: Bearer token for authentication
func New(_ context.Context) (*Exporter, error) {
	exp := &Exporter{
		Endpoint:  getEnv(EnvHTTPEndpoint, ""),
		Timeout:   parseDuration(getEnv(EnvHTTPTimeout, "30s"), DefaultTimeout),
		AuthToken: getEnv(EnvHTTPAuthToken, ""),
	}

	if err := exp.Validate(); err != nil {
		return nil, fmt.Errorf("HTTP configuration validation failed: %w", err)
	}

	// Create HTTP client
	exp.client = &http.Client{
		Timeout: exp.Timeout,
	}

	return exp, nil
}

// Write sends GPU serial number readings to HTTP endpoint as JSON.
func (e *Exporter) Write(ctx context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
	if len(records) == 0 {
		return nil
	}

	// Serialize records to JSON
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("failed to serialize records: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gpuid-http-exporter/1.0")
	req.Header.Set("X-Records-Count", strconv.Itoa(len(records)))
	req.Header.Set("X-Timestamp", time.Now().UTC().Format(time.RFC3339))

	// Add authentication if configured
	if e.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.AuthToken)
	}

	// Send request
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	log.Info("export completed",
		"endpoint", e.Endpoint,
		"records", len(records),
		"size_bytes", len(data),
		"status", resp.StatusCode)

	return nil
}

// Close performs cleanup of HTTP client resources.
func (e *Exporter) Close(_ context.Context) error {
	if e == nil || e.client == nil {
		return fmt.Errorf("exporter or client is nil")
	}
	if e.client != nil {
		e.client.CloseIdleConnections()
	}
	return nil
}

// Health performs a health check by sending a HEAD request to the configured endpoint.
func (e *Exporter) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, e.Endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Add authentication if configured
	if e.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.AuthToken)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send health check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

// Helper functions

// getEnv retrieves an environment variable value with a fallback default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseDuration parses a duration string with fallback to default
func parseDuration(s string, defaultValue time.Duration) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return defaultValue
}
