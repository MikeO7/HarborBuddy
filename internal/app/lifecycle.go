package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/MikeO7/HarborBuddy/internal/buildinfo"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
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
		Str("version", buildinfo.Version).
		Str("commit", buildinfo.Commit).
		Str("build_date", buildinfo.Date).
		Msg("HarborBuddy starting")

	client, err := deps.NewDockerClient(ctx, cfg.Docker.Host)
	if err != nil {
		return fmt.Errorf("connect to Docker: %w", err)
	}
	defer mergeCloseError(&runErr, "Docker client", client.Close)

	if err := deps.RunScheduler(ctx, cfg, client, logger); err != nil {
		if _, ok := selfupdate.AsShutdownRequired(err); ok {
			logger.Info().Msg("Self-update helper started; shutting down successfully")
			return nil
		}
		return fmt.Errorf("run scheduler: %w", err)
	}
	logger.Info().Msg("HarborBuddy stopped")
	return nil
}

func runHelper(ctx context.Context, stdout io.Writer, deps Dependencies, values *flagValues) (runErr error) {
	if values.targetContainer == "" || values.newImage == "" {
		return errors.New("updater mode requires --target-container-id and --new-image-id")
	}
	if values.helperStop < 0 || values.helperStartup < 0 {
		return errors.New("updater mode timeouts cannot be negative")
	}

	logger, _, closeLogger, err := deps.NewLogger(config.Default().Log, stdout)
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

	logger.Info().Msg("Self-update helper starting")
	if _, err := fmt.Fprintln(stdout, docker.SelfUpdateHelperReadyMarker); err != nil {
		return fmt.Errorf("acknowledge self-update helper readiness: %w", err)
	}
	request := selfupdate.UpdaterRequest{
		TargetContainerID: values.targetContainer,
		TargetImageID:     values.newImage,
		StopTimeout:       values.helperStop,
		StartupTimeout:    values.helperStartup,
	}
	if err := deps.RunHelper(ctx, client, request); err != nil {
		return fmt.Errorf("run self-update helper: %w", err)
	}
	return nil
}

func mergeCloseError(runErr *error, resource string, closeResource func() error) {
	if err := closeResource(); err != nil && *runErr == nil {
		*runErr = fmt.Errorf("close %s: %w", resource, err)
	}
}
