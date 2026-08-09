package cleanup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	operational "github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/rs/zerolog"
)

type resultCounts struct {
	Total       int
	Removed     int
	WouldRemove int
	Failed      int
	Skipped     int
}

func logResult(logger zerolog.Logger, result Result) {
	name, truncated := operational.BoundedField(result.Resource.Name)
	event := cleanupResultEvent(logger, result).
		Str("event", "cleanup_resource_result").
		Str("resource", string(result.Resource.Kind)).
		Str("resource_id", displayResourceID(result.Resource)).
		Str("resource_name", name).
		Bool("resource_name_truncated", truncated).
		Int64("resource_size_bytes", result.Resource.Size).
		Int64("age_hours", result.AgeHours).
		Bool("dangling", result.Resource.Dangling).
		Bool("in_use", result.Resource.InUse).
		Bool("protected", result.Resource.Protected)
	if !result.ReferenceAt.IsZero() {
		event = event.Time("reference_time", result.ReferenceAt)
	}
	switch {
	case result.Err != nil:
		event.Err(result.Err).Str("result", "failed").Msg("Cleanup resource failed")
	case result.Removed:
		event.Str("result", "removed").Msg("Cleanup resource removed")
	case result.WouldRemove:
		event.Str("result", "would_remove").Msg("Cleanup resource would be removed")
	default:
		event.Str("result", "skipped").Str("reason", result.Reason).Msg("Cleanup resource skipped")
	}
}

func cleanupResultEvent(logger zerolog.Logger, result Result) *zerolog.Event {
	switch {
	case result.Err != nil:
		return logger.Error()
	case result.Removed || result.WouldRemove:
		return logger.Info()
	default:
		return logger.Debug()
	}
}

func logReport(logger zerolog.Logger, report Report, dryRun bool, cleanupErr error) {
	kinds := reportKinds(report)
	for _, kind := range kinds {
		counts := countResults(report.Results, kind)
		logger.Info().Str("event", "cleanup_resource_summary").Str("resource", string(kind)).
			Int("total", counts.Total).Int("removed", counts.Removed).Int("would_remove", counts.WouldRemove).
			Int("failed", counts.Failed).Int("skipped", counts.Skipped).Bool("dry_run", dryRun).
			Msg("Cleanup resource summary")
	}
	totals := countResults(report.Results, "")
	event := logger.Info()
	outcome := "success"
	if cleanupErr != nil {
		event = logger.Error().Err(cleanupErr)
		outcome = "failed"
	} else if totals.Failed > 0 {
		event = logger.Error()
		outcome = "partial_failure"
	}
	event.Str("event", "cleanup_complete").Str("outcome", outcome).
		Int("resources", totals.Total).Int("removed", totals.Removed).Int("would_remove", totals.WouldRemove).
		Int("failed", totals.Failed).Int("skipped", totals.Skipped).
		Int64("estimated_reclaimed_bytes", report.ReclaimedBytes).Str("estimated_reclaimed", formatBytes(report.ReclaimedBytes)).
		Int64("duration_ms", report.Duration.Milliseconds()).Bool("dry_run", dryRun).Msg("Cleanup complete")
}

func reportKinds(report Report) []docker.CleanupResourceKind {
	seen := make(map[docker.CleanupResourceKind]struct{})
	for _, result := range report.Results {
		seen[result.Resource.Kind] = struct{}{}
	}
	kinds := make([]docker.CleanupResourceKind, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

func countResults(results []Result, kind docker.CleanupResourceKind) resultCounts {
	counts := resultCounts{}
	for _, result := range results {
		if kind != "" && result.Resource.Kind != kind {
			continue
		}
		counts.Total++
		switch {
		case result.Err != nil:
			counts.Failed++
		case result.Removed:
			counts.Removed++
		case result.WouldRemove:
			counts.WouldRemove++
		default:
			counts.Skipped++
		}
	}
	return counts
}

func displayResourceID(resource docker.CleanupResource) string {
	if resource.Kind == docker.CleanupVolume {
		return resource.ID
	}
	return shortID(resource.ID)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor, exponent := int64(unit), 0
	for value := bytes / unit; value >= unit; value /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(divisor), "KMGTPE"[exponent])
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
