package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// Compile-time check to ensure MockUpdater implements Updater interface
var _ Updater = (*MockUpdater)(nil)

// MockUpdater for testing - implements the Updater interface
type MockUpdater struct {
	node        *corev1.Node
	updateCount int
	shouldFail  bool
	failCount   int
}

func NewMockUpdater(node *corev1.Node) *MockUpdater {
	return &MockUpdater{node: node}
}

func (m *MockUpdater) GetNode(_ context.Context, _ string) (*corev1.Node, error) {
	if m.node == nil {
		return nil, errors.New("node not found")
	}
	// Return a copy to simulate Kubernetes behavior
	nodeCopy := m.node.DeepCopy()
	return nodeCopy, nil
}

func (m *MockUpdater) UpdateNode(_ context.Context, node *corev1.Node) (*corev1.Node, error) {
	m.updateCount++

	if m.shouldFail && m.updateCount <= m.failCount {
		return nil, errors.New("simulated update failure")
	}

	// Simulate successful update
	m.node = node.DeepCopy()
	return m.node, nil
}

func (m *MockUpdater) SetFailure(shouldFail bool, failCount int) {
	m.shouldFail = shouldFail
	m.failCount = failCount
}

func TestEnsureLabels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	tests := []struct {
		name           string
		initialLabels  map[string]string
		serials        []*gpu.Serials
		expectedLabels map[string]string
		shouldFail     bool
		failCount      int
	}{
		{
			name:          "add new GPU labels",
			initialLabels: map[string]string{"existing": "label"},
			serials: []*gpu.Serials{
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu1", "gpu2"},
				},
			},
			expectedLabels: map[string]string{
				"existing":                         "label",
				"gpuid.github.com/chassis-0":       "chassis1",
				"gpuid.github.com/chassis-0-gpu-0": "gpu1",
				"gpuid.github.com/chassis-0-gpu-1": "gpu2",
				"gpuid.github.com/chassis-count":   "1",
			},
		},
		{
			name: "replace existing GPU labels",
			initialLabels: map[string]string{
				"existing":                         "label",
				"gpuid.github.com/chassis-0":       "chassis1",
				"gpuid.github.com/chassis-0-gpu-0": "oldgpu",
			},
			serials: []*gpu.Serials{
				{
					Chassis: "newchassis",
					GPU:     []string{"newgpu"},
				},
			},
			expectedLabels: map[string]string{
				"existing":                         "label",
				"gpuid.github.com/chassis-0":       "newchassis",
				"gpuid.github.com/chassis-0-gpu-0": "newgpu",
				"gpuid.github.com/chassis-count":   "1",
			},
		},
		{
			name:          "handle multiple chassis with ordering",
			initialLabels: map[string]string{},
			serials: []*gpu.Serials{
				{
					Chassis: "chassis2",
					GPU:     []string{"gpu0"},
				},
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu0", "gpu1"},
				},
			},
			expectedLabels: map[string]string{
				"gpuid.github.com/chassis-0":       "chassis1", // sorted by chassis name
				"gpuid.github.com/chassis-0-gpu-0": "gpu0",
				"gpuid.github.com/chassis-0-gpu-1": "gpu1",
				"gpuid.github.com/chassis-1":       "chassis2",
				"gpuid.github.com/chassis-1-gpu-0": "gpu0",
				"gpuid.github.com/chassis-count":   "2",
			},
		},
		{
			name:          "handle empty serials",
			initialLabels: map[string]string{"existing": "label"},
			serials:       []*gpu.Serials{},
			expectedLabels: map[string]string{
				"existing": "label",
			},
		},
		{
			name:          "retry on failure",
			initialLabels: map[string]string{},
			serials: []*gpu.Serials{
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu1"},
				},
			},
			expectedLabels: map[string]string{
				"gpuid.github.com/chassis-0":       "chassis1",
				"gpuid.github.com/chassis-0-gpu-0": "gpu1",
				"gpuid.github.com/chassis-count":   "1",
			},
			shouldFail: true,
			failCount:  2, // Fail first 2 attempts, succeed on 3rd
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock node with initial labels
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: make(map[string]string),
				},
			}
			for k, v := range tt.initialLabels {
				node.Labels[k] = v
			}

			// Create mock updater
			mockUpdater := NewMockUpdater(node)
			if tt.shouldFail {
				mockUpdater.SetFailure(true, tt.failCount)
			}

			// Test the function
			err := EnsureLabels(ctx, logger, mockUpdater, "test-node", tt.serials)

			if tt.shouldFail && tt.failCount >= maxRetries {
				if err == nil {
					t.Error("Expected error due to max retries exceeded, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify labels
			finalLabels := mockUpdater.node.Labels
			for k, expectedV := range tt.expectedLabels {
				if actualV, exists := finalLabels[k]; !exists {
					t.Errorf("Expected label %s not found", k)
				} else if actualV != expectedV {
					t.Errorf("Label %s: expected %s, got %s", k, expectedV, actualV)
				}
			}

			// Verify no unexpected GPU labels
			for k := range finalLabels {
				if _, expected := tt.expectedLabels[k]; !expected {
					if k != "existing" { // Allow non-GPU labels
						t.Errorf("Unexpected label found: %s", k)
					}
				}
			}

			// Verify retry behavior
			if tt.shouldFail {
				expectedUpdates := tt.failCount + 1 // Failed attempts + successful attempt
				if mockUpdater.updateCount != expectedUpdates {
					t.Errorf("Expected %d update attempts, got %d", expectedUpdates, mockUpdater.updateCount)
				}
			}
		})
	}
}

