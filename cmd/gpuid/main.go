package main

import (
	"os"

	"github.com/mchmarny/gpuid/pkg/runner"
)

func main() {
	os.Exit(runner.Run())
}
