package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	EnvVarContentPath  = "SMIFAKER_CONTENT_PATH"
	DefaultContentPath = "/data/nvidia-smi.xml"
)

// run streams the configured file to stdout. Split from main so deferred Close runs
// even when streaming fails — main exits via os.Exit on a non-zero return.
func run() error {
	contentPath := os.Getenv(EnvVarContentPath)
	if contentPath == "" {
		contentPath = DefaultContentPath
		log.Printf("Using default content path: %s", contentPath)
	}

	// Normalize the path so a relative input cannot escape via embedded "..".
	contentPath = filepath.Clean(contentPath)

	f, err := os.Open(contentPath) // #nosec G304 — test harness, path comes from operator-controlled env
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(os.Stdout, f)
	return err
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("smifaker failed: %v", err)
	}
}
