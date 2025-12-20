package scheduler

import (
	"testing"
	"time"
)

func TestCalculateNextRun_DST(t *testing.T) {
	t.Log("Testing next run time calculation across DST transitions")

	locNY, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		now          time.Time
		scheduleTime string
		expected     time.Time
	}{
		{
			// Spring Forward: March 10, 2024 (2am -> 3am)
			// On March 9th (Sat) at 11am, scheduling for 10am.
			// Next run should be March 10th (Sun) at 10am.
			// 10am is safe from the jump (happens at 2am).
			name:         "Spring Forward - standard time",
			now:          time.Date(2024, 3, 9, 11, 0, 0, 0, locNY),
			scheduleTime: "10:00",
			expected:     time.Date(2024, 3, 10, 10, 0, 0, 0, locNY),
		},
		{
			// Spring Forward: March 10, 2024
			// On March 9th at 11am, scheduling for 2:30am?
			// 2:30am doesn't exist on March 10th. It jumps 2->3.
			// Go time normalization usually picks the corresponding time (e.g. 3:30 or 1:30 depending on implementation)
			// But for this test, let's stick to a time that DOES exist, like 12:00pm.
			// 24 hours after March 9 12:00pm is March 10 12:00pm.
			// But the duration is 23 hours.
			name:         "Spring Forward - verify local time preservation",
			now:          time.Date(2024, 3, 9, 13, 0, 0, 0, locNY),
			scheduleTime: "12:00",
			expected:     time.Date(2024, 3, 10, 12, 0, 0, 0, locNY),
		},
		{
			// Fall Back: Nov 3, 2024 (2am -> 1am)
			// On Nov 2nd (Sat) at 11am, scheduling for 10am.
			// Next run should be Nov 3rd (Sun) at 10am.
			// Duration is 25 hours.
			name:         "Fall Back - standard time",
			now:          time.Date(2024, 11, 2, 11, 0, 0, 0, locNY),
			scheduleTime: "10:00",
			expected:     time.Date(2024, 11, 3, 10, 0, 0, 0, locNY),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateNextRun(tt.now, tt.scheduleTime, locNY)

			if !got.Equal(tt.expected) {
				t.Errorf("calculateNextRun() = %v, want %v", got, tt.expected)
				t.Logf("  Difference: %v", got.Sub(tt.expected))
			} else {
				t.Logf("✓ Correctly calculated: %v", got)
			}
		})
	}
}
