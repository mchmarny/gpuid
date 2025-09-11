package gpu

import (
	"fmt"
	"time"
)

// SerialNumberReading represents a single reading of a GPU serial number associated with a pod.
type SerialNumberReading struct {
	Cluster string    `json:"cluster" yaml:"cluster"`
	Node    string    `json:"node" yaml:"node"`
	Machine string    `json:"machine" yaml:"machine"`
	Source  string    `json:"source" yaml:"source"`
	GPU     string    `json:"gpu" yaml:"gpu"`
	Time    time.Time `json:"time" yaml:"time"`
}

// Slice returns the SerialNumberReading fields as a slice of strings for CSV serialization.
func (r *SerialNumberReading) Slice() []string {
	return []string{
		r.Cluster,
		r.Node,
		r.Machine,
		r.Source,
		r.GPU,
		r.Time.Format(time.RFC3339),
	}
}

// Validate checks if the SerialNumberReading has all required fields populated.
func (r *SerialNumberReading) Validate() error {
	if r.Cluster == "" {
		return fmt.Errorf("cluster name is required")
	}
	if r.Node == "" {
		return fmt.Errorf("node name is required")
	}
	if r.Machine == "" {
		return fmt.Errorf("machine name is required")
	}
	if r.Source == "" {
		return fmt.Errorf("source name is required")
	}
	if r.GPU == "" {
		return fmt.Errorf("GPU identifier is required")
	}
	if r.Time.IsZero() {
		return fmt.Errorf("time is required")
	}
	return nil
}
