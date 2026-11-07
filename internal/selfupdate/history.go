package selfupdate

import (
	"context"
	"errors"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

const (
	// HelperRetention is how long a finished helper is kept for its logs
	// before the daemon removes it.
	HelperRetention = 24 * time.Hour
	// helperSettleTimeout bounds how long a daemon waits for a running helper
	// that targets it to finish before treating it as an active handoff.
	helperSettleTimeout = time.Minute
)

// HelperHistory is implemented by Docker clients that can list and remove
// kept self-update helper containers.
type HelperHistory interface {
	ListSelfUpdateHelpers(context.Context) ([]docker.SelfUpdateHelperRecord, error)
	RemoveSelfUpdateHelper(context.Context, string) error
}

// PruneHelpers removes finished helpers created before the retention window.
// Helpers that targeted selfID are kept regardless of age: a finished helper
// whose target is still running records a failed handoff, and keeping it stops
// the daemon from retrying the same image for as long as it runs.
func PruneHelpers(ctx context.Context, client HelperHistory, records []docker.SelfUpdateHelperRecord, selfID string, now time.Time) ([]string, error) {
	cutoff := now.Add(-HelperRetention)
	var removed []string
	var removeErrors []error
	for _, record := range records {
		if !record.Finished || !record.CreatedAt.Before(cutoff) || (selfID != "" && record.TargetContainerID == selfID) {
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

// FailedHandoff returns the finished helper that already tried to move the
// running daemon selfID to imageID. A helper that succeeded replaces and
// removes its target, so a finished helper whose target is still running
// always records a failure.
func FailedHandoff(records []docker.SelfUpdateHelperRecord, selfID, imageID string) string {
	if selfID == "" || imageID == "" {
		return ""
	}
	for _, record := range records {
		if record.Finished && record.TargetContainerID == selfID && record.TargetImageID == imageID {
			return record.ID
		}
	}
	return ""
}

// AwaitHelperExit gives a running helper that targets the current daemon a
// bounded time to finish. A helper that rolls back restarts the daemon before
// it exits, so the restarted daemon can briefly see it still running. It
// reports true once the helper has exited, and false when the helper is still
// running, which is a genuine handoff in progress, or cannot be awaited.
func AwaitHelperExit(ctx context.Context, client docker.Client, helperID string) bool {
	waiter, ok := client.(ContainerExitWaiter)
	if !ok {
		return false
	}
	waitCtx, cancel := context.WithTimeout(ctx, helperSettleTimeout)
	defer cancel()
	return waiter.WaitContainerExit(waitCtx, helperID) == nil
}
