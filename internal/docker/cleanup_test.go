package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	buildtypes "github.com/moby/moby/api/types/build"
	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	networktypes "github.com/moby/moby/api/types/network"
	volumetypes "github.com/moby/moby/api/types/volume"
)

func TestCleanupAdapterListsAndRemovesEveryResourceKind(t *testing.T) {
	created := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	lastUsed := created.Add(time.Hour)
	transport := newMockTransport()
	transport.register("GET", "/v1.41/system/df", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("verbose") != "1" || r.URL.Query().Get("type") == "" {
			t.Fatalf("disk usage query = %q", r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"Images": []imagetypes.Summary{
				{ID: "sha256:dangling", Created: created.Unix(), Size: 10, Containers: 0},
				{ID: "sha256:tagged", RepoTags: []string{"app:old"}, Created: created.Unix(), Size: -1, Containers: 2},
				{ID: "sha256:rollback", RepoTags: []string{rollbackImageRepositoryPrefix + "abc:1"}, Created: created.Unix(), Size: 20},
			},
			"Volumes": []volumetypes.Volume{
				{Name: "unused", CreatedAt: created.Format(time.RFC3339Nano), UsageData: &volumetypes.UsageData{RefCount: 0, Size: 20}},
				{Name: "used", CreatedAt: created.Format(time.RFC3339Nano), UsageData: &volumetypes.UsageData{RefCount: 1, Size: -1}},
				{Name: "unknown", CreatedAt: "bad-time"},
				{Name: "unknown-ref", CreatedAt: created.Format(time.RFC3339Nano), UsageData: &volumetypes.UsageData{RefCount: -1, Size: 1}},
			},
			"BuildCache": []buildtypes.CacheRecord{
				{ID: "cache-old", Description: "old", CreatedAt: created, LastUsedAt: &lastUsed, Size: 30},
				{ID: "cache-active", CreatedAt: created, InUse: true, Size: -1},
			},
		})
	})
	transport.register("GET", "/v1.41/containers/json", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" || r.URL.Query().Get("size") != "1" {
			t.Fatalf("container list query = %q", r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: "created", Names: []string{"/created"}, Created: created.Unix(), State: containertypes.StateCreated, SizeRw: 5},
			{ID: "exited", Created: created.Unix(), State: containertypes.StateExited},
			{ID: "dead", Created: created.Unix(), State: containertypes.StateDead},
			{ID: "running", Created: created.Unix(), State: containertypes.StateRunning},
		})
	})
	transport.register("GET", "/v1.41/networks", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []networktypes.Summary{
			{Network: networktypes.Network{ID: "bridge", Name: "bridge", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "host", Name: "host", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "none", Name: "none", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "swarm", Name: "swarm", Scope: "swarm", Created: created, Ingress: true, ConfigOnly: true}},
			{Network: networktypes.Network{ID: "unused-net", Name: "unused-net", Scope: "local", Created: created}},
			{Network: networktypes.Network{ID: "used-net", Name: "used-net", Scope: "local", Created: created}},
		})
	})
	transport.register("GET", "/v1.41/networks/unused-net", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, networktypes.Inspect{Network: networktypes.Network{ID: "unused-net"}})
	})
	transport.register("GET", "/v1.41/networks/used-net", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, networktypes.Inspect{Network: networktypes.Network{ID: "used-net"}, Containers: map[string]networktypes.EndpointResource{"container": {}}})
	})
	transport.register("DELETE", "/v1.41/images/sha256:dangling", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []imagetypes.DeleteResponse{{Deleted: "sha256:dangling"}})
	})
	for _, endpoint := range []string{"/v1.41/containers/created", "/v1.41/networks/unused-net", "/v1.41/volumes/unused"} {
		transport.register("DELETE", endpoint, func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNoContent, nil)
		})
	}
	transport.register("POST", "/v1.41/build/prune", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" || !strings.Contains(r.URL.Query().Get("filters"), "until") {
			t.Fatalf("build prune query = %q", r.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, buildtypes.CachePruneReport{CachesDeleted: []string{"cache-old"}, SpaceReclaimed: 30})
	})
	client := testDockerClient(t, transport)

	images, err := client.ListCleanupResources(context.Background(), CleanupImage)
	if err != nil || len(images) != 3 || !images[0].Dangling || images[1].Size != 0 || !images[1].InUse || !images[2].Protected {
		t.Fatalf("cleanup images = %+v, %v", images, err)
	}
	containers, err := client.ListCleanupResources(context.Background(), CleanupContainer)
	if err != nil || len(containers) != 4 || containers[0].Name != "created" || containers[0].InUse || !containers[3].InUse {
		t.Fatalf("cleanup containers = %+v, %v", containers, err)
	}
	networks, err := client.ListCleanupResources(context.Background(), CleanupNetwork)
	if err != nil || len(networks) != 6 || !networks[0].Protected || networks[4].InUse || !networks[5].InUse {
		t.Fatalf("cleanup networks = %+v, %v", networks, err)
	}
	volumes, err := client.ListCleanupResources(context.Background(), CleanupVolume)
	if err != nil || len(volumes) != 4 || volumes[0].InUse || !volumes[1].InUse || !volumes[2].Protected || !volumes[3].Protected {
		t.Fatalf("cleanup volumes = %+v, %v", volumes, err)
	}
	caches, err := client.ListCleanupResources(context.Background(), CleanupBuildCache)
	if err != nil || len(caches) != 2 || caches[0].LastUsedAt != lastUsed || !caches[1].InUse || caches[1].Size != 0 {
		t.Fatalf("cleanup caches = %+v, %v", caches, err)
	}
	if _, err := client.ListCleanupResources(context.Background(), "mystery"); err == nil {
		t.Fatal("unsupported cleanup kind returned nil error")
	}

	for _, resource := range []CleanupResource{
		{Kind: CleanupImage, ID: "sha256:dangling"},
		{Kind: CleanupContainer, ID: "created", Name: "created"},
		{Kind: CleanupNetwork, ID: "unused-net", Name: "unused-net"},
		{Kind: CleanupVolume, ID: "unused", Name: "unused"},
	} {
		if err := client.RemoveCleanupResource(context.Background(), resource); err != nil {
			t.Fatalf("RemoveCleanupResource(%s) error = %v", resource.Kind, err)
		}
	}
	if err := client.RemoveCleanupResource(context.Background(), CleanupResource{Kind: CleanupBuildCache}); err == nil {
		t.Fatal("individual build-cache removal returned nil error")
	}
	if err := client.RemoveCleanupResource(context.Background(), CleanupResource{Kind: "mystery"}); err == nil {
		t.Fatal("unknown resource removal returned nil error")
	}
	pruned, err := client.PruneBuildCache(context.Background(), created)
	if err != nil || pruned.ReclaimedBytes != 30 || len(pruned.Deleted) != 1 {
		t.Fatalf("PruneBuildCache() = %+v, %v", pruned, err)
	}
}

