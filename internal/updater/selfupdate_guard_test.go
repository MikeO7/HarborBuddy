package updater

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

// guardFakeClient adds the helper wait and helper history operations the real
// Docker client provides on top of the basic self-update fake.
type guardFakeClient struct {
	*selfUpdateFakeClient
	waitErr   error
	waited    []string
	records   []docker.SelfUpdateHelperRecord
	listErr   error
	removeErr error
	removed   []string
}

func (f *guardFakeClient) WaitContainerExit(_ context.Context, id string) error {
	f.waited = append(f.waited, id)
	return f.waitErr
}

func (f *guardFakeClient) ListSelfUpdateHelpers(context.Context) ([]docker.SelfUpdateHelperRecord, error) {
	return f.records, f.listErr
}

func (f *guardFakeClient) RemoveSelfUpdateHelper(_ context.Context, id string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, id)
	return nil
}

var guardSelf = docker.ContainerSummary{ID: "self", Name: "harborbuddy", ImageRef: "harborbuddy:latest", ImageID: "sha256:old"}

func newGuardClient(t *testing.T, extra ...docker.ContainerSummary) *guardFakeClient {
	t.Helper()
	originalDetector := detectCurrentContainer
	detectCurrentContainer = func([]docker.ContainerSummary) string { return guardSelf.ID }
	t.Cleanup(func() { detectCurrentContainer = originalDetector })
	return &guardFakeClient{selfUpdateFakeClient: &selfUpdateFakeClient{
		containers: append([]docker.ContainerSummary{guardSelf}, extra...),
		details:    map[string]docker.ContainerDetails{guardSelf.ID: runningDetails(guardSelf)},
		images:     map[string]docker.ImageInfo{"harborbuddy:latest": {ID: "sha256:new"}},
	}}
}

func runGuardCycle(t *testing.T, cfg config.Config, client docker.Client) (Report, string, error) {
	t.Helper()
	var logs bytes.Buffer
	report, err := RunUpdateCycle(context.Background(), cfg, client, zerolog.New(&logs))
	return report, logs.String(), err
}

func TestRunOnceNeverStartsSelfUpdate(t *testing.T) {
	client := newGuardClient(t)
	cfg := config.Default()
	cfg.RunOnce = true
	report, _, err := runGuardCycle(t, cfg, client)
	if err != nil || resultStatus(report, guardSelf.ID) != StatusExcluded || len(client.actions) != 0 {
		t.Fatalf("run-once self result = %+v err %v actions %v", report.Results, err, client.actions)
	}
	if reason := report.Results[0].Reason; !strings.Contains(reason, "run-once mode") {
		t.Fatalf("run-once reason = %q", reason)
	}
}

func TestPreviouslyFailedSelfUpdateImageIsNotRetried(t *testing.T) {
	client := newGuardClient(t)
	client.records = []docker.SelfUpdateHelperRecord{{ID: "failed-helper", TargetContainerID: guardSelf.ID, TargetImageID: "sha256:new", Finished: true}}
	report, logs, err := runGuardCycle(t, config.Default(), client)
	if err != nil || resultStatus(report, guardSelf.ID) != StatusSelfUpdateBlocked || len(client.actions) != 0 {
		t.Fatalf("blocked self result = %+v err %v actions %v", report.Results, err, client.actions)
	}
	if report.Results[0].HelperID != "failed-helper" || !strings.Contains(logs, `"event":"self_update_blocked"`) || !strings.Contains(logs, `"level":"warn"`) {
		t.Fatalf("blocked self-update was not reported clearly: %+v\n%s", report.Results[0], logs)
	}

	client = newGuardClient(t)
	client.records = []docker.SelfUpdateHelperRecord{{ID: "older-failure", TargetContainerID: guardSelf.ID, TargetImageID: "sha256:older", Finished: true}}
	if _, _, err := runGuardCycle(t, config.Default(), client); !isShutdown(err) || !slices.Contains(client.actions, "helper:self") {
		t.Fatalf("a different image must be tried: err %v actions %v", err, client.actions)
	}
}

func TestRolledBackHelperStillRunningIsAwaitedInsteadOfTreatedAsHandoff(t *testing.T) {
	helper := docker.ContainerSummary{ID: "rollback-helper", Labels: map[string]string{docker.SelfUpdateHelperLabel: "true", docker.SelfUpdateTargetLabel: guardSelf.ID}}
	client := newGuardClient(t, helper)
	// The helper exits after restarting the daemon, so the next listing shows it finished.
	client.records = []docker.SelfUpdateHelperRecord{{ID: helper.ID, TargetContainerID: guardSelf.ID, TargetImageID: "sha256:new", Finished: true}}
	report, logs, err := runGuardCycle(t, config.Default(), client)
	if err != nil || !slices.Equal(client.waited, []string{helper.ID}) || resultStatus(report, guardSelf.ID) != StatusSelfUpdateBlocked {
		t.Fatalf("rolled-back helper handling = err %v waited %v results %+v", err, client.waited, report.Results)
	}
	if !strings.Contains(logs, `"event":"self_update_helper_finished"`) {
		t.Fatalf("finished helper was not logged:\n%s", logs)
	}

	client = newGuardClient(t, helper)
	client.waitErr = context.DeadlineExceeded
	_, _, err = runGuardCycle(t, config.Default(), client)
	if signal, ok := selfupdate.AsShutdownRequired(err); !ok || signal.HelperContainerID != helper.ID || len(client.actions) != 0 {
		t.Fatalf("a helper that keeps running must still be treated as a handoff: err %v actions %v", err, client.actions)
	}
}

func TestFinishedHelpersArePrunedOutsideDryRun(t *testing.T) {
	old := time.Now().Add(-selfupdate.HelperRetention - time.Hour)
	records := []docker.SelfUpdateHelperRecord{
		{ID: "stale", TargetContainerID: "replaced-daemon", TargetImageID: "sha256:new", Finished: true, CreatedAt: old},
		{ID: "evidence", TargetContainerID: guardSelf.ID, TargetImageID: "sha256:older", Finished: true, CreatedAt: old},
	}
	client := newGuardClient(t)
	client.records = records
	_, logs, _ := runGuardCycle(t, config.Default(), client)
	if !slices.Equal(client.removed, []string{"stale"}) || !strings.Contains(logs, `"event":"self_update_helpers_pruned"`) {
		t.Fatalf("pruned helpers = %v\n%s", client.removed, logs)
	}

	dryRun := config.Default()
	dryRun.Updates.DryRun = true
	client = newGuardClient(t)
	client.records = records
	if _, _, err := runGuardCycle(t, dryRun, client); err != nil || len(client.removed) != 0 {
		t.Fatalf("dry-run removed helpers: %v", client.removed)
	}

	client = newGuardClient(t)
	client.records = records
	client.removeErr = errors.New("conflict")
	if _, logs, _ = runGuardCycle(t, config.Default(), client); !strings.Contains(logs, `"event":"self_update_helpers_prune_failed"`) {
		t.Fatalf("prune failure was not logged:\n%s", logs)
	}

	client = newGuardClient(t)
	client.listErr = errors.New("daemon unavailable")
	_, logs, err := runGuardCycle(t, config.Default(), client)
	if !isShutdown(err) || !strings.Contains(logs, `"event":"self_update_helpers_unavailable"`) {
		t.Fatalf("history failure must not block self-update: err %v\n%s", err, logs)
	}
}

func isShutdown(err error) bool {
	_, ok := selfupdate.AsShutdownRequired(err)
	return ok
}
