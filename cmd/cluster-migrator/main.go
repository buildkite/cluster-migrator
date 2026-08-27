package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/buildkite/cluster-migrator/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, http.DefaultClient); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cluster-migrator:", err)
		os.Exit(1)
	}
}
