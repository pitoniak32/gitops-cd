package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cmd.NewRoot().ExecuteContext(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}
