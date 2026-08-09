package updater

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/rs/zerolog"
)

func TestCandidateProcessingClassifiesInspectionReplacementAndSelfBoundaries(t *testing.T) {
	container := docker.ContainerSummary{ID: "app", Name: "app", ImageRef: "app:latest", ImageID: "sha256:old"}
	result := ContainerResult{Container: container, Status: StatusCurrent, TargetImageID: "sha256:new"}
	client := &cycleFakeClient{}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusCurrent {
		t.Fatalf("non-candidate processing = %q, %v", result.Status, err)
	}

	result.Status = StatusWouldUpdate
	client.inspectErrors = map[string]error{"app": errors.New("inspect failed")}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusFailed || result.Err == nil {
		t.Fatalf("inspect failure classification = %+v, %v", result, err)
	}

	client.inspectErrors = nil
	client.details = map[string]docker.ContainerDetails{"app": {Summary: container, State: &containertypes.State{Running: false}}}
	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusChangedExternally {
		t.Fatalf("external change classification = %+v, %v", result, err)
	}

	client.details["app"] = cycleRunningDetails(container)
	client.checkErrors = map[string]error{"app": &docker.UnsupportedError{Reason: "unsafe mount"}}
	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusUnsupported {
		t.Fatalf("unsupported replacement classification = %+v, %v", result, err)
	}

	client.checkErrors = map[string]error{"app": errors.New("check failed")}
	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusFailed || result.Err == nil {
		t.Fatalf("replacement check failure = %+v, %v", result, err)
	}

	client.checkErrors = nil
	client.replaceErrors = map[string]error{"app": errors.New("replace failed")}
	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusFailed {
		t.Fatalf("replacement failure = %+v, %v", result, err)
	}

	client.replaceErrors = nil
	client.replaceResults = map[string]docker.ReplaceResult{"app": {BackupCleanupErr: errors.New("backup warning")}}
	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusUpdated || result.Warning == nil {
		t.Fatalf("replacement success warning = %+v, %v", result, err)
	}

	result = ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "sha256:new"}
	cfg := config.Default()
	cfg.Updates.DryRun = true
	if err := processCandidate(context.Background(), cfg, client, &result, false); err != nil || result.Status != StatusWouldUpdate {
		t.Fatalf("dry-run candidate = %+v, %v", result, err)
	}
}

func TestSelfCandidateWithoutHelperIsReportedAsFailure(t *testing.T) {
	container := docker.ContainerSummary{ID: "self", Name: "self", ImageRef: "harborbuddy:latest", ImageID: "old"}
	result := ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: "new"}
	client := &cycleFakeClient{details: map[string]docker.ContainerDetails{"self": cycleRunningDetails(container)}}
	if err := processCandidate(context.Background(), config.Default(), client, &result, true); err != nil || result.Status != StatusFailed || result.Err == nil {
		t.Fatalf("self candidate without helper = %+v, %v", result, err)
	}
}

func TestDiscoveryClassifiesCurrentPullFailureCancellationAndOrdering(t *testing.T) {
	containers := []docker.ContainerSummary{
		{ID: "b", Name: "same"},
		{ID: "a", Name: "same"},
		{ID: "z", Name: "z"},
	}
	sortContainers(containers)
	if containers[0].ID != "a" || containers[1].ID != "b" {
		t.Fatalf("sortContainers() = %+v", containers)
	}
	results := newResults(containers)
	client := &cycleFakeClient{images: map[string]docker.ImageInfo{"same:latest": {ID: "new"}}, pullErrors: map[string]error{"same:latest": errors.New("pull failed")}}
	results[0].Container.ImageRef = "same:latest"
	results[1].Container.ImageRef = "same:latest"
	discoverCandidates(context.Background(), config.UpdatesConfig{AllowImages: []string{"*"}}, client, results[:2], "")
	if results[0].Status != StatusFailed || results[0].Err == nil || results[1].Status != StatusFailed {
		t.Fatalf("pull failures = %+v", results[:2])
	}

	client = &cycleFakeClient{images: map[string]docker.ImageInfo{"current:latest": {ID: "same"}}}
	current := ContainerResult{Container: docker.ContainerSummary{ID: "current", ImageRef: "current:latest", ImageID: "same"}}
	cache := NewSafePullCache()
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	discoverCandidate(context.Background(), client, cache, make(chan struct{}, 1), &waitGroup, &current)
	if current.Status != StatusCurrent || current.TargetImageID != "same" {
		t.Fatalf("current candidate = %+v", current)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	semaphore := make(chan struct{}, 1)
	semaphore <- struct{}{}
	if _, err := pullImage(ctx, client, semaphore, "current:latest"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pullImage(canceled) error = %v", err)
	}
	result := ContainerResult{Container: docker.ContainerSummary{ImageRef: "app:latest"}}
	setPullError(ctx, &result, context.Canceled)
	if result.Status != StatusCancelled || !strings.Contains(result.Err.Error(), "pull app:latest") {
		t.Fatalf("canceled pull result = %+v", result)
	}
	setPullError(context.Background(), &result, errors.New("failed"))
	if result.Status != StatusFailed {
		t.Fatalf("failed pull result status = %q", result.Status)
	}
}

func TestRunUpdateCycleReportsListAndContextErrors(t *testing.T) {
	client := &cycleFakeClient{listErr: errors.New("list failed")}
	if _, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "list containers") {
		t.Fatalf("list error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client = &cycleFakeClient{}
	if _, err := RunUpdateCycle(ctx, config.Default(), client, zerolog.Nop()); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestReportLoggingAndShortIDCoverOptionalFields(t *testing.T) {
	var output bytes.Buffer
	logger := zerolog.New(&output)
	result := ContainerResult{
		Container:     docker.ContainerSummary{ID: "sha256:1234567890123456", Name: "app", ImageRef: "app:latest"},
		Status:        StatusFailed,
		TargetImageID: "sha256:abcdef0123456789",
		Err:           errors.New("failed"),
		Warning:       errors.New("warning"),
	}
	logReport(logger, Report{Results: []ContainerResult{result}, Duration: 2}, false)
	if !strings.Contains(output.String(), "warning") || !strings.Contains(output.String(), "failed") || !strings.Contains(output.String(), "123456789012") {
		t.Fatalf("report log = %q", output.String())
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID(short) = %q", got)
	}
}

func TestDetermineEligibilityAllowsUnrestrictedEmptyAllowList(t *testing.T) {
	decision := DetermineEligibility(docker.ContainerSummary{ImageRef: "anything:latest"}, config.UpdatesConfig{})
	if !decision.Eligible || decision.Reason != "eligible for updates" {
		t.Fatalf("empty allow list decision = %+v", decision)
	}
	if matchesPattern("image", "unrelated") {
		t.Fatal("unrelated pattern matched")
	}
}
