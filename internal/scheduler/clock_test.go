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
