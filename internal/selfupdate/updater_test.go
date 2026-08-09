package selfupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	containertypes "github.com/moby/moby/api/types/container"
)

type fakeClient struct {
	containers     []docker.ContainerSummary
	details        docker.ContainerDetails
	helperID       string
	helperErr      error
	helperRequest  docker.SelfUpdateHelperRequest
	waitedFor      string
	waitErr        error
	inspectErr     error
	replaceErr     error
	backupErr      error
	replaced       bool
	replaceCurrent docker.ContainerDetails
	replaceTarget  docker.ImageInfo
	replaceOpts    docker.ReplaceOptions
}

func (f *fakeClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) {
	return f.containers, nil
}
func (f *fakeClient) InspectContainer(context.Context, string) (docker.ContainerDetails, error) {
	return f.details, f.inspectErr
}
func (f *fakeClient) PullImage(context.Context, string) (docker.ImageInfo, error) {
	return docker.ImageInfo{}, nil
}
func (f *fakeClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error { return nil }
func (f *fakeClient) ReplaceContainer(_ context.Context, current docker.ContainerDetails, target docker.ImageInfo, options docker.ReplaceOptions) (docker.ReplaceResult, error) {
	f.replaced = true
	f.replaceCurrent = current
	f.replaceTarget = target
	f.replaceOpts = options
	return docker.ReplaceResult{BackupCleanupErr: f.backupErr}, f.replaceErr
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
	return f.waitErr
}

type clientWithoutWait struct{ docker.Client }

func TestShutdownRequiredErrorAndUnwrapDetection(t *testing.T) {
	signal := &ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
	if got := signal.Error(); !strings.Contains(got, "helper") || !strings.Contains(got, "target") {
		t.Fatalf("ShutdownRequiredError.Error() = %q", got)
	}
	if got, ok := AsShutdownRequired(errors.New("ordinary")); ok || got != nil {
		t.Fatalf("AsShutdownRequired(ordinary) = %v, %v", got, ok)
	}
	wrapped := errors.Join(signal)
	if got, ok := AsShutdownRequired(wrapped); !ok || got != signal {
		t.Fatalf("AsShutdownRequired(signal) = %v, %v", got, ok)
	}
}

func TestTriggerValidatesIdentityAndHelperID(t *testing.T) {
	client := &fakeClient{helperID: "helper"}
	for _, test := range []struct {
		name    string
		current docker.ContainerDetails
		target  docker.ImageInfo
		want    string
	}{
		{name: "missing current id", current: docker.ContainerDetails{Summary: docker.ContainerSummary{Name: "harborbuddy"}}, target: docker.ImageInfo{ID: "new"}, want: "identity is incomplete"},
		{name: "missing current name", current: docker.ContainerDetails{Summary: docker.ContainerSummary{ID: "self"}}, target: docker.ImageInfo{ID: "new"}, want: "identity is incomplete"},
		{name: "missing target", current: docker.ContainerDetails{Summary: docker.ContainerSummary{ID: "self", Name: "harborbuddy"}}, want: "target image identity is missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Trigger(context.Background(), client, test.current, test.target, TriggerOptions{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Trigger() error = %v, want %q", err, test.want)
			}
		})
	}
	client.helperID = ""
	err := Trigger(context.Background(), client, docker.ContainerDetails{Summary: docker.ContainerSummary{ID: "self", Name: "harborbuddy"}}, docker.ImageInfo{ID: "new"}, TriggerOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty helper ID") {
		t.Fatalf("Trigger() error = %v, want empty helper ID error", err)
	}
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

