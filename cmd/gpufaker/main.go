package main

import (
	"flag"
	"log"

	"github.com/mchmarny/gpuid/pkg/gpu/faker"
)

var (
	xmlFile = flag.String("f", "pkg/gpu/examples/h100.xml", "Path to XML file to serve as nvidia-smi output")
)

func main() {
	flag.Parse()

	gpuFaker, err := faker.New(*xmlFile)
	if err != nil {
		log.Fatalf("Failed to create GPU faker: %v", err)
	}

	if err := gpuFaker.ServeForever(); err != nil {
		log.Fatalf("GPU faker failed: %v", err)
	}
}
