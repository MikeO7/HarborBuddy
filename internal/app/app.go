package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MikeO7/HarborBuddy/internal/buildinfo"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/scheduler"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

// DockerClient is the application-facing Docker client contract.
type DockerClient interface {
	docker.Client
	Close() error
}

// Dependencies contains process-bound operations that tests can replace.
type Dependencies struct {
	LookupEnv         func(string) (string, bool)
	NewDockerClient   func(context.Context, string) (DockerClient, error)
	NewLogger         func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error)
	StartLevelSignals func(context.Context, zerolog.Logger, *logging.LevelController) func()
	RunScheduler      func(context.Context, config.Config, docker.Client, zerolog.Logger) error
	RunHelper         func(context.Context, docker.Client, selfupdate.UpdaterRequest) error
}

var newFlagSetFn = newFlagSet

// Run executes HarborBuddy without terminating the process.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return RunWithDependencies(ctx, args, stdout, stderr, defaultDependencies())
}

// RunWithDependencies is Run with replaceable process integrations.
func RunWithDependencies(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) error {
	ctx, stdout, stderr = normalizeProcessInputs(ctx, stdout, stderr)
	if err := validateDependencies(deps); err != nil {
		return err
	}

	flags, values, err := newFlagSetFn(stderr)
	if err != nil {
		return err
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if values.version {
		_, err := fmt.Fprintln(stdout, buildinfo.String())
		return err
	}
	if values.updaterMode {
		return runHelper(ctx, stdout, deps, values)
	}

	cfg, err := loadConfig(flags, values, deps.LookupEnv)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	return runDaemon(ctx, stdout, deps, cfg)
}

func normalizeProcessInputs(ctx context.Context, stdout, stderr io.Writer) (context.Context, io.Writer, io.Writer) {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return ctx, stdout, stderr
}

func defaultDependencies() Dependencies {
	return Dependencies{
		LookupEnv: os.LookupEnv,
		NewDockerClient: func(ctx context.Context, host string) (DockerClient, error) {
			return docker.NewClient(ctx, host)
		},
		NewLogger:         logging.New,
		StartLevelSignals: logging.NotifyLevelSignals,
		RunScheduler:      scheduler.Run,
		RunHelper:         selfupdate.RunUpdater,
	}
}

func validateDependencies(deps Dependencies) error {
	switch {
	case deps.LookupEnv == nil:
		return errors.New("app dependency LookupEnv is nil")
	case deps.NewDockerClient == nil:
		return errors.New("app dependency NewDockerClient is nil")
	case deps.NewLogger == nil:
		return errors.New("app dependency NewLogger is nil")
	case deps.StartLevelSignals == nil:
		return errors.New("app dependency StartLevelSignals is nil")
	case deps.RunScheduler == nil:
		return errors.New("app dependency RunScheduler is nil")
	case deps.RunHelper == nil:
		return errors.New("app dependency RunHelper is nil")
	default:
		return nil
	}
}
