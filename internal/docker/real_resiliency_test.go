package docker

import (
	"context"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// TestRealClientResiliency tests the internal/docker/containers.go logic
// using a mocked HTTP transport to simulate the Docker Daemon responses.
func TestRealClientResiliency(t *testing.T) {
	t.Log("Testing Docker Client Resiliency (Real Wrapper Logic)")

	t.Run("ListContainers handles empty JSON response gracefully", func(t *testing.T) {
		transport := newMockTransport()

		// Simulate Docker API returning empty list.
		// Note: We use a wildcard/prefix because SDK prepends version (e.g. /v1.41/...)
		// The mockTransport uses simple exact match or prefix if key ends in *
		// Since we don't know the exact version, we can't easily register a prefix match for just containers/json
		// unless we register "GET *" and route inside.
		// However, looking at mock_transport_test.go, it supports prefix if key ends in *.
		// But "/containers/json" is at the END of the URL.
		// So we register "GET *" and check suffix inside.
		transport.register("GET", "*", func(req *http.Request) (*http.Response, error) {
			if contextPath(req.URL.Path) == "/containers/json" {
				return jsonResponse(200, []types.Container{})
			}
			return jsonResponse(404, nil)
		})

		// Create the REAL DockerClient with mocked transport
		cli, err := client.NewClientWithOpts(
			client.WithHTTPClient(&http.Client{Transport: transport}),
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		dockerClient := &DockerClient{cli: cli}

		// Execute
		containers, err := dockerClient.ListContainers(context.Background())
		if err != nil {
			t.Errorf("ListContainers returned error: %v", err)
		}
		if containers == nil {
			t.Error("ListContainers returned nil slice, expected empty slice")
		}
		if len(containers) != 0 {
			t.Errorf("ListContainers returned %d items, expected 0", len(containers))
		}
	})

	t.Run("ListContainers handles null JSON response gracefully", func(t *testing.T) {
		transport := newMockTransport()

		// Simulate Docker API returning "null" or nil body (unlikely but possible edge case)
		transport.register("GET", "*", func(req *http.Request) (*http.Response, error) {
			if contextPath(req.URL.Path) == "/containers/json" {
				// Sending "null" as body, which unmarshals to nil slice
				// We must pass a typed nil so jsonResponse marshals it to "null"
				var nilSlice []types.Container = nil
				return jsonResponse(200, nilSlice)
			}
			return jsonResponse(404, nil)
		})

		cli, _ := client.NewClientWithOpts(
			client.WithHTTPClient(&http.Client{Transport: transport}),
			client.WithAPIVersionNegotiation(),
		)
		dockerClient := &DockerClient{cli: cli}

		containers, err := dockerClient.ListContainers(context.Background())
		if err != nil {
			t.Errorf("ListContainers returned error: %v", err)
		}
		if containers == nil {
			t.Error("ListContainers returned nil slice, expected empty slice")
		}
	})
}
