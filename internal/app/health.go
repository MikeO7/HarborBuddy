package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/health"
	"github.com/rs/zerolog"
)

var healthInterval = health.HeartbeatInterval

// runHealthcheck backs the container HEALTHCHECK: it reads the status file the
// daemon in the same container writes and fails when it is missing or stale.
func runHealthcheck(stdout io.Writer, deps Dependencies) error {
	if err := health.Check(health.PathFromEnv(deps.LookupEnv), time.Now()); err != nil {
		return fmt.Errorf("unhealthy: %w", err)
	}
	_, err := fmt.Fprintln(stdout, "healthy")
	return err
}

// startHealthMonitor runs the Docker heartbeat for a long-running daemon. It
// returns the context the scheduler should use and a function that stops the
// heartbeat and waits for it to exit.
func startHealthMonitor(ctx context.Context, cfg config.Config, deps Dependencies, pinger health.Pinger, logger zerolog.Logger) (context.Context, func()) {
	if cfg.RunOnce || cfg.CleanupOnly {
		return ctx, func() {}
	}
	monitor := health.NewMonitor(health.PathFromEnv(deps.LookupEnv), pinger)
	monitorCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(monitorCtx, healthInterval, func(err error) {
			if err != nil {
				logger.Warn().Str("event", "health_heartbeat_failed").Err(err).
					Msg("Health heartbeat failed; the container healthcheck will report unhealthy")
				return
			}
			logger.Info().Str("event", "health_heartbeat_recovered").Msg("Health heartbeat recovered")
		})
	}()
	return health.WithMonitor(ctx, monitor), func() {
		cancel()
		<-done
	}
}
