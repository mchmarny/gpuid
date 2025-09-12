package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

func TestExporter_Configuration(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		timeout     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid_config",
			endpoint:    "https://api.example.com/gpu-data",
			timeout:     "30s",
			expectError: false,
		},
		{
			name:        "missing_endpoint",
			endpoint:    "",
			expectError: true,
			errorMsg:    "HTTP endpoint URL is required",
		},
		{
			name:        "invalid_endpoint_format",
			endpoint:    "not-a-url",
			expectError: true,
			errorMsg:    "HTTP endpoint must be a valid HTTP/HTTPS URL",
		},
		{
			name:        "http_endpoint_allowed",
			endpoint:    "http://localhost:8080/gpu-data",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(EnvHTTPEndpoint, tt.endpoint)
			if tt.timeout != "" {
				os.Setenv(EnvHTTPTimeout, tt.timeout)
			}

			defer func() {
				os.Unsetenv(EnvHTTPEndpoint)
				os.Unsetenv(EnvHTTPTimeout)
			}()

			_, err := New(context.Background())

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExporter_EnvironmentVariableLoading(t *testing.T) {
	os.Setenv(EnvHTTPEndpoint, "https://api.example.com/data")
	os.Setenv(EnvHTTPTimeout, "45s")
	os.Setenv(EnvHTTPAuthToken, "test-token")

	defer func() {
		os.Unsetenv(EnvHTTPEndpoint)
		os.Unsetenv(EnvHTTPTimeout)
		os.Unsetenv(EnvHTTPAuthToken)
	}()

	exporter, err := New(context.Background())
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	if exporter.Endpoint != "https://api.example.com/data" {
		t.Errorf("Expected endpoint %q, got %q", "https://api.example.com/data", exporter.Endpoint)
	}

	if exporter.Timeout != 45*time.Second {
		t.Errorf("Expected timeout %v, got %v", 45*time.Second, exporter.Timeout)
	}

	if exporter.AuthToken != "test-token" {
		t.Errorf("Expected auth token %q, got %q", "test-token", exporter.AuthToken)
	}
}

func TestExporter_HTTPServerIntegration(t *testing.T) {
	var receivedData []byte
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()

		body := make([]byte, r.ContentLength)
		_, err := r.Body.Read(body)
		if err != nil && err.Error() != "EOF" {
			t.Errorf("Failed to read request body: %v", err)
		}
		receivedData = body

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	exporter := &Exporter{
		Endpoint:  server.URL,
		Timeout:   10 * time.Second,
		AuthToken: "test-token",
		client:    server.Client(),
	}

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

	ctx := context.Background()
	logger := slog.Default()

	err := exporter.Write(ctx, logger, records)
	if err != nil {
		t.Fatalf("Failed to write records: %v", err)
	}

	if len(receivedData) == 0 {
		t.Error("Expected to receive data, got none")
	}

	if auth := receivedHeaders.Get("Authorization"); auth != "Bearer test-token" {
		t.Errorf("Expected Authorization header %q, got %q", "Bearer test-token", auth)
	}

	if contentType := receivedHeaders.Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type %q, got %q", "application/json", contentType)
	}

	if recordCount := receivedHeaders.Get("X-Records-Count"); recordCount != "1" {
		t.Errorf("Expected X-Records-Count %q, got %q", "1", recordCount)
	}

	t.Logf("Successfully sent %d records to HTTP server", len(records))
}

func TestExporter_HealthCheck(t *testing.T) {
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer healthyServer.Close()

	exporter := &Exporter{
		Endpoint: healthyServer.URL,
		client:   healthyServer.Client(),
	}

	err := exporter.Health(context.Background())
	if err != nil {
		t.Errorf("Health check should pass, got error: %v", err)
	}

	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unhealthyServer.Close()

	exporter.Endpoint = unhealthyServer.URL
	exporter.client = unhealthyServer.Client()

	err = exporter.Health(context.Background())
	if err == nil {
		t.Error("Health check should fail for server returning 500")
	}
}

func TestExporter_EmptyRecords(t *testing.T) {
	exporter := &Exporter{
		Endpoint: "https://example.com",
		Timeout:  10 * time.Second,
		client:   http.DefaultClient,
	}

	ctx := context.Background()
	logger := slog.Default()

	err := exporter.Write(ctx, logger, nil)
	if err != nil {
		t.Errorf("Write with nil records should not error, got: %v", err)
	}

	err = exporter.Write(ctx, logger, []*gpu.SerialNumberReading{})
	if err != nil {
		t.Errorf("Write with empty records should not error, got: %v", err)
	}
}

func TestHelperFunctions(t *testing.T) {
	// Test getEnv
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	if getEnv("TEST_VAR", "default") != "test_value" {
		t.Error("getEnv should return environment variable value")
	}

	if getEnv("NONEXISTENT_VAR", "default") != "default" {
		t.Error("getEnv should return default value for non-existent variable")
	}

	// Test parseDuration
	if parseDuration("30s", time.Second) != 30*time.Second {
		t.Error("parseDuration should parse valid duration")
	}

	if parseDuration("invalid", time.Minute) != time.Minute {
		t.Error("parseDuration should return default for invalid duration")
	}
}
