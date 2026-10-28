package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ribeirogab/zapgate/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.System())
	stop()
	os.Exit(code)
}
