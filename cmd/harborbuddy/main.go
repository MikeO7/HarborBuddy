package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MikeO7/HarborBuddy/internal/app"
	"github.com/spf13/pflag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Restore default handling after the first signal so a second signal can
		// force termination if graceful shutdown or rollback stalls.
		stop()
	}()

	err := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	// --help already printed usage; asking for help is not a failure.
	if err != nil && !errors.Is(err, pflag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
