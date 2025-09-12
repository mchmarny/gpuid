package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mchmarny/gpuid/pkg/faker"
)

var (
	xmlFile  = flag.String("f", "", "Path to XML file to serve as nvidia-smi output")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	showHelp = flag.Bool("help", false, "Show help message")
)

func main() {
	flag.Parse()

	if *showHelp {
		showUsage()
		return
	}

	if *xmlFile == "" {
		log.Fatalf("XML file not specified. Use -f flag")
	}

	// Create faker config
	config := faker.Config{
		XMLFilePath: *xmlFile,
		LogLevel:    *logLevel,
	}

	// Create faker instance
	gpuFaker, err := faker.New(config)
	if err != nil {
		log.Fatalf("Failed to create GPU faker: %v", err)
	}

	fmt.Printf("Starting GPU Faker...\n")
	fmt.Printf("XML File: %s\n", *xmlFile)
	fmt.Printf("Log Level: %s\n", *logLevel)

	// Start the faker in server mode
	if err := gpuFaker.ServeForever(); err != nil {
		log.Fatalf("GPU faker failed: %v", err)
	}
}

func showUsage() {
	fmt.Printf("GPU Faker - Simulates nvidia-smi for testing\n\n")
	fmt.Printf("Usage: %s [options]\n\n", filepath.Base(os.Args[0]))
	fmt.Printf("Options:\n")
	flag.PrintDefaults()
}
