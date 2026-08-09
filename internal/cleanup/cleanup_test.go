package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type fakeClient struct {
	images             []docker.ImageInfo
	listErr            error
	removeErr          map[string]error
	removed            []string
	danglingReq        bool
	resources          map[docker.CleanupResourceKind][]docker.CleanupResource
	resourceListErr    map[docker.CleanupResourceKind]error
	removedResources   []string
	removeResourceErr  map[string]error
	pruneResult        docker.CleanupPruneResult
	pruneErr           error
	buildCachePruneCut time.Time
	cancelAfterList    context.CancelFunc
}

func (f *fakeClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return nil, nil
}
func (f *fakeClient) InspectContainer(context.Context, string) (docker.ContainerDetails, error) {
	return docker.ContainerDetails{}, nil
}
func (f *fakeClient) PullImage(context.Context, string) (docker.ImageInfo, error) {
	return docker.ImageInfo{}, nil
}
func (f *fakeClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error { return nil }
func (f *fakeClient) ReplaceContainer(context.Context, docker.ContainerDetails, docker.ImageInfo, docker.ReplaceOptions) (docker.ReplaceResult, error) {
	return docker.ReplaceResult{}, nil
}
func (f *fakeClient) ListImages(context.Context) ([]docker.ImageInfo, error) {
	return f.images, f.listErr
}
func (f *fakeClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	f.danglingReq = true
	return f.images, f.listErr
}
func (f *fakeClient) RemoveImage(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return f.removeErr[id]
}
func (f *fakeClient) ListCleanupResources(_ context.Context, kind docker.CleanupResourceKind) ([]docker.CleanupResource, error) {
	if f.cancelAfterList != nil {
		f.cancelAfterList()
		f.cancelAfterList = nil
	}
	return f.resources[kind], f.resourceListErr[kind]
}
func (f *fakeClient) RemoveCleanupResource(_ context.Context, resource docker.CleanupResource) error {
	f.removedResources = append(f.removedResources, string(resource.Kind)+":"+resource.ID)
	return f.removeResourceErr[resource.ID]
}
func (f *fakeClient) PruneBuildCache(_ context.Context, before time.Time) (docker.CleanupPruneResult, error) {
	f.buildCachePruneCut = before
	return f.pruneResult, f.pruneErr
}

func TestCleanupExactAgeAndFailureIsolation(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{
		images: []docker.ImageInfo{
			{ID: "new", Dangling: true, CreatedAt: now.Add(-23 * time.Hour), Size: 1},
			{ID: "boundary", Dangling: true, CreatedAt: now.Add(-24 * time.Hour), Size: 2},
			{ID: "old", Dangling: true, CreatedAt: now.Add(-48 * time.Hour), Size: 4},
		},
		removeErr: map[string]error{"boundary": errors.New("in use")},
	}
	cfg := config.Default()
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if len(client.removed) != 2 || client.removed[0] != "old" || client.removed[1] != "boundary" {
		t.Fatalf("removal attempts = %v, want [old boundary]", client.removed)
	}
	if report.ReclaimedBytes != 4 {
		t.Fatalf("reclaimed = %d, want 4", report.ReclaimedBytes)
	}
	if !report.Results[0].Removed || report.Results[1].Err == nil {
		t.Fatalf("unexpected results: %+v", report.Results)
	}
}

func TestCleanupDryRunNeverRemoves(t *testing.T) {
	now := time.Now()
	client := &fakeClient{images: []docker.ImageInfo{{ID: "old", Dangling: true, CreatedAt: now.Add(-48 * time.Hour)}}}
	cfg := config.Default()
	cfg.Updates.DryRun = true

	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if len(client.removed) != 0 {
		t.Fatalf("dry-run removed images: %v", client.removed)
	}
	if len(report.Results) != 1 || !report.Results[0].WouldRemove {
		t.Fatalf("dry-run report = %+v", report)
	}
}

func TestCleanupListError(t *testing.T) {
	client := &fakeClient{listErr: errors.New("daemon unavailable")}
	cfg := config.Default()
	if _, err := RunCleanup(context.Background(), cfg, client, zerolog.Nop()); err == nil {
		t.Fatal("RunCleanup() error = nil, want error")
	}
	if !client.danglingReq {
		t.Fatal("default cleanup did not request dangling images")
	}
}

func TestCleanupListsAllImagesAndSkipsRecentImages(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{images: []docker.ImageInfo{
		{ID: "recent", CreatedAt: now.Add(-time.Hour), Dangling: true},
		{ID: "tagged", CreatedAt: now.Add(-48 * time.Hour), Dangling: false},
		{ID: "old", CreatedAt: now.Add(-48 * time.Hour), Dangling: true, Size: 9},
	}}
	cfg := config.Default()
	cfg.Cleanup.DanglingOnly = false
	cfg.Cleanup.MinAgeHours = 24
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if client.danglingReq || len(client.removed) != 2 || report.ReclaimedBytes != 9 {
		t.Fatalf("all-image cleanup removed=%v dangling=%v reclaimed=%d", client.removed, client.danglingReq, report.ReclaimedBytes)
	}
	if !report.Results[0].Removed || !report.Results[1].Removed || report.Results[2].Reason == "" {
		t.Fatalf("cleanup decisions = %+v", report.Results)
	}
}

func TestCleanupStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeClient{images: []docker.ImageInfo{{ID: "old", CreatedAt: time.Now().Add(-48 * time.Hour), Dangling: true}}}
	report, err := runCleanupAt(ctx, config.Default(), client, zerolog.Nop(), time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCleanupAt() error = %v, want context cancellation", err)
	}
	if len(report.Results) != 0 {
		t.Fatalf("canceled cleanup produced results: %+v", report.Results)
	}
}

