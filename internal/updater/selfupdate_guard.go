package updater

import (
	"context"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

var helperClock = time.Now

// settleActiveHelper handles a running helper that targets this daemon. A
// helper that rolled back restarts the daemon moments before it exits, so the
// daemon waits briefly for it; only a helper that keeps running is treated as a
// handoff in progress, which requires this process to shut down.
func settleActiveHelper(ctx context.Context, client docker.Client, results []ContainerResult, selfID string, logger zerolog.Logger) (string, error) {
	helperID := activeSelfUpdateHelper(results, selfID)
	if helperID == "" {
		return "", nil
	}
	if !selfupdate.AwaitHelperExit(ctx, client, helperID) {
		return "", &selfupdate.ShutdownRequiredError{TargetContainerID: selfID, HelperContainerID: helperID}
	}
	logger.Info().Str("event", "self_update_helper_finished").
		Str("helper_container_id", shortID(helperID)).
		Str("container_id", shortID(selfID)).
		Msg("A self-update helper for this container finished without replacing it; continuing")
	return helperID, nil
}

// markFinished records that a helper the daemon just watched exit has
// finished, even if the engine still lists it in a transitional state.
func markFinished(records []docker.SelfUpdateHelperRecord, helperID string) {
	for index := range records {
		if helperID != "" && records[index].ID == helperID {
			records[index].Finished = true
		}
	}
}

func activeSelfUpdateHelper(results []ContainerResult, selfID string) string {
	if selfID == "" {
		return ""
	}
	containers := make([]docker.ContainerSummary, len(results))
	for index := range results {
		containers[index] = results[index].Container
	}
	return docker.ActiveSelfUpdateHelper(containers, selfID)
}

// loadHelperHistory lists kept helpers and, outside dry-run, removes finished
// ones past the retention window. History is best-effort: when it cannot be
// read, self-update proceeds without the failed-handoff guard.
func loadHelperHistory(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) []docker.SelfUpdateHelperRecord {
	history, ok := client.(selfupdate.HelperHistory)
	if !ok {
		return nil
	}
	records, err := history.ListSelfUpdateHelpers(ctx)
	if err != nil {
		logger.Warn().Str("event", "self_update_helpers_unavailable").Err(err).Msg("Could not list self-update helpers")
		return nil
	}
	if cfg.Updates.DryRun {
		return records
	}
	removed, err := selfupdate.PruneHelpers(ctx, history, records, helperClock())
	if len(removed) > 0 {
		logger.Info().Str("event", "self_update_helpers_pruned").Int("removed", len(removed)).
			Msg("Removed finished self-update helpers older than the retention window")
	}
	if err != nil {
		logger.Warn().Str("event", "self_update_helpers_prune_failed").Err(err).Msg("Could not remove finished self-update helpers")
	}
	return records
}

// skipSelfCandidate stops a self-update that must not start: one-shot runs
// never hand off to a helper, and an image that a previous helper already
// failed to start is not retried for a day unless a different image is published.
func skipSelfCandidate(cfg config.Config, result *ContainerResult, history []docker.SelfUpdateHelperRecord, logger zerolog.Logger) bool {
	if result.Status != StatusWouldUpdate {
		return false
	}
	if cfg.RunOnce {
		result.Status = StatusExcluded
		result.Reason = "self-update never runs in run-once mode"
		return true
	}
	helperID := selfupdate.FailedHandoff(history, result.Container.ID, result.TargetImageID, helperClock())
	if helperID == "" {
		return false
	}
	result.Status = StatusSelfUpdateBlocked
	result.HelperID = helperID
	result.Reason = "a previous self-update to this image failed and was rolled back; retrying after 24 hours or when a different image is published"
	logger.Warn().Str("event", "self_update_blocked").
		Str("container_id", shortID(result.Container.ID)).
		Str("target_image_id", shortID(result.TargetImageID)).
		Str("helper_container_id", shortID(helperID)).
		Msg("Skipping self-update to an image that already failed; inspect the helper logs")
	return true
}
