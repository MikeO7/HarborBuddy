package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func TestCreateContainerLike_SmartCopy(t *testing.T) {
	tests := []struct {
		name               string
		oldCmd             []string
		oldEntrypoint      []string
		imageCmd           []string
		imageEntrypoint    []string
		expectedCmd        []string
		expectedEntrypoint []string
	}{
		{
			name:               "match_defaults_should_reset",
			oldCmd:             []string{"default-cmd"},
			oldEntrypoint:      []string{"default-entry"},
			imageCmd:           []string{"default-cmd"},
			imageEntrypoint:    []string{"default-entry"},
			expectedCmd:        nil,
			expectedEntrypoint: nil,
		},
		{
			name:               "mismatch_should_preserve",
			oldCmd:             []string{"custom-cmd"},
			oldEntrypoint:      []string{"default-entry"},
			imageCmd:           []string{"default-cmd"},
			imageEntrypoint:    []string{"default-entry"},
			expectedCmd:        []string{"custom-cmd"},
			expectedEntrypoint: nil,
		},
		{
			name:               "override_entrypoint",
			oldCmd:             []string{"default-cmd"},
			oldEntrypoint:      []string{"custom-entry"},
			imageCmd:           []string{"default-cmd"},
			imageEntrypoint:    []string{"default-entry"},
			expectedCmd:        nil,
			expectedEntrypoint: []string{"custom-entry"},
		},
		{
			name:               "nil_vs_empty_considered_equal",
			oldCmd:             nil,
			oldEntrypoint:      []string{},
			imageCmd:           []string{},
			imageEntrypoint:    nil,
			expectedCmd:        nil,
			expectedEntrypoint: nil,
		},
		{
			name:               "subset_mismatch_should_preserve",
			oldCmd:             []string{"cmd"},
			oldEntrypoint:      nil,
			imageCmd:           []string{"cmd", "arg"},
			imageEntrypoint:    nil,
			expectedCmd:        []string{"cmd"},
			expectedEntrypoint: nil,
		},
		{
			name:               "superset_mismatch_should_preserve",
			oldCmd:             []string{"cmd", "arg", "extra"},
			oldEntrypoint:      nil,
			imageCmd:           []string{"cmd", "arg"},
			imageEntrypoint:    nil,
			expectedCmd:        []string{"cmd", "arg", "extra"},
			expectedEntrypoint: nil,
		},
		{
			// Special case: Test that if inspect fails, we preserve old config (fallback)
			name:               "inspect_error_fallback",
			oldCmd:             []string{"default-cmd"},
			oldEntrypoint:      []string{"default-entry"},
			imageCmd:           nil, // Simulate inspect error by making mock return error
			imageEntrypoint:    nil,
			expectedCmd:        []string{"default-cmd"},
			expectedEntrypoint: []string{"default-entry"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := newMockTransport()

			// Mock Inspect old image (sha256:old-img)
			// Mock Inspect old image (sha256:old-img)
			// Mock Inspect old image (sha256:old-img)
			transport.register("GET", "/v1.41/images/sha256:old-img/json", func(req *http.Request) (*http.Response, error) {
				if tt.name == "inspect_error_fallback" {
					return jsonResponse(404, map[string]string{"message": "image not found"})
				}
				// Use map to avoid type errors with container.Config vs specs.DockerOCIImageConfig
				return jsonResponse(200, map[string]interface{}{
					"Id": "sha256:old-img",
					"Config": map[string]interface{}{
						"Cmd":        tt.imageCmd,
						"Entrypoint": tt.imageEntrypoint,
					},
				})
			})

			// Mock ContainerCreate
			transport.register("POST", "/v1.41/containers/create", func(req *http.Request) (*http.Response, error) {
				// Decode body to verify what we sent found
				var receivedConfig container.Config
				if err := json.NewDecoder(req.Body).Decode(&receivedConfig); err != nil {
					return jsonResponse(400, "bad request")
				}

				// Check correctness
				// Use specific check for Cmd/Entrypoint to handle []string vs strslice.StrSlice and nil/empty
				compareSlices := func(name string, expected, actual []string) {
					if len(expected) != len(actual) {
						t.Errorf("Expected %s %v, got %v (len mismatch)", name, expected, actual)
						return
					}
					for i := range expected {
						if expected[i] != actual[i] {
							t.Errorf("Expected %s %v, got %v (mismatch at %d)", name, expected, actual, i)
							return
						}
					}
				}

				compareSlices("Cmd", tt.expectedCmd, receivedConfig.Cmd)
				compareSlices("Entrypoint", tt.expectedEntrypoint, receivedConfig.Entrypoint)

				return jsonResponse(201, container.CreateResponse{ID: "new-id"})
			})

			cli, _ := client.NewClientWithOpts(
				client.WithHTTPClient(&http.Client{Transport: transport}),
				client.WithVersion("1.41"),
			)
			d := &DockerClient{cli: cli}

			oldContainer := ContainerInfo{
				ID:      "old-id",
				Name:    "my-app",
				ImageID: "sha256:old-img",
				Config: &container.Config{
					Cmd:        tt.oldCmd,
					Entrypoint: tt.oldEntrypoint,
				},
				// Minimal other fields
				HostConfig: &container.HostConfig{},
			}

			_, err := d.CreateContainerLike(context.Background(), oldContainer, "new-image")
			if err != nil {
				t.Errorf("CreateContainerLike failed: %v", err)
			}
		})
	}
}
