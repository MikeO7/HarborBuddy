package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func testDockerClient(t *testing.T, transport http.RoundTripper) *DockerClient {
	t.Helper()
	cli, err := client.New(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithAPIVersion("1.41"),
	)
	if err != nil {
		t.Fatalf("NewClientWithOpts() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return &DockerClient{cli: cli}
}

func TestListAndInspectKeepSummaryAndDetailsDistinct(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{{
			ID: "container-id", Names: []string{"/web"}, Image: "app:latest", ImageID: "sha256:old",
		}})
	})
	transport.register("GET", "/v1.41/containers/container-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("container-id", "web", "sha256:old", true))
	})
	client := testDockerClient(t, transport)

	summaries, err := client.ListContainers(context.Background())
	if err != nil || len(summaries) != 1 {
		t.Fatalf("ListContainers() = %+v, %v", summaries, err)
	}
	if summaries[0].Name != "web" || summaries[0].ImageRef != "app:latest" {
		t.Fatalf("summary = %+v", summaries[0])
	}
	details, err := client.InspectContainer(context.Background(), "container-id")
	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}
	if details.Config == nil || details.Host == nil || details.State == nil || !details.State.Running {
		t.Fatalf("details incomplete: %+v", details)
	}
}

func TestCheckReplacementRejectsUnsafeContainers(t *testing.T) {
	base := ContainerDetails{
		Summary: ContainerSummary{ID: "id", Name: "app", ImageRef: "app:latest", ImageID: "old"},
		Config:  &containertypes.Config{},
		Host:    &containertypes.HostConfig{},
		State:   &containertypes.State{Running: true},
	}
	client := &DockerClient{}

	tests := []struct {
		name   string
		mutate func(*ContainerDetails)
	}{
		{"auto-remove", func(details *ContainerDetails) { details.Host.AutoRemove = true }},
		{"stopped", func(details *ContainerDetails) { details.State.Running = false }},
		{"swarm task", func(details *ContainerDetails) {
			details.Config.Labels = map[string]string{"com.docker.swarm.task.id": "task"}
		}},
		{"container network", func(details *ContainerDetails) { details.Host.NetworkMode = "container:other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			details := base
			host := *base.Host
			config := *base.Config
			state := *base.State
			details.Host, details.Config, details.State = &host, &config, &state
			test.mutate(&details)
			err := client.CheckReplacement(details, ImageInfo{ID: "new"})
			if !IsUnsupported(err) {
				t.Fatalf("CheckReplacement() error = %v, want UnsupportedError", err)
			}
		})
	}
}

func TestReplaceContainerSuccessPreservesConfig(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "not found"})
	})
	transport.register("POST", "/v1.41/containers/old/stop", noContent)
	transport.register("POST", "/v1.41/containers/old/rename", noContent)
	transport.register("POST", "/v1.41/containers/create", func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("name") != "app" {
			t.Errorf("create name = %q, want app", request.URL.Query().Get("name"))
		}
		var config containertypes.Config
		if err := json.NewDecoder(request.Body).Decode(&config); err != nil {
			t.Errorf("decode create request: %v", err)
		}
		if config.Image != "app:latest" || config.User != "1000:1000" || config.Labels["custom"] != "value" {
			t.Errorf("create config not preserved: %+v", config)
		}
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "new"})
	})
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", true))
	})
	transport.register("POST", "/v1.41/containers/new/start", noContent)
	transport.register("DELETE", "/v1.41/containers/old", noContent)

	client := testDockerClient(t, transport)
	current := replacementFixture()
	result, err := client.ReplaceContainer(context.Background(), current, ImageInfo{ID: "sha256:new"}, ReplaceOptions{
		StopTimeout:       time.Second,
		StartupTimeout:    time.Second,
		StabilizationTime: time.Millisecond,
		PollInterval:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ReplaceContainer() error = %v", err)
	}
	if result.NewContainerID != "new" || result.BackupCleanupErr != nil {
		t.Fatalf("result = %+v", result)
	}
	calls := transport.getCalls()
	if !slices.Contains(calls, "DELETE /v1.41/containers/old") {
		t.Fatalf("backup was not removed; calls = %v", calls)
	}
}

