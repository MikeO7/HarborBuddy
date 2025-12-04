package scheduler

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/cleanup"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/updater"
	"github.com/MikeO7/HarborBuddy/pkg/log"
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

	// Normal loop mode - check if using scheduled time or interval
	if cfg.Updates.ScheduleTime != "" {
		return runScheduledMode(ctx, cfg, dockerClient)
	}

	return runIntervalMode(ctx, cfg, dockerClient)
}

// runIntervalMode runs cycles at regular intervals
func runIntervalMode(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
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

// runScheduledMode runs cycles at a specific time each day
func runScheduledMode(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	location, err := time.LoadLocation(cfg.Updates.Timezone)
	if err != nil {
		return err
	}

	log.Infof("Starting scheduler with daily schedule: %s (%s)", cfg.Updates.ScheduleTime, cfg.Updates.Timezone)

	for {
		// Calculate next run time
		nextRun := calculateNextRun(cfg.Updates.ScheduleTime, location)
		now := time.Now().In(location)
		waitDuration := nextRun.Sub(now)

		log.Infof("Next scheduled run: %s (in %v)", nextRun.Format("2006-01-02 15:04:05 MST"), waitDuration.Round(time.Second))

		// Wait until scheduled time or cancellation
		timer := time.NewTimer(waitDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			log.Info("Scheduler stopped")
			return nil
		case <-timer.C:
			// Run the cycle at scheduled time
			if err := runCycle(ctx, cfg, dockerClient); err != nil {
				log.ErrorErr("Error in scheduled cycle", err)
			}
		}
	}
}

// calculateNextRun calculates the next scheduled run time
func calculateNextRun(scheduleTime string, location *time.Location) time.Time {
	now := time.Now().In(location)
	
	// Parse the schedule time (HH:MM format)
	scheduledTime, _ := time.Parse("15:04", scheduleTime)
	
	// Create a time for today at the scheduled time
	nextRun := time.Date(
		now.Year(), now.Month(), now.Day(),
		scheduledTime.Hour(), scheduledTime.Minute(), 0, 0,
		location,
	)
	
	// If the scheduled time has already passed today, schedule for tomorrow
	if nextRun.Before(now) || nextRun.Equal(now) {
		nextRun = nextRun.Add(24 * time.Hour)
	}
	
	return nextRun
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
