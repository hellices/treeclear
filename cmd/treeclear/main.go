package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, version.Value)
}
