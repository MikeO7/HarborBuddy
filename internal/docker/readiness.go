package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
)

type readinessTracker struct {
	stableSince   time.Time
	lastPID       int
	lastStarted   string
	healthcheck   bool
	stabilization time.Duration
}

func (d *DockerClient) waitUntilReady(ctx context.Context, id string, healthcheck *containertypes.HealthConfig, options ReplaceOptions) error {
	readyCtx, cancel := context.WithTimeout(ctx, options.StartupTimeout)
	defer cancel()
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()

	tracker := readinessTracker{
		healthcheck:   healthcheckEnabled(healthcheck),
		stabilization: options.StabilizationTime,
	}
	for {
		ready, err := d.inspectReadiness(readyCtx, id, &tracker)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("startup timeout: %w", readyCtx.Err())
		case <-ticker.C:
		}
	}
}

func (d *DockerClient) inspectReadiness(ctx context.Context, id string, tracker *readinessTracker) (bool, error) {
	details, err := d.InspectContainer(ctx, id)
	if err != nil {
		return false, err
	}
	if err := validateRunningState(details.State); err != nil {
		return false, err
	}

	now := time.Now()
	tracker.observeProcess(details.State, now)
	if tracker.healthcheck {
		return healthReady(details.State)
	}
	return now.Sub(tracker.stableSince) >= tracker.stabilization, nil
}

func validateRunningState(state *containertypes.State) error {
	if state == nil {
		return errors.New("container state is unavailable")
	}
	if !state.Running {
		return fmt.Errorf("container exited with code %d: %s", state.ExitCode, state.Error)
	}
	if state.Restarting || state.Paused || state.Dead || state.OOMKilled {
		return fmt.Errorf(
			"container entered an unstable state: status=%s restarting=%t paused=%t dead=%t oom_killed=%t",
			state.Status,
			state.Restarting,
			state.Paused,
			state.Dead,
			state.OOMKilled,
		)
	}
	return nil
}

func (t *readinessTracker) observeProcess(state *containertypes.State, now time.Time) {
	if !t.stableSince.IsZero() && state.Pid == t.lastPID && state.StartedAt == t.lastStarted {
		return
	}
	t.stableSince = now
	t.lastPID = state.Pid
	t.lastStarted = state.StartedAt
}

func healthcheckEnabled(healthcheck *containertypes.HealthConfig) bool {
	return healthcheck != nil && len(healthcheck.Test) > 0 && healthcheck.Test[0] != "NONE"
}

func healthReady(state *containertypes.State) (bool, error) {
	if state.Health == nil {
		return false, nil
	}
	switch state.Health.Status {
	case containertypes.Healthy:
		return true, nil
	case containertypes.Unhealthy:
		return false, errors.New("container reported unhealthy")
	case containertypes.NoHealthcheck, containertypes.Starting:
		return false, nil
	default:
		return false, nil
	}
}
