package faker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
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
		file    string
		wantErr bool
	}{
		{
			name:    "valid config with XML file",
			file:    xmlFile,
			wantErr: false,
		},
		{
			name:    "config without XML file",
			wantErr: true,
		},
		{
			name:    "config with non-existent XML file",
			file:    "/non/existent/file.xml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			faker, err := New(tt.file)
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
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "test_smi.xml")
	xmlContent := `<?xml version="1.0"?><nvidia_smi_log><attached_gpus>1</attached_gpus></nvidia_smi_log>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create test XML file: %v", err)
	}

	faker, err := New(xmlFile)
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
			name:    "nvidia-smi with no args",
			args:    []string{},
			wantErr: false,
			wantXML: true,
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

func TestExecuteCommand(t *testing.T) {
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "test.xml")
	xmlContent := `<?xml version="1.0"?><test/>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create XML file: %v", err)
	}

	faker, err := New(xmlFile)
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
			_, stderr, err := faker.ExecuteCommand(tt.command, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && stderr != "" {
				t.Errorf("Unexpected stderr output: %s", stderr)
			}
		})
	}
}

func TestFakerServerMode(t *testing.T) {
	tmpDir := t.TempDir()
	xmlFile := filepath.Join(tmpDir, "server_test.xml")
	xmlContent := `<?xml version="1.0"?><nvidia_smi_log><attached_gpus>1</attached_gpus></nvidia_smi_log>`

	if err := os.WriteFile(xmlFile, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("Failed to create XML file: %v", err)
	}

	faker, err := New(xmlFile)
	if err != nil {
		t.Errorf("Failed to create faker for server mode: %v", err)
	}

	if faker.GetXMLContent() == "" {
		t.Error("Faker should have loaded XML content")
	}

	if faker.xmlFile != xmlFile {
		t.Errorf("XML file path not set correctly: got %s, want %s", faker.xmlFile, xmlFile)
	}
}