func TestCalculateGPULabels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tests := []struct {
		name           string
		serials        []*gpu.Serials
		expectedLabels map[string]string
	}{
		{
			name:           "empty serials",
			serials:        []*gpu.Serials{},
			expectedLabels: map[string]string{},
		},
		{
			name: "single chassis with multiple GPUs",
			serials: []*gpu.Serials{
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu2", "gpu1"}, // Test sorting
				},
			},
			expectedLabels: map[string]string{
				"gpuid.github.com/chassis-0":       "chassis1",
				"gpuid.github.com/chassis-0-gpu-0": "gpu1", // Should be sorted
				"gpuid.github.com/chassis-0-gpu-1": "gpu2",
				"gpuid.github.com/chassis-count":   "1",
			},
		},
		{
			name: "multiple chassis",
			serials: []*gpu.Serials{
				{
					Chassis: "chassis2",
					GPU:     []string{"gpu3"},
				},
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu1"},
				},
			},
			expectedLabels: map[string]string{
				"gpuid.github.com/chassis-0":       "chassis1", // Sorted by chassis name
				"gpuid.github.com/chassis-0-gpu-0": "gpu1",
				"gpuid.github.com/chassis-1":       "chassis2",
				"gpuid.github.com/chassis-1-gpu-0": "gpu3",
				"gpuid.github.com/chassis-count":   "2",
			},
		},
		{
			name: "handle nil entries",
			serials: []*gpu.Serials{
				nil,
				{
					Chassis: "chassis1",
					GPU:     []string{"gpu1"},
				},
				nil,
			},
			expectedLabels: map[string]string{
				"gpuid.github.com/chassis-0":       "chassis1",
				"gpuid.github.com/chassis-0-gpu-0": "gpu1",
				"gpuid.github.com/chassis-count":   "1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := calculateGPULabels(logger, tt.serials)

			if len(labels) != len(tt.expectedLabels) {
				t.Errorf("Expected %d labels, got %d", len(tt.expectedLabels), len(labels))
			}

			for k, expectedV := range tt.expectedLabels {
				if actualV, exists := labels[k]; !exists {
					t.Errorf("Expected label %s not found", k)
				} else if actualV != expectedV {
					t.Errorf("Label %s: expected %s, got %s", k, expectedV, actualV)
				}
			}
		})
	}
}

