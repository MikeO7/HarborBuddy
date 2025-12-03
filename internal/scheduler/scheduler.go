package scheduler

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikeo/harborbuddy/internal/cleanup"
	"github.com/mikeo/harborbuddy/internal/config"
	"github.com/mikeo/harborbuddy/internal/docker"
	"github.com/mikeo/harborbuddy/internal/updater"
	"github.com/mikeo/harborbuddy/pkg/log"
)

// Run starts the scheduler main loop
func Run(cfg config.Config, dockerClient docker.Client) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigChan
		log.Infof("Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()

	// Run once mode
	if cfg.RunOnce {
		log.Info("Running in once mode")
		return runCycle(ctx, cfg, dockerClient)
	}

	// Cleanup only mode
	if cfg.CleanupOnly {
		log.Info("Running in cleanup-only mode")
		return cleanup.RunCleanup(ctx, cfg, dockerClient)
	}

	// Normal loop mode
	log.Infof("Starting scheduler with interval: %v", cfg.Updates.CheckInterval)

	// Run initial cycle immediately
	if err := runCycle(ctx, cfg, dockerClient); err != nil {
		log.ErrorErr("Error in initial cycle", err)
	}

	// Set up ticker for periodic cycles
	ticker := time.NewTicker(cfg.Updates.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Scheduler stopped")
			return nil
		case <-ticker.C:
			if err := runCycle(ctx, cfg, dockerClient); err != nil {
				log.ErrorErr("Error in update cycle", err)
			}
		}
	}
}

// runCycle runs a single update and cleanup cycle
func runCycle(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	log.Info("==== Starting new cycle ====")

	// Run updates if enabled
	if cfg.Updates.Enabled {
		if err := updater.RunUpdateCycle(ctx, cfg, dockerClient); err != nil {
			return err
		}
	} else {
		log.Info("Updates are disabled, skipping update cycle")
	}

	// Run cleanup if enabled
	if cfg.Cleanup.Enabled {
		if err := cleanup.RunCleanup(ctx, cfg, dockerClient); err != nil {
			return err
		}
	}

	log.Info("==== Cycle complete ====")
	return nil
}
