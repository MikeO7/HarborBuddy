package docker

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func postgresLikeOldImage(*http.Request) (*http.Response, error) {
	return jsonResponse(http.StatusOK, map[string]any{
		"Id": "sha256:old",
		"Config": map[string]any{
			"Env":          []string{"PATH=/usr/lib/postgresql/17/bin:/usr/bin", "PG_MAJOR=17", "PG_VERSION=17.1", "LANG=en_US.utf8"},
			"Labels":       map[string]string{"org.opencontainers.image.version": "17.1", "maintainer": "upstream"},
			"WorkingDir":   "/var/lib/postgresql",
			"User":         "postgres",
			"StopSignal":   "SIGINT",
			"ExposedPorts": map[string]any{"5432/tcp": map[string]any{}},
			"Volumes":      map[string]any{"/var/lib/postgresql/data": map[string]any{}},
			"Cmd":          []string{"postgres"},
			"Entrypoint":   []string{"docker-entrypoint.sh"},
			"Shell":        []string{"/bin/sh", "-c"},
		},
	})
}

func inheritedPostgresContainer() ContainerDetails {
	current := replacementFixture()
	current.Config = &containertypes.Config{
		Image: "postgres:17",
		Env: []string{
			"APP_MODE=custom", "LANG=C.UTF-8",
			"PATH=/usr/lib/postgresql/17/bin:/usr/bin", "PG_MAJOR=17", "PG_VERSION=17.1",
		},
		Labels: map[string]string{
			"org.opencontainers.image.version": "17.1", "maintainer": "me",
			"com.docker.compose.service": "db",
		},
		WorkingDir: "/var/lib/postgresql",
		User:       "postgres",
		StopSignal: "SIGINT",
		ExposedPorts: network.PortSet{
			network.MustParsePort("5432/tcp"): {},
			network.MustParsePort("9187/tcp"): {},
		},
		Volumes:    map[string]struct{}{"/var/lib/postgresql/data": {}, "/extra": {}},
		Cmd:        []string{"postgres"},
		Entrypoint: []string{"docker-entrypoint.sh"},
		Shell:      []string{"/bin/sh", "-c"},
	}
	return current
}

func TestReplacementConfigDropsOldImageDefaultsButKeepsUserSettings(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", postgresLikeOldImage)
	client := testDockerClient(t, transport)
	current := inheritedPostgresContainer()

	config, _, _, warning := client.replacementConfig(context.Background(), current)
	if warning != nil {
		t.Fatalf("unexpected warning: %v", warning)
	}
	if want := []string{"APP_MODE=custom", "LANG=C.UTF-8"}; !slices.Equal(config.Env, want) {
		t.Fatalf("Env = %q, want %q", config.Env, want)
	}
	if _, ok := config.Labels["org.opencontainers.image.version"]; ok || config.Labels["maintainer"] != "me" || config.Labels["com.docker.compose.service"] != "db" {
		t.Fatalf("Labels = %v", config.Labels)
	}
	if config.WorkingDir != "" || config.User != "" || config.StopSignal != "" {
		t.Fatalf("inherited scalar settings kept: workdir=%q user=%q stop=%q", config.WorkingDir, config.User, config.StopSignal)
	}
	if _, ok := config.ExposedPorts[network.MustParsePort("9187/tcp")]; !ok || len(config.ExposedPorts) != 1 {
		t.Fatalf("ExposedPorts = %v", config.ExposedPorts)
	}
	if _, ok := config.Volumes["/extra"]; !ok || len(config.Volumes) != 1 {
		t.Fatalf("Volumes = %v", config.Volumes)
	}
	if config.Cmd != nil || config.Entrypoint != nil || config.Shell != nil {
		t.Fatalf("inherited command kept: %+v", config)
	}
	if len(current.Config.Env) != 5 || len(current.Config.Labels) != 3 || len(current.Config.ExposedPorts) != 2 || len(current.Config.Volumes) != 2 {
		t.Fatalf("inspected config was mutated: %+v", current.Config)
	}

	current.Config.User = "1000:1000"
	current.Config.WorkingDir = "/srv"
	current.Config.StopSignal = "SIGTERM"
	current.Config.ExposedPorts = nil
	current.Config.Volumes = nil
	config, _, _, _ = client.replacementConfig(context.Background(), current)
	if config.User != "1000:1000" || config.WorkingDir != "/srv" || config.StopSignal != "SIGTERM" || config.ExposedPorts != nil || config.Volumes != nil {
		t.Fatalf("explicit scalar settings were not preserved: %+v", config)
	}
}

func TestReplacementConfigWarnsWhenOldImageHasNoConfig(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"Id": "sha256:old"})
	})
	client := testDockerClient(t, transport)
	current := inheritedPostgresContainer()
	config, _, _, warning := client.replacementConfig(context.Background(), current)
	if warning == nil || !strings.Contains(warning.Error(), "image has no configuration") {
		t.Fatalf("warning = %v", warning)
	}
	if len(config.Env) != 5 || config.User != "postgres" {
		t.Fatalf("config changed without old-image data: %+v", config)
	}
}

func TestReplaceContainerSurfacesOldImageWarningAndCreatesWithNewDefaults(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", postgresLikeOldImage)
	transport.register("POST", "/v1.41/containers/old/stop", noContent)
	transport.register("POST", "/v1.41/containers/old/rename", noContent)
	var created containertypes.CreateRequest
	transport.register("POST", "/v1.41/containers/create", func(request *http.Request) (*http.Response, error) {
		if err := decodeJSON(request, &created); err != nil {
			t.Errorf("decode create: %v", err)
		}
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "new"})
	})
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", true))
	})
	transport.register("POST", "/v1.41/containers/new/start", noContent)
	transport.register("DELETE", "/v1.41/containers/old", noContent)
	client := testDockerClient(t, transport)
	options := ReplaceOptions{StopTimeout: time.Second, StartupTimeout: time.Second, StabilizationTime: time.Millisecond, PollInterval: time.Millisecond}

	result, err := client.ReplaceContainer(context.Background(), inheritedPostgresContainer(), ImageInfo{ID: "sha256:new"}, options)
	if err != nil || result.ConfigWarning != nil {
		t.Fatalf("ReplaceContainer() = %+v, %v", result, err)
	}
	if created.Config == nil || slices.Contains(created.Env, "PG_VERSION=17.1") || created.User != "" {
		t.Fatalf("replacement inherited old image defaults: %+v", created.Config)
	}

	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "no such image"})
	})
	result, err = client.ReplaceContainer(context.Background(), inheritedPostgresContainer(), ImageInfo{ID: "sha256:new"}, options)
	if err != nil || result.ConfigWarning == nil || !strings.Contains(result.ConfigWarning.Error(), "sha256:old") {
		t.Fatalf("ReplaceContainer(uninspectable old image) = %+v, %v", result, err)
	}
}
