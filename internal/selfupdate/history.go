package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

const (
	// HelperRetention is how long a finished helper is kept for its logs
	// before the daemon removes it.
	HelperRetention = 7 * 24 * time.Hour
	// FailedHandoffWindow is how long a failed handoff blocks another attempt
	// at the same image. After it, the daemon tries again, so a failure caused
	// by a passing engine problem does not block that image for good.
	FailedHandoffWindow = 24 * time.Hour
)

// helperSettleTimeout bounds how long a daemon waits for a running helper
// that targets it to finish before treating it as an active handoff.
var helperSettleTimeout = time.Minute

// HelperHistory is implemented by Docker clients that can list and remove
// kept self-update helper containers.
type HelperHistory interface {
	ListSelfUpdateHelpers(context.Context) ([]docker.SelfUpdateHelperRecord, error)
	RemoveSelfUpdateHelper(context.Context, string) error
}

// PruneHelpers removes finished helpers created before the retention window.
// The window outlasts FailedHandoffWindow, so removing any instance's helper
// never lifts a block that is still in force.
func PruneHelpers(ctx context.Context, client HelperHistory, records []docker.SelfUpdateHelperRecord, now time.Time) ([]string, error) {
	cutoff := now.Add(-HelperRetention)
	var removed []string
	var removeErrors []error
	for _, record := range records {
		if !record.Finished || !record.CreatedAt.Before(cutoff) {
			continue
		}
		if err := client.RemoveSelfUpdateHelper(ctx, record.ID); err != nil {
			removeErrors = append(removeErrors, err)
			continue
		}
		removed = append(removed, record.ID)
	}
	return removed, errors.Join(removeErrors...)
}

// FailedHandoff returns the finished helper that tried to move the running
// daemon selfID to imageID within FailedHandoffWindow. A helper that succeeded
// replaces and removes its target, so a finished helper whose target is still
// running always records a failure.
func FailedHandoff(records []docker.SelfUpdateHelperRecord, selfID, imageID string, now time.Time) string {
	if selfID == "" || imageID == "" {
		return ""
	}
	since := now.Add(-FailedHandoffWindow)
	for _, record := range records {
		if record.Finished && record.TargetContainerID == selfID && record.TargetImageID == imageID && record.CreatedAt.After(since) {
			return record.ID
		}
	}
	return ""
}

// AwaitHelperExit gives a running helper that targets the current daemon a
// bounded time to finish. A helper that rolls back restarts the daemon before
// it exits, so the restarted daemon can briefly see it still running. It
// reports true once the helper has exited, and false when the helper is still
// running after the settle timeout, which is a genuine handoff in progress, or
// cannot be awaited. Any other failure to wait, such as shutdown or a Docker
// API error, is returned: it says nothing about whether a handoff is active.
func AwaitHelperExit(ctx context.Context, client docker.Client, helperID string) (bool, error) {
	waiter, ok := client.(ContainerExitWaiter)
	if !ok {
		return false, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, helperSettleTimeout)
	defer cancel()
	err := waiter.WaitContainerExit(waitCtx, helperID)
	switch {
	case err == nil:
		return true, nil
	case ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		return false, nil
	default:
		return false, fmt.Errorf("wait for self-update helper %s: %w", helperID, err)
	}
}
