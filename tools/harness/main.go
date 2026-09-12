package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/hellices/treeclear/internal/harness"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(harness.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
