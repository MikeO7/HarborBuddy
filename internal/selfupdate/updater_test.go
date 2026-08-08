package selfupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	containertypes "github.com/docker/docker/api/types/container"
)

type fakeClient struct {
	containers    []docker.ContainerSummary
	details       docker.ContainerDetails
	helperID      string
	helperErr     error
	helperRequest docker.SelfUpdateHelperRequest
	waitedFor     string
	replaced      bool
	replaceTarget docker.ImageInfo
	replaceOpts   docker.ReplaceOptions
}

func (f *fakeClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return f.containers, nil
}
func (f *fakeClient) InspectContainer(context.Context, string) (docker.ContainerDetails, error) {
	return f.details, nil
}
func (f *fakeClient) PullImage(context.Context, string) (docker.ImageInfo, error) {
	return docker.ImageInfo{}, nil
}
func (f *fakeClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error { return nil }
func (f *fakeClient) ReplaceContainer(_ context.Context, _ docker.ContainerDetails, target docker.ImageInfo, options docker.ReplaceOptions) (docker.ReplaceResult, error) {
	f.replaced = true
	f.replaceTarget = target
	f.replaceOpts = options
	return docker.ReplaceResult{}, nil
}
func (f *fakeClient) ListImages(context.Context) ([]docker.ImageInfo, error) { return nil, nil }
func (f *fakeClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (f *fakeClient) RemoveImage(context.Context, string) error { return nil }
func (f *fakeClient) StartSelfUpdateHelper(_ context.Context, _ docker.ContainerDetails, request docker.SelfUpdateHelperRequest) (string, error) {
	f.helperRequest = request
	return f.helperID, f.helperErr
}
func (f *fakeClient) WaitContainerExit(_ context.Context, id string) error {
	f.waitedFor = id
	return nil
}

func TestDetectCurrentContainer(t *testing.T) {
	containers := []docker.ContainerSummary{
		{ID: strings.Repeat("a", 64)},
		{ID: strings.Repeat("b", 64)},
	}

	t.Run("cgroup takes precedence", func(t *testing.T) {
		cgroup := []byte("0::/system.slice/docker-" + containers[1].ID + ".scope\n")
		if got := detectCurrentContainer(containers, containers[0].ID[:12], cgroup); got != containers[1].ID {
			t.Fatalf("detected %q, want cgroup container %q", got, containers[1].ID)
		}
	})

	t.Run("Docker hostname prefix", func(t *testing.T) {
		if got := detectCurrentContainer(containers, containers[0].ID[:12], nil); got != containers[0].ID {
			t.Fatalf("detected %q, want hostname container %q", got, containers[0].ID)
		}
	})

	t.Run("ambiguous identity is rejected", func(t *testing.T) {
		ambiguous := []docker.ContainerSummary{{ID: "abcdef123456" + strings.Repeat("0", 52)}, {ID: "abcdef123456" + strings.Repeat("1", 52)}}
		if got := detectCurrentContainer(ambiguous, "abcdef123456", nil); got != "" {
			t.Fatalf("detected ambiguous container %q", got)
		}
	})

	t.Run("role label is not positive identity", func(t *testing.T) {
		withRole := append([]docker.ContainerSummary(nil), containers...)
		withRole[1].Labels = map[string]string{"com.harborbuddy.role": "daemon"}
		if got := detectCurrentContainer(withRole, "custom-hostname", nil); got != "" {
			t.Fatalf("detected role-labeled remote container %q", got)
		}
	})
}

func TestDetectCurrentContainerExplicitID(t *testing.T) {
	container := docker.ContainerSummary{ID: strings.Repeat("c", 64)}
	t.Setenv("HARBORBUDDY_CONTAINER_ID", container.ID[:12])
	if got := DetectCurrentContainer([]docker.ContainerSummary{container}); got != container.ID {
		t.Fatalf("DetectCurrentContainer() = %q, want %q", got, container.ID)
	}
}

func TestDetectCurrentContainerExplicitName(t *testing.T) {
	container := docker.ContainerSummary{ID: strings.Repeat("d", 64), Name: "harborbuddy"}
	t.Setenv("HARBORBUDDY_CONTAINER_NAME", "harborbuddy")
	if got := DetectCurrentContainer([]docker.ContainerSummary{container}); got != container.ID {
		t.Fatalf("DetectCurrentContainer() = %q, want %q", got, container.ID)
	}
}

func TestTriggerReturnsShutdownSignal(t *testing.T) {
	client := &fakeClient{helperID: "helper-123"}
	current := docker.ContainerDetails{Summary: docker.ContainerSummary{ID: "self-123", Name: "harborbuddy"}}
	err := Trigger(context.Background(), client, current, docker.ImageInfo{ID: "sha256:new"}, TriggerOptions{
		DockerHost:     "tcp://docker:2376",
		StopTimeout:    7 * time.Second,
		StartupTimeout: 19 * time.Second,
	})

	signal, ok := AsShutdownRequired(err)
	if !ok {
		t.Fatalf("Trigger() error = %v, want ShutdownRequiredError", err)
	}
	if signal.HelperContainerID != "helper-123" || signal.TargetContainerID != "self-123" {
		t.Fatalf("unexpected signal: %+v", signal)
	}
	if client.helperRequest.TargetImageID != "sha256:new" || client.helperRequest.DockerHost != "tcp://docker:2376" || client.helperRequest.StopTimeout != 7*time.Second || client.helperRequest.StartupTimeout != 19*time.Second {
		t.Fatalf("unexpected helper request: %+v", client.helperRequest)
	}
}

func TestTriggerReportsHelperStartFailure(t *testing.T) {
	client := &fakeClient{helperErr: errors.New("permission denied")}
	current := docker.ContainerDetails{Summary: docker.ContainerSummary{ID: "self-123", Name: "harborbuddy"}}
	err := Trigger(context.Background(), client, current, docker.ImageInfo{ID: "sha256:new"}, TriggerOptions{})
	if err == nil || !strings.Contains(err.Error(), "start self-update helper for self-123") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Trigger() error = %v, want clear helper start error", err)
	}
	if _, ok := AsShutdownRequired(err); ok {
		t.Fatal("helper start failure must not request shutdown")
	}
}

func TestRunUpdaterUsesTransactionalReplacement(t *testing.T) {
	client := &fakeClient{details: docker.ContainerDetails{
		Summary: docker.ContainerSummary{ID: "self-123", Name: "harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:old"},
		Config:  &containertypes.Config{},
		Host:    &containertypes.HostConfig{},
		State:   &containertypes.State{Running: false},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	request := UpdaterRequest{
		TargetContainerID: "self-123",
		TargetImageID:     "sha256:new",
		StopTimeout:       6 * time.Second,
		StartupTimeout:    17 * time.Second,
	}
	if err := RunUpdater(ctx, client, request); err != nil {
		t.Fatalf("RunUpdater() error = %v", err)
	}
	if client.waitedFor != "self-123" {
		t.Fatalf("waited for %q, want self-123", client.waitedFor)
	}
	if !client.replaced || client.replaceTarget.ID != "sha256:new" {
		t.Fatalf("transactional replacement not called with target: %+v", client.replaceTarget)
	}
	if !client.replaceOpts.CurrentAlreadyStopped {
		t.Fatal("stopped target must use CurrentAlreadyStopped transaction mode")
	}
	if client.replaceOpts.StopTimeout != request.StopTimeout || client.replaceOpts.StartupTimeout != request.StartupTimeout {
		t.Fatalf("replacement timeouts = stop %s startup %s, want stop %s startup %s", client.replaceOpts.StopTimeout, client.replaceOpts.StartupTimeout, request.StopTimeout, request.StartupTimeout)
	}
}
