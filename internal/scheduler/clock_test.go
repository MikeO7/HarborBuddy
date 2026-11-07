package scheduler

import (
	"errors"
	"testing"
	"time"
)

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

func TestCalculateNextRunSpringForwardGapRunsAfterGap(t *testing.T) {
	for _, test := range []struct {
		name, zone string
		now, want  func(*time.Location) time.Time
	}{
		{
			// Go normalizes the skipped 02:30 back to 01:30 PST here.
			name: "Los Angeles day before", zone: "America/Los_Angeles",
			now:  func(l *time.Location) time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, l) },
			want: func(l *time.Location) time.Time { return time.Date(2026, 3, 8, 3, 0, 0, 0, l) },
		},
		{
			// A daemon awake in the hour before the gap must still run that day.
			name: "Los Angeles before gap", zone: "America/Los_Angeles",
			now:  func(l *time.Location) time.Time { return time.Date(2026, 3, 8, 1, 45, 0, 0, l) },
			want: func(l *time.Location) time.Time { return time.Date(2026, 3, 8, 3, 0, 0, 0, l) },
		},
		{
			// Go normalizes the skipped 02:30 forward to 03:30 CEST here.
			name: "Berlin day before", zone: "Europe/Berlin",
			now:  func(l *time.Location) time.Time { return time.Date(2026, 3, 28, 12, 0, 0, 0, l) },
			want: func(l *time.Location) time.Time { return time.Date(2026, 3, 29, 3, 0, 0, 0, l) },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			location, err := time.LoadLocation(test.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := calculateNextRun(test.now(location), "02:30", location), test.want(location); !got.Equal(want) {
				t.Fatalf("calculateNextRun() = %v, want %v", got, want)
			}
		})
	}
}

func TestCalculateNextRunFallBackRunsOnce(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	first := calculateNextRun(time.Date(2026, 10, 31, 12, 0, 0, 0, location), "01:30", location)
	if want := time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC); !first.Equal(want) {
		t.Fatalf("first run = %v, want the first 01:30 (PDT) %v", first, want)
	}
	// After the first run, including during the repeated hour, the next run is the next day.
	want := time.Date(2026, 11, 2, 1, 30, 0, 0, location)
	for _, now := range []time.Time{first.Add(time.Second), first.Add(time.Hour + time.Minute)} {
		if got := calculateNextRun(now.In(location), "01:30", location); !got.Equal(want) {
			t.Fatalf("calculateNextRun(%v) = %v, want %v", now, got, want)
		}
	}
}

func TestGenerateCycleIDFallsBackWhenRandomSourceFails(t *testing.T) {
	original := randomRead
	randomRead = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	t.Cleanup(func() { randomRead = original })
	now := time.Date(2026, 8, 8, 14, 5, 6, 0, time.UTC)
	if got := generateCycleID(now); got != "140506" {
		t.Fatalf("generateCycleID() = %q, want timestamp fallback", got)
	}
}

func TestRealClockAndTimersExposeUnderlyingChannels(t *testing.T) {
	clock := realClock{}
	if clock.Now().IsZero() {
		t.Fatal("realClock.Now() returned zero time")
	}
	timer := clock.NewTimer(time.Hour)
	if timer.C() == nil || !timer.Stop() {
		t.Fatal("real timer was not created or stopped")
	}
	ticker := clock.NewTicker(time.Hour)
	if ticker.C() == nil {
		t.Fatal("real ticker channel is nil")
	}
	ticker.Stop()
}
