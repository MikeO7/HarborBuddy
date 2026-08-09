package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

func TestRunDaemonReportsInitializationAndSchedulerFailures(t *testing.T) {
	loggerErr := errors.New("logger unavailable")
	deps := testDependencies(nil)
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.Logger{}, nil, nil, loggerErr
	}
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, config.Default()); !errors.Is(err, loggerErr) || !strings.Contains(err.Error(), "initialize logging") {
		t.Fatalf("runDaemon() error = %v, want logging initialization error", err)
	}

	dockerErr := errors.New("socket unavailable")
	deps = testDependencies(nil)
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return nil, dockerErr }
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, config.Default()); !errors.Is(err, dockerErr) || !strings.Contains(err.Error(), "connect to Docker") {
		t.Fatalf("runDaemon() error = %v, want Docker connection error", err)
	}

	runErr := errors.New("cycle failed")
	client := &fakeDockerClient{}
	deps = testDependencies(nil)
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return client, nil }
	deps.StartLevelSignals = func(context.Context, zerolog.Logger, *logging.LevelController) func() { return nil }
	deps.RunScheduler = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return runErr }
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, config.Default()); !errors.Is(err, runErr) || !strings.Contains(err.Error(), "run scheduler") {
		t.Fatalf("runDaemon() error = %v, want scheduler error", err)
	}
	if !client.closed {
		t.Fatal("Docker client was not closed after scheduler failure")
	}
}

func TestRunDaemonCloseLoggerErrorIsReturnedWhenWorkSucceeds(t *testing.T) {
	closeErr := errors.New("logger close failed")
	deps := testDependencies(nil)
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.Nop(), &logging.LevelController{}, func() error { return closeErr }, nil
	}
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, config.Default()); !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "close logging") {
		t.Fatalf("runDaemon() error = %v, want logger close error", err)
	}
}

func TestRunHelperReportsDependencyFailuresAndCloseErrors(t *testing.T) {
	loggerErr := errors.New("helper logger failed")
	deps := testDependencies(nil)
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.Logger{}, nil, nil, loggerErr
	}
	values := &flagValues{targetContainer: "target", newImage: "image"}
	if err := runHelper(context.Background(), &bytes.Buffer{}, deps, values); !errors.Is(err, loggerErr) || !strings.Contains(err.Error(), "initialize helper logging") {
		t.Fatalf("runHelper() error = %v, want logger error", err)
	}

	dockerErr := errors.New("helper socket failed")
	deps = testDependencies(nil)
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return nil, dockerErr }
	if err := runHelper(context.Background(), &bytes.Buffer{}, deps, values); !errors.Is(err, dockerErr) || !strings.Contains(err.Error(), "connect helper to Docker") {
		t.Fatalf("runHelper() error = %v, want Docker error", err)
	}

	runErr := errors.New("replacement failed")
	closeErr := errors.New("helper close failed")
	client := &fakeDockerClient{closeErr: closeErr}
	deps = testDependencies(nil)
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return client, nil }
	deps.RunHelper = func(context.Context, docker.Client, selfupdate.UpdaterRequest) error { return runErr }
	if err := runHelper(context.Background(), &bytes.Buffer{}, deps, values); !errors.Is(err, runErr) || errors.Is(err, closeErr) || !strings.Contains(err.Error(), "run self-update helper") {
		t.Fatalf("runHelper() error = %v, want helper error only", err)
	}

	deps.RunHelper = func(context.Context, docker.Client, selfupdate.UpdaterRequest) error { return nil }
	if err := runHelper(context.Background(), &bytes.Buffer{}, deps, values); !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "close Docker client") {
		t.Fatalf("runHelper() error = %v, want Docker close error", err)
	}
}

func TestMergeCloseErrorPreservesExistingRunError(t *testing.T) {
	runErr := errors.New("run failed")
	closeErr := errors.New("close failed")
	mergeCloseError(&runErr, "resource", func() error { return closeErr })
	if !errors.Is(runErr, runErr) || errors.Is(runErr, closeErr) {
		t.Fatalf("mergeCloseError replaced existing error: %v", runErr)
	}
	var nilErr error
	mergeCloseError(&nilErr, "resource", func() error { return nil })
	if nilErr != nil {
		t.Fatalf("mergeCloseError(nil close error) = %v", nilErr)
	}
}

func TestRunHelperReportsReadinessWriteFailure(t *testing.T) {
	deps := testDependencies(nil)
	values := &flagValues{targetContainer: "target", newImage: "image"}
	if err := runHelper(context.Background(), failingWriter{}, deps, values); err == nil || !strings.Contains(err.Error(), "acknowledge self-update helper readiness") {
		t.Fatalf("runHelper() error = %v, want readiness write error", err)
	}
}

func TestDefaultDependenciesExposeWorkingProcessIntegrations(t *testing.T) {
	deps := defaultDependencies()
	if deps.LookupEnv == nil || deps.NewDockerClient == nil || deps.NewLogger == nil || deps.StartLevelSignals == nil || deps.RunScheduler == nil || deps.RunHelper == nil {
		t.Fatal("default dependencies contain a nil integration")
	}
	cfg := config.Default()
	cfg.RunOnce = true
	cfg.Updates.Enabled = false
	cfg.Cleanup.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := deps.RunScheduler(ctx, cfg, nil, zerolog.Nop()); err != nil {
		t.Fatalf("default scheduler dependency error = %v", err)
	}
	if err := deps.RunHelper(ctx, nil, selfupdate.UpdaterRequest{}); err == nil {
		t.Fatal("default helper dependency accepted incomplete request")
	}
	stop := deps.StartLevelSignals(ctx, zerolog.Nop(), &logging.LevelController{})
	if stop == nil {
		t.Fatal("default signal dependency returned nil stop function")
	}
	stop()
	if _, err := deps.NewDockerClient(ctx, "unix:///definitely-missing-harborbuddy.sock"); err == nil {
		t.Fatal("default Docker dependency connected to missing socket")
	}
}