func TestReplaceContainerRollsBackAfterStartFailure(t *testing.T) {
	transport := newMockTransport()
	updateCalls := 0
	transport.register("POST", "/v1.41/containers/old/update", func(*http.Request) (*http.Response, error) {
		updateCalls++
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "not found"})
	})
	transport.register("POST", "/v1.41/containers/old/stop", noContent)
	transport.register("POST", "/v1.41/containers/old/rename", noContent)
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "new"})
	})
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", false))
	})
	transport.register("POST", "/v1.41/containers/new/start", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "start failed"})
	})
	transport.register("POST", "/v1.41/containers/new/stop", noContent)
	transport.register("DELETE", "/v1.41/containers/new", noContent)
	transport.register("POST", "/v1.41/containers/old/start", noContent)

	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Host.RestartPolicy = containertypes.RestartPolicy{Name: "always"}
	_, err := client.ReplaceContainer(context.Background(), current, ImageInfo{ID: "sha256:new"}, ReplaceOptions{StopTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "start replacement") {
		t.Fatalf("ReplaceContainer() error = %v", err)
	}
	calls := transport.getCalls()
	for _, expected := range []string{
		"DELETE /v1.41/containers/new",
		"POST /v1.41/containers/old/rename",
		"POST /v1.41/containers/old/start",
	} {
		if !slices.Contains(calls, expected) {
			t.Errorf("missing rollback call %q in %v", expected, calls)
		}
	}
	if updateCalls != 2 {
		t.Fatalf("restart policy updates = %d, want suppress and restore", updateCalls)
	}
}

func replacementFixture() ContainerDetails {
	return ContainerDetails{
		Summary: ContainerSummary{ID: "old", Name: "app", ImageRef: "app:latest", ImageID: "sha256:old"},
		Config: &containertypes.Config{
			Image: "app:latest", User: "1000:1000", Labels: map[string]string{"custom": "value"},
			Cmd: []string{"serve"},
		},
		Host:     &containertypes.HostConfig{},
		Networks: map[string]*network.EndpointSettings{},
		State:    &containertypes.State{Running: true},
	}
}

func inspectedContainer(id, name, imageID string, running bool) containertypes.InspectResponse {
	return containertypes.InspectResponse{
		ID: id, Name: "/" + name, Image: imageID, Created: "2026-07-30T12:00:00Z",
		State: &containertypes.State{Running: running}, HostConfig: &containertypes.HostConfig{},
		Config:          &containertypes.Config{Image: "app:latest", Labels: map[string]string{}},
		NetworkSettings: &containertypes.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
	}
}

func noContent(*http.Request) (*http.Response, error) {
	return jsonResponse(http.StatusNoContent, nil)
}

func TestReplacementConfigPreservesAnonymousVolume(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "not found"})
	})
	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Summary.ID = strings.Repeat("a", 64)
	current.Config.Hostname = current.Summary.ID[:12]
	current.Mounts = []containertypes.MountPoint{{
		Type: mount.TypeVolume, Name: "anonymous-volume-id", Destination: "/data", RW: true,
	}}

	containerConfig, host, _ := client.replacementConfig(context.Background(), current)
	if containerConfig.Hostname != "" {
		t.Fatalf("default Docker hostname was preserved across replacement: %q", containerConfig.Hostname)
	}
	if len(host.Mounts) != 1 || host.Mounts[0].Type != mount.TypeVolume || host.Mounts[0].Source != "anonymous-volume-id" || host.Mounts[0].Target != "/data" {
		t.Fatalf("anonymous volume was not preserved: %+v", host.Mounts)
	}
}

func TestWaitUntilReadyRejectsRestartLoop(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		inspected := inspectedContainer("new", "app", "sha256:new", true)
		inspected.State.Restarting = true
		return jsonResponse(http.StatusOK, inspected)
	})
	client := testDockerClient(t, transport)

	err := client.waitUntilReady(context.Background(), "new", nil, normalizeReplaceOptions(ReplaceOptions{
		StartupTimeout:    time.Second,
		StabilizationTime: time.Millisecond,
		PollInterval:      time.Millisecond,
	}))
	if err == nil || !strings.Contains(err.Error(), "unstable state") {
		t.Fatalf("waitUntilReady() error = %v, want unstable-state error", err)
	}
}