func TestCleanupSkipsNonDanglingImageWhenConfigured(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{images: []docker.ImageInfo{{ID: "tagged", CreatedAt: now.Add(-48 * time.Hour), Dangling: false}}}
	cfg := config.Default()
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil || len(report.Results) != 1 || report.Results[0].Eligible || report.Results[0].Reason != "image is not dangling" {
		t.Fatalf("non-dangling cleanup = %+v, %v", report, err)
	}
}

func TestCleanupAllRemovesEveryUnusedResourceClass(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	client := &fakeClient{
		resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
			docker.CleanupImage: {
				{Kind: docker.CleanupImage, ID: "tagged", Name: "app:old", CreatedAt: old, Size: 2},
			},
			docker.CleanupContainer: {
				{Kind: docker.CleanupContainer, ID: "stopped", CreatedAt: old, Size: 3},
				{Kind: docker.CleanupContainer, ID: "running", CreatedAt: old, InUse: true},
			},
			docker.CleanupNetwork: {
				{Kind: docker.CleanupNetwork, ID: "unused-net", CreatedAt: old},
				{Kind: docker.CleanupNetwork, ID: "bridge", CreatedAt: old, Protected: true},
			},
			docker.CleanupVolume: {
				{Kind: docker.CleanupVolume, ID: "unused-vol", CreatedAt: old, Size: 5},
			},
			docker.CleanupBuildCache: {
				{Kind: docker.CleanupBuildCache, ID: "old-cache", CreatedAt: old, Size: 7},
				{Kind: docker.CleanupBuildCache, ID: "new-cache", CreatedAt: now.Add(-time.Hour), Size: 11},
			},
		},
		pruneResult: docker.CleanupPruneResult{Deleted: []string{"old-cache"}, ReclaimedBytes: 7},
	}
	cfg := config.Default()
	cfg.Cleanup.All = true
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if client.danglingReq {
		t.Fatal("cleanup.all must include unused tagged images")
	}
	if len(client.removed) != 0 {
		t.Fatalf("cleanup.all bypassed the resource inventory: %v", client.removed)
	}
	if got := strings.Join(client.removedResources, ","); got != "image:tagged,container:stopped,network:unused-net,volume:unused-vol" {
		t.Fatalf("removed resources = %q", got)
	}
	if client.buildCachePruneCut != now.Add(-24*time.Hour) {
		t.Fatalf("build cache cutoff = %v", client.buildCachePruneCut)
	}
	if report.ReclaimedBytes != 17 {
		t.Fatalf("reclaimed = %d, want 17", report.ReclaimedBytes)
	}
}

func TestCleanupAllDryRunNeverMutatesExtendedResources(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
		docker.CleanupContainer:  {{Kind: docker.CleanupContainer, ID: "container", CreatedAt: old}},
		docker.CleanupNetwork:    {{Kind: docker.CleanupNetwork, ID: "network", CreatedAt: old}},
		docker.CleanupVolume:     {{Kind: docker.CleanupVolume, ID: "volume", CreatedAt: old}},
		docker.CleanupBuildCache: {{Kind: docker.CleanupBuildCache, ID: "cache", CreatedAt: old}},
	}}
	cfg := config.Default()
	cfg.Cleanup.All = true
	cfg.Updates.DryRun = true
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("runCleanupAt() error = %v", err)
	}
	if len(client.removedResources) != 0 || !client.buildCachePruneCut.IsZero() {
		t.Fatalf("dry-run mutated resources: removed=%v cutoff=%v", client.removedResources, client.buildCachePruneCut)
	}
	wouldRemove := 0
	for _, result := range report.Results {
		if result.WouldRemove {
			wouldRemove++
		}
	}
	if wouldRemove != 4 {
		t.Fatalf("would-remove results = %d, want 4", wouldRemove)
	}
}

