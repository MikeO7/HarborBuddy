package updater

import (
	"slices"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

// Podman can still list a helper as running or stopped right after the daemon
// watched it exit. The awaited helper must still block a retry of its image.
func TestAwaitedHelperBlocksRetryEvenWhenListedAsActive(t *testing.T) {
	helper := docker.ContainerSummary{ID: "rollback-helper", Labels: map[string]string{docker.SelfUpdateHelperLabel: "true", docker.SelfUpdateTargetLabel: guardSelf.ID}}
	client := newGuardClient(t, helper)
	client.records = []docker.SelfUpdateHelperRecord{{ID: helper.ID, TargetContainerID: guardSelf.ID, TargetImageID: "sha256:new", CreatedAt: time.Now()}}
	report, _, err := runGuardCycle(t, config.Default(), client)
	if err != nil || !slices.Equal(client.waited, []string{helper.ID}) || resultStatus(report, guardSelf.ID) != StatusSelfUpdateBlocked || len(client.actions) != 0 {
		t.Fatalf("awaited helper = err %v waited %v actions %v results %+v", err, client.waited, client.actions, report.Results)
	}
}

func TestMarkFinishedIgnoresEmptyHelper(t *testing.T) {
	records := []docker.SelfUpdateHelperRecord{{ID: "a"}}
	markFinished(records, "")
	if records[0].Finished {
		t.Fatal("an empty helper ID must not mark any record finished")
	}
}
