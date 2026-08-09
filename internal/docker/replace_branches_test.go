package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestCheckReplacementCoversSafetyBoundaries(t *testing.T) {
	base := replacementFixture()
	tests := []struct {
		name    string
		mutate  func(*ContainerDetails)
		stopped bool
		valid   bool
		want    string
	}{
		{name: "missing id", mutate: func(c *ContainerDetails) { c.Summary.ID = "" }, want: "identity is incomplete"},
		{name: "missing name", mutate: func(c *ContainerDetails) { c.Summary.Name = "" }, want: "identity is incomplete"},
		{name: "missing config", mutate: func(c *ContainerDetails) { c.Config = nil }, want: "inspection is incomplete"},
		{name: "missing host", mutate: func(c *ContainerDetails) { c.Host = nil }, want: "inspection is incomplete"},
		{name: "missing state", mutate: func(c *ContainerDetails) { c.State = nil }, want: "inspection is incomplete"},
		{name: "stopped", mutate: func(c *ContainerDetails) { c.State.Running = false }, want: "no longer running"},
		{name: "missing target", mutate: func(*ContainerDetails) {}, want: "target image identity is missing"},
		{name: "auto remove", mutate: func(c *ContainerDetails) { c.Host.AutoRemove = true }, want: "auto-remove"},
		{name: "network namespace", mutate: func(c *ContainerDetails) { c.Host.NetworkMode = "container:other" }, want: "namespace dependencies"},
		{name: "pid namespace", mutate: func(c *ContainerDetails) { c.Host.PidMode = "container:other" }, want: "namespace dependencies"},
		{name: "ipc namespace", mutate: func(c *ContainerDetails) { c.Host.IpcMode = "container:other" }, want: "namespace dependencies"},
		{name: "uts namespace", mutate: func(c *ContainerDetails) { c.Host.UTSMode = "container:other" }, want: "namespace dependencies"},
		{name: "swarm task", mutate: func(c *ContainerDetails) { c.Config.Labels = map[string]string{"com.docker.swarm.task.id": "task"} }, want: "Swarm"},
		{name: "anonymous volume", mutate: func(c *ContainerDetails) {
			c.Mounts = []containertypes.MountPoint{{Type: mount.TypeVolume, Destination: "/data"}}
		}, want: "no reusable Docker volume"},
		{name: "anonymous bind", mutate: func(c *ContainerDetails) {
			c.Mounts = []containertypes.MountPoint{{Type: mount.TypeBind, Destination: "/data"}}
		}, want: "no reusable source"},
		{name: "named pipe", mutate: func(c *ContainerDetails) {
			c.Mounts = []containertypes.MountPoint{{Type: mount.TypeNamedPipe, Destination: "/pipe"}}
		}, want: "cannot be recreated safely"},
		{name: "unknown mount", mutate: func(c *ContainerDetails) {
			c.Mounts = []containertypes.MountPoint{{Type: mount.Type("unknown"), Destination: "/other"}}
		}, want: "unknown mount type"},
		{name: "configured mount is safe", mutate: func(c *ContainerDetails) {
			c.Host.Binds = []string{"/host/data:/data:ro"}
			c.Mounts = []containertypes.MountPoint{{Type: mount.TypeVolume, Destination: "/data"}}
		}, valid: true},
		{name: "stopped helper mode", mutate: func(c *ContainerDetails) { c.State.Running = false }, stopped: true, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := cloneDetailsForTest(base)
			test.mutate(&current)
			target := ImageInfo{ID: "new"}
			if test.name == "missing target" {
				target.ID = ""
			}
			err := checkReplacement(current, target, test.stopped)
			if test.valid {
				if err != nil {
					t.Fatalf("checkReplacement() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) || !IsUnsupported(err) {
				t.Fatalf("checkReplacement() error = %v, want unsupported %q", err, test.want)
			}
		})
	}
}

func TestReplacementHelpersCoverMountsOptionsAndTimeouts(t *testing.T) {
	host := &containertypes.HostConfig{}
	if hasContainerNamespaceDependency(host) {
		t.Fatal("empty host unexpectedly has namespace dependency")
	}
	for _, mountPoint := range []containertypes.MountPoint{
		{Type: mount.TypeVolume, Name: "volume", Destination: "/volume"},
		{Type: mount.TypeBind, Source: "/host", Destination: "/bind"},
		{Type: mount.TypeTmpfs, Destination: "/tmp"},
	} {
		if err := validateReusableMount(mountPoint); err != nil {
			t.Fatalf("validateReusableMount(%+v) error = %v", mountPoint, err)
		}
	}
	if err := validateInspectedMounts(ContainerDetails{Host: host, Mounts: nil}); err != nil {
		t.Fatalf("validateInspectedMounts(empty) error = %v", err)
	}

	options := normalizeReplaceOptions(ReplaceOptions{})
	if options.StopTimeout != 10*time.Second || options.StartupTimeout != 30*time.Second || options.StabilizationTime != 2*time.Second || options.PollInterval != 250*time.Millisecond {
		t.Fatalf("normalizeReplaceOptions() = %+v", options)
	}
	if seconds, err := dockerTimeoutSeconds(1500 * time.Millisecond); err != nil || seconds != 2 {
		t.Fatalf("dockerTimeoutSeconds(1.5s) = %d, %v", seconds, err)
	}
	if seconds, err := dockerTimeoutSeconds(0); err != nil || seconds != 0 {
		t.Fatalf("dockerTimeoutSeconds(0) = %d, %v", seconds, err)
	}
	original := dockerPlatformIntSize
	dockerPlatformIntSize = 32
	t.Cleanup(func() { dockerPlatformIntSize = original })
	if _, err := dockerTimeoutSeconds(time.Duration(1<<31) * time.Second); err == nil || !strings.Contains(err.Error(), "platform limit") {
		t.Fatalf("dockerTimeoutSeconds(platform overflow) = %v", err)
	}
}

func TestReplaceContainerReportsStopTimeoutAndRestoresRestartPolicy(t *testing.T) {
	client := &DockerClient{}
	if _, err := client.ReplaceContainer(context.Background(), ContainerDetails{}, ImageInfo{ID: "new"}, ReplaceOptions{}); err == nil || !IsUnsupported(err) {
		t.Fatalf("ReplaceContainer(incomplete) error = %v", err)
	}
	originalSize := dockerPlatformIntSize
	dockerPlatformIntSize = 32
	t.Cleanup(func() { dockerPlatformIntSize = originalSize })
	client = &DockerClient{}
	if _, err := client.ReplaceContainer(context.Background(), replacementFixture(), ImageInfo{ID: "new"}, ReplaceOptions{StopTimeout: time.Duration(1<<31) * time.Second}); err == nil || !strings.Contains(err.Error(), "platform limit") {
		t.Fatalf("ReplaceContainer(timeout overflow) error = %v", err)
	}

	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/old/update", noContent)
	client = testDockerClient(t, transport)
	current := replacementFixture()
	current.Host.RestartPolicy.Name = "always"
	stopErr := errors.New("stop failed")
	if err := client.handleStopFailure(context.Background(), current, true, stopErr); !errors.Is(err, stopErr) {
		t.Fatalf("handleStopFailure() error = %v", err)
	}
	if err := client.handleStopFailure(context.Background(), current, false, stopErr); !errors.Is(err, stopErr) {
		t.Fatalf("handleStopFailure(no suppression) error = %v", err)
	}
}

func TestStartReplacementReportsInspectMismatchStartAndReadinessFailures(t *testing.T) {
	tests := []struct {
		name      string
		inspect   func() containertypes.InspectResponse
		startCode int
		want      string
	}{
		{name: "inspect", want: "inspect replacement"},
		{name: "mismatch", inspect: func() containertypes.InspectResponse { return inspectedContainer("new", "app", "sha256:other", true) }, want: "image mismatch"},
		{name: "start", inspect: func() containertypes.InspectResponse { return inspectedContainer("new", "app", "sha256:new", true) }, startCode: http.StatusInternalServerError, want: "start replacement"},
		{name: "readiness", inspect: func() containertypes.InspectResponse {
			response := inspectedContainer("new", "app", "sha256:new", true)
			response.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "true"}}
			response.State.Health = &containertypes.Health{Status: "unhealthy"}
			return response
		}, want: "replacement did not become ready"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			if test.inspect != nil {
				transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) { return jsonResponse(http.StatusOK, test.inspect()) })
			}
			if test.startCode != 0 {
				transport.register("POST", "/v1.41/containers/new/start", func(*http.Request) (*http.Response, error) {
					return jsonResponse(test.startCode, map[string]string{"message": "start failed"})
				})
			} else {
				transport.register("POST", "/v1.41/containers/new/start", noContent)
			}
			client := testDockerClient(t, transport)
			err := client.startReplacement(context.Background(), "new", ImageInfo{ID: "sha256:new"}, normalizeReplaceOptions(ReplaceOptions{StartupTimeout: 20 * time.Millisecond, PollInterval: time.Millisecond}))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("startReplacement() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReplaceContainerReportsTransactionBoundaryFailures(t *testing.T) {
	tests := []struct {
		name     string
		current  func() ContainerDetails
		register func(*mockTransport)
		want     string
	}{
		{name: "restart policy", current: func() ContainerDetails { c := replacementFixture(); c.Host.RestartPolicy.Name = "always"; return c }, want: "disable old container restart policy"},
		{name: "stop", register: func(m *mockTransport) {
			m.register("POST", "/v1.41/containers/old/stop", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "stop failed"})
			})
		}, want: "stop old container"},
		{name: "disconnect", current: func() ContainerDetails {
			c := replacementFixture()
			c.Networks = map[string]*network.EndpointSettings{"net": {}}
			return c
		}, register: func(m *mockTransport) {
			m.register("POST", "/v1.41/containers/old/stop", noContent)
			m.register("POST", "/v1.41/networks/net/disconnect", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "disconnect failed"})
			})
		}, want: "release old container network endpoints"},
		{name: "rename", register: func(m *mockTransport) { m.register("POST", "/v1.41/containers/old/stop", noContent) }, want: "rename old container"},
		{name: "create", register: func(m *mockTransport) {
			m.register("POST", "/v1.41/containers/old/stop", noContent)
			m.register("POST", "/v1.41/containers/old/rename", noContent)
			m.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "create failed"})
			})
		}, want: "create replacement container"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			if test.register != nil {
				test.register(transport)
			}
			client := testDockerClient(t, transport)
			current := replacementFixture()
			if test.current != nil {
				current = test.current()
			}
			_, err := client.ReplaceContainer(context.Background(), current, ImageInfo{ID: "sha256:new"}, ReplaceOptions{StopTimeout: time.Second, StartupTimeout: time.Second})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReplaceContainer() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReplaceContainerReportsBackupCleanupWarning(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/old/stop", noContent)
	transport.register("POST", "/v1.41/containers/old/rename", noContent)
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "new"})
	})
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		response := inspectedContainer("new", "app", "sha256:new", true)
		response.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "true"}}
		response.State.Health = &containertypes.Health{Status: "healthy"}
		return jsonResponse(http.StatusOK, response)
	})
	transport.register("POST", "/v1.41/containers/new/start", noContent)
	transport.register("DELETE", "/v1.41/containers/old", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "backup busy"})
	})
	client := testDockerClient(t, transport)
	result, err := client.ReplaceContainer(context.Background(), replacementFixture(), ImageInfo{ID: "sha256:new"}, ReplaceOptions{StopTimeout: time.Second, StartupTimeout: time.Second})
	if err != nil || result.BackupCleanupErr == nil {
		t.Fatalf("ReplaceContainer() = %+v, %v; want backup cleanup warning", result, err)
	}
}

