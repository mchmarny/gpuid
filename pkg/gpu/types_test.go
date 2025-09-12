package gpu

import (
	"reflect"
	"testing"
	"time"
)

func TestSerialNumberReading_Slice(t *testing.T) {
	testTime := time.Date(2025, 9, 12, 10, 30, 0, 0, time.UTC)

	reading := &SerialNumberReading{
		Cluster: "test-cluster",
		Node:    "test-node",
		Machine: "test-machine",
		Source:  "test-namespace/test-pod",
		GPU:     "GPU-12345",
		Time:    testTime,
	}

	expected := []string{
		"test-cluster",
		"test-node",
		"test-machine",
		"test-namespace/test-pod",
		"GPU-12345",
		testTime.Format(time.RFC3339),
	}

	result := reading.Slice()
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Slice() = %v, want %v", result, expected)
	}
}

func TestSerialNumberReading_Validate(t *testing.T) {
	validTime := time.Date(2025, 9, 12, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		reading *SerialNumberReading
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid reading",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Machine: "test-machine",
				Source:  "test-namespace/test-pod",
				GPU:     "GPU-12345",
				Time:    validTime,
			},
			wantErr: false,
		},
		{
			name: "missing cluster",
			reading: &SerialNumberReading{
				Node:    "test-node",
				Machine: "test-machine",
				Source:  "test-namespace/test-pod",
				GPU:     "GPU-12345",
				Time:    validTime,
			},
			wantErr: true,
			errMsg:  "cluster name is required",
		},
		{
			name: "missing node",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Machine: "test-machine",
				Source:  "test-namespace/test-pod",
				GPU:     "GPU-12345",
				Time:    validTime,
			},
			wantErr: true,
			errMsg:  "node name is required",
		},
		{
			name: "missing machine",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Source:  "test-namespace/test-pod",
				GPU:     "GPU-12345",
				Time:    validTime,
			},
			wantErr: true,
			errMsg:  "machine name is required",
		},
		{
			name: "missing source",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Machine: "test-machine",
				GPU:     "GPU-12345",
				Time:    validTime,
			},
			wantErr: true,
			errMsg:  "source name is required",
		},
		{
			name: "missing GPU",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Machine: "test-machine",
				Source:  "test-namespace/test-pod",
				Time:    validTime,
			},
			wantErr: true,
			errMsg:  "GPU identifier is required",
		},
		{
			name: "missing time",
			reading: &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Machine: "test-machine",
				Source:  "test-namespace/test-pod",
				GPU:     "GPU-12345",
				Time:    time.Time{}, // zero time
			},
			wantErr: true,
			errMsg:  "time is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reading.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSerialNumberReading_SliceConsistency(t *testing.T) {
	// Test that Slice() produces consistent output
	reading := &SerialNumberReading{
		Cluster: "cluster",
		Node:    "node",
		Machine: "machine",
		Source:  "source",
		GPU:     "gpu",
		Time:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	slice1 := reading.Slice()
	slice2 := reading.Slice()

	if !reflect.DeepEqual(slice1, slice2) {
		t.Error("Slice() should produce consistent output")
	}

	if len(slice1) != 6 {
		t.Errorf("Slice() should return 6 elements, got %d", len(slice1))
	}
}

func TestSerialNumberReading_TimeFormatting(t *testing.T) {
	// Test different time formats
	tests := []struct {
		name string
		time time.Time
	}{
		{
			name: "UTC time",
			time: time.Date(2025, 9, 12, 10, 30, 45, 123456789, time.UTC),
		},
		{
			name: "local time",
			time: time.Date(2025, 9, 12, 10, 30, 45, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reading := &SerialNumberReading{
				Cluster: "test-cluster",
				Node:    "test-node",
				Machine: "test-machine",
				Source:  "test-source",
				GPU:     "test-gpu",
				Time:    tt.time,
			}

			slice := reading.Slice()
			timeStr := slice[5] // Time is the last element

			// Parse the time back to ensure it's valid RFC3339
			_, err := time.Parse(time.RFC3339, timeStr)
			if err != nil {
				t.Errorf("Time in slice is not valid RFC3339: %v", err)
			}
		})
	}
}
