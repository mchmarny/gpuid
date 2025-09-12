package runner

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"testing"
	"time"
)

const testClusterName = "test-cluster"

func TestOptionFunctions(t *testing.T) {
	tests := []struct {
		name     string
		option   Option
		expected func(*Command) bool
	}{
		{
			name:   "WithExporterType",
			option: WithExporterType("test-exporter"),
			expected: func(c *Command) bool {
				return c.ExporterType == "test-exporter"
			},
		},
		{
			name:   "WithClusterName",
			option: WithClusterName(testClusterName),
			expected: func(c *Command) bool {
				return c.Cluster == testClusterName
			},
		},
		{
			name:   "WithNamespace",
			option: WithNamespace("test-namespace"),
			expected: func(c *Command) bool {
				return c.Namespace == "test-namespace"
			},
		},
		{
			name:   "WithPodLabelSelector",
			option: WithPodLabelSelector("app=test"),
			expected: func(c *Command) bool {
				return c.PodLabelSelector == "app=test"
			},
		},
		{
			name:   "WithContainer",
			option: WithContainer("test-container"),
			expected: func(c *Command) bool {
				return c.Container == "test-container"
			},
		},
		{
			name:   "WithWorkers",
			option: WithWorkers(5),
			expected: func(c *Command) bool {
				return c.Workers == 5
			},
		},
		{
			name:   "WithTimeout",
			option: WithTimeout(30 * time.Second),
			expected: func(c *Command) bool {
				return c.Timeout == 30*time.Second
			},
		},
		{
			name:   "WithResync",
			option: WithResync(5 * time.Minute),
			expected: func(c *Command) bool {
				return c.Resync == 5*time.Minute
			},
		},
		{
			name:   "WithQPS",
			option: WithQPS(25.5),
			expected: func(c *Command) bool {
				return c.QPS == 25.5
			},
		},
		{
			name:   "WithBurst",
			option: WithBurst(150),
			expected: func(c *Command) bool {
				return c.Burst == 150
			},
		},
		{
			name:   "WithKubeconfig",
			option: WithKubeconfig("/path/to/kubeconfig"),
			expected: func(c *Command) bool {
				return c.Kubeconfig == "/path/to/kubeconfig"
			},
		},
		{
			name:   "WithLogLevel",
			option: WithLogLevel("debug"),
			expected: func(c *Command) bool {
				return c.LogLevel == "debug"
			},
		},
		{
			name:   "WithServerPort",
			option: WithServerPort(9090),
			expected: func(c *Command) bool {
				return c.ServerPort == 9090
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{}
			tt.option(cmd)
			if !tt.expected(cmd) {
				t.Errorf("Option %s did not set expected value", tt.name)
			}
		})
	}
}

func TestNewCommand(t *testing.T) {
	// Test with no options (should use defaults)
	cmd := NewCommand()
	if cmd == nil {
		t.Fatal("NewCommand() returned nil")
	}

	// Verify some default values
	if cmd.ExporterType != DefaultExporterType {
		t.Errorf("Expected ExporterType %s, got %s", DefaultExporterType, cmd.ExporterType)
	}
	if cmd.Workers != DefaultWorkers {
		t.Errorf("Expected Workers %d, got %d", DefaultWorkers, cmd.Workers)
	}

	// Test with options
	cmd2 := NewCommand(
		WithExporterType("custom"),
		WithWorkers(10),
		WithClusterName(testClusterName),
	)

	if cmd2.ExporterType != "custom" {
		t.Errorf("Expected ExporterType 'custom', got %s", cmd2.ExporterType)
	}
	if cmd2.Workers != 10 {
		t.Errorf("Expected Workers 10, got %d", cmd2.Workers)
	}
	if cmd2.Cluster != testClusterName {
		t.Errorf("Expected Cluster %s, got %s", testClusterName, cmd2.Cluster)
	}
}

