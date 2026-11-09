package main

import (
	"os"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cmd"
)

func main() {
	if err := cmd.NewRoot().Execute(); err != nil {
		os.Exit(1)
	}
}
