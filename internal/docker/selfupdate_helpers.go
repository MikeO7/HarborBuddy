package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const selfUpdateRecoveryTimeout = 30 * time.Second

// SelfUpdateHelperRecord is a self-update helper container, running or kept
// after it finished.
type SelfUpdateHelperRecord struct {
	ID                string
	Name              string
	TargetContainerID string
	TargetImageID     string
	Finished          bool
	CreatedAt         time.Time
}

// ListSelfUpdateHelpers returns every helper container, including finished
// ones. A finished helper whose target container is still running is the
// durable record of a handoff that did not replace that target.
func (d *DockerClient) ListSelfUpdateHelpers(ctx context.Context) ([]SelfUpdateHelperRecord, error) {
	filters := make(client.Filters).Add("label", SelfUpdateHelperLabel+"=true")
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, fmt.Errorf("list self-update helpers: %w", err)
	}
	records := make([]SelfUpdateHelperRecord, 0, len(listed.Items))
	for _, current := range listed.Items {
		// The filter is applied again here so a daemon that ignores it cannot
		// make an ordinary container look like a helper.
		if current.Labels[SelfUpdateHelperLabel] != "true" {
			continue
		}
		records = append(records, SelfUpdateHelperRecord{
			ID:                current.ID,
			Name:              primaryName(current.Names),
			TargetContainerID: current.Labels[SelfUpdateTargetLabel],
			TargetImageID:     current.Labels[SelfUpdateImageLabel],
			Finished:          helperFinished(current.State),
			CreatedAt:         time.Unix(current.Created, 0),
		})
	}
	return records, nil
}

// helperFinished reports whether a helper has run and stopped. Podman briefly
// reports a helper that just exited as "stopped" before its cleanup, so any
// state other than an active or never-started one counts as finished.
func helperFinished(state containertypes.ContainerState) bool {
	switch state {
	case containertypes.StateRunning, containertypes.StateRestarting, containertypes.StatePaused, containertypes.StateCreated, "":
		return false
	case containertypes.StateExited, containertypes.StateDead, containertypes.StateRemoving:
		return true
	default:
		return true
	}
}

// RemoveSelfUpdateHelper removes a finished helper. It never forces removal,
// so a helper that is still running is left alone.
func (d *DockerClient) RemoveSelfUpdateHelper(ctx context.Context, id string) error {
	if _, err := d.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{}); err != nil {
		return fmt.Errorf("remove self-update helper %s: %w", id, err)
	}
	return nil
}

// RecoverSelfUpdateTarget restores the daemon's original restart policy and
// starts it again when it is not running. The helper calls it on every failure
// after the daemon handed off, because the daemon disabled its own restart
// policy before exiting. It is idempotent, bounded, and survives cancellation
// of the helper's context.
func (d *DockerClient) RecoverSelfUpdateTarget(parent context.Context, id string, policy containertypes.RestartPolicy) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), selfUpdateRecoveryTimeout)
	defer cancel()

	target := ContainerDetails{Summary: ContainerSummary{ID: id}, Host: &containertypes.HostConfig{RestartPolicy: policy}}
	var recoveryErrors []error
	recoveryErrors = appendError(recoveryErrors, d.restoreRestartPolicy(ctx, target))
	details, err := d.InspectContainer(ctx, id)
	if err != nil {
		return errors.Join(append(recoveryErrors, err)...)
	}
	if details.State == nil || !details.State.Running {
		recoveryErrors = appendError(recoveryErrors, d.restartOldContainer(ctx, id))
	}
	return errors.Join(recoveryErrors...)
}
