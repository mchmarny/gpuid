package logger

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNewProductionLogger(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantNil bool
	}{
		{
			name: "basic config",
			config: Config{
				Service: "test-service",
				Version: "1.0.0",
				Env:     "test",
			},
			wantNil: false,
		},
		{
			name:    "empty config",
			config:  Config{},
			wantNil: false,
		},
		{
			name: "with level and source",
			config: Config{
				Service:   "test-service",
				Level:     "debug",
				AddSource: true,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewProductionLogger(tt.config)
			if (logger == nil) == !tt.wantNil {
				t.Errorf("NewProductionLogger() = %v, wantNil %v", logger == nil, tt.wantNil)
			}

			// Test that the logger can log without panicking
			if logger != nil {
				logger.Info("test log message")
			}
		})
	}
}

func TestSetDefault(t *testing.T) {
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger) // Restore original

	config := Config{
		Service: "test-service",
		Version: "1.0.0",
	}

	logger := SetDefault(config)
	if logger == nil {
		t.Fatal("SetDefault() returned nil")
	}

	// Verify that the default logger was actually set
	if slog.Default() == originalLogger {
		t.Error("SetDefault() did not change the default logger")
	}

	// Test that we can log with the new default
	slog.Info("test message")
}

func TestNewTestLogger(t *testing.T) {
	logger := NewTestLogger(t)
	if logger == nil {
		t.Fatal("NewTestLogger() returned nil")
	}

	// Test that the logger can log without panicking
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
}

func TestTestWriter_Write(t *testing.T) {
	writer := testWriter{t: t}

	testData := []byte("test message\n")
	n, err := writer.Write(testData)

	if err != nil {
		t.Errorf("Write() error = %v, want nil", err)
	}

	if n != len(testData) {
		t.Errorf("Write() returned %d, want %d", n, len(testData))
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"", slog.LevelInfo}, // default
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},    // unknown defaults to info
		{"  debug  ", slog.LevelDebug}, // trimmed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLevel(tt.input)
			// Compare the actual level values since Leveler is an interface
			if result.Level() != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result.Level(), tt.expected)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		vals     []string
		expected string
	}{
		{
			name:     "first non-empty",
			vals:     []string{"", "second", "third"},
			expected: "second",
		},
		{
			name:     "all empty",
			vals:     []string{"", "", ""},
			expected: "",
		},
		{
			name:     "whitespace only",
			vals:     []string{"  ", "\t", "actual"},
			expected: "actual",
		},
		{
			name:     "first is non-empty",
			vals:     []string{"first", "second"},
			expected: "first",
		},
		{
			name:     "empty slice",
			vals:     []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := firstNonEmpty(tt.vals...)
			if result != tt.expected {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.vals, result, tt.expected)
			}
		})
	}
}

func TestProductionLoggerWithEnvironmentVariable(t *testing.T) {
	// Test that LOG_LEVEL environment variable is respected
	originalLevel := os.Getenv("LOG_LEVEL")
	defer os.Setenv("LOG_LEVEL", originalLevel)

	os.Setenv("LOG_LEVEL", "debug")

	logger := NewProductionLogger(Config{})
	if logger == nil {
		t.Fatal("NewProductionLogger() returned nil")
	}

	// We can't easily test the actual level without accessing internals,
	// but we can test that the logger works
	logger.Debug("debug message")
}

func TestLoggerOutput(t *testing.T) {
	// Capture stderr output to test that production logger writes to stderr
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stderr = w

	// Create logger and log a message
	logger := NewProductionLogger(Config{
		Service: "test-service",
	})
	logger.Info("test message")

	// Close writer and restore stderr
	w.Close()
	os.Stderr = originalStderr

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output contains expected elements
	if !strings.Contains(output, "test message") {
		t.Error("Output does not contain log message")
	}
	if !strings.Contains(output, "test-service") {
		t.Error("Output does not contain service name")
	}
	if !strings.Contains(output, "level") {
		t.Error("Output does not contain level field")
	}
}
