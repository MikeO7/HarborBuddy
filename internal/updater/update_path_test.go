package updater

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type updatePathFakeClient struct {
	cycleFakeClient
	dependentsErr error
	recoveries    []docker.BackupRecovery
	recoveryErr   error
	recoveryDry   []bool
}

func (f *updatePathFakeClient) CheckNamespaceDependents(context.Context, docker.ContainerDetails) error {
	return f.dependentsErr
}

func (f *updatePathFakeClient) RecoverStrandedBackups(_ context.Context, dryRun bool) ([]docker.BackupRecovery, error) {
	f.recoveryDry = append(f.recoveryDry, dryRun)
	return f.recoveries, f.recoveryErr
}

func resetFailedTargets(t *testing.T) {
	t.Helper()
	failedTargets = newFailedTargetRecord()
	t.Cleanup(func() { failedTargets = newFailedTargetRecord() })
}

func updatePathCandidate(target string) (docker.ContainerSummary, ContainerResult) {
	container := docker.ContainerSummary{ID: "app", Name: "app", ImageRef: "app:latest", ImageID: "sha256:old"}
	return container, ContainerResult{Container: container, Status: StatusWouldUpdate, TargetImageID: target}
}

func TestProcessCandidateStopsStartingTransactionsAfterShutdown(t *testing.T) {
	container, result := updatePathCandidate("sha256:new")
	client := &updatePathFakeClient{cycleFakeClient: cycleFakeClient{details: map[string]docker.ContainerDetails{"app": cycleRunningDetails(container)}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processCandidate(ctx, config.Default(), client, &result, false); err != nil || result.Status != StatusCancelled || len(client.replacements) != 0 {
		t.Fatalf("cancelled candidate = %+v, %v; replacements=%v", result, err, client.replacements)
	}

	_, result = updatePathCandidate("sha256:new")
	client.replaceResults = map[string]docker.ReplaceResult{"app": {FailureStage: docker.FailureStageCancelledBeforeStart}}
	client.replaceErrors = map[string]error{"app": context.Canceled}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusCancelled || result.Err != nil {
		t.Fatalf("replacement cancelled before start = %+v, %v", result, err)
	}
}

func TestFailedTargetIsSkippedUntilTheImageChanges(t *testing.T) {
	resetFailedTargets(t)
	container, result := updatePathCandidate("sha256:bad")
	client := &updatePathFakeClient{cycleFakeClient: cycleFakeClient{
		details:        map[string]docker.ContainerDetails{"app": cycleRunningDetails(container)},
		replaceResults: map[string]docker.ReplaceResult{"app": {FailureStage: "start_replacement", RollbackAttempted: true}},
		replaceErrors:  map[string]error{"app": errors.New("replacement did not become ready")},
	}}
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusFailed {
		t.Fatalf("first failure = %+v, %v", result, err)
	}

	_, result = updatePathCandidate("sha256:bad")
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusPreviouslyFailed || len(client.replacements) != 1 {
		t.Fatalf("retry of failed target = %+v, %v; replacements=%v", result, err, client.replacements)
	}
	var output bytes.Buffer
	logReport(zerolog.New(&output), Report{Results: []ContainerResult{result}}, false)
	if text := output.String(); !strings.Contains(text, `"level":"warn"`) || !strings.Contains(text, `"previously_failed":1`) || !strings.Contains(text, "previously failed and rolled back") {
		t.Fatalf("previously failed log = %s", text)
	}

	_, selfResult := updatePathCandidate("sha256:bad")
	_ = processCandidate(context.Background(), config.Default(), client, &selfResult, true)
	if selfResult.Status == StatusPreviouslyFailed {
		t.Fatal("self-update flow must not consult the ordinary failed-target record")
	}

	client.replaceErrors, client.replaceResults = nil, nil
	_, result = updatePathCandidate("sha256:fixed")
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusUpdated {
		t.Fatalf("new target = %+v, %v", result, err)
	}
	if failedTargets.failed("app", "sha256:bad") || failedTargets.failed("app", "") {
		t.Fatal("successful update did not clear the failed-target record")
	}
	failedTargets.observe("app", "sha256:other", docker.ReplaceResult{FailureStage: "stop_old_container"}, errors.New("stop failed"))
	if failedTargets.failed("app", "sha256:other") {
		t.Fatal("transient pre-start failure was recorded as a bad target")
	}
}

func TestCandidateChecksNamespaceDependentsAndSurfacesConfigWarning(t *testing.T) {
	container, result := updatePathCandidate("sha256:new")
	client := &updatePathFakeClient{
		cycleFakeClient: cycleFakeClient{details: map[string]docker.ContainerDetails{"app": cycleRunningDetails(container)}},
		dependentsErr:   &docker.UnsupportedError{Reason: "container vpn-client depends on this container's network namespace"},
	}
	cfg := config.Default()
	cfg.Updates.DryRun = true
	if err := processCandidate(context.Background(), cfg, client, &result, false); err != nil || result.Status != StatusUnsupported || !strings.Contains(result.Reason, "vpn-client") {
		t.Fatalf("namespace provider (dry-run) = %+v, %v", result, err)
	}

	client.dependentsErr = errors.New("list failed")
	_, result = updatePathCandidate("sha256:new")
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusFailed || len(client.replacements) != 0 {
		t.Fatalf("dependents check failure = %+v, %v", result, err)
	}

	client.dependentsErr = nil
	client.replaceResults = map[string]docker.ReplaceResult{"app": {ConfigWarning: errors.New("inspect previous image")}}
	_, result = updatePathCandidate("sha256:new")
	if err := processCandidate(context.Background(), config.Default(), client, &result, false); err != nil || result.Status != StatusUpdated || result.Warning == nil {
		t.Fatalf("config warning = %+v, %v", result, err)
	}
}

func TestDiscoverySkipsContainersCreatedFromImageIDs(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	results := newResults([]docker.ContainerSummary{{ID: "a", Name: "a", ImageRef: id}, {ID: "b", Name: "b", ImageRef: strings.Repeat("b", 64)}})
	client := &cycleFakeClient{}
	discoverCandidates(context.Background(), config.UpdatesConfig{}, client, results, "")
	for _, result := range results {
		if result.Status != StatusUnsupported || !strings.Contains(result.Reason, "image ID") {
			t.Fatalf("image-ID container = %+v", result)
		}
	}
	if len(client.pulls) != 0 {
		t.Fatalf("image IDs were pulled: %v", client.pulls)
	}
	if got := resultMessage(results[0], false); !strings.Contains(got, "cannot be updated safely") {
		t.Fatalf("resultMessage() = %q", got)
	}
}

func TestRunUpdateCycleRecoversStrandedBackupsAndLogsOutcomes(t *testing.T) {
	withoutDetectedSelf(t)
	client := &updatePathFakeClient{recoveries: []docker.BackupRecovery{
		{ContainerID: "web-backup", BackupName: "web-harborbuddy-backup-1", OriginalName: "web", Restored: true, Warning: errors.New("verify networks")},
		{ContainerID: "db-backup", BackupName: "db-harborbuddy-backup-2", OriginalName: "db", Reason: "original name db is in use"},
		{ContainerID: "busy-backup", BackupName: "busy-harborbuddy-backup-3", OriginalName: "busy", Deferred: true, Reason: "a self-update helper is running"},
		{ContainerID: "bad-backup", BackupName: "bad-harborbuddy-backup-4", OriginalName: "bad", Restored: true, Err: errors.New("start failed")},
	}}
	var output bytes.Buffer
	cfg := config.Default()
	cfg.Updates.DryRun = true
	if _, err := RunUpdateCycle(context.Background(), cfg, client, zerolog.New(&output).Level(zerolog.InfoLevel)); err != nil {
		t.Fatalf("RunUpdateCycle() error = %v", err)
	}
	text := output.String()
	for _, want := range []string{
		`"level":"warn","event":"backup_recovery","container_id":"web-backup","backup_name":"web-harborbuddy-backup-1","container_name":"web","restored":true,"warning":"verify networks"`,
		`"reason":"original name db is in use"`,
		`"level":"error"`, "could not be fully restored",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("recovery log missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "busy-backup") || len(client.recoveryDry) != 1 || !client.recoveryDry[0] {
		t.Fatalf("deferred recovery logged above debug or dry-run not passed: dry=%v\n%s", client.recoveryDry, text)
	}

	output.Reset()
	client = &updatePathFakeClient{recoveryErr: errors.New("list failed")}
	if _, err := RunUpdateCycle(context.Background(), config.Default(), client, zerolog.New(&output)); err != nil {
		t.Fatalf("RunUpdateCycle() with recovery failure error = %v", err)
	}
	if !strings.Contains(output.String(), "backup_recovery_failed") {
		t.Fatalf("recovery failure not logged: %s", output.String())
	}
}
