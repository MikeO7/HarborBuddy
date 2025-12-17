package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func TestDockerClient_ReplaceContainer_Rollback(t *testing.T) {
	transport := newMockTransport()

	// 1. Stop old container
	transport.register("POST", "/v1.41/containers/old123/stop", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(204, nil)
	})

	// 2. Rename old container to backup
	transport.register("POST", "/v1.41/containers/old123/rename", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(204, nil)
	})

	// 3. Rename new container to original name
	transport.register("POST", "/v1.41/containers/new456/rename", func(req *http.Request) (*http.Response, error) {
		name := req.URL.Query().Get("name")
		if name != "my-app" {
			return jsonResponse(400, map[string]string{"message": "wrong name"})
		}
		return jsonResponse(204, nil)
	})

	// 4. Start new container -> FAIL to trigger rollback
	transport.register("POST", "/v1.41/containers/new456/start", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(500, map[string]string{"message": "start failed"})
	})

	// Rollback: 5. Stop new container
	transport.register("POST", "/v1.41/containers/new456/stop", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(204, nil)
	})

	// Rollback: 6. Remove new container
	transport.register("DELETE", "/v1.41/containers/new456", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(204, nil)
	})

	// Rollback: 7. Rename old container back
	// Matches same handler as step 2 but we check calls later

	// Rollback: 8. Start old container
	transport.register("POST", "/v1.41/containers/old123/start", func(req *http.Request) (*http.Response, error) {
		return jsonResponse(204, nil)
	})

	// Create client
	cli, err := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithVersion("1.41"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	d := &DockerClient{cli: cli}

	// Act
	err = d.ReplaceContainer(context.Background(), "old123", "new456", "my-app", 1*time.Second)

	// Assert
	if err == nil {
		t.Error("expected error due to start failure")
	} else {
		if !strings.Contains(err.Error(), "failed to start new container") {
			t.Errorf("unexpected error message: %v", err)
		}
	}

	calls := transport.getCalls()

	// Verify critical calls were made
	expectedCalls := []string{
		"POST /v1.41/containers/old123/stop",
		"POST /v1.41/containers/old123/rename", // Backup
		"POST /v1.41/containers/new456/rename",
		"POST /v1.41/containers/new456/start",
		// Rollback starts here
		"POST /v1.41/containers/new456/stop",
		"DELETE /v1.41/containers/new456",
		"POST /v1.41/containers/old123/rename", // Restore
		"POST /v1.41/containers/old123/start",
	}

	for _, expected := range expectedCalls {
		found := false
		for _, call := range calls {
			if call == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected call %s was not made. Calls: %v", expected, calls)
		}
	}
}

func TestDockerClient_ReplaceContainer_Success(t *testing.T) {
	transport := newMockTransport()

	transport.register("POST", "/v1.41/containers/old123/stop", func(req *http.Request) (*http.Response, error) { return jsonResponse(204, nil) })
	transport.register("POST", "/v1.41/containers/old123/rename", func(req *http.Request) (*http.Response, error) { return jsonResponse(204, nil) })
	transport.register("POST", "/v1.41/containers/new456/rename", func(req *http.Request) (*http.Response, error) { return jsonResponse(204, nil) })
	transport.register("POST", "/v1.41/containers/new456/start", func(req *http.Request) (*http.Response, error) { return jsonResponse(204, nil) })
	transport.register("DELETE", "/v1.41/containers/old123", func(req *http.Request) (*http.Response, error) { return jsonResponse(204, nil) })

	cli, _ := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithVersion("1.41"),
	)
	d := &DockerClient{cli: cli}

	err := d.ReplaceContainer(context.Background(), "old123", "new456", "my-app", 1*time.Second)

	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}

	calls := transport.getCalls()
	expected := "DELETE /v1.41/containers/old123"
	found := false
	for _, call := range calls {
		if call == expected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Old container was not removed. Calls: %v", calls)
	}
}

func TestDockerClient_ListContainers_Parsing(t *testing.T) {
	transport := newMockTransport()

	transport.register("GET", "/v1.41/containers/json", func(req *http.Request) (*http.Response, error) {
		containers := []types.Container{
			{
				ID:      "c1",
				Names:   []string{"/my-container"}, // Note the slash
				Image:   "nginx:latest",
				ImageID: "sha256:123",
				Created: time.Now().Unix(),
				Labels:  map[string]string{"foo": "bar"},
			},
		}
		return jsonResponse(200, containers)
	})

	cli, _ := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithVersion("1.41"),
	)
	d := &DockerClient{cli: cli}

	containers, err := d.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}

	c := containers[0]
	if c.Name != "my-container" {
		t.Errorf("Expected name 'my-container', got '%s' (slash stripping failed?)", c.Name)
	}
	if c.ID != "c1" {
		t.Errorf("Expected ID c1, got %s", c.ID)
	}
}

func TestDockerClient_InspectContainer_Parsing(t *testing.T) {
	transport := newMockTransport()

	transport.register("GET", "/v1.41/containers/c1/json", func(req *http.Request) (*http.Response, error) {
		// Only populate fields we care about parsing
		c := types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:      "c1",
				Name:    "/my-container",
				Created: "2023-01-01T12:00:00.123456789Z",
				State:   &types.ContainerState{Running: true},
				Image:   "sha256:imgid",
			},
			Config: &container.Config{
				Image:  "nginx:latest",
				Labels: map[string]string{"env": "prod"},
			},
			NetworkSettings: &types.NetworkSettings{
				Networks: make(map[string]*network.EndpointSettings),
			},
		}
		return jsonResponse(200, c)
	})

	cli, _ := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithVersion("1.41"),
	)
	d := &DockerClient{cli: cli}

	info, err := d.InspectContainer(context.Background(), "c1")
	if err != nil {
		t.Fatalf("InspectContainer failed: %v", err)
	}

	if info.Name != "my-container" {
		t.Errorf("Expected name 'my-container', got '%s'", info.Name)
	}
	if info.CreatedAt.Year() != 2023 {
		t.Errorf("Expected year 2023, got %d", info.CreatedAt.Year())
	}
	if info.CreatedAt.Nanosecond() != 123456789 {
		t.Errorf("Expected 123456789 nanoseconds, got %d", info.CreatedAt.Nanosecond())
	}
	if info.Config == nil {
		t.Error("Expected Config to be populated")
	}
}
