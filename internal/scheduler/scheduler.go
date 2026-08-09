package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/cleanup"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/MikeO7/HarborBuddy/internal/updater"
	"github.com/rs/zerolog"
)

// Run starts the scheduler using the caller's lifecycle context and logger.
func Run(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	s := newScheduler(realClock{})
	return s.run(ctx, cfg, client, logger)
}

type scheduler struct {
	clock      clock
	runUpdate  func(context.Context, config.Config, docker.Client, zerolog.Logger) error
	runCleanup func(context.Context, config.Config, docker.Client, zerolog.Logger) error
	cycleID    func(time.Time) string
}

var randomRead = rand.Read

func newScheduler(clock clock) *scheduler {
	return &scheduler{
		clock: clock,
		runUpdate: func(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
			_, err := updater.RunUpdateCycle(ctx, cfg, client, logger)
			return err
		},
		runCleanup: func(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
			_, err := cleanup.RunCleanup(ctx, cfg, client, logger)
			return err
		},
		cycleID: generateCycleID,
	}
}

func (s *scheduler) run(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.Info().Str("event", "scheduler_started").Msg("Scheduler started")

	if cfg.CleanupOnly {
		return gracefulResult(ctx, s.runCleanupOnly(ctx, cfg, client, logger))
	}
	if cfg.RunOnce {
		return gracefulResult(ctx, s.runCycle(ctx, cfg, client, logger))
	}
	if cfg.Updates.ScheduleTime != "" {
		return s.runScheduled(ctx, cfg, client, logger)
	}
	return s.runInterval(ctx, cfg, client, logger)
}

func (s *scheduler) runInterval(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	logger.Info().Str("event", "scheduler_interval_started").Dur("interval", cfg.Updates.CheckInterval).Msg("Starting interval scheduler")
	if stop, err := s.runContinuousCycle(ctx, cfg, client, logger); stop {
		return err
	}

	ticker := s.clock.NewTicker(cfg.Updates.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info().Str("event", "scheduler_stopped").Msg("Scheduler stopped")
			return nil
		case <-ticker.C():
			if stop, err := s.runContinuousCycle(ctx, cfg, client, logger); stop {
				return err
			}
		}
	}
}

func (s *scheduler) runScheduled(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	location, err := time.LoadLocation(cfg.Updates.Timezone)
	if err != nil {
		return fmt.Errorf("load schedule timezone %q: %w", cfg.Updates.Timezone, err)
	}
	logger.Info().Str("event", "scheduler_daily_started").Str("schedule_time", cfg.Updates.ScheduleTime).Str("timezone", cfg.Updates.Timezone).Msg("Starting daily scheduler")

	for {
		now := s.clock.Now().In(location)
		next := calculateNextRun(now, cfg.Updates.ScheduleTime, location)
		logger.Info().Str("event", "scheduler_next_run").Time("next_run", next).Dur("wait", next.Sub(now)).Msg("Next scheduled run")
		timer := s.clock.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			logger.Info().Str("event", "scheduler_stopped").Msg("Scheduler stopped")
			return nil
		case <-timer.C():
			if stop, err := s.runContinuousCycle(ctx, cfg, client, logger); stop {
				return err
			}
		}
	}
}

func (s *scheduler) runContinuousCycle(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) (bool, error) {
	err := s.runCycle(ctx, cfg, client, logger)
	if err == nil {
		return false, nil
	}
	if _, ok := selfupdate.AsShutdownRequired(err); ok {
		return true, err
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return true, nil
	}
	return false, nil
}