func TestDetectCurrentContainerFallsBackToRuntimeSourcesWhenHintsMiss(t *testing.T) {
	t.Setenv("HARBORBUDDY_CONTAINER_NAME", "missing")
	t.Setenv("HARBORBUDDY_CONTAINER_ID", "missing")
	containers := []docker.ContainerSummary{{ID: strings.Repeat("z", 64), Name: "other"}}
	if got := DetectCurrentContainer(containers); got != "" {
		t.Fatalf("runtime fallback detected unrelated container %q", got)
	}
	if got := uniqueCgroupMatch(containers, []byte("runtime/12345678901234567890")); got != "" {
		t.Fatalf("unrelated cgroup token matched %q", got)
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
		RestartPolicy: containertypes.RestartPolicy{
			Name:              "on-failure",
			MaximumRetryCount: 3,
		},
	}
	if _, err := RunUpdater(ctx, client, request); err != nil {
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
	if got := client.replaceCurrent.Host.RestartPolicy; got != request.RestartPolicy {
		t.Fatalf("replacement restart policy = %+v, want %+v", got, request.RestartPolicy)
	}
	if client.replaceOpts.StopTimeout != request.StopTimeout || client.replaceOpts.StartupTimeout != request.StartupTimeout {
		t.Fatalf("replacement timeouts = stop %s startup %s, want stop %s startup %s", client.replaceOpts.StopTimeout, client.replaceOpts.StartupTimeout, request.StopTimeout, request.StartupTimeout)
	}
}

func TestRunUpdaterReportsValidationWaitInspectAndReplaceFailures(t *testing.T) {
	client := &fakeClient{}
	for _, test := range []struct {
		name    string
		client  docker.Client
		request UpdaterRequest
		want    string
	}{
		{name: "missing IDs", client: client, request: UpdaterRequest{}, want: "requires target container and image IDs"},
		{name: "client cannot wait", client: &clientWithoutWait{}, request: UpdaterRequest{TargetContainerID: "target", TargetImageID: "new"}, want: "cannot wait for the target container to exit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := RunUpdater(context.Background(), test.client, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("RunUpdater() error = %v, want %q", err, test.want)
			}
		})
	}

	baseRequest := UpdaterRequest{TargetContainerID: "target", TargetImageID: "new"}
	client = &fakeClient{waitErr: errors.New("wait failed")}
	if _, err := RunUpdater(context.Background(), client, baseRequest); err == nil || !strings.Contains(err.Error(), "wait for HarborBuddy container") {
		t.Fatalf("wait failure = %v", err)
	}
	client = &fakeClient{inspectErr: errors.New("inspect failed")}
	if _, err := RunUpdater(context.Background(), client, baseRequest); err == nil || !strings.Contains(err.Error(), "inspect HarborBuddy container") {
		t.Fatalf("inspect failure = %v", err)
	}
	client = &fakeClient{details: docker.ContainerDetails{Host: nil}}
	if _, err := RunUpdater(context.Background(), client, baseRequest); err == nil || !strings.Contains(err.Error(), "host configuration is missing") {
		t.Fatalf("missing host configuration = %v", err)
	}
	client = &fakeClient{details: docker.ContainerDetails{Host: &containertypes.HostConfig{}}, replaceErr: errors.New("replace failed")}
	if _, err := RunUpdater(context.Background(), client, baseRequest); err == nil || !strings.Contains(err.Error(), "transactionally replace HarborBuddy container") {
		t.Fatalf("replace failure = %v", err)
	}

	client = &fakeClient{details: docker.ContainerDetails{Host: &containertypes.HostConfig{}, State: nil}, backupErr: errors.New("backup cleanup warning")}
	if _, err := RunUpdater(context.Background(), client, baseRequest); err != nil {
		t.Fatalf("RunUpdater() with nil state and cleanup warning = %v", err)
	}
	if !client.replaceOpts.CurrentAlreadyStopped {
		t.Fatal("successful wait must use CurrentAlreadyStopped transaction mode")
	}
}

func TestIdentityHelpersRejectAmbiguityAndNormalizeIDs(t *testing.T) {
	containers := []docker.ContainerSummary{{ID: "abcdef1234560000"}, {ID: "abcdef1234561111"}}
	if got := uniqueNameMatch(containers, "missing"); got != "" {
		t.Fatalf("uniqueNameMatch(missing) = %q", got)
	}
	containers[0].Name = "same"
	containers[1].Name = "same"
	if got := uniqueNameMatch(containers, "same"); got != "" {
		t.Fatalf("ambiguous uniqueNameMatch() = %q", got)
	}
	if got := uniquePrefixMatch(containers, "abc"); got != "" {
		t.Fatalf("short uniquePrefixMatch() = %q", got)
	}
	if got := uniqueCgroupMatch(containers, nil); got != "" {
		t.Fatalf("empty uniqueCgroupMatch() = %q", got)
	}
	if got := normalizedContainerID(" SHA256:ABCDEF "); got != "sha256:abcdef" {
		t.Fatalf("normalizedContainerID() = %q", got)
	}
}