func TestCleanupAdapterReportsDockerFailures(t *testing.T) {
	listCases := []struct {
		name string
		kind CleanupResourceKind
		path string
	}{
		{name: "images", kind: CleanupImage, path: "/v1.41/system/df"},
		{name: "containers", kind: CleanupContainer, path: "/v1.41/containers/json"},
		{name: "networks", kind: CleanupNetwork, path: "/v1.41/networks"},
		{name: "volumes", kind: CleanupVolume, path: "/v1.41/system/df"},
		{name: "build cache", kind: CleanupBuildCache, path: "/v1.41/system/df"},
	}
	for _, test := range listCases {
		t.Run("list "+test.name, func(t *testing.T) {
			transport := newMockTransport()
			transport.register("GET", test.path, func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "failed"})
			})
			client := testDockerClient(t, transport)
			if _, err := client.ListCleanupResources(context.Background(), test.kind); err == nil || !strings.Contains(err.Error(), "cleanup") {
				t.Fatalf("ListCleanupResources(%s) error = %v", test.kind, err)
			}
		})
	}

	t.Run("network inspect", func(t *testing.T) {
		transport := newMockTransport()
		transport.register("GET", "/v1.41/networks", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, []networktypes.Summary{{Network: networktypes.Network{ID: "custom", Name: "custom", Scope: "local", Created: time.Now()}}})
		})
		transport.register("GET", "/v1.41/networks/custom", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "failed"})
		})
		client := testDockerClient(t, transport)
		if _, err := client.ListCleanupResources(context.Background(), CleanupNetwork); err == nil || !strings.Contains(err.Error(), "inspect network") {
			t.Fatalf("network inspect error = %v", err)
		}
	})

	removeCases := []struct {
		name     string
		resource CleanupResource
		path     string
	}{
		{name: "image", resource: CleanupResource{Kind: CleanupImage, ID: "image"}, path: "/v1.41/images/image"},
		{name: "container", resource: CleanupResource{Kind: CleanupContainer, ID: "container"}, path: "/v1.41/containers/container"},
		{name: "network", resource: CleanupResource{Kind: CleanupNetwork, ID: "network", Name: "named"}, path: "/v1.41/networks/network"},
		{name: "volume", resource: CleanupResource{Kind: CleanupVolume, ID: "volume"}, path: "/v1.41/volumes/volume"},
	}
	for _, test := range removeCases {
		t.Run("remove "+test.name, func(t *testing.T) {
			transport := newMockTransport()
			transport.register("DELETE", test.path, func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "failed"})
			})
			client := testDockerClient(t, transport)
			if err := client.RemoveCleanupResource(context.Background(), test.resource); err == nil || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("RemoveCleanupResource(%s) error = %v", test.name, err)
			}
		})
	}

	transport := newMockTransport()
	transport.register("POST", "/v1.41/build/prune", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "failed"})
	})
	client := testDockerClient(t, transport)
	if _, err := client.PruneBuildCache(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "prune build cache") {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestCleanupAdapterHelpers(t *testing.T) {
	if firstContainerName(nil) != "" || firstContainerName([]string{"/name"}) != "name" {
		t.Fatal("firstContainerName did not normalize names")
	}
	if !removableContainerState(containertypes.StateExited) || removableContainerState(containertypes.StatePaused) {
		t.Fatal("removableContainerState classified a state incorrectly")
	}
	if !isDefaultNetwork("bridge") || !isDefaultNetwork("host") || !isDefaultNetwork("none") || isDefaultNetwork("custom") {
		t.Fatal("isDefaultNetwork classified a network incorrectly")
	}
	if resourceLabel(CleanupResource{ID: "id", Name: "name"}) != "name" || resourceLabel(CleanupResource{ID: "id"}) != "id" {
		t.Fatal("resourceLabel chose the wrong identifier")
	}
	if nonNegative(-1) != 0 || nonNegative(1) != 1 {
		t.Fatal("nonNegative did not clamp a negative size")
	}
}
