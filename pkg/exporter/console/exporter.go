package console

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mchmarny/gpuid/pkg/gpu"
)

// Export writes the GPU serial number readings to the provided logger.
// If the logger is nil, it defaults to the standard logger.
// If the records slice is nil, it returns an error.
func Export(_ context.Context, log *slog.Logger, records []*gpu.SerialNumberReading) error {
	if log == nil {
		log = slog.Default()
	}

	if records == nil {
		return fmt.Errorf("records is nil")
	}

	for _, reading := range records {
		log.Info("GPU Serial Number Reading",
			"cluster", reading.Cluster,
			"node", reading.Node,
			"machine", reading.Machine,
			"source", reading.Source,
			"gpu", reading.GPU,
			"time", reading.ReadTime.Format("2006-01-02T15:04:05Z07:00"),
		)
	}

	return nil
}
