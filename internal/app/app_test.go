package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/buildinfo"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
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

func TestHelperFlagsAreHidden(t *testing.T) {
	flags, _, err := newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	flags.SetOutput(&output)
	flags.PrintDefaults()
	for _, name := range []string{"updater-mode", "target-container-id", "new-image-id", "helper-stop-timeout", "helper-startup-timeout"} {
		if strings.Contains(output.String(), name) {
			t.Fatalf("help output contains hidden flag %q: %s", name, output.String())
		}
	}
}

func TestHelperModeInvokesHelper(t *testing.T) {
	deps := testDependencies(map[string]string{"HARBORBUDDY_DOCKER_HOST": "tcp://helper:2375"})
	var gotHost, gotTarget, gotImage string
	var gotStop, gotStartup time.Duration
	deps.NewDockerClient = func(_ context.Context, host string) (DockerClient, error) {
		gotHost = host
		return &fakeDockerClient{}, nil
	}
	deps.RunHelper = func(_ context.Context, _ docker.Client, request selfupdate.UpdaterRequest) error {
		gotTarget, gotImage = request.TargetContainerID, request.TargetImageID
		gotStop, gotStartup = request.StopTimeout, request.StartupTimeout
		return nil
	}

	var stdout bytes.Buffer
	err := RunWithDependencies(context.Background(), []string{
		"--updater-mode",
		"--target-container-id=target",
		"--new-image-id=image",
		"--helper-stop-timeout=7s",
		"--helper-startup-timeout=19s",
	}, &stdout, &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if gotHost != "tcp://helper:2375" || gotTarget != "target" || gotImage != "image" || gotStop != 7*time.Second || gotStartup != 19*time.Second {
		t.Fatalf("helper arguments host=%q target=%q image=%q stop=%s startup=%s", gotHost, gotTarget, gotImage, gotStop, gotStartup)
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

func testDependencies(env map[string]string) Dependencies {
	return Dependencies{
		LookupEnv: func(name string) (string, bool) {
			value, ok := env[name]
			return value, ok
		},
		NewDockerClient: func(context.Context, string) (DockerClient, error) {
			return &fakeDockerClient{}, nil
		},
		NewLogger: func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
			return zerolog.Nop(), &logging.LevelController{}, func() error { return nil }, nil
		},
		StartLevelSignals: func(context.Context, zerolog.Logger, *logging.LevelController) func() {
			return func() {}
		},
		RunScheduler: func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return nil },
		RunHelper:    func(context.Context, docker.Client, selfupdate.UpdaterRequest) error { return nil },
	}
}

type fakeDockerClient struct {
	closed   bool
	closeErr error
}

func (c *fakeDockerClient) Close() error {
	c.closed = true
	return c.closeErr
}
func (c *fakeDockerClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return nil, nil
}
func (c *fakeDockerClient) InspectContainer(context.Context, string) (docker.ContainerDetails, error) {
	return docker.ContainerDetails{}, nil
}
func (c *fakeDockerClient) PullImage(context.Context, string) (docker.ImageInfo, error) {
	return docker.ImageInfo{}, nil
}
func (c *fakeDockerClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error {
	return nil
}
func (c *fakeDockerClient) ReplaceContainer(context.Context, docker.ContainerDetails, docker.ImageInfo, docker.ReplaceOptions) (docker.ReplaceResult, error) {
	return docker.ReplaceResult{}, nil
}
func (c *fakeDockerClient) ListImages(context.Context) ([]docker.ImageInfo, error) { return nil, nil }
func (c *fakeDockerClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (c *fakeDockerClient) RemoveImage(context.Context, string) error { return nil }

var _ DockerClient = (*fakeDockerClient)(nil)

func writeAppConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harborbuddy.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
