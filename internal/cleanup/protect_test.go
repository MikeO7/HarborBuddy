package cleanup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

func reasons(report Report) map[string]string {
	result := make(map[string]string, len(report.Results))
	for _, item := range report.Results {
		result[string(item.Resource.Kind)+":"+item.Resource.ID] = item.Reason
	}
	return result
}

func TestCleanupSkipsImagesThatContainersUseInEveryImageMode(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	images := []docker.ImageInfo{
		{ID: "running-dangling", Dangling: true, InUse: true, CreatedAt: old},
		{ID: "stopped-tagged", RepoTags: []string{"app:old"}, InUse: true, CreatedAt: old},
		{ID: "free", Dangling: true, CreatedAt: old},
	}
	for _, danglingOnly := range []bool{true, false} {
		client := &fakeClient{images: images}
		cfg := config.Default()
		cfg.Cleanup.DanglingOnly = danglingOnly
		report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
		if err != nil {
			t.Fatalf("dangling_only=%v: runCleanupAt() error = %v", danglingOnly, err)
		}
		if strings.Join(client.removed, ",") != "free" {
			t.Fatalf("dangling_only=%v: removed = %v, want only the unused image", danglingOnly, client.removed)
		}
		if counts := countResults(report.Results, ""); counts.Failed != 0 || counts.Skipped != 2 {
			t.Fatalf("dangling_only=%v: counts = %+v", danglingOnly, counts)
		}
		if got := reasons(report)["image:running-dangling"]; got != "resource is in use" {
			t.Fatalf("dangling_only=%v: in-use reason = %q", danglingOnly, got)
		}
	}
}

func TestKeepImagesMatchesDigestReferences(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	client := &fakeClient{images: []docker.ImageInfo{
		{ID: "tool", RepoDigests: []string{"ghcr.io/org/tool@sha256:abc"}, Dangling: true, CreatedAt: old},
		{ID: "exact", RepoDigests: []string{"registry.local/helper@sha256:def"}, Dangling: true, CreatedAt: old},
		{ID: "containerd", RepoTags: []string{"ghcr.io/org/other@sha256:123"}, CreatedAt: old},
		{ID: "unkept", RepoDigests: []string{"ghcr.io/else/app@sha256:456"}, Dangling: true, CreatedAt: old},
	}}
	cfg := config.Default()
	cfg.Cleanup.DanglingOnly = false
	cfg.Cleanup.KeepImages = []string{"ghcr.io/org/*", "registry.local/helper"}
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if strings.Join(client.removed, ",") != "unkept" {
		t.Fatalf("removed = %v, want only the image outside keep_images", client.removed)
	}
	for _, id := range []string{"tool", "exact", "containerd"} {
		if got := reasons(report)["image:"+id]; got != "image is in cleanup.keep_images" {
			t.Fatalf("%s reason = %q", id, got)
		}
	}
}

func TestImageReferencesSkipPlaceholders(t *testing.T) {
	got := imageReferences(docker.CleanupResource{Name: "<none>:<none>", RepoDigests: []string{"<none>@<none>", "app@sha256:abc", "@sha256:def"}})
	if strings.Join(got, ",") != "app@sha256:abc,app,@sha256:def" {
		t.Fatalf("imageReferences() = %v", got)
	}
	if got := imageReferences(docker.CleanupResource{}); len(got) != 0 {
		t.Fatalf("imageReferences(empty) = %v", got)
	}
}

func TestCleanupKeepsContainersAndNetworksAfterAFailedUpdate(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
		docker.CleanupContainer: {
			{Kind: docker.CleanupContainer, ID: "original", Name: "web", CreatedAt: old},
			{Kind: docker.CleanupContainer, ID: "backup", Name: "db-harborbuddy-backup-1", CreatedAt: old, Protected: true, ProtectedReason: "container is a HarborBuddy update backup"},
			{Kind: docker.CleanupContainer, ID: "leftover", CreatedAt: old},
		},
		docker.CleanupNetwork: {
			{Kind: docker.CleanupNetwork, ID: "app-net", CreatedAt: old},
			{Kind: docker.CleanupNetwork, ID: "bridge", CreatedAt: old, Protected: true},
		},
		docker.CleanupVolume: {{Kind: docker.CleanupVolume, ID: "original-data", CreatedAt: old, InUse: true}},
	}}
	cfg := config.Default()
	cfg.Cleanup.StoppedContainers, cfg.Cleanup.UnusedNetworks, cfg.Cleanup.UnusedVolumes = true, true, true
	report, err := RunCleanupWithOptions(context.Background(), cfg, client, zerolog.Nop(), Options{ProtectedContainerIDs: []string{"original"}})
	if err != nil {
		t.Fatalf("RunCleanupWithOptions() error = %v", err)
	}
	if strings.Join(client.removedResources, ",") != "container:leftover" {
		t.Fatalf("removed = %v, want only the unrelated stopped container", client.removedResources)
	}
	got := reasons(report)
	for key, want := range map[string]string{
		"container:original":   failedUpdateContainerReason,
		"container:backup":     "container is a HarborBuddy update backup",
		"network:app-net":      failedUpdateNetworkReason,
		"network:bridge":       "resource is protected",
		"volume:original-data": "resource is in use",
	} {
		if got[key] != want {
			t.Fatalf("%s reason = %q, want %q", key, got[key], want)
		}
	}

	client.removedResources = nil
	if _, err := RunCleanupWithOptions(context.Background(), cfg, client, zerolog.Nop(), Options{}); err != nil {
		t.Fatalf("RunCleanupWithOptions(no failures) error = %v", err)
	}
	if strings.Join(client.removedResources, ",") != "container:leftover,container:original,network:app-net" {
		t.Fatalf("removed without failures = %v", client.removedResources)
	}
}

func TestBuildCachePruneWaitsWhenItWouldRemoveKeptRecords(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	for _, test := range []struct {
		name string
		kept docker.CleanupResource
		safe bool
	}{
		{name: "unknown age", kept: docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "kept"}},
		{name: "never used but new", kept: docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "kept", CreatedAt: now}},
		{name: "unknown age but in use", kept: docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "kept", InUse: true}, safe: true},
		{name: "recently used", kept: docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "kept", CreatedAt: old, LastUsedAt: now}, safe: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
				docker.CleanupBuildCache: {{Kind: docker.CleanupBuildCache, ID: "old", CreatedAt: old, LastUsedAt: old}, test.kept},
			}, pruneResult: docker.CleanupPruneResult{Deleted: []string{"old"}}}
			cfg := config.Default()
			cfg.Cleanup.BuildCache = true
			report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
			if err != nil {
				t.Fatalf("runCleanupAt() error = %v", err)
			}
			if pruned := !client.buildCachePruneCut.IsZero(); pruned != test.safe {
				t.Fatalf("pruned = %v, want %v", pruned, test.safe)
			}
			if !test.safe && reasons(report)["build_cache:old"] != unsafeBuildCachePruneReason {
				t.Fatalf("skipped prune reasons = %v", reasons(report))
			}
		})
	}
}
