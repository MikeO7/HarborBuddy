package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/buildinfo"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func TestRunConfigurationPrecedence(t *testing.T) {
	path := writeAppConfig(t, `
updates:
  check_interval: 1h
  dry_run: false
  self_update: false
log:
  level: info
`)
	env := map[string]string{
		"HARBORBUDDY_CONFIG":              path,
		"HARBORBUDDY_INTERVAL":            "2h",
		"HARBORBUDDY_DRY_RUN":             "true",
		"HARBORBUDDY_SELF_UPDATE_ENABLED": "true",
		"HARBORBUDDY_LOG_LEVEL":           "debug",
	}
	var got config.Config
	deps := testDependencies(env)
	deps.RunScheduler = func(_ context.Context, cfg config.Config, _ docker.Client, _ zerolog.Logger) error {
		got = cfg
		return nil
	}

	err := RunWithDependencies(context.Background(), []string{
		"--interval=3h",
		"--dry-run=false",
		"--log-level=error",
	}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if got.Updates.CheckInterval != 3*time.Hour || got.Updates.DryRun || !got.Updates.SelfUpdate || got.Log.Level != "error" {
		t.Fatalf("merged config = %+v", got)
	}
}

func TestExplicitConfigBeatsEnvironmentConfig(t *testing.T) {
	envPath := writeAppConfig(t, "updates:\n  check_interval: 2h\n")
	cliPath := writeAppConfig(t, "updates:\n  check_interval: 3h\n")
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": envPath})
	var got config.Config
	deps.RunScheduler = func(_ context.Context, cfg config.Config, _ docker.Client, _ zerolog.Logger) error {
		got = cfg
		return nil
	}

	if err := RunWithDependencies(context.Background(), []string{"--config", cliPath}, &bytes.Buffer{}, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if got.Updates.CheckInterval != 3*time.Hour {
		t.Fatalf("CheckInterval = %v, want CLI config value", got.Updates.CheckInterval)
	}
}

func TestMissingExplicitConfigReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	deps := testDependencies(nil)
	called := false
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) {
		called = true
		return &fakeDockerClient{}, nil
	}

	err := RunWithDependencies(context.Background(), []string{"--config", missing}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "open config file") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
	if called {
		t.Fatal("Docker client was created after config error")
	}
}

func TestMissingEnvironmentConfigReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": missing})
	err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "open config file") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
}

func TestInvalidEnvironmentValueReturnsError(t *testing.T) {
	deps := testDependencies(map[string]string{"HARBORBUDDY_DRY_RUN": "perhaps"})
	err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "HARBORBUDDY_DRY_RUN") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
}

func TestInvalidSelfUpdateEnvironmentValueReturnsError(t *testing.T) {
	deps := testDependencies(map[string]string{"HARBORBUDDY_SELF_UPDATE_ENABLED": "sometimes"})
	err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "HARBORBUDDY_SELF_UPDATE_ENABLED") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
}

