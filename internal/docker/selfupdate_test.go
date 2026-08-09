package docker

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestSelfUpdateHelperConfigIsRestricted(t *testing.T) {
	current := ContainerDetails{
		Config: &containertypes.Config{
			User: "1000:1000",
			Env: []string{
				"SECRET=do-not-copy",
				"DOCKER_TLS_VERIFY=1",
				"DOCKER_CERT_PATH=/run/docker-certs/client",
			},
		},
		Host: &containertypes.HostConfig{
			Binds: []string{
				"/run/podman/podman.sock:/var/run/docker.sock:rprivate,nosuid,nodev,rbind",
				"/host/certs:/run/docker-certs:ro",
				"/host/config:/config:ro",
			},
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: "/another/socket", Target: "/run/alternate/docker.sock"},
				{Type: mount.TypeBind, Source: "/data", Target: "/data"},
			},
			NetworkMode:     "bridge",
			PortBindings:    network.PortMap{network.MustParsePort("8080/tcp"): {{HostPort: "8080"}}},
			PublishAllPorts: true,
			RestartPolicy:   containertypes.RestartPolicy{Name: "always"},
			AutoRemove:      false,
			GroupAdd:        []string{"docker"},
			SecurityOpt:     []string{"label=disable"},
		},
		Networks: map[string]*network.EndpointSettings{
			"app": {NetworkID: "network-id", EndpointID: "endpoint-id", IPAddress: netip.MustParseAddr("172.20.0.5"), Aliases: []string{"harborbuddy"}},
		},
	}
	request := SelfUpdateHelperRequest{
		Name:              "harborbuddy-updater",
		TargetContainerID: "self-123",
		TargetImageID:     "sha256:new",
		DockerHost:        "unix:///var/run/docker.sock",
		StopTimeout:       7 * time.Second,
		StartupTimeout:    19 * time.Second,
	}

	config, host, networking, err := selfUpdateHelperConfig(current, request)
	if err != nil {
		t.Fatalf("selfUpdateHelperConfig() error = %v", err)
	}
	if config.Image != request.TargetImageID || !slices.Equal(config.Entrypoint, []string{"/harborbuddy"}) {
		t.Fatalf("unexpected helper image/entrypoint: image=%q entrypoint=%v", config.Image, config.Entrypoint)
	}
	command := strings.Join(config.Cmd, " ")
	if strings.Contains(command, "/app/harborbuddy") || !strings.Contains(command, "--updater-mode --target-container-id self-123 --new-image-id sha256:new") {
		t.Fatalf("unexpected helper command: %q", command)
	}
	if !strings.Contains(command, "--helper-stop-timeout 7s --helper-startup-timeout 19s") {
		t.Fatalf("helper replacement policy missing from command: %q", command)
	}
	if !strings.Contains(command, "--helper-restart-policy always --helper-restart-max-retries 0") {
		t.Fatalf("original restart policy missing from helper command: %q", command)
	}
	if config.Labels[SelfUpdateHelperLabel] != "true" || config.Labels[SelfUpdateTargetLabel] != request.TargetContainerID {
		t.Fatalf("helper ownership labels = %v", config.Labels)
	}
	for _, entry := range config.Env {
		if strings.HasPrefix(entry, "SECRET=") {
			t.Fatalf("unrelated environment variable copied: %q", entry)
		}
	}
	if !slices.Contains(config.Env, "HARBORBUDDY_DOCKER_HOST=unix:///var/run/docker.sock") {
		t.Fatalf("Docker host not propagated: %v", config.Env)
	}
	if !host.AutoRemove || host.RestartPolicy.Name != "no" {
		t.Fatalf("helper cleanup policy is unsafe: auto_remove=%v restart=%q", host.AutoRemove, host.RestartPolicy.Name)
	}
	if host.PortBindings != nil || host.PublishAllPorts || config.ExposedPorts != nil {
		t.Fatalf("helper must not publish ports: %+v", host.PortBindings)
	}
	if !slices.Equal(host.SecurityOpt, current.Host.SecurityOpt) {
		t.Fatalf("security options were not preserved: got=%v want=%v", host.SecurityOpt, current.Host.SecurityOpt)
	}
	if len(host.Binds) != 2 || slices.Contains(host.Binds, "/host/config:/config:ro") {
		t.Fatalf("unexpected helper binds: %v", host.Binds)
	}
	if len(host.Mounts) != 0 {
		t.Fatalf("unrelated structured mounts copied: %+v", host.Mounts)
	}
	endpoint := networking.EndpointsConfig["app"]
	if endpoint.NetworkID != "" || endpoint.EndpointID != "" || endpoint.IPAddress.IsValid() || endpoint.Aliases != nil {
		t.Fatalf("network endpoint was not sanitized: %+v", endpoint)
	}
}

