package scheduler

import (
	"context"
	"errors"
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

func TestCalculateNextRunPreservesDailyWallClockAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "spring forward",
			now:  time.Date(2026, 3, 7, 13, 0, 0, 0, location),
			want: time.Date(2026, 3, 8, 12, 0, 0, 0, location),
		},
		{
			name: "fall back",
			now:  time.Date(2026, 10, 31, 13, 0, 0, 0, location),
			want: time.Date(2026, 11, 1, 12, 0, 0, 0, location),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := calculateNextRun(test.now, "12:00", location); !got.Equal(test.want) {
				t.Fatalf("calculateNextRun() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCalculateNextRunUsesTomorrowWhenExactlyScheduled(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 1, 31, 3, 0, 0, 0, location)
	want := time.Date(2026, 2, 1, 3, 0, 0, 0, location)
	if got := calculateNextRun(now, "03:00", location); !got.Equal(want) {
		t.Fatalf("calculateNextRun() = %v, want %v", got, want)
	}
}

func waitForCalls(t *testing.T, mu *sync.Mutex, calls *int, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := *calls
		mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("calls did not reach %d", want)
}

type fixedClock struct {
	now    time.Time
	timer  timer
	ticker ticker
}

func (c fixedClock) Now() time.Time { return c.now }
func (c fixedClock) NewTimer(time.Duration) timer {
	if c.timer != nil {
		return c.timer
	}
	return &fakeTimer{channel: make(chan time.Time)}
}
func (c fixedClock) NewTicker(time.Duration) ticker {
	if c.ticker != nil {
		return c.ticker
	}
	return &fakeTicker{channel: make(chan time.Time)}
}

type fakeTimer struct{ channel chan time.Time }

func (t *fakeTimer) C() <-chan time.Time { return t.channel }
func (t *fakeTimer) Stop() bool          { return true }

type fakeTicker struct{ channel chan time.Time }

func (t *fakeTicker) C() <-chan time.Time { return t.channel }
func (t *fakeTicker) Stop()               {}
