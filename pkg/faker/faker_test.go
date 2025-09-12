package faker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	// Create a temporary XML file for testing
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "test_smi.xml")
	xmlContent := `<?xml version="1.0"?>
<nvidia_smi_log>
    <attached_gpus>2</attached_gpus>
    <gpu id="00000000:04:00.0">
        <serial>1234567890</serial>
    </gpu>
    <gpu id="00000000:05:00.0">
        <serial>0987654321</serial>
    </gpu>
</nvidia_smi_log>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create test XML file: %v", err)
	}

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with XML file",
			config: Config{
				XMLFilePath: xmlFile,
				LogLevel:    "info",
			},
			wantErr: false,
		},
		{
			name: "config without XML file",
			config: Config{
				LogLevel: "debug",
			},
			wantErr: true,
		},
		{
			name: "config with non-existent XML file",
			config: Config{
				XMLFilePath: "/non/existent/file.xml",
				LogLevel:    "info",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			faker, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && faker == nil {
				t.Error("New() returned nil faker without error")
			}
		})
	}
}

func TestHandleNvidiaSMI(t *testing.T) {
	// Create faker with test XML content
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "test_smi.xml")
	xmlContent := `<?xml version="1.0"?><nvidia_smi_log><attached_gpus>1</attached_gpus></nvidia_smi_log>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create test XML file: %v", err)
	}

	faker, err := New(Config{
		XMLFilePath: xmlFile,
		LogLevel:    "info",
	})
	if err != nil {
		t.Fatalf("Failed to create faker: %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr bool
		wantXML bool
	}{
		{
			name:    "nvidia-smi -q -x",
			args:    []string{"-q", "-x"},
			wantErr: false,
			wantXML: true,
		},
		{
			name:    "nvidia-smi --version",
			args:    []string{"--version"},
			wantErr: false,
			wantXML: false,
		},
		{
			name:    "nvidia-smi with no args",
			args:    []string{},
			wantErr: false,
			wantXML: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := faker.HandleNvidiaSMI(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("HandleNvidiaSMI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantXML {
				if !strings.Contains(output, "nvidia_smi_log") {
					t.Error("Expected XML output but got different content")
				}
				if !strings.Contains(output, "attached_gpus") {
					t.Error("Expected XML to contain attached_gpus")
				}
			} else {
				if strings.Contains(output, "nvidia_smi_log") {
					t.Error("Expected non-XML output but got XML")
				}
			}
		})
	}
}

func TestGenerateNvidiaSMIScript(t *testing.T) {
	faker := &GPUFaker{
		config: Config{
			XMLFilePath: "/test/path/smi.xml",
		},
	}

	script := faker.generateNvidiaSMIScript()

	// Check that script contains expected elements
	expectedElements := []string{
		"#!/bin/bash",
		"-q -x",
		"/test/path/smi.xml",
		"cat",
		"nvidia-smi",
	}

	for _, element := range expectedElements {
		if !strings.Contains(script, element) {
			t.Errorf("Generated script missing expected element: %s", element)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string // We'll check the string representation
	}{
		{"debug", "DEBUG"},
		{"DEBUG", "DEBUG"},
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"ERROR", "ERROR"},
		{"invalid", "INFO"},    // defaults to INFO
		{"", "INFO"},           // defaults to INFO
		{"  debug  ", "DEBUG"}, // trimmed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := parseLogLevel(tt.input)
			if level.String() != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, level.String(), tt.expected)
			}
		})
	}
}

func TestExecuteCommand(t *testing.T) {
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "test.xml")
	xmlContent := `<?xml version="1.0"?><test/>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create XML file: %v", err)
	}

	faker, err := New(Config{
		XMLFilePath: xmlFile,
	})
	if err != nil {
		t.Fatalf("Failed to create faker: %v", err)
	}

	tests := []struct {
		name    string
		command string
		args    []string
		wantErr bool
	}{
		{
			name:    "nvidia-smi command",
			command: "nvidia-smi",
			args:    []string{"-q", "-x"},
			wantErr: false,
		},
		{
			name:    "echo command",
			command: "echo",
			args:    []string{"hello"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := faker.ExecuteCommand(tt.command, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.command == "nvidia-smi" && !strings.Contains(stdout, "test") {
				t.Error("nvidia-smi command did not return expected XML content")
			}

			if tt.command == "echo" && !strings.Contains(stdout, "hello") {
				t.Error("echo command did not return expected output")
			}

			// stderr should be empty for successful commands
			if !tt.wantErr && stderr != "" {
				t.Errorf("Unexpected stderr output: %s", stderr)
			}
		})
	}
}

func TestFakerServerMode(t *testing.T) {
	// This test checks that the faker can be created and configured for server mode
	// We don't actually run ServeForever as it blocks indefinitely

	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "server_test.xml")
	xmlContent := `<?xml version="1.0"?><nvidia_smi_log><attached_gpus>1</attached_gpus></nvidia_smi_log>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create XML file: %v", err)
	}

	config := Config{
		XMLFilePath: xmlFile,
		LogLevel:    "debug",
	}

	faker, err := New(config)
	if err != nil {
		t.Errorf("Failed to create faker for server mode: %v", err)
	}

	if faker.GetXMLContent() == "" {
		t.Error("Faker should have loaded XML content")
	}

	// Test that configuration is correct
	if faker.config.XMLFilePath != xmlFile {
		t.Errorf("XML file path not set correctly: got %s, want %s", faker.config.XMLFilePath, xmlFile)
	}
}
