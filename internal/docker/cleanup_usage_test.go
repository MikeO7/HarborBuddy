package docker

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	networktypes "github.com/moby/moby/api/types/network"
)

// usageContainers models a host where a running container uses an image whose
// tag has moved (now dangling) and a stopped container keeps another image and
// a custom network.
func usageContainers() []containertypes.Summary {
	stopped := containertypes.Summary{ID: "stopped", ImageID: "sha256:stopped-image", State: containertypes.StateExited}
	stopped.HostConfig.NetworkMode = "mode-net"
	stopped.NetworkSettings = &containertypes.NetworkSettingsSummary{Networks: map[string]*networktypes.EndpointSettings{
		"named-net": nil,
		"other":     {NetworkID: "id-net"},
	}}
	return []containertypes.Summary{
		{ID: "running", ImageID: "sha256:running-dangling", State: containertypes.StateRunning},
		stopped,
		{ID: "no-network", State: containertypes.StateCreated},
	}
}

func usageImages() []imagetypes.Summary {
	return []imagetypes.Summary{
		{ID: "sha256:running-dangling", Containers: -1},
		{ID: "sha256:stopped-image", RepoTags: []string{"app:old"}, Containers: -1},
		{ID: "sha256:parent", Containers: -1},
		{ID: "sha256:child", ParentID: "sha256:parent", RepoTags: []string{"child:1"}, Containers: -1},
		{ID: "sha256:counted", Containers: 1},
		{ID: "sha256:digest-only", RepoDigests: []string{"ghcr.io/org/tool@sha256:abc", "<none>@<none>"}, Containers: -1},
		{ID: "sha256:unused", RepoDigests: []string{"<none>@<none>"}, Containers: -1},
	}
}

func usageTransport(t *testing.T) *mockTransport {
	t.Helper()
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" {
			t.Fatalf("container usage must include stopped containers: %q", r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, usageContainers())
	})
	transport.register("GET", "/v1.41/images/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, usageImages())
	})
	transport.register("GET", "/v1.41/system/df", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"Images": usageImages()})
	})
	return transport
}

func inUseByID(images []ImageInfo) map[string]bool {
	result := make(map[string]bool, len(images))
	for _, image := range images {
		result[image.ID] = image.InUse
	}
	return result
}

func assertImageUsage(t *testing.T, name string, inUse map[string]bool) {
	t.Helper()
	want := map[string]bool{
		"sha256:running-dangling": true, "sha256:stopped-image": true, "sha256:parent": true,
		"sha256:child": false, "sha256:counted": true, "sha256:digest-only": false, "sha256:unused": false,
	}
	for id, wantInUse := range want {
		if inUse[id] != wantInUse {
			t.Fatalf("%s: image %s in use = %v, want %v (all: %v)", name, id, inUse[id], wantInUse, inUse)
		}
	}
}

func TestImageListingsMarkImagesThatContainersOrChildrenUse(t *testing.T) {
	client := testDockerClient(t, usageTransport(t))
	images, err := client.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	assertImageUsage(t, "ListImages", inUseByID(images))
	dangling, err := client.ListDanglingImages(context.Background())
	if err != nil {
		t.Fatalf("ListDanglingImages() error = %v", err)
	}
	assertImageUsage(t, "ListDanglingImages", inUseByID(dangling))
	for _, image := range images {
		if image.ID == "sha256:digest-only" && !slices.Equal(image.RepoDigests, []string{"ghcr.io/org/tool@sha256:abc"}) {
			t.Fatalf("digest-only image digests = %v", image.RepoDigests)
		}
		if image.ID == "sha256:unused" && len(image.RepoDigests) != 0 {
			t.Fatalf("placeholder digest was kept: %v", image.RepoDigests)
		}
	}

	resources, err := client.ListCleanupResources(context.Background(), CleanupImage)
	if err != nil {
		t.Fatalf("ListCleanupResources(image) error = %v", err)
	}
	inUse := make(map[string]bool, len(resources))
	for _, resource := range resources {
		inUse[resource.ID] = resource.InUse
		if resource.ID == "sha256:digest-only" && len(resource.RepoDigests) != 1 {
			t.Fatalf("digest-only cleanup resource = %+v", resource)
		}
	}
	assertImageUsage(t, "ListCleanupResources", inUse)
}

func TestImageListingsFailWhenUsageIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		list func(*DockerClient) error
		want string
	}{
		{name: "images without containers", path: "/v1.41/containers/json", want: "list all containers for cleanup usage", list: func(client *DockerClient) error {
			_, err := client.ListImages(context.Background())
			return err
		}},
		{name: "dangling without containers", path: "/v1.41/containers/json", want: "list all containers for cleanup usage", list: func(client *DockerClient) error {
			_, err := client.ListDanglingImages(context.Background())
			return err
		}},
		{name: "cleanup images without containers", path: "/v1.41/containers/json", want: "list images for cleanup", list: func(client *DockerClient) error {
			_, err := client.ListCleanupResources(context.Background(), CleanupImage)
			return err
		}},
		{name: "networks without containers", path: "/v1.41/containers/json", want: "list networks for cleanup", list: func(client *DockerClient) error {
			_, err := client.ListCleanupResources(context.Background(), CleanupNetwork)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := usageTransport(t)
			transport.register("GET", "/v1.41/networks", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, []networktypes.Summary{})
			})
			registerServerError(transport, "GET", test.path, "usage failed")
			if err := test.list(testDockerClient(t, transport)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	t.Run("dangling without the full image list", func(t *testing.T) {
		transport := usageTransport(t)
		transport.register("GET", "/v1.41/images/json", func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Query().Get("filters"), "dangling") {
				return jsonResponse(http.StatusOK, usageImages())
			}
			return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "list failed"})
		})
		if _, err := testDockerClient(t, transport).ListDanglingImages(context.Background()); err == nil || !strings.Contains(err.Error(), "list images") {
			t.Fatalf("ListDanglingImages() error = %v", err)
		}
	})
}

