package updater

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/rs/zerolog"
)

type selfUpdateFakeClient struct {
	containers []docker.ContainerSummary
	details    map[string]docker.ContainerDetails
	images     map[string]docker.ImageInfo
	actions    []string
	helperErr  error
	helperReq  docker.SelfUpdateHelperRequest
}

func (f *selfUpdateFakeClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return append([]docker.ContainerSummary(nil), f.containers...), nil
}
func (f *selfUpdateFakeClient) InspectContainer(_ context.Context, id string) (docker.ContainerDetails, error) {
	return f.details[id], nil
}
func (f *selfUpdateFakeClient) PullImage(_ context.Context, image string) (docker.ImageInfo, error) {
	return f.images[image], nil
}
func (f *selfUpdateFakeClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error {
	return nil
}
func (f *selfUpdateFakeClient) ReplaceContainer(_ context.Context, current docker.ContainerDetails, _ docker.ImageInfo, _ docker.ReplaceOptions) (docker.ReplaceResult, error) {
	f.actions = append(f.actions, "replace:"+current.Summary.ID)
	return docker.ReplaceResult{}, nil
}
func (f *selfUpdateFakeClient) ListImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (f *selfUpdateFakeClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (f *selfUpdateFakeClient) RemoveImage(context.Context, string) error { return nil }
func (f *selfUpdateFakeClient) StartSelfUpdateHelper(_ context.Context, current docker.ContainerDetails, request docker.SelfUpdateHelperRequest) (string, error) {
	f.actions = append(f.actions, "helper:"+current.Summary.ID)
	f.helperReq = request
	if f.helperErr != nil {
		return "", f.helperErr
	}
	return "helper-123", nil
}

func TestRunUpdateCycleProcessesSelfLast(t *testing.T) {
	self := docker.ContainerSummary{ID: "self", Name: "aaa-harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:self-old", Labels: map[string]string{"com.harborbuddy.role": "daemon"}}
	ordinary := docker.ContainerSummary{ID: "app", Name: "zzz-app", ImageRef: "app:latest", ImageID: "sha256:app-old"}
	client := &selfUpdateFakeClient{
		containers: []docker.ContainerSummary{self, ordinary},
		details: map[string]docker.ContainerDetails{
			"self": runningDetails(self),
			"app":  runningDetails(ordinary),
		},
		images: map[string]docker.ImageInfo{
			"harborbuddy:latest": {ID: "sha256:self-new"},
			"app:latest":         {ID: "sha256:app-new"},
		},
	}
	originalDetector := detectCurrentContainer
	detectCurrentContainer = func([]docker.ContainerSummary) string { return "self" }
	defer func() { detectCurrentContainer = originalDetector }()

	cfg := config.Default()
	report, err := RunUpdateCycle(context.Background(), cfg, client, zerolog.Nop())
	if _, ok := selfupdate.AsShutdownRequired(err); !ok {
		t.Fatalf("RunUpdateCycle() error = %v, want shutdown signal", err)
	}
	if len(client.actions) != 2 || client.actions[0] != "replace:app" || client.actions[1] != "helper:self" {
		t.Fatalf("update order = %v, want ordinary replacement before helper", client.actions)
	}
	if resultStatus(report, "app") != StatusUpdated || resultStatus(report, "self") != StatusSelfUpdateStarted {
		t.Fatalf("unexpected report results: %+v", report.Results)
	}
	if client.helperReq.StopTimeout != cfg.Updates.StopTimeout || client.helperReq.StartupTimeout != cfg.Updates.StartupTimeout {
		t.Fatalf("helper timeouts = stop %s startup %s, want stop %s startup %s", client.helperReq.StopTimeout, client.helperReq.StartupTimeout, cfg.Updates.StopTimeout, cfg.Updates.StartupTimeout)
	}
}

func TestRunUpdateCycleHelperFailureDoesNotRequestShutdown(t *testing.T) {
	self := docker.ContainerSummary{ID: "self", Name: "harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:old"}
	client := &selfUpdateFakeClient{
		containers: []docker.ContainerSummary{self},
		details:    map[string]docker.ContainerDetails{"self": runningDetails(self)},
		images:     map[string]docker.ImageInfo{"harborbuddy:latest": {ID: "sha256:new"}},
		helperErr:  errors.New("Docker socket is not writable"),
	}
	originalDetector := detectCurrentContainer
	detectCurrentContainer = func([]docker.ContainerSummary) string { return "self" }
	defer func() { detectCurrentContainer = originalDetector }()

	report, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v, helper start failure should be reported per-container", err)
	}
	if got := resultStatus(report, "self"); got != StatusFailed {
		t.Fatalf("self result = %q, want %q", got, StatusFailed)
	}
	if report.Results[0].Err == nil || !errors.Is(report.Results[0].Err, client.helperErr) {
		t.Fatalf("helper start error not preserved: %v", report.Results[0].Err)
	}
}

func TestRunUpdateCycleCanDisableSelfUpdate(t *testing.T) {
	self := docker.ContainerSummary{ID: "self", Name: "harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:old"}
	client := &selfUpdateFakeClient{
		containers: []docker.ContainerSummary{self},
		details:    map[string]docker.ContainerDetails{"self": runningDetails(self)},
		images:     map[string]docker.ImageInfo{"harborbuddy:latest": {ID: "sha256:new"}},
	}
	originalDetector := detectCurrentContainer
	detectCurrentContainer = func([]docker.ContainerSummary) string { return "self" }
	defer func() { detectCurrentContainer = originalDetector }()
	cfg := config.Default()
	cfg.Updates.SelfUpdate = false

	report, err := RunUpdateCycle(context.Background(), cfg, client, zerolog.Nop())
	if err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	if got := resultStatus(report, "self"); got != StatusExcluded {
		t.Fatalf("self result = %q, want %q", got, StatusExcluded)
	}
	if len(client.actions) != 0 {
		t.Fatalf("disabled self-update performed actions: %v", client.actions)
	}
	if report.Results[0].Reason != "self-update is disabled" {
		t.Fatalf("disabled reason = %q", report.Results[0].Reason)
	}
}

func TestDaemonRoleLabelIsNotAnIdentityCheck(t *testing.T) {
	decision := DetermineEligibility(docker.ContainerSummary{
		ImageRef: "harborbuddy:latest",
		Labels:   map[string]string{"com.harborbuddy.role": "daemon"},
	}, config.UpdatesConfig{AllowImages: []string{"*"}})
	if !decision.Eligible {
		t.Fatalf("role-labeled container excluded as self: %s", decision.Reason)
	}
}

func runningDetails(summary docker.ContainerSummary) docker.ContainerDetails {
	return docker.ContainerDetails{
		Summary: summary,
		Config:  &containertypes.Config{},
		Host:    &containertypes.HostConfig{},
		State:   &containertypes.State{Running: true},
	}
}

func resultStatus(report Report, id string) Status {
	for _, result := range report.Results {
		if result.Container.ID == id {
			return result.Status
		}
	}
	return ""
}
