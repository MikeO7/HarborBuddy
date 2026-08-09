package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

func TestRunCycleRunsUpdateAndCleanupIndependently(t *testing.T) {
	updateErr := errors.New("update failed")
	cleanupErr := errors.New("cleanup failed")
	var calls []string
	s := newScheduler(fixedClock{now: time.Now()})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		calls = append(calls, "update")
		return updateErr
	}
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		calls = append(calls, "cleanup")
		return cleanupErr
	}

	cfg := config.Default()
	err := s.runCycle(context.Background(), cfg, nil, zerolog.Nop())
	if len(calls) != 2 || calls[0] != "update" || calls[1] != "cleanup" {
		t.Fatalf("calls = %v, want update then cleanup", calls)
	}
	if !errors.Is(err, updateErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("runCycle() error = %v, want both errors", err)
	}
}

func TestRunPropagatesSelfUpdateSignalWithoutCleanup(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	cleanupCalled := false
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
	}
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		cleanupCalled = true
		return nil
	}
	cfg := config.Default()
	cfg.RunOnce = true

	if err := s.run(context.Background(), cfg, nil, zerolog.Nop()); err == nil {
		t.Fatal("run() error = nil, want typed self-update shutdown signal")
	} else if _, ok := selfupdate.AsShutdownRequired(err); !ok {
		t.Fatalf("run() error = %v, want typed self-update shutdown signal", err)
	}
	if cleanupCalled {
		t.Fatal("cleanup ran after self-update helper started; shutdown must begin immediately")
	}
}

func TestRunOnceReturnsOrdinaryCycleError(t *testing.T) {
	want := errors.New("update failed")
	s := newScheduler(fixedClock{now: time.Now()})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return want }
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return nil }
	cfg := config.Default()
	cfg.RunOnce = true

	if err := s.run(context.Background(), cfg, nil, zerolog.Nop()); !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want %v", err, want)
	}
}

func TestRunHandlesNilContextAndDisabledCycle(t *testing.T) {
	cfg := config.Default()
	cfg.RunOnce = true
	cfg.Updates.Enabled = false
	cfg.Cleanup.Enabled = false
	//nolint:staticcheck // This test verifies the scheduler normalizes a nil context.
	if err := newScheduler(fixedClock{now: time.Now()}).run(nil, cfg, nil, zerolog.Nop()); err != nil {
		t.Fatalf("run(nil context) error = %v", err)
	}
}

func TestRunCycleSkipsDisabledWork(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	cfg := config.Default()
	cfg.Updates.Enabled = false
	cfg.Cleanup.Enabled = false
	if err := s.runCycle(context.Background(), cfg, nil, zerolog.Nop()); err != nil {
		t.Fatalf("runCycle() error = %v", err)
	}
}

func TestDefaultSchedulerIntegrationsAndRunWrapper(t *testing.T) {
	cfg := config.Default()
	cfg.RunOnce = true
	cfg.Updates.Enabled = false
	cfg.Cleanup.Enabled = false
	if err := Run(context.Background(), cfg, noopClient{}, zerolog.Nop()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	s := newScheduler(fixedClock{now: time.Now()})
	if err := s.runUpdate(context.Background(), cfg, noopClient{}, zerolog.Nop()); err != nil {
		t.Fatalf("default runUpdate error = %v", err)
	}
	if err := s.runCleanup(context.Background(), cfg, noopClient{}, zerolog.Nop()); err != nil {
		t.Fatalf("default runCleanup error = %v", err)
	}
}

func TestGracefulResultHandlesNilCancellationAndOrdinaryErrors(t *testing.T) {
	if err := gracefulResult(context.Background(), nil); err != nil {
		t.Fatalf("gracefulResult(nil) = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gracefulResult(ctx, context.Canceled); err != nil {
		t.Fatalf("gracefulResult(canceled) = %v", err)
	}
	want := errors.New("ordinary")
	if err := gracefulResult(context.Background(), want); !errors.Is(err, want) {
		t.Fatalf("gracefulResult(ordinary) = %v", err)
	}
}

func TestContinuousCycleClassifiesErrors(t *testing.T) {
	ordinaryErr := errors.New("update failed")
	shutdownErr := &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}

	for _, test := range []struct {
		name     string
		ctx      context.Context
		cycleErr error
		wantStop bool
		wantErr  error
	}{
		{name: "ordinary failure continues", ctx: context.Background(), cycleErr: ordinaryErr},
		{name: "self-update stops and propagates", ctx: context.Background(), cycleErr: shutdownErr, wantStop: true, wantErr: shutdownErr},
		{name: "cancellation stops cleanly", ctx: canceledContext(), cycleErr: context.Canceled, wantStop: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newScheduler(fixedClock{now: time.Now()})
			s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return test.cycleErr }
			s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return nil }
			cfg := config.Default()
			cfg.Cleanup.Enabled = false

			stop, err := s.runContinuousCycle(test.ctx, cfg, nil, zerolog.Nop())
			if stop != test.wantStop || !errors.Is(err, test.wantErr) {
				t.Fatalf("runContinuousCycle() = stop %v, error %v; want stop %v, error %v", stop, err, test.wantStop, test.wantErr)
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestCleanupOnlySkipsUpdate(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	updateCalled := false
	cleanupCalled := false
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		updateCalled = true
		return nil
	}
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		cleanupCalled = true
		return nil
	}
	cfg := config.Default()
	cfg.CleanupOnly = true
	cfg.RunOnce = true
	cfg.Cleanup.Enabled = false

	if err := s.run(context.Background(), cfg, nil, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if updateCalled || !cleanupCalled {
		t.Fatalf("updateCalled=%v cleanupCalled=%v", updateCalled, cleanupCalled)
	}
}

func TestIntervalRunsImmediatelyThenOnTicker(t *testing.T) {
	tickerChannel := make(chan time.Time, 1)
	clock := fixedClock{now: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC), ticker: &fakeTicker{channel: tickerChannel}}
	s := newScheduler(clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	calls := 0
	second := make(chan struct{})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current == 2 {
			close(second)
			cancel()
		}
		return nil
	}
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return nil }
	cfg := config.Default()
	cfg.Cleanup.Enabled = false

	done := make(chan error, 1)
	go func() { done <- s.run(ctx, cfg, nil, zerolog.Nop()) }()
	waitForCalls(t, &mu, &calls, 1)
	tickerChannel <- clock.now.Add(cfg.Updates.CheckInterval)

	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("ticker cycle did not run")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIntervalStopsWhenTickerCycleRequestsSelfUpdate(t *testing.T) {
	tickerChannel := make(chan time.Time, 1)
	clock := fixedClock{now: time.Now(), ticker: &fakeTicker{channel: tickerChannel}}
	s := newScheduler(clock)
	var mu sync.Mutex
	var calls int
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current == 2 {
			return &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
		}
		return nil
	}
	cfg := config.Default()
	cfg.Cleanup.Enabled = false
	done := make(chan error, 1)
	go func() { done <- s.run(context.Background(), cfg, nil, zerolog.Nop()) }()
	waitForCalls(t, &mu, &calls, 1)
	tickerChannel <- time.Now()
	if err := <-done; err == nil {
		t.Fatal("interval scheduler returned nil after self-update cycle")
	} else if _, ok := selfupdate.AsShutdownRequired(err); !ok {
		t.Fatalf("interval scheduler error = %v, want shutdown signal", err)
	}
}