func TestVersionExitsBeforeRuntimeInitialization(t *testing.T) {
	oldVersion, oldCommit, oldDate := buildinfo.Version, buildinfo.Commit, buildinfo.Date
	t.Cleanup(func() { buildinfo.Version, buildinfo.Commit, buildinfo.Date = oldVersion, oldCommit, oldDate })
	buildinfo.Version, buildinfo.Commit, buildinfo.Date = "9.8.7", "commit", "date"

	deps := testDependencies(nil)
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) {
		t.Fatal("NewDockerClient called for --version")
		return nil, nil
	}
	var stdout bytes.Buffer
	if err := RunWithDependencies(context.Background(), []string{"--version"}, &stdout, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "9.8.7") || !strings.Contains(stdout.String(), "commit") || !strings.Contains(stdout.String(), "date") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunUsesDefaultDependenciesForVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"--version"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(--version) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "HarborBuddy") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunNormalizesNilProcessInputs(t *testing.T) {
	deps := testDependencies(nil)
	called := false
	deps.RunScheduler = func(ctx context.Context, _ config.Config, _ docker.Client, _ zerolog.Logger) error {
		called = ctx != nil
		return nil
	}
	path := writeAppConfig(t, "updates:\n  enabled: false\ncleanup:\n  enabled: false\n")
	deps.LookupEnv = func(name string) (string, bool) {
		if name == "HARBORBUDDY_CONFIG" {
			return path, true
		}
		return "", false
	}
	//nolint:staticcheck // This test verifies the public boundary normalizes a nil context.
	if err := RunWithDependencies(nil, nil, nil, nil, deps); err != nil {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
	if !called {
		t.Fatal("normalized background context was not passed to scheduler")
	}
}

func TestRunReportsFlagVersionWriteAndValidationErrors(t *testing.T) {
	deps := testDependencies(nil)
	deps.LookupEnv = nil
	if err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps); err == nil || !strings.Contains(err.Error(), "LookupEnv") {
		t.Fatalf("missing dependency error = %v", err)
	}
	deps = testDependencies(nil)
	if err := RunWithDependencies(context.Background(), []string{"--does-not-exist"}, &bytes.Buffer{}, &bytes.Buffer{}, deps); err == nil {
		t.Fatal("unknown flag returned nil error")
	}
	if err := RunWithDependencies(context.Background(), []string{"--version"}, failingWriter{}, &bytes.Buffer{}, deps); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("version write error = %v", err)
	}
	path := writeAppConfig(t, "updates:\n  check_interval: 0s\n")
	deps = testDependencies(map[string]string{"HARBORBUDDY_CONFIG": path})
	if err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps); err == nil || !strings.Contains(err.Error(), "validate configuration") {
		t.Fatalf("configuration validation error = %v", err)
	}
}

func TestNewFlagSetReportsInjectedHideFailure(t *testing.T) {
	original := hideFlagsFn
	hideFlagsFn = func(*pflag.FlagSet, ...string) error { return errors.New("hide failed") }
	t.Cleanup(func() { hideFlagsFn = original })
	if _, _, err := newFlagSet(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "hide failed") {
		t.Fatalf("newFlagSet() error = %v", err)
	}
}

func TestRunReportsInjectedFlagConstructionFailure(t *testing.T) {
	original := newFlagSetFn
	newFlagSetFn = func(io.Writer) (*pflag.FlagSet, *flagValues, error) {
		return nil, nil, errors.New("flag construction failed")
	}
	t.Cleanup(func() { newFlagSetFn = original })
	if err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, testDependencies(nil)); err == nil || !strings.Contains(err.Error(), "flag construction failed") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
}

func TestValidateDependenciesReportsEveryMissingDependency(t *testing.T) {
	base := testDependencies(nil)
	tests := []struct {
		name string
		edit func(*Dependencies)
		want string
	}{
		{name: "lookup env", edit: func(d *Dependencies) { d.LookupEnv = nil }, want: "LookupEnv"},
		{name: "docker client", edit: func(d *Dependencies) { d.NewDockerClient = nil }, want: "NewDockerClient"},
		{name: "logger", edit: func(d *Dependencies) { d.NewLogger = nil }, want: "NewLogger"},
		{name: "signals", edit: func(d *Dependencies) { d.StartLevelSignals = nil }, want: "StartLevelSignals"},
		{name: "scheduler", edit: func(d *Dependencies) { d.RunScheduler = nil }, want: "RunScheduler"},
		{name: "helper", edit: func(d *Dependencies) { d.RunHelper = nil }, want: "RunHelper"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := base
			test.edit(&deps)
			err := validateDependencies(deps)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateDependencies() error = %v, want %q", err, test.want)
			}
		})
	}
	if err := validateDependencies(base); err != nil {
		t.Fatalf("valid dependencies returned error: %v", err)
	}
}

