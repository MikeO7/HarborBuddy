package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type fakeClient struct {
	images      []docker.ImageInfo
	listErr     error
	removeErr   map[string]error
	removed     []string
	danglingReq bool
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