func TestIntervalStopsWhenImmediateCycleRequestsSelfUpdate(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
	}
	cfg := config.Default()
	cfg.Cleanup.Enabled = false
	err := s.run(context.Background(), cfg, nil, zerolog.Nop())
	if err == nil {
		t.Fatal("immediate interval cycle returned nil")
	}
	if _, ok := selfupdate.AsShutdownRequired(err); !ok {
		t.Fatalf("immediate interval error = %v", err)
	}
}

func TestScheduledModeWaitsBeforeFirstCycle(t *testing.T) {
	timerChannel := make(chan time.Time, 1)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now, timer: &fakeTimer{channel: timerChannel}}
	s := newScheduler(clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan struct{})
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		close(called)
		cancel()
		return nil
	}
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error { return nil }
	cfg := config.Default()
	cfg.Cleanup.Enabled = false
	cfg.Updates.ScheduleTime = "11:00"
	cfg.Updates.Timezone = "UTC"

	done := make(chan error, 1)
	go func() { done <- s.run(ctx, cfg, nil, zerolog.Nop()) }()
	select {
	case <-called:
		t.Fatal("daily scheduler ran before timer fired")
	case <-time.After(20 * time.Millisecond):
	}
	timerChannel <- now.Add(time.Hour)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("daily scheduler did not run after timer")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestScheduledModeStopsTimerWhenContextIsCanceled(t *testing.T) {
	timer := &recordingTimer{channel: make(chan time.Time)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := fixedClock{
		now:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		timer: timer,
	}
	cfg := config.Default()
	cfg.Updates.ScheduleTime = "11:00"
	cfg.Updates.Timezone = "UTC"
	if err := newScheduler(clock).run(ctx, cfg, nil, zerolog.Nop()); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !timer.stopped {
		t.Fatal("scheduled timer was not stopped after cancellation")
	}
}

func TestScheduledModeStopsWhenTimerCycleRequestsSelfUpdate(t *testing.T) {
	timerChannel := make(chan time.Time, 1)
	clock := fixedClock{now: time.Now(), timer: &fakeTimer{channel: timerChannel}}
	s := newScheduler(clock)
	s.runUpdate = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return &selfupdate.ShutdownRequiredError{TargetContainerID: "target", HelperContainerID: "helper"}
	}
	cfg := config.Default()
	cfg.Updates.ScheduleTime = "11:00"
	cfg.Updates.Timezone = "UTC"
	done := make(chan error, 1)
	go func() { done <- s.run(context.Background(), cfg, nil, zerolog.Nop()) }()
	timerChannel <- time.Now()
	if err := <-done; err == nil {
		t.Fatal("scheduled scheduler returned nil after self-update cycle")
	} else if _, ok := selfupdate.AsShutdownRequired(err); !ok {
		t.Fatalf("scheduled scheduler error = %v, want shutdown signal", err)
	}
}

func TestScheduledModeRejectsUnknownTimezone(t *testing.T) {
	cfg := config.Default()
	cfg.Updates.ScheduleTime = "11:00"
	cfg.Updates.Timezone = "Mars/Base"
	err := newScheduler(fixedClock{now: time.Now()}).run(context.Background(), cfg, nil, zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), "load schedule timezone") {
		t.Fatalf("run() error = %v, want timezone error", err)
	}
}