func TestCleanupContainersProtectUpdateBackups(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: "backup", Names: []string{"/web-harborbuddy-backup-1700000000000000000"}, State: containertypes.StateExited},
			{ID: "plain", Names: []string{"/web"}, State: containertypes.StateExited},
		})
	})
	registerInspectWithRestart(transport, "plain", "no")
	containers, err := testDockerClient(t, transport).ListCleanupResources(context.Background(), CleanupContainer)
	if err != nil || len(containers) != 2 {
		t.Fatalf("cleanup containers = %+v, %v", containers, err)
	}
	if !containers[0].Protected || containers[0].ProtectedReason == "" || containers[1].Protected {
		t.Fatalf("backup protection = %+v", containers)
	}
}

func TestCleanupNetworksCountStoppedContainers(t *testing.T) {
	created := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	transport := usageTransport(t)
	transport.register("GET", "/v1.41/networks", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []networktypes.Summary{
			{Network: networktypes.Network{ID: "id-net", Name: "by-id", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "x1", Name: "named-net", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "x2", Name: "mode-net", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "free", Name: "free", Scope: "local", Created: created}},
		})
	})
	transport.register("GET", "/v1.41/networks/free", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, networktypes.Inspect{Network: networktypes.Network{ID: "free"}})
	})
	networks, err := testDockerClient(t, transport).ListCleanupResources(context.Background(), CleanupNetwork)
	if err != nil || len(networks) != 4 {
		t.Fatalf("cleanup networks = %+v, %v", networks, err)
	}
	for _, network := range networks[:3] {
		if !network.InUse {
			t.Fatalf("network %s used by a stopped container looks unused", network.Name)
		}
	}
	if networks[3].InUse {
		t.Fatalf("unused network looks used: %+v", networks[3])
	}
	for _, call := range transport.getCalls() {
		if strings.HasPrefix(call, "GET /v1.41/networks/") && call != "GET /v1.41/networks/free" {
			t.Fatalf("network already known to be in use was inspected: %s", call)
		}
	}
}

func TestCleanupUsageHelpers(t *testing.T) {
	if !isBackupContainerName("app-harborbuddy-backup-1") || isBackupContainerName("app") {
		t.Fatal("isBackupContainerName classified a name incorrectly")
	}
	if got := repoDigests([]string{"", "<none>@<none>", "app@sha256:abc"}); !slices.Equal(got, []string{"app@sha256:abc"}) {
		t.Fatalf("repoDigests() = %v", got)
	}
	usage := containerUsage{images: map[string]struct{}{}, networks: map[string]struct{}{"name": {}}}
	if !usage.usesNetwork("id", "name") || usage.usesNetwork("id", "other") || usage.usesImage("image") {
		t.Fatal("containerUsage reported usage incorrectly")
	}
}

func TestRollbackImagesAreReleasedOnlyWhenNoContainerUsesTheirReference(t *testing.T) {
	live := rollbackImageRepository("registry.example/app:stable") + ":1"
	gone := rollbackImageRepository("registry.example/removed:stable") + ":1"
	images := func() []ImageInfo {
		return []ImageInfo{
			{ID: "sha256:live", RepoTags: []string{live}, Protected: true},
			{ID: "sha256:gone", RepoTags: []string{gone, "registry.example/removed:previous"}, Protected: true},
			{ID: "sha256:both", RepoTags: []string{gone, live}, Protected: true},
		}
	}
	protected := func(containers []containertypes.Summary) []bool {
		transport := newMockTransport()
		transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, containers)
		})
		marked, err := testDockerClient(t, transport).markImagesInUse(context.Background(), images(), nil)
		if err != nil {
			t.Fatalf("markImagesInUse() error = %v", err)
		}
		return []bool{marked[0].Protected, marked[1].Protected, marked[2].Protected}
	}

	resolved := []containertypes.Summary{{ID: "app", Image: "registry.example/app:stable", ImageID: "sha256:current"}}
	if got := protected(resolved); !slices.Equal(got, []bool{true, false, true}) {
		t.Fatalf("protected = %v, want only the image of a removed reference released", got)
	}
	// A container listed by image ID hides its reference, so nothing is released.
	unresolved := append(resolved, containertypes.Summary{ID: "moved", Image: "sha256:other", ImageID: "sha256:other"})
	if got := protected(unresolved); !slices.Equal(got, []bool{true, true, true}) {
		t.Fatalf("protected = %v, want every rollback image kept while a reference is unknown", got)
	}
}
