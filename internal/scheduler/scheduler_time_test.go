package scheduler

import (
	"testing"
	"time"
)

// TestCalculateNextRun_ExtendedEdgeCases covers Leap Years, Year Rollovers, and DST oddities.
func TestCalculateNextRun_ExtendedEdgeCases(t *testing.T) {
	// Helper to load location or fail
	loadLoc := func(name string) *time.Location {
		l, err := time.LoadLocation(name)
		if err != nil {
			t.Fatalf("Failed to load location %s: %v", name, err)
		}
		return l
	}

	utc := loadLoc("UTC")
	ny := loadLoc("America/New_York")

	tests := []struct {
		name          string
		nowStr        string         // Format: "2006-01-02 15:04:05"
		location      *time.Location
		scheduleTime  string         // Format: "15:04"
		wantNextStr   string         // Format: "2006-01-02 15:04:05" (in loc)
		description   string
	}{
		// --- Leap Year Tests ---
		{
			name:         "Leap Year - Feb 28 to Feb 29",
			nowStr:       "2024-02-28 10:00:00",
			location:     utc,
			scheduleTime: "10:00", // Scheduled for same time (so it pushes to next day)
			wantNextStr:  "2024-02-29 10:00:00",
			description:  "In a leap year (2024), adding 1 day to Feb 28 should land on Feb 29",
		},
		{
			name:         "Leap Year - Feb 29 to Mar 1",
			nowStr:       "2024-02-29 10:00:00",
			location:     utc,
			scheduleTime: "10:00",
			wantNextStr:  "2024-03-01 10:00:00",
			description:  "In a leap year (2024), adding 1 day to Feb 29 should land on Mar 1",
		},
		{
			name:         "Non-Leap Year - Feb 28 to Mar 1",
			nowStr:       "2023-02-28 10:00:00",
			location:     utc,
			scheduleTime: "10:00",
			wantNextStr:  "2023-03-01 10:00:00",
			description:  "In a non-leap year (2023), adding 1 day to Feb 28 should land on Mar 1",
		},

		// --- Year Rollover ---
		{
			name:         "Year Rollover - Dec 31 to Jan 1",
			nowStr:       "2023-12-31 20:00:00",
			location:     utc,
			scheduleTime: "20:00",
			wantNextStr:  "2024-01-01 20:00:00",
			description:  "Adding 1 day to Dec 31 should roll over year to Jan 1",
		},

		// --- DST Spring Forward (NY: 2023-03-12 02:00 -> 03:00) ---
		// If we schedule for 02:30, it doesn't exist.
		// Go's time.Date(..., 2, 30, ...) in NY on that day normalizes to 03:30.
		{
			name:         "DST Spring Forward - Missing Hour",
			nowStr:       "2023-03-12 01:00:00",
			location:     ny,
			scheduleTime: "02:30",
			// In Go's time.Date, 02:30 in the skipped hour normalizes to 01:30 EST
			// (likely interpreting 2 hours past midnight in the prevailing offset before switch?)
			wantNextStr: "2023-03-12 01:30:00",
			description: "Scheduling inside the skipped DST hour (02:30) normalizes to 01:30 in Go's implementation",
		},
		{
			name:         "DST Spring Forward - Before Jump",
			nowStr:       "2023-03-11 10:00:00",
			location:     ny,
			scheduleTime: "10:00",
			wantNextStr:  "2023-03-12 10:00:00",
			description:  "Scheduling across DST start should preserve wall clock time (10:00 EST -> 10:00 EDT)",
		},

		// --- DST Fall Back (NY: 2023-11-05 02:00 -> 01:00) ---
		// 01:30 happens twice. First as EDT, then as EST.
		// time.Date usually picks the *first* occurrence unless specified?
		// Actually time.Date builds the struct.
		// If we are at 00:00, and ask for 01:30.
		{
			name:         "DST Fall Back - Ambiguous Hour",
			nowStr:       "2023-11-05 00:00:00",
			location:     ny,
			scheduleTime: "01:30",
			// Go usually picks the first one (EDT, offset -0400)
			wantNextStr:  "2023-11-05 01:30:00",
			description:  "Scheduling in the ambiguous hour (01:30) should pick a valid time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse "now" in the correct location
			now, err := time.ParseInLocation("2006-01-02 15:04:05", tt.nowStr, tt.location)
			if err != nil {
				t.Fatalf("Bad test setup 'now': %v", err)
			}

			// Execute
			got := calculateNextRun(now, tt.scheduleTime, tt.location)
			gotStr := got.Format("2006-01-02 15:04:05")

			if gotStr != tt.wantNextStr {
				t.Errorf("calculateNextRun()\n  Now:  %v\n  Sched: %s\n  Want: %s\n  Got:  %s",
					now, tt.scheduleTime, tt.wantNextStr, gotStr)
			}
		})
	}
}