func TestCommand_Validate(t *testing.T) {
	tests := []struct {
		name    string
		command *Command
		wantErr bool
	}{
		{
			name: "valid command",
			command: &Command{
				ExporterType:     "stdout",
				Cluster:          testClusterName,
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          3,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: false,
		},
		{
			name: "missing exporter type",
			command: &Command{
				Cluster:          "test-cluster",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          3,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: true,
		},
		{
			name: "missing cluster",
			command: &Command{
				ExporterType:     "stdout",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          3,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: true,
		},
		{
			name: "invalid workers",
			command: &Command{
				ExporterType:     "stdout",
				Cluster:          "test-cluster",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          0,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: true,
		},
		{
			name: "too many workers",
			command: &Command{
				ExporterType:     "stdout",
				Cluster:          "test-cluster",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          101,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: true,
		},
		{
			name: "invalid timeout",
			command: &Command{
				ExporterType:     "stdout",
				Cluster:          "test-cluster",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          3,
				Timeout:          0,
				QPS:              20,
				Burst:            50,
				ServerPort:       8080,
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			command: &Command{
				ExporterType:     "stdout",
				Cluster:          "test-cluster",
				Namespace:        "default",
				PodLabelSelector: "app=test",
				Container:        "main",
				Workers:          3,
				Timeout:          30 * time.Second,
				QPS:              20,
				Burst:            50,
				ServerPort:       999,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.command.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommand_Init(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	tests := []struct {
		name         string
		exporterType string
		wantErr      bool
	}{
		{
			name:         "valid exporter type",
			exporterType: "stdout",
			wantErr:      false,
		},
		{
			name:         "empty exporter type",
			exporterType: "",
			wantErr:      true,
		},
		{
			name:         "invalid exporter type",
			exporterType: "nonexistent",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{
				ExporterType: tt.exporterType,
			}
			err := cmd.Init(ctx, log)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListEnvVars(t *testing.T) {
	envVars := ListEnvVars()
	if len(envVars) == 0 {
		t.Error("ListEnvVars() returned empty slice")
	}

	expectedVars := []string{
		EnvVarExporterType,
		EnvVarClusterName,
		EnvVarNamespace,
		EnvVarPodLabelSelector,
		EnvVarContainer,
		EnvVarWorkers,
		EnvVarTimeout,
		EnvVarResync,
		EnvVarQPS,
		EnvVarBurst,
		EnvVarKubeconfig,
		EnvVarLogLevel,
		EnvVarServerPort,
	}

	if !reflect.DeepEqual(envVars, expectedVars) {
		t.Errorf("ListEnvVars() = %v, want %v", envVars, expectedVars)
	}
}

func TestNewCommandFromEnvVars(t *testing.T) {
	// Save original env vars and restore them after test
	originalVars := make(map[string]string)
	for _, envVar := range ListEnvVars() {
		originalVars[envVar] = os.Getenv(envVar)
	}
	defer func() {
		for envVar, value := range originalVars {
			if value == "" {
				os.Unsetenv(envVar)
			} else {
				os.Setenv(envVar, value)
			}
		}
	}()

	// Set test environment variables
	os.Setenv(EnvVarExporterType, "stdout")
	os.Setenv(EnvVarClusterName, testClusterName)
	os.Setenv(EnvVarWorkers, "5")

	cmd := NewCommandFromEnvVars()
	if cmd == nil {
		t.Fatal("NewCommandFromEnvVars() returned nil")
	}

	if cmd.ExporterType != "stdout" {
		t.Errorf("Expected ExporterType 'stdout', got %s", cmd.ExporterType)
	}
	if cmd.Cluster != testClusterName {
		t.Errorf("Expected Cluster %s, got %s", testClusterName, cmd.Cluster)
	}
	if cmd.Workers != 5 {
		t.Errorf("Expected Workers 5, got %d", cmd.Workers)
	}
}

func TestLookupEnv(t *testing.T) {
	// Test with existing environment variable
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	value, exists := LookupEnv("TEST_VAR")
	if !exists {
		t.Error("LookupEnv() should return true for existing env var")
	}
	if value != "test_value" {
		t.Errorf("LookupEnv() returned %s, want test_value", value)
	}

	// Test with non-existing environment variable
	value, exists = LookupEnv("NON_EXISTENT_VAR")
	if exists {
		t.Error("LookupEnv() should return false for non-existent env var")
	}
	if value != "" {
		t.Errorf("LookupEnv() should return empty string for non-existent env var, got %s", value)
	}
}