func TestRollbackHelpersReportAndAggregateFailures(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/networks/a/disconnect", noContent)
	transport.register("POST", "/v1.41/networks/b/disconnect", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "disconnect"})
	})
	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Networks = map[string]*network.EndpointSettings{"a": {}, "b": {}}
	if disconnected, err := client.disconnectNetworks(context.Background(), current); err == nil || len(disconnected) != 1 || !strings.Contains(err.Error(), "network b") {
		t.Fatalf("disconnectNetworks() = %v, %v", disconnected, err)
	}

	transport = newMockTransport()
	client = testDockerClient(t, transport)
	current = replacementFixture()
	current.Host.RestartPolicy.Name = "always"
	if ok, err := client.suppressRestartPolicy(context.Background(), current); err == nil || ok || !strings.Contains(err.Error(), "disable old container") {
		t.Fatalf("suppressRestartPolicy() = %v, %v", ok, err)
	}
	if err := client.restoreRestartPolicy(context.Background(), current); err == nil || !strings.Contains(err.Error(), "restore original restart policy") {
		t.Fatalf("restoreRestartPolicy() error = %v", err)
	}

	transport = newMockTransport()
	transport.register("POST", "/v1.41/containers/old/stop", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "stop"})
	})
	transport.register("GET", "/v1.41/containers/old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("old", "app", "sha256:old", false))
	})
	client = testDockerClient(t, transport)
	if err := client.ensureContainerStopped(context.Background(), "old", 1); err != nil {
		t.Fatalf("ensureContainerStopped() should accept an already exited container: %v", err)
	}

	transport = newMockTransport()
	transport.register("POST", "/v1.41/containers/old/stop", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "stop"})
	})
	transport.register("GET", "/v1.41/containers/old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "inspect"})
	})
	client = testDockerClient(t, transport)
	if err := client.ensureContainerStopped(context.Background(), "old", 1); err == nil || !strings.Contains(err.Error(), "stop old container") {
		t.Fatalf("ensureContainerStopped() error = %v", err)
	}

	transport = newMockTransport()
	transport.register("POST", "/v1.41/containers/replacement/stop", noContent)
	transport.register("DELETE", "/v1.41/containers/replacement", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "remove"})
	})
	client = testDockerClient(t, transport)
	if err := client.removeReplacement(context.Background(), "replacement", 1); err == nil || !strings.Contains(err.Error(), "remove failed replacement") {
		t.Fatalf("removeReplacement() error = %v", err)
	}

	transport = newMockTransport()
	transport.register("POST", "/v1.41/containers/old/rename", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "rename"})
	})
	transport.register("POST", "/v1.41/containers/old/start", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "start"})
	})
	client = testDockerClient(t, transport)
	if err := client.restoreContainerName(context.Background(), current); err == nil || !strings.Contains(err.Error(), "restore original container name") {
		t.Fatalf("restoreContainerName() error = %v", err)
	}
	if err := client.restartOldContainer(context.Background(), "old"); err == nil || !strings.Contains(err.Error(), "restart original container") {
		t.Fatalf("restartOldContainer() error = %v", err)
	}

	if got := appendError(nil, nil); got != nil {
		t.Fatalf("appendError(nil, nil) = %v", got)
	}
	if got := appendError(nil, errors.New("one")); len(got) != 1 {
		t.Fatalf("appendError() length = %d", len(got))
	}

	transport = newMockTransport()
	transport.register("POST", "/v1.41/networks/net/connect", noContent)
	client = testDockerClient(t, transport)
	current.Networks = map[string]*network.EndpointSettings{"net": {}}
	if errs := client.reconnectNetworks(context.Background(), current, []string{"net"}); len(errs) != 0 {
		t.Fatalf("successful reconnect errors = %v", errs)
	}
	transport.register("POST", "/v1.41/networks/bad/connect", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "connect"})
	})
	if errs := client.reconnectNetworks(context.Background(), current, []string{"bad"}); len(errs) != 1 || !strings.Contains(errs[0].Error(), "reconnect original container") {
		t.Fatalf("failed reconnect errors = %v", errs)
	}
}

func TestRestartPolicyUpdateNotFoundIsACompatibleNoOp(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/old/update", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("Not Found")), Header: make(http.Header)}, nil
	})
	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Host.RestartPolicy.Name = "unless-stopped"
	if changed, err := client.suppressRestartPolicy(context.Background(), current); err != nil || changed {
		t.Fatalf("suppressRestartPolicy() = %v, %v; want compatible no-op", changed, err)
	}
	if err := client.restoreRestartPolicy(context.Background(), current); err != nil {
		t.Fatalf("restoreRestartPolicy() = %v; want compatible no-op", err)
	}
}

func cloneDetailsForTest(source ContainerDetails) ContainerDetails {
	copy := source
	if source.Config != nil {
		config := *source.Config
		copy.Config = &config
	}
	if source.Host != nil {
		host := *source.Host
		copy.Host = &host
	}
	if source.State != nil {
		state := *source.State
		copy.State = &state
	}
	return copy
}
