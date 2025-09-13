package faker

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mchmarny/gpuid/pkg/logger"
)

// GPUFaker simulates nvidia-smi command behavior
type GPUFaker struct {
	logger     *slog.Logger
	xmlFile    string
	xmlContent string
}

// New creates a new GPU faker instance
func New(file string) (*GPUFaker, error) {
	logger := logger.NewProductionLogger(logger.Config{
		Service: "gpufaker",
		Level:   "info",
	})

	faker := &GPUFaker{
		xmlFile: file,
		logger:  logger,
	}

	if err := faker.loadXMLContent(); err != nil {
		return nil, fmt.Errorf("failed to load XML content: %w", err)
	}

	return faker, nil
}

// loadXMLContent reads the XML file content
func (f *GPUFaker) loadXMLContent() error {
	if f.xmlFile == "" {
		return fmt.Errorf("XML file path is not configured")
	}

	// Check if file exists
	if _, err := os.Stat(f.xmlFile); os.IsNotExist(err) {
		return fmt.Errorf("XML file does not exist: %s", f.xmlFile)
	}

	// Read file content
	content, err := os.ReadFile(f.xmlFile)
	if err != nil {
		return fmt.Errorf("failed to read XML file: %w", err)
	}

	f.xmlContent = string(content)
	f.logger.Info("loaded XML content",
		"file", f.xmlFile,
		"size", len(f.xmlContent))

	return nil
}

// HandleNvidiaSMI processes nvidia-smi commands and returns fake output
func (f *GPUFaker) HandleNvidiaSMI(args []string) (string, error) {
	f.logger.Debug("handling nvidia-smi command", "args", args)

	if f.xmlContent == "" {
		return "", fmt.Errorf("XML content not loaded")
	}
	f.logger.Info("returning fake nvidia-smi XML output")
	return f.xmlContent, nil
}

// ServeForever runs the faker in server mode, keeping the container alive
func (f *GPUFaker) ServeForever() error {
	f.logger.Info("starting GPU faker in server mode")

	// Keep the process running
	f.logger.Info("GPU faker is ready and waiting...")

	// Read from stdin to keep alive (useful for kubernetes exec)
	// This allows the pod to stay running and respond to exec commands
	select {} // Block forever
}

// ExecuteCommand simulates command execution (for direct API usage)
func (f *GPUFaker) ExecuteCommand(command string, args []string) (string, string, error) {
	f.logger.Debug("executing command", "command", command, "args", args)

	// Doesn't matter what the command is, we only handle nvidia-smi
	output, err := f.HandleNvidiaSMI(args)
	if err != nil {
		return "", err.Error(), err
	}
	return output, "", nil
}

// GetXMLContent returns the loaded XML content
func (f *GPUFaker) GetXMLContent() string {
	return f.xmlContent
}
