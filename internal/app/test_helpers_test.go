package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

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