func TestCleanupExtendedPoliciesAreOptInAndRequireCapableClient(t *testing.T) {
	if kinds := enabledResourceKinds(config.Default().Cleanup); len(kinds) != 0 {
		t.Fatalf("default extended kinds = %v", kinds)
	}
	cfg := config.Default()
	cfg.Cleanup.StoppedContainers = true
	legacy := struct{ docker.Client }{Client: &fakeClient{}}
	_, err := runCleanupAt(context.Background(), cfg, legacy, zerolog.Nop(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "does not support extended cleanup") {
		t.Fatalf("extended cleanup error = %v", err)
	}
}

func TestCleanupExtendedErrorAndAgePaths(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	t.Run("list failure", func(t *testing.T) {
		client := &fakeClient{resourceListErr: map[docker.CleanupResourceKind]error{docker.CleanupContainer: errors.New("list failed")}}
		cfg := config.Default()
		cfg.Cleanup.StoppedContainers = true
		if _, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now); err == nil || !strings.Contains(err.Error(), "list container resources") {
			t.Fatalf("list error = %v", err)
		}
	})

	t.Run("all-image inventory failure", func(t *testing.T) {
		client := &fakeClient{resourceListErr: map[docker.CleanupResourceKind]error{docker.CleanupImage: errors.New("image list failed")}}
		cfg := config.Default()
		cfg.Cleanup.All = true
		if _, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now); err == nil || !strings.Contains(err.Error(), "list images for cleanup") {
			t.Fatalf("image inventory error = %v", err)
		}
	})

	t.Run("build cache has no eligible records", func(t *testing.T) {
		client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
			docker.CleanupBuildCache: {{Kind: docker.CleanupBuildCache, ID: "new", CreatedAt: now}},
		}}
		cfg := config.Default()
		cfg.Cleanup.BuildCache = true
		if _, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now); err != nil || !client.buildCachePruneCut.IsZero() {
			t.Fatalf("no-op build prune error=%v cutoff=%v", err, client.buildCachePruneCut)
		}
	})

	t.Run("build cache prune failure", func(t *testing.T) {
		client := &fakeClient{
			resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
				docker.CleanupBuildCache: {{Kind: docker.CleanupBuildCache, ID: "old", CreatedAt: old}},
			},
			pruneErr: errors.New("prune failed"),
		}
		cfg := config.Default()
		cfg.Cleanup.BuildCache = true
		if _, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now); err == nil || !strings.Contains(err.Error(), "prune failed") {
			t.Fatalf("prune error = %v", err)
		}
	})

	t.Run("cancellation between categories", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{}, cancelAfterList: cancel}
		cfg := config.Default()
		cfg.Cleanup.StoppedContainers = true
		cfg.Cleanup.UnusedNetworks = true
		if _, err := runCleanupAt(ctx, cfg, client, zerolog.Nop(), now); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	})

	t.Run("cancellation after final category", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{}, cancelAfterList: cancel}
		cfg := config.Default()
		cfg.Cleanup.StoppedContainers = true
		if _, err := runCleanupAt(ctx, cfg, client, zerolog.Nop(), now); !errors.Is(err, context.Canceled) {
			t.Fatalf("final cancellation error = %v", err)
		}
	})

	lastUsed := now.Add(-time.Hour)
	if result := classify(docker.CleanupResource{CreatedAt: old, LastUsedAt: lastUsed}, config.Default().Cleanup, now); result.Reason != "resource is newer than the minimum age" {
		t.Fatalf("last-used classification = %+v", result)
	}
	if result := classify(docker.CleanupResource{}, config.Default().Cleanup, now); result.Reason != "resource age is unavailable" {
		t.Fatalf("unknown-age classification = %+v", result)
	}
}

func TestFormatBytesAndShortIDBoundaries(t *testing.T) {
	for _, test := range []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KiB"},
		{bytes: 1024 * 1024, want: "1.0 MiB"},
		{bytes: 1024 * 1024 * 1024, want: "1.0 GiB"},
	} {
		if got := formatBytes(test.bytes); got != test.want {
			t.Errorf("formatBytes(%d) = %q, want %q", test.bytes, got, test.want)
		}
	}
	for _, test := range []struct {
		id   string
		want string
	}{
		{id: "sha256:1234567890123456", want: "123456789012"},
		{id: "short", want: "short"},
		{id: "sha256:", want: ""},
	} {
		if got := shortID(test.id); got != test.want {
			t.Errorf("shortID(%q) = %q, want %q", test.id, got, test.want)
		}
	}
}
