package updater

import (
	"context"
	"sync"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/rs/zerolog"
)

type cycleFakeClient struct {
	containers     []docker.ContainerSummary
	details        map[string]docker.ContainerDetails
	images         map[string]docker.ImageInfo
	listErr        error
	inspectErrors  map[string]error
	pullErrors     map[string]error
	checkErrors    map[string]error
	replaceErrors  map[string]error
	replaceResults map[string]docker.ReplaceResult

	mu           sync.Mutex
	pulls        map[string]int
	replacements []string
}

func (f *cycleFakeClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return append([]docker.ContainerSummary(nil), f.containers...), f.listErr
}
func (f *cycleFakeClient) InspectContainer(_ context.Context, id string) (docker.ContainerDetails, error) {
	return f.details[id], f.inspectErrors[id]
}
func (f *cycleFakeClient) PullImage(_ context.Context, ref string) (docker.ImageInfo, error) {
	f.mu.Lock()
	if f.pulls == nil {
		f.pulls = make(map[string]int)
	}
	f.pulls[ref]++
	f.mu.Unlock()
	return f.images[ref], f.pullErrors[ref]
}
func (f *cycleFakeClient) CheckReplacement(current docker.ContainerDetails, _ docker.ImageInfo) error {
	return f.checkErrors[current.Summary.ID]
}
func (f *cycleFakeClient) ReplaceContainer(_ context.Context, current docker.ContainerDetails, _ docker.ImageInfo, _ docker.ReplaceOptions) (docker.ReplaceResult, error) {
	f.replacements = append(f.replacements, current.Summary.ID)
	return f.replaceResults[current.Summary.ID], f.replaceErrors[current.Summary.ID]
}
func (f *cycleFakeClient) ListImages(context.Context) ([]docker.ImageInfo, error) { return nil, nil }
func (f *cycleFakeClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (f *cycleFakeClient) RemoveImage(context.Context, string) error { return nil }

func TestRunUpdateCycleDeterministicResults(t *testing.T) {
	current := docker.ContainerSummary{ID: "current", Name: "b-current", ImageRef: "current:latest", ImageID: "sha256:current"}
	outdated := docker.ContainerSummary{ID: "outdated", Name: "a-outdated", ImageRef: "outdated:latest", ImageID: "sha256:old"}
	excluded := docker.ContainerSummary{ID: "excluded", Name: "c-excluded", ImageRef: "excluded:latest", ImageID: "sha256:old", Labels: map[string]string{AutoUpdateLabel: "false"}}
	client := &cycleFakeClient{
		containers: []docker.ContainerSummary{current, excluded, outdated},
		details: map[string]docker.ContainerDetails{
			"current":  cycleRunningDetails(current),
			"outdated": cycleRunningDetails(outdated),
		},
		images: map[string]docker.ImageInfo{
			"current:latest":  {ID: "sha256:current"},
			"outdated:latest": {ID: "sha256:new"},
		},
	}
	withoutDetectedSelf(t)

	report, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	if len(report.Results) != 3 || report.Results[0].Container.ID != "outdated" || report.Results[1].Container.ID != "current" || report.Results[2].Container.ID != "excluded" {
		t.Fatalf("results are not deterministically name-sorted: %+v", report.Results)
	}
	if report.Results[0].Status != StatusUpdated || report.Results[1].Status != StatusCurrent || report.Results[2].Status != StatusExcluded {
		t.Fatalf("unexpected statuses: %+v", report.Results)
	}
}

func TestRunUpdateCyclePullsSharedImageOnce(t *testing.T) {
	first := docker.ContainerSummary{ID: "one", Name: "one", ImageRef: "shared:latest", ImageID: "sha256:old"}
	second := docker.ContainerSummary{ID: "two", Name: "two", ImageRef: "shared:latest", ImageID: "sha256:old"}
	client := &cycleFakeClient{
		containers: []docker.ContainerSummary{first, second},
		details: map[string]docker.ContainerDetails{
			"one": cycleRunningDetails(first),
			"two": cycleRunningDetails(second),
		},
		images: map[string]docker.ImageInfo{"shared:latest": {ID: "sha256:new"}},
	}
	withoutDetectedSelf(t)

	report, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	if client.pulls["shared:latest"] != 1 {
		t.Fatalf("shared image pulled %d times, want 1", client.pulls["shared:latest"])
	}
	if report.Count(StatusUpdated) != 2 || len(client.replacements) != 2 {
		t.Fatalf("shared-image containers not both updated: results=%+v replacements=%v", report.Results, client.replacements)
	}
}

func TestRunUpdateCycleDryRunDiscoversWithoutReplacing(t *testing.T) {
	container := docker.ContainerSummary{ID: "app", Name: "app", ImageRef: "app:latest", ImageID: "sha256:old"}
	client := &cycleFakeClient{
		containers: []docker.ContainerSummary{container},
		details:    map[string]docker.ContainerDetails{"app": cycleRunningDetails(container)},
		images:     map[string]docker.ImageInfo{"app:latest": {ID: "sha256:new"}},
	}
	withoutDetectedSelf(t)
	cfg := config.Default()
	cfg.Updates.DryRun = true

	report, err := RunUpdateCycle(context.Background(), cfg, client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	if report.Count(StatusWouldUpdate) != 1 || len(client.replacements) != 0 {
		t.Fatalf("dry run mutated containers: results=%+v replacements=%v", report.Results, client.replacements)
	}
}

func TestRunUpdateCycleNeverOrdinarilyReplacesUnidentifiedDaemon(t *testing.T) {
	container := docker.ContainerSummary{
		ID: "daemon", Name: "harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:old",
		Labels: map[string]string{RoleLabel: DaemonRole},
	}
	client := &cycleFakeClient{
		containers: []docker.ContainerSummary{container},
		details:    map[string]docker.ContainerDetails{"daemon": cycleRunningDetails(container)},
		images:     map[string]docker.ImageInfo{"harborbuddy:latest": {ID: "sha256:new"}},
	}
	withoutDetectedSelf(t)

	report, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	if report.Count(StatusExcluded) != 1 || len(client.replacements) != 0 || client.pulls["harborbuddy:latest"] != 0 {
		t.Fatalf("unidentified daemon was not protected: results=%+v pulls=%v replacements=%v", report.Results, client.pulls, client.replacements)
	}
}

func cycleRunningDetails(summary docker.ContainerSummary) docker.ContainerDetails {
	return docker.ContainerDetails{
		Summary: summary,
		Config:  &containertypes.Config{},
		Host:    &containertypes.HostConfig{},
		State:   &containertypes.State{Running: true},
	}
}

func withoutDetectedSelf(t *testing.T) {
	t.Helper()
	original := detectCurrentContainer
	detectCurrentContainer = func([]docker.ContainerSummary) string { return "" }
	t.Cleanup(func() { detectCurrentContainer = original })
}