func TestNeedsLabelUpdate(t *testing.T) {
	tests := []struct {
		name     string
		current  map[string]string
		desired  map[string]string
		expected bool
	}{
		{
			name:     "no update needed",
			current:  map[string]string{"gpuid.github.com/chassis-0": "chassis1"},
			desired:  map[string]string{"gpuid.github.com/chassis-0": "chassis1"},
			expected: false,
		},
		{
			name:     "new label needed",
			current:  map[string]string{},
			desired:  map[string]string{"gpuid.github.com/chassis-0": "chassis1"},
			expected: true,
		},
		{
			name:     "label removal needed",
			current:  map[string]string{"gpuid.github.com/chassis-0": "chassis1"},
			desired:  map[string]string{},
			expected: true,
		},
		{
			name:     "label value change needed",
			current:  map[string]string{"gpuid.github.com/chassis-0": "old"},
			desired:  map[string]string{"gpuid.github.com/chassis-0": "new"},
			expected: true,
		},
		{
			name: "ignore non-GPU labels",
			current: map[string]string{
				"other-label":                "value",
				"gpuid.github.com/chassis-0": "chassis1",
			},
			desired: map[string]string{
				"gpuid.github.com/chassis-0": "chassis1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsLabelUpdate(tt.current, tt.desired)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestLabelerImplementsInterface ensures Labeler implements Updater interface at compile time
func TestLabelerImplementsInterface(t *testing.T) {
	var _ Updater = (*Labeler)(nil)
	t.Log("Labeler correctly implements Updater interface")
}

// Benchmark for performance testing with large numbers of serials
func BenchmarkCalculateGPULabels(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create 100 chassis with 8 GPUs each (simulating large-scale scenario)
	serials := make([]*gpu.Serials, 100)
	for i := 0; i < 100; i++ {
		gpus := make([]string, 8)
		for j := 0; j < 8; j++ {
			gpus[j] = fmt.Sprintf("gpu-%d-%d", i, j)
		}
		serials[i] = &gpu.Serials{
			Chassis: fmt.Sprintf("chassis-%d", i),
			GPU:     gpus,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateGPULabels(logger, serials)
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "N/A should become unknown",
			input:    "N/A",
			expected: notSetDefault,
		},
		{
			name:     "Valid alphanumeric should remain unchanged",
			input:    "1652823055647",
			expected: "1652823055647",
		},
		{
			name:     "Valid with dashes and underscores",
			input:    "valid-value_123",
			expected: "valid-value_123",
		},
		{
			name:     "Invalid characters should be replaced with underscores",
			input:    "invalid@value#123",
			expected: "invalid-value-123",
		},
		{
			name:     "Leading/trailing invalid chars should be trimmed",
			input:    "!@#valid123$%^",
			expected: "valid123",
		},
		{
			name:     "Empty string should remain empty",
			input:    "",
			expected: notSetDefault,
		},
		{
			name:     "Only invalid characters should become unknown",
			input:    "!@#$%^",
			expected: notSetDefault,
		},
		{
			name:     "Real chassis serial with periods",
			input:    "1234.5678.9012",
			expected: "1234.5678.9012",
		},
		{
			name:     "GPU serial with special chars",
			input:    "GPU-123/456",
			expected: "GPU-123-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeLabelValue(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}

			// Verify the result is a valid Kubernetes label value (unless empty)
			if result != notSetDefault && !labelValueRegex.MatchString(result) {
				t.Errorf("sanitizeLabelValue(%q) = %q is not a valid Kubernetes label value", tt.input, result)
			}
		})
	}
}

func TestSanitizeLabelValuePatterns(t *testing.T) {
	// Test patterns from the actual error logs
	problematicValues := []string{
		"N/A",
		"N/A-1652823055647",
		"N/A-1652923033989",
		"N/A-1652823054567",
		"N/A-1652923034028",
		"N/A-1653023018213",
		"N/A-1652923034291",
		"N/A-1652823055931",
		"N/A-1652823055642",
	}

	for _, value := range problematicValues {
		t.Run("problematic_"+value, func(t *testing.T) {
			result := sanitizeLabelValue(value)

			// Should not be the original problematic value
			if result == value {
				t.Errorf("sanitizeLabelValue(%q) returned the original problematic value", value)
			}

			// Should be a valid Kubernetes label value
			if result != "" && !labelValueRegex.MatchString(result) {
				t.Errorf("sanitizeLabelValue(%q) = %q is not a valid Kubernetes label value", value, result)
			}

			// Log what the transformation produces
			t.Logf("sanitizeLabelValue(%q) = %q", value, result)
		})
	}
}
