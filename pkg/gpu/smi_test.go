package gpu

import (
	"encoding/xml"
	"os"
	"testing"
)

func TestParseNvidiaSMILogs(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		gpus    int
		wantErr bool
	}{
		{
			name:    "parse h100 log",
			file:    "examples/h100.xml",
			gpus:    8,
			wantErr: false,
		},
		{
			name:    "parse gb200 log",
			file:    "examples/gb200.xml",
			gpus:    4,
			wantErr: false,
		},
	}

	for _, test := range tests {
		data, err := os.ReadFile(test.file)
		if err != nil {
			t.Fatalf("failed to read %s file: %v", test.file, err)
		}

		var d NVSMIDevice
		if err := xml.Unmarshal(data, &d); err != nil {
			t.Fatalf("failed to unmarshal XML: %v", err)
		}

		// Basic validations
		if d.Timestamp == "" {
			t.Error("expected timestamp to be set")
		}
		if d.DriverVersion == "" {
			t.Error("expected driverVersion to be set")
		}
		if d.CudaVersion == "" {
			t.Error("expected cudaVersion to be set")
		}
		if len(d.GPUs) != test.gpus {
			t.Errorf("expected %d GPUs to be present", test.gpus)
		}
		for _, gpu := range d.GPUs {
			if gpu.Serial == "" {
				t.Error("expected GPU serial to be set")
			}
			if gpu.ProductName == "" {
				t.Error("expected GPU productName to be set")
			}
			if gpu.UUID == "" {
				t.Error("expected GPU UUID to be set")
			}
			if gpu.FbMemoryUsage.Total == "" {
				t.Error("expected fbMemoryUsage.total to be set")
			}
		}
	}
}
