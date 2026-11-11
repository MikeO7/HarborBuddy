package cleanup

import (
	"context"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

const unsafeBuildCachePruneReason = "build-cache prune skipped: Docker's prune would also remove a record that cleanup keeps"

// pruneBuildCache removes eligible build-cache records with one Docker prune.
// The prune API cannot target individual records: its "until" filter removes
// every unused record whose last use is older than the cut-off, and a record
// with no last-used time matches it regardless of its creation time. When the
// prune would also match a record cleanup decided to keep, such as one whose
// age is unknown, the prune is skipped for the cycle.
func pruneBuildCache(
	ctx context.Context,
	cfg config.Config,
	cleaner resourceCleaner,
	logger zerolog.Logger,
	now time.Time,
	resources []docker.CleanupResource,
	report Report,
) (Report, error) {
	start := len(report.Results)
	report = removeResources(ctx, cfg, cleaner, logger, now, docker.CleanupBuildCache, resources, report)
	results := report.Results[start:]
	if !anyEligible(results) {
		logResults(logger, results)
		return report, nil
	}
	cutoff := now.Add(-minAgeDuration(cfg.Cleanup.MinAgeHours))
	if !buildCachePruneIsPrecise(results, cutoff) {
		for index := range results {
			if results[index].Eligible {
				results[index].WouldRemove = false
				results[index].Reason = unsafeBuildCachePruneReason
			}
		}
		logResults(logger, results)
		return report, nil
	}
	if cfg.Updates.DryRun {
		logResults(logger, results)
		return report, nil
	}
	pruned, err := cleaner.PruneBuildCache(ctx, cutoff)
	if err != nil {
		for index := range results {
			if results[index].Eligible {
				results[index].Err = err
				results[index].Reason = ""
			}
		}
		logResults(logger, results)
		return report, err
	}
	deleted := make(map[string]struct{}, len(pruned.Deleted))
	for _, id := range pruned.Deleted {
		deleted[id] = struct{}{}
	}
	for index := range results {
		if _, ok := deleted[results[index].Resource.ID]; ok {
			results[index].Removed = true
			results[index].Reason = ""
		} else if results[index].Eligible {
			results[index].Reason = "Docker prune retained the eligible cache record"
		}
	}
	logResults(logger, results)
	report.ReclaimedBytes += pruned.ReclaimedBytes
	return report, nil
}

// buildCachePruneIsPrecise reports whether a prune with cutoff would remove
// only eligible records. Docker never prunes a record that is in use.
func buildCachePruneIsPrecise(results []Result, cutoff time.Time) bool {
	for _, result := range results {
		lastUsed := result.Resource.LastUsedAt
		if !result.Eligible && !result.Resource.InUse && (lastUsed.IsZero() || lastUsed.Before(cutoff)) {
			return false
		}
	}
	return true
}

func anyEligible(results []Result) bool {
	for _, result := range results {
		if result.Eligible {
			return true
		}
	}
	return false
}

func logResults(logger zerolog.Logger, results []Result) {
	for _, result := range results {
		logResult(logger, result)
	}
}