func (s *scheduler) runCycle(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	started := time.Now()
	cycleLogger := logger.With().Str("cycle_id", s.cycleID(s.clock.Now())).Logger()
	cycleLogger.Info().Str("event", "cycle_started").Bool("updates", cfg.Updates.Enabled).
		Bool("cleanup", cfg.Cleanup.Enabled).Bool("cleanup_all", cfg.Cleanup.All).
		Int("cleanup_min_age_hours", cfg.Cleanup.MinAgeHours).Bool("dry_run", cfg.Updates.DryRun).
		Msg("Cycle started")

	var cycleErrors []error
	if cfg.Updates.Enabled {
		updateLogger := cycleLogger.With().Str("operation", "update").Logger()
		if err := s.runUpdate(ctx, cfg, client, updateLogger); err != nil {
			if signal, ok := selfupdate.AsShutdownRequired(err); ok {
				cycleLogger.Info().Str("event", "cycle_complete").Str("outcome", "self_update_handoff").
					Str("target_container_id", shortID(signal.TargetContainerID)).Str("helper_container_id", shortID(signal.HelperContainerID)).
					Int64("duration_ms", time.Since(started).Milliseconds()).Msg("Cycle handed off to self-update helper")
				return err
			}
			cycleErrors = append(cycleErrors, fmt.Errorf("update cycle: %w", err))
		}
	} else {
		cycleLogger.Debug().Str("event", "update_skipped").Str("reason", "updates_disabled").Msg("Updates are disabled")
	}

	if cfg.Cleanup.Enabled {
		cleanupLogger := cycleLogger.With().Str("operation", "cleanup").Logger()
		if err := s.runCleanup(ctx, cfg, client, cleanupLogger); err != nil {
			cycleErrors = append(cycleErrors, fmt.Errorf("cleanup cycle: %w", err))
		}
	} else {
		cycleLogger.Debug().Str("event", "cleanup_skipped").Str("reason", "cleanup_disabled").Msg("Cleanup is disabled")
	}

	joined := errors.Join(cycleErrors...)
	completion := cycleLogger.Info()
	outcome := "completed"
	if ctx.Err() != nil {
		outcome = "cancelled"
	} else if joined != nil {
		completion = cycleLogger.Error().Err(joined)
		outcome = "failed"
	}
	completion.Str("event", "cycle_complete").Str("outcome", outcome).
		Int64("duration_ms", time.Since(started).Milliseconds()).Msg("Cycle complete")
	return joined
}

func (s *scheduler) runCleanupOnly(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) error {
	started := time.Now()
	cycleLogger := logger.With().Str("cycle_id", s.cycleID(s.clock.Now())).Str("operation", "cleanup").Logger()
	cycleLogger.Info().Str("event", "cycle_started").Str("mode", "cleanup_only").Bool("dry_run", cfg.Updates.DryRun).
		Bool("cleanup_all", cfg.Cleanup.All).Int("cleanup_min_age_hours", cfg.Cleanup.MinAgeHours).Msg("Cleanup-only cycle started")
	err := s.runCleanup(ctx, cfg, client, cycleLogger)
	event := cycleLogger.Info()
	outcome := "completed"
	if err != nil {
		event = cycleLogger.Error().Err(err)
		outcome = "failed"
	}
	event.Str("event", "cycle_complete").Str("mode", "cleanup_only").Str("outcome", outcome).
		Int64("duration_ms", time.Since(started).Milliseconds()).Msg("Cleanup-only cycle complete")
	return err
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func gracefulResult(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}

func calculateNextRun(now time.Time, scheduleTime string, location *time.Location) time.Time {
	scheduled, _ := time.Parse("15:04", scheduleTime)
	next := time.Date(now.Year(), now.Month(), now.Day(), scheduled.Hour(), scheduled.Minute(), 0, 0, location)
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, scheduled.Hour(), scheduled.Minute(), 0, 0, location)
	}
	return next
}

func generateCycleID(now time.Time) string {
	bytes := make([]byte, 4)
	if _, err := randomRead(bytes); err != nil {
		return now.Format("150405")
	}
	return hex.EncodeToString(bytes)
}

type clock interface {
	Now() time.Time
	NewTimer(time.Duration) timer
	NewTicker(time.Duration) ticker
}

type timer interface {
	C() <-chan time.Time
	Stop() bool
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) NewTimer(d time.Duration) timer   { return realTimer{Timer: time.NewTimer(d)} }
func (realClock) NewTicker(d time.Duration) ticker { return realTicker{Ticker: time.NewTicker(d)} }

type realTimer struct{ *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.Timer.C }

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }
