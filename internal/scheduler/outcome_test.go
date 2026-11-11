package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/updater"
	"github.com/rs/zerolog"
)

type failingRemovalClient struct{ noopClient }

func (failingRemovalClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return []docker.ImageInfo{{ID: "sha256:old", Dangling: true, CreatedAt: time.Now().Add(-1000 * time.Hour)}}, nil
}

func (failingRemovalClient) RemoveImage(context.Context, string) error {
	return errors.New("image is in use")
}

func TestPartialFailuresFailOneShotRunsWithoutFailingTheCycleLog(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) (updater.Report, error) {
		return updater.Report{Results: []updater.ContainerResult{{Status: updater.StatusFailed}, {Status: updater.StatusUpdated}}}, nil
	}
	cfg := config.Default()
	cfg.RunOnce = true
	var output bytes.Buffer
	err := s.run(context.Background(), cfg, failingRemovalClient{}, zerolog.New(&output))
	if err == nil || !strings.Contains(err.Error(), "1 container update(s) failed") || !strings.Contains(err.Error(), "1 cleanup removal(s) failed") {
		t.Fatalf("run(--once) error = %v, want both partial failures", err)
	}
	if text := output.String(); !strings.Contains(text, `"event":"cycle_complete","outcome":"partial_failure"`) || strings.Contains(text, `"event":"cycle_complete","outcome":"failed"`) {
		t.Fatalf("cycle log = %s, want a partial_failure warning", text)
	}
}

func TestCycleCompletionOutcomes(t *testing.T) {
	hard := errors.New("list containers failed")
	partial := &partialFailureError{message: "1 container update(s) failed"}
	for _, test := range []struct {
		name string
		ctx  context.Context
		errs []error
		want string
	}{
		{name: "clean", ctx: context.Background(), want: "completed"},
		{name: "partial", ctx: context.Background(), errs: []error{fmt.Errorf("cleanup cycle: %w", partial)}, want: "partial_failure"},
		{name: "hard", ctx: context.Background(), errs: []error{partial, hard}, want: "failed"},
		{name: "cancelled", ctx: canceledContext(), errs: []error{hard}, want: "cancelled"},
	} {
		joined := errors.Join(test.errs...)
		if _, got := cycleCompletion(test.ctx, zerolog.Nop(), joined, onlyPartialFailures(test.errs...)); got != test.want {
			t.Errorf("%s: outcome = %q, want %q", test.name, got, test.want)
		}
	}
}