func TestHelperModeInvokesHelper(t *testing.T) {
	deps := testDependencies(map[string]string{"HARBORBUDDY_DOCKER_HOST": "tcp://helper:2375"})
	var gotHost, gotTarget, gotImage string
	var gotStop, gotStartup time.Duration
	var gotRestart containertypes.RestartPolicy
	deps.NewDockerClient = func(_ context.Context, host string) (DockerClient, error) {
		gotHost = host
		return &fakeDockerClient{}, nil
	}
	deps.RunHelper = func(_ context.Context, _ docker.Client, request selfupdate.UpdaterRequest) error {
		gotTarget, gotImage = request.TargetContainerID, request.TargetImageID
		gotStop, gotStartup = request.StopTimeout, request.StartupTimeout
		gotRestart = request.RestartPolicy
		return nil
	}

	var stdout bytes.Buffer
	err := RunWithDependencies(context.Background(), []string{
		"--updater-mode",
		"--target-container-id=target",
		"--new-image-id=image",
		"--helper-stop-timeout=7s",
		"--helper-startup-timeout=19s",
		"--helper-restart-policy=unless-stopped",
		"--helper-restart-max-retries=3",
	}, &stdout, &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if gotHost != "tcp://helper:2375" || gotTarget != "target" || gotImage != "image" || gotStop != 7*time.Second || gotStartup != 19*time.Second || gotRestart.Name != "unless-stopped" || gotRestart.MaximumRetryCount != 3 {
		t.Fatalf("helper arguments host=%q target=%q image=%q stop=%s startup=%s restart=%+v", gotHost, gotTarget, gotImage, gotStop, gotStartup, gotRestart)
	}
	if !strings.Contains(stdout.String(), docker.SelfUpdateHelperReadyMarker) {
		t.Fatalf("helper readiness marker missing from output: %q", stdout.String())
	}
}

func TestHelperModeRequiresInternalArguments(t *testing.T) {
	deps := testDependencies(nil)
	err := RunWithDependencies(context.Background(), []string{"--updater-mode"}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "requires --target-container-id") {
		t.Fatalf("RunWithDependencies() error = %v", err)
	}
}

func TestHelperModeRejectsNegativeTimeout(t *testing.T) {
	deps := testDependencies(nil)
	err := RunWithDependencies(context.Background(), []string{
		"--updater-mode",
		"--target-container-id=target",
		"--new-image-id=image",
		"--helper-stop-timeout=-1s",
	}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "timeouts cannot be negative") {
		t.Fatalf("RunWithDependencies() error = %v, want negative-timeout error", err)
	}
	err = RunWithDependencies(context.Background(), []string{
		"--updater-mode",
		"--target-container-id=target",
		"--new-image-id=image",
		"--helper-restart-max-retries=-1",
	}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "restart retries cannot be negative") {
		t.Fatalf("RunWithDependencies() error = %v, want negative-retry error", err)
	}
}

func TestRunTreatsSelfUpdateSignalAsSuccess(t *testing.T) {
	path := writeAppConfig(t, "updates:\n  enabled: false\ncleanup:\n  enabled: false\n")
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": path})
	deps.RunScheduler = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
	}
	if err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps); err != nil {
		t.Fatalf("RunWithDependencies() error = %v, want successful shutdown", err)
	}
}

func TestRunClosesDockerAndLogger(t *testing.T) {
	path := writeAppConfig(t, "updates:\n  enabled: false\ncleanup:\n  enabled: false\n")
	client := &fakeDockerClient{}
	loggerClosed := false
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": path})
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return client, nil }
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.Nop(), &logging.LevelController{}, func() error { loggerClosed = true; return nil }, nil
	}

	if err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if !client.closed || !loggerClosed {
		t.Fatalf("client.closed=%v loggerClosed=%v", client.closed, loggerClosed)
	}
}

func TestRunReturnsCloseErrorWhenWorkSucceeds(t *testing.T) {
	path := writeAppConfig(t, "updates:\n  enabled: false\ncleanup:\n  enabled: false\n")
	closeErr := errors.New("close failed")
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": path})
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) {
		return &fakeDockerClient{closeErr: closeErr}, nil
	}

	err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "close Docker client") {
		t.Fatalf("RunWithDependencies() error = %v, want Docker close error", err)
	}
}

func TestRunKeepsSchedulerErrorWhenCloseAlsoFails(t *testing.T) {
	path := writeAppConfig(t, "updates:\n  enabled: false\ncleanup:\n  enabled: false\n")
	runErr := errors.New("scheduler failed")
	closeErr := errors.New("close failed")
	deps := testDependencies(map[string]string{"HARBORBUDDY_CONFIG": path})
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) {
		return &fakeDockerClient{closeErr: closeErr}, nil
	}
	deps.RunScheduler = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return runErr
	}

	err := RunWithDependencies(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if !errors.Is(err, runErr) || errors.Is(err, closeErr) {
		t.Fatalf("RunWithDependencies() error = %v, want only scheduler error", err)
	}
}
