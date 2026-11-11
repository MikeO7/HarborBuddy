package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

func TestReportFailedCountsOnlyFailedRemovals(t *testing.T) {
	report := Report{Results: []Result{{Removed: true}, {Err: errors.New("in use")}, {Reason: "skipped"}, {Err: errors.New("busy")}}}
	if got := report.Failed(); got != 2 {
		t.Fatalf("Failed() = %d, want 2", got)
	}
}

func TestDryRunBuildCacheMatchesWhatARealRunWouldPrune(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	cfg := config.Default()
	cfg.Updates.DryRun = true
	cfg.Cleanup.BuildCache = true
	cfg.Cleanup.MinAgeHours = 24
	eligible := docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "eligible", CreatedAt: old, LastUsedAt: old}

	client := &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{docker.CleanupBuildCache: {eligible}}}
	report, err := runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil || len(report.Results) != 1 || !report.Results[0].WouldRemove {
		t.Fatalf("precise dry-run = %+v, %v; want would_remove", report.Results, err)
	}

	// A kept record with no last-used time makes Docker's prune imprecise, so
	// a real run skips it; dry-run must say the same.
	kept := docker.CleanupResource{Kind: docker.CleanupBuildCache, ID: "kept", CreatedAt: now}
	client = &fakeClient{resources: map[docker.CleanupResourceKind][]docker.CleanupResource{docker.CleanupBuildCache: {eligible, kept}}}
	report, err = runCleanupAt(context.Background(), cfg, client, zerolog.Nop(), now)
	if err != nil {
		t.Fatalf("imprecise dry-run error = %v", err)
	}
	for _, result := range report.Results {
		if result.WouldRemove {
			t.Fatalf("dry-run reported %s as would_remove although a real run skips the prune", result.Resource.ID)
		}
	}
	if !client.buildCachePruneCut.IsZero() {
		t.Fatal("dry-run pruned the build cache")
	}
}
