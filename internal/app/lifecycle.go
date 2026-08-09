package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/buildinfo"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	containertypes "github.com/moby/moby/api/types/container"
)

func runDaemon(ctx context.Context, stdout io.Writer, deps Dependencies, cfg config.Config) (runErr error) {
	logger, levelController, closeLogger, err := deps.NewLogger(cfg.Log, stdout)
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	defer mergeCloseError(&runErr, "logging", closeLogger)

	stopLevelSignals := deps.StartLevelSignals(ctx, logger, levelController)
	if stopLevelSignals != nil {
		defer stopLevelSignals()
	}

	logger.Info().
		Str("event", "daemon_starting").
		Str("version", buildinfo.Version).
		Str("commit", buildinfo.Commit).
		Str("build_date", buildinfo.Date).
		Msg("HarborBuddy starting")
	logEffectiveConfig(logger, cfg)

	client, err := deps.NewDockerClient(ctx, cfg.Docker.Host)
	if err != nil {
		logger.Error().Str("event", "daemon_start_failed").Str("stage", "docker_connect").Err(err).Msg("HarborBuddy failed to connect to Docker")
		return fmt.Errorf("connect to Docker: %w", err)
	}
	defer mergeCloseError(&runErr, "Docker client", client.Close)

	if err := deps.RunScheduler(ctx, cfg, client, logger); err != nil {
		if _, ok := selfupdate.AsShutdownRequired(err); ok {
			logger.Info().Str("event", "daemon_self_update_handoff").Msg("Self-update helper started; shutting down successfully")
			return nil
		}
		logger.Error().Str("event", "daemon_failed").Str("stage", "scheduler").Err(err).Msg("HarborBuddy stopped after a scheduler failure")
		return fmt.Errorf("run scheduler: %w", err)
	}
	logger.Info().Str("event", "daemon_stopped").Msg("HarborBuddy stopped")
	return nil
}

func runHelper(ctx context.Context, stdout io.Writer, deps Dependencies, values *flagValues) (runErr error) {
	if values.targetContainer == "" || values.newImage == "" {
		return errors.New("updater mode requires --target-container-id and --new-image-id")
	}
	if values.helperStop < 0 || values.helperStartup < 0 {
		return errors.New("updater mode timeouts cannot be negative")
	}
	if values.helperRetries < 0 {
		return errors.New("updater mode restart retries cannot be negative")
	}

	helperCfg := config.Default()
	if err := helperCfg.ApplyEnvironment(func(name string) string {
		value, _ := deps.LookupEnv(name)
		return value
	}); err != nil {
		return fmt.Errorf("load helper environment: %w", err)
	}
	logger, _, closeLogger, err := deps.NewLogger(helperCfg.Log, stdout)
	if err != nil {
		return fmt.Errorf("initialize helper logging: %w", err)
	}
	defer mergeCloseError(&runErr, "helper logging", closeLogger)

	host, _ := deps.LookupEnv("HARBORBUDDY_DOCKER_HOST")
	client, err := deps.NewDockerClient(ctx, host)
	if err != nil {
		return fmt.Errorf("connect helper to Docker: %w", err)
	}
	defer mergeCloseError(&runErr, "Docker client", client.Close)

	started := time.Now()
	logger.Info().Str("event", "self_update_helper_starting").
		Str("target_container_id", shortOperationalID(values.targetContainer)).
		Str("target_image_id", shortOperationalID(values.newImage)).
		Msg("Self-update helper starting")
	if _, err := fmt.Fprintln(stdout, docker.SelfUpdateHelperReadyMarker); err != nil {
		return fmt.Errorf("acknowledge self-update helper readiness: %w", err)
	}
	request := selfupdate.UpdaterRequest{
		TargetContainerID:      values.targetContainer,
		TargetImageID:          values.newImage,
		StopTimeout:            values.helperStop,
		StartupTimeout:         values.helperStartup,
		RollbackImageRetention: values.helperRollbackImageRetention,
		RestartPolicy: containertypes.RestartPolicy{
			Name:              containertypes.RestartPolicyMode(values.helperRestart),
			MaximumRetryCount: values.helperRetries,
		},
	}
	result, helperErr := deps.RunHelper(ctx, client, request)
	if helperErr != nil {
		logger.Error().Str("event", "self_update_helper_failed").
			Str("target_container_id", shortOperationalID(values.targetContainer)).
			Str("target_image_id", shortOperationalID(values.newImage)).
			Str("failure_stage", result.FailureStage).
			Bool("rollback_attempted", result.RollbackAttempted).
			Str("rollback_outcome", rollbackOutcome(result)).
			Int64("duration_ms", time.Since(started).Milliseconds()).Err(helperErr).
			Msg("Self-update helper failed")
		return fmt.Errorf("run self-update helper: %w", helperErr)
	}
	event := logger.Info().Str("event", "self_update_helper_complete").
		Str("target_container_id", shortOperationalID(values.targetContainer)).
		Str("target_image_id", shortOperationalID(values.newImage)).
		Str("new_container_id", shortOperationalID(result.NewContainerID)).
		Int64("duration_ms", time.Since(started).Milliseconds())
	warning := errors.Join(result.BackupCleanupErr, result.RollbackImageRetentionErr)
	if warning != nil {
		event = logger.Warn().Str("event", "self_update_helper_complete").
			Str("target_container_id", shortOperationalID(values.targetContainer)).
			Str("target_image_id", shortOperationalID(values.newImage)).
			Str("new_container_id", shortOperationalID(result.NewContainerID)).
			Str("warning", warning.Error()).
			Int64("duration_ms", time.Since(started).Milliseconds())
	}
	event.Msg("Self-update helper completed")
	return nil
}

func rollbackOutcome(result docker.ReplaceResult) string {
	if !result.RollbackAttempted {
		return "not_attempted"
	}
	if result.RollbackErr != nil {
		return "failed"
	}
	return "succeeded"
}

func shortOperationalID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func mergeCloseError(runErr *error, resource string, closeResource func() error) {
	if err := closeResource(); err != nil && *runErr == nil {
		*runErr = fmt.Errorf("close %s: %w", resource, err)
	}
}