func TestActiveSelfUpdateHelperMatchesBothOwnershipLabels(t *testing.T) {
	containers := []ContainerSummary{
		{ID: "wrong-kind", Labels: map[string]string{SelfUpdateTargetLabel: "target"}},
		{ID: "wrong-target", Labels: map[string]string{SelfUpdateHelperLabel: "true", SelfUpdateTargetLabel: "other"}},
		{ID: "helper", Labels: map[string]string{SelfUpdateHelperLabel: "true", SelfUpdateTargetLabel: "target"}},
	}
	if got := ActiveSelfUpdateHelper(containers, "target"); got != "helper" {
		t.Fatalf("ActiveSelfUpdateHelper() = %q, want helper", got)
	}
	if got := ActiveSelfUpdateHelper(containers, "missing"); got != "" {
		t.Fatalf("ActiveSelfUpdateHelper(missing) = %q", got)
	}
}

func TestSelfUpdateHelperConfigCopiesConfiguredSocketMount(t *testing.T) {
	current := ContainerDetails{
		Config: &containertypes.Config{},
		Host: &containertypes.HostConfig{Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: "/host/custom.sock", Target: "/run/docker/custom.sock"},
			{Type: mount.TypeBind, Source: "/host/other", Target: "/other"},
		}},
	}
	config, host, _, err := selfUpdateHelperConfig(current, SelfUpdateHelperRequest{
		Name:              "helper",
		TargetContainerID: "self",
		TargetImageID:     "sha256:new",
		DockerHost:        "unix:///run/docker/custom.sock",
	})
	if err != nil {
		t.Fatalf("selfUpdateHelperConfig() error = %v", err)
	}
	if len(host.Mounts) != 1 || host.Mounts[0].Target != "/run/docker/custom.sock" {
		t.Fatalf("custom Docker socket mount not isolated: %+v", host.Mounts)
	}
	if !slices.Contains(config.Env, "HARBORBUDDY_DOCKER_HOST=unix:///run/docker/custom.sock") {
		t.Fatalf("custom Docker host not propagated: %v", config.Env)
	}
}

func TestStartSelfUpdateHelperWaitsForStableHelper(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "helper-id"})
	})
	transport.register("POST", "/v1.41/containers/helper-id/start", noContent)
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(SelfUpdateHelperReadyMarker + "\n")), Header: make(http.Header)}, nil
	})
	transport.register("GET", "/v1.41/containers/helper-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("helper-id", "helper", "sha256:new", true))
	})
	transport.register("POST", "/v1.41/containers/self/update", func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatalf("read restart-policy update: %v", readErr)
		}
		if strings.Contains(string(body), `"MemorySwappiness":0`) {
			t.Fatalf("restart-policy update resent inspected resource defaults: %s", body)
		}
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	client := testDockerClient(t, transport)
	zeroSwappiness := int64(0)

	id, err := client.StartSelfUpdateHelper(context.Background(), ContainerDetails{
		Summary: ContainerSummary{ID: "self"},
		Config:  &containertypes.Config{},
		Host: &containertypes.HostConfig{
			RestartPolicy: containertypes.RestartPolicy{Name: "unless-stopped"},
			Resources: containertypes.Resources{
				MemorySwappiness: &zeroSwappiness,
			},
		},
	}, SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "self", TargetImageID: "sha256:new"})
	if err != nil || id != "helper-id" {
		t.Fatalf("StartSelfUpdateHelper() = %q, %v", id, err)
	}
	if !slices.Contains(transport.getCalls(), "POST /v1.41/containers/self/update") {
		t.Fatalf("target restart policy was not suppressed before handoff: %v", transport.getCalls())
	}
}

func TestStartSelfUpdateHelperRejectsImmediateCrash(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "helper-id"})
	})
	transport.register("POST", "/v1.41/containers/helper-id/start", noContent)
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	transport.register("DELETE", "/v1.41/containers/helper-id", noContent)
	client := testDockerClient(t, transport)

	_, err := client.StartSelfUpdateHelper(context.Background(), ContainerDetails{
		Config: &containertypes.Config{},
		Host:   &containertypes.HostConfig{},
	}, SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "self", TargetImageID: "sha256:new"})
	if err == nil || !strings.Contains(err.Error(), "failed readiness") {
		t.Fatalf("StartSelfUpdateHelper() error = %v", err)
	}
	if !slices.Contains(transport.getCalls(), "DELETE /v1.41/containers/helper-id") {
		t.Fatalf("crashed helper was not removed; calls = %v", transport.getCalls())
	}
}

func TestStartSelfUpdateHelperRemovesContainerWhenStartFails(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "helper-id"})
	})
	transport.register("POST", "/v1.41/containers/helper-id/start", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "permission denied"})
	})
	transport.register("DELETE", "/v1.41/containers/helper-id", noContent)
	client := testDockerClient(t, transport)

	_, err := client.StartSelfUpdateHelper(context.Background(), ContainerDetails{
		Config: &containertypes.Config{},
		Host:   &containertypes.HostConfig{},
	}, SelfUpdateHelperRequest{
		Name:              "helper",
		TargetContainerID: "self",
		TargetImageID:     "sha256:new",
	})
	if err == nil || !strings.Contains(err.Error(), "start self-update helper helper-id") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("StartSelfUpdateHelper() error = %v", err)
	}
	if !slices.Contains(transport.getCalls(), "DELETE /v1.41/containers/helper-id") {
		t.Fatalf("unstarted helper was not removed; calls = %v", transport.getCalls())
	}
}
