package main

import (
	"log"
	"os"
)

const (
	EnvVarContentPath  = "SMIFAKER_CONTENT_PATH"
	DefaultContentPath = "/data/nvidia-smi.xml" // Default fallback
)

func main() {
	contentPath := os.Getenv(EnvVarContentPath)
	if contentPath == "" {
		contentPath = DefaultContentPath
		log.Printf("Using default content path: %s", contentPath)
	}

	if _, err := os.Stat(contentPath); os.IsNotExist(err) {
		log.Fatalf("file does not exist: %s", contentPath)
	}

	b, err := os.ReadFile(contentPath)
	if err != nil {
		log.Fatalf("failed to read file %s: %v", contentPath, err)
	}

	if _, err := os.Stdout.Write(b); err != nil {
		log.Fatalf("failed to write to stdout: %v", err)
	}
}
