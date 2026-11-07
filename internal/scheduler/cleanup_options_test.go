package scheduler

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/cleanup"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/updater"
	"github.com/rs/zerolog"
)

func TestRunCycleProtectsContainersWhoseUpdateFailed(t *testing.T) {
	report := updater.Report{Results: []updater.ContainerResult{
		{Container: docker.ContainerSummary{ID: "failed"}, Status: updater.StatusFailed},
		{Container: docker.ContainerSummary{ID: "rolled-back"}, Status: updater.StatusFailed, RollbackTried: true},
		{Container: docker.ContainerSummary{ID: "rollback-only"}, Status: updater.StatusUpdated, RollbackTried: true},
		{Container: docker.ContainerSummary{ID: "updated"}, Status: updater.StatusUpdated},
		{Container: docker.ContainerSummary{ID: "current"}, Status: updater.StatusCurrent},
	}}
	s := newScheduler(fixedClock{now: time.Now()})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) (updater.Report, error) {
		return report, errors.New("update failed")
	}
	var got cleanup.Options
	s.runCleanup = func(_ context.Context, _ config.Config, _ docker.Client, _ zerolog.Logger, options cleanup.Options) error {
		got = options
		return nil
	}
	if err := s.runCycle(context.Background(), config.Default(), nil, zerolog.Nop()); err == nil {
		t.Fatal("runCycle() error = nil, want the update failure")
	}
	if want := []string{"failed", "rolled-back", "rollback-only"}; !slices.Equal(got.ProtectedContainerIDs, want) {
		t.Fatalf("protected containers = %v, want %v", got.ProtectedContainerIDs, want)
	}
}

func TestCleanupOnlyHasNoUpdateProtections(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	var got *cleanup.Options
	s.runCleanup = func(_ context.Context, _ config.Config, _ docker.Client, _ zerolog.Logger, options cleanup.Options) error {
		got = &options
		return nil
	}
	if err := s.runCleanupOnly(context.Background(), config.Default(), nil, zerolog.Nop()); err != nil {
		t.Fatalf("runCleanupOnly() error = %v", err)
	}
	if got == nil || len(got.ProtectedContainerIDs) != 0 {
		t.Fatalf("cleanup-only options = %+v", got)
	}
}
