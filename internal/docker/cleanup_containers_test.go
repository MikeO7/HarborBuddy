package docker

import (
	"context"
	"net/http"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
)

func registerInspectWithRestart(transport *mockTransport, id, policy string) {
	transport.register("GET", "/v1.41/containers/"+id+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, containertypes.InspectResponse{
			ID: id, Name: "/" + id, Image: "sha256:image",
			HostConfig: &containertypes.HostConfig{RestartPolicy: containertypes.RestartPolicy{Name: containertypes.RestartPolicyMode(policy)}},
		})
	})
}

func TestStoppedContainerProtection(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: "helper", Names: []string{"/hb-harborbuddy-updater-1"}, State: containertypes.StateExited, Labels: map[string]string{SelfUpdateHelperLabel: "true"}},
			{ID: "always", Names: []string{"/always"}, State: containertypes.StateExited},
			{ID: "none", Names: []string{"/none"}, State: containertypes.StateExited},
			{ID: "empty", Names: []string{"/empty"}, State: containertypes.StateCreated},
			{ID: "unknown", Names: []string{"/unknown"}, State: containertypes.StateDead},
			{ID: "running", Names: []string{"/running"}, State: containertypes.StateRunning},
		})
	})
	registerInspectWithRestart(transport, "always", "unless-stopped")
	registerInspectWithRestart(transport, "none", "no")
	registerInspectWithRestart(transport, "empty", "")
	transport.register("GET", "/v1.41/containers/unknown/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "inspect failed"})
	})

	containers, err := testDockerClient(t, transport).ListCleanupResources(context.Background(), CleanupContainer)
	if err != nil {
		t.Fatalf("ListCleanupResources() error = %v", err)
	}
	want := map[string]string{
		"helper":  "container is a HarborBuddy self-update record",
		"always":  "container has a restart policy",
		"none":    "",
		"empty":   "",
		"unknown": "restart policy is unknown",
		"running": "",
	}
	for _, container := range containers {
		reason, ok := want[container.ID]
		if !ok {
			t.Fatalf("unexpected container %q", container.ID)
		}
		if container.ProtectedReason != reason || container.Protected != (reason != "") {
			t.Errorf("%s protection = %v %q, want %q", container.ID, container.Protected, container.ProtectedReason, reason)
		}
	}
}
