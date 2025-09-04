package gpu

import (
	"fmt"
	"time"
)

// SerialNumberReading represents a single reading of a GPU serial number associated with a pod.
type SerialNumberReading struct {
	Cluster  string
	Node     string
	Machine  string
	Source   string
	GPU      string
	ReadTime time.Time
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
	if r.ReadTime.IsZero() {
		return fmt.Errorf("read time is required")
	}
	return nil
}
