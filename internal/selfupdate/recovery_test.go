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

type recoveringClient struct {
	*fakeClient
	recoveredID     string
	recoveredPolicy containertypes.RestartPolicy
	recoverCalls    int
	recoverErr      error
}

func (r *recoveringClient) RecoverSelfUpdateTarget(_ context.Context, id string, policy containertypes.RestartPolicy) error {
	r.recoverCalls++
	r.recoveredID = id
	r.recoveredPolicy = policy
	return r.recoverErr
}

func TestRunUpdaterRestoresDaemonAfterEveryHelperFailure(t *testing.T) {
	policy := containertypes.RestartPolicy{Name: "unless-stopped"}
	request := UpdaterRequest{TargetContainerID: "self", TargetImageID: "sha256:new", RestartPolicy: policy}
	for _, test := range []struct {
		name   string
		client *fakeClient
		want   string
	}{
		{name: "daemon never exits", client: &fakeClient{waitErr: context.DeadlineExceeded}, want: "wait for HarborBuddy container to exit"},
		{name: "re-inspect fails", client: &fakeClient{inspectErr: errors.New("inspect failed")}, want: "inspect HarborBuddy container after exit"},
		{name: "host config missing", client: &fakeClient{}, want: "host configuration is missing"},
		{name: "replacement rejected before rollback", client: &fakeClient{details: docker.ContainerDetails{Host: &containertypes.HostConfig{}}, replaceErr: errors.New("validate_replacement")}, want: "transactionally replace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &recoveringClient{fakeClient: test.client}
			_, err := RunUpdater(context.Background(), client, request)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "original HarborBuddy container was restored") {
				t.Fatalf("RunUpdater() error = %v, want %q with recovery", err, test.want)
			}
			if client.recoverCalls != 1 || client.recoveredID != "self" || client.recoveredPolicy != policy {
				t.Fatalf("recovery = %d calls id %q policy %+v, want original policy %+v", client.recoverCalls, client.recoveredID, client.recoveredPolicy, policy)
			}
			var handoff *HandoffError
			if !errors.As(err, &handoff) || !errors.Is(err, handoff.Err) {
				t.Fatalf("RunUpdater() error %v does not unwrap to its cause", err)
			}
			if got := RecoveryOutcome(err); got != "succeeded" {
				t.Fatalf("RecoveryOutcome() = %q", got)
			}
		})
	}
}

func TestRunUpdaterReportsRecoveryFailureAndSkipsRecoveryOnSuccess(t *testing.T) {
	request := UpdaterRequest{TargetContainerID: "self", TargetImageID: "sha256:new"}
	cause := errors.New("wait failed")
	recoveryErr := errors.New("start refused")
	client := &recoveringClient{fakeClient: &fakeClient{waitErr: cause}, recoverErr: recoveryErr}
	_, err := RunUpdater(context.Background(), client, request)
	if err == nil || !errors.Is(err, cause) || !errors.Is(err, recoveryErr) || !strings.Contains(err.Error(), "recovering the original HarborBuddy container failed") {
		t.Fatalf("RunUpdater() error = %v", err)
	}
	if got := RecoveryOutcome(err); got != "failed" {
		t.Fatalf("RecoveryOutcome() = %q, want failed", got)
	}

	_, err = RunUpdater(context.Background(), &fakeClient{waitErr: cause}, request)
	if err == nil || !strings.Contains(err.Error(), "cannot restore the original HarborBuddy container") || RecoveryOutcome(err) != "failed" {
		t.Fatalf("RunUpdater() without recoverer error = %v", err)
	}

	success := &recoveringClient{fakeClient: &fakeClient{details: docker.ContainerDetails{Host: &containertypes.HostConfig{}}}}
	if _, err := RunUpdater(context.Background(), success, request); err != nil || success.recoverCalls != 0 {
		t.Fatalf("successful RunUpdater() = %v with %d recoveries", err, success.recoverCalls)
	}
	if got := RecoveryOutcome(errors.New("ordinary")); got != "not_attempted" {
		t.Fatalf("RecoveryOutcome(ordinary) = %q", got)
	}
}

func TestPruneHelpersRemovesOnlyFinishedHelpersPastRetention(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	old := now.Add(-HelperRetention - time.Minute)
	records := []docker.SelfUpdateHelperRecord{
		{ID: "running", TargetContainerID: "gone", CreatedAt: old},
		{ID: "recent", TargetContainerID: "gone", Finished: true, CreatedAt: now.Add(-time.Hour)},
		{ID: "recent-failure", TargetContainerID: "self", Finished: true, CreatedAt: now.Add(-FailedHandoffWindow - time.Hour)},
		{ID: "stale", TargetContainerID: "gone", Finished: true, CreatedAt: old},
		{ID: "stuck", TargetContainerID: "gone", Finished: true, CreatedAt: old},
	}
	history := &fakeHistory{removeErr: map[string]error{"stuck": errors.New("conflict")}}
	removed, err := PruneHelpers(context.Background(), history, records, now)
	if len(removed) != 1 || removed[0] != "stale" || err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("PruneHelpers() = %v, %v", removed, err)
	}
	if HelperRetention <= FailedHandoffWindow {
		t.Fatal("pruning must never remove a record that still blocks a retry")
	}
}

func TestFailedHandoffMatchesOnlyRecentFinishedHelpersForThisDaemonAndImage(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	records := []docker.SelfUpdateHelperRecord{
		{ID: "running", TargetContainerID: "self", TargetImageID: "sha256:new", CreatedAt: recent},
		{ID: "other-image", TargetContainerID: "self", TargetImageID: "sha256:older", Finished: true, CreatedAt: recent},
		{ID: "other-daemon", TargetContainerID: "other", TargetImageID: "sha256:new", Finished: true, CreatedAt: recent},
		{ID: "expired", TargetContainerID: "self", TargetImageID: "sha256:retry", Finished: true, CreatedAt: now.Add(-FailedHandoffWindow - time.Minute)},
		{ID: "failed", TargetContainerID: "self", TargetImageID: "sha256:new", Finished: true, CreatedAt: recent},
	}
	if got := FailedHandoff(records, "self", "sha256:new", now); got != "failed" {
		t.Fatalf("FailedHandoff() = %q, want failed", got)
	}
	for _, test := range [][2]string{{"", "sha256:new"}, {"self", ""}, {"self", "sha256:newest"}, {"self", "sha256:retry"}} {
		if got := FailedHandoff(records, test[0], test[1], now); got != "" {
			t.Fatalf("FailedHandoff(%q, %q) = %q", test[0], test[1], got)
		}
	}
}

func TestAwaitHelperExit(t *testing.T) {
	if !AwaitHelperExit(context.Background(), &fakeClient{}, "helper") {
		t.Fatal("an exited helper must be reported as finished")
	}
	if AwaitHelperExit(context.Background(), &fakeClient{waitErr: context.DeadlineExceeded}, "helper") {
		t.Fatal("a helper that keeps running must be treated as an active handoff")
	}
	if AwaitHelperExit(context.Background(), &clientWithoutWait{}, "helper") {
		t.Fatal("a client that cannot wait must keep the conservative handoff behavior")
	}
}

type fakeHistory struct {
	removeErr map[string]error
}

func (f *fakeHistory) ListSelfUpdateHelpers(context.Context) ([]docker.SelfUpdateHelperRecord, error) {
	return nil, nil
}

func (f *fakeHistory) RemoveSelfUpdateHelper(_ context.Context, id string) error {
	return f.removeErr[id]
}
