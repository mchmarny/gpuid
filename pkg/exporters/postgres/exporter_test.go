package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// TestExporter_Configuration tests that the PostgreSQL exporter validates configuration correctly
func TestExporter_Configuration(t *testing.T) {
	// Test configuration validation without requiring actual database connection

	testCases := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: false,
		},
		{
			name: "missing_host",
			config: Config{
				Port:     5432,
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres host is required",
		},
		{
			name: "missing_database",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres database name is required",
		},
		{
			name: "missing_user",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres user is required",
		},
		{
			name: "missing_password",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				User:     "testuser",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres password is required",
		},
		{
			name: "invalid_port_zero",
			config: Config{
				Host:     "localhost",
				Port:     0,
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres port must be between 1 and 65535",
		},
		{
			name: "invalid_port_high",
			config: Config{
				Host:     "localhost",
				Port:     70000,
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "gpu_readings",
			},
			expectError: true,
			errorMsg:    "postgres port must be between 1 and 65535",
		},
		{
			name: "empty_table",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "require",
				Table:    "",
			},
			expectError: true,
			errorMsg:    "postgres table name cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tc.errorMsg != "" && !contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error message to contain %q, got: %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestConfig_ConnectionString tests that connection strings are built correctly
func TestConfig_ConnectionString(t *testing.T) {
	config := Config{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		User:     "testuser",
		Password: "testpass",
		SSLMode:  "require",
		Table:    "gpu_readings",
	}

	connStr := config.ConnectionString()
	expected := "host=localhost port=5432 dbname=testdb user=testuser password=testpass sslmode=require"

	if connStr != expected {
		t.Errorf("Expected connection string %q, got %q", expected, connStr)
	}
}

// TestLoadConfigFromEnv tests environment variable loading (without actually setting env vars)
func TestLoadConfigFromEnv(t *testing.T) {
	// This test verifies the loadConfigFromEnv function structure
	// In a real test environment, you would set environment variables first

	// Test default values
	config := loadConfigFromEnv()

	// Verify default values are applied
	if config.Port != defaultPostgresPort {
		t.Errorf("Expected default port %d, got %d", defaultPostgresPort, config.Port)
	}

	if config.SSLMode != defaultSSLMode {
		t.Errorf("Expected default SSL mode %q, got %q", defaultSSLMode, config.SSLMode)
	}

	if config.Table != defaultPostgresTable {
		t.Errorf("Expected default table %q, got %q", defaultPostgresTable, config.Table)
	}
}

// TestExporter_DataPreparation tests data handling without database operations
func TestExporter_DataPreparation(t *testing.T) {
	// Test data preparation logic that doesn't require database connection

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
		nil, // Test nil record handling
	}

	// Test that we have the expected number of valid records
	validRecords := 0
	for _, record := range records {
		if record != nil {
			validRecords++
		}
	}

	expectedValid := 2
	if validRecords != expectedValid {
		t.Errorf("Expected %d valid records, got %d", expectedValid, validRecords)
	}

	// Test record validation
	for i, record := range records {
		if record == nil {
			continue
		}

		if err := record.Validate(); err != nil {
			t.Errorf("Record %d failed validation: %v", i, err)
		}
	}

	t.Logf("Would insert %d valid records into PostgreSQL", validRecords)
}

// TestGetEnvFunctions tests the utility functions for environment variable handling
func TestGetEnvFunctions(t *testing.T) {
	// Test getEnv with default
	result := getEnv("NONEXISTENT_VAR", "default_value")
	if result != "default_value" {
		t.Errorf("Expected default value, got %q", result)
	}

	// Test getEnvAsInt with default
	intResult := getEnvAsInt("NONEXISTENT_INT_VAR", 42)
	if intResult != 42 {
		t.Errorf("Expected default int value 42, got %d", intResult)
	}
}

// TestConstants tests that the constants are defined correctly
func TestConstants(t *testing.T) {
	// Test that environment variable names are defined
	expectedEnvVars := map[string]string{
		"POSTGRES_HOST":     EnvPostgresHost,
		"POSTGRES_PORT":     EnvPostgresPort,
		"POSTGRES_DB":       EnvPostgresDB,
		"POSTGRES_USER":     EnvPostgresUser,
		"POSTGRES_PASSWORD": EnvPostgresPassword,
		"POSTGRES_SSLMODE":  EnvPostgresSSLMode,
		"POSTGRES_TABLE":    EnvPostgresTable,
	}

	for expected, actual := range expectedEnvVars {
		if actual != expected {
			t.Errorf("Expected environment variable %q, got %q", expected, actual)
		}
	}

	// Test default values
	if defaultPostgresPort != 5432 {
		t.Errorf("Expected default port 5432, got %d", defaultPostgresPort)
	}

	if defaultSSLMode != "require" {
		t.Errorf("Expected default SSL mode 'require', got %q", defaultSSLMode)
	}

	if defaultPostgresTable != "gpu" {
		t.Errorf("Expected default table 'gpu', got %q", defaultPostgresTable)
	}
}

// TestInsertQueryTemplate tests that the SQL template is valid
func TestInsertQueryTemplate(t *testing.T) {
	tableName := "test_table"
	query := fmt.Sprintf(insertQueryTemplate, tableName)

	expectedStart := "INSERT INTO test_table"
	if !hasPrefix(query, expectedStart) {
		t.Errorf("Expected query to start with %q, got: %q", expectedStart, query)
	}

	// Count the number of placeholders
	placeholderCount := 0
	for i := 1; i <= 10; i++ {
		placeholder := fmt.Sprintf("$%d", i)
		if contains(query, placeholder) {
			placeholderCount++
		}
	}

	expectedPlaceholders := 7 // cluster, node, machine, source, gpu, read_time, created_at
	if placeholderCount != expectedPlaceholders {
		t.Errorf("Expected %d placeholders in query, found %d", expectedPlaceholders, placeholderCount)
	}
}

// Helper functions for testing

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexOfSubstring(s, substr) >= 0))
}

// hasPrefix checks if string s has the given prefix
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// indexOfSubstring finds the index of substring in string (simple implementation)
func indexOfSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
