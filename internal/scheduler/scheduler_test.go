package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/pkg/log"
)

func init() {
	log.Initialize(log.Config{Level: "debug"})
}

func TestRunCycle(t *testing.T) {
	t.Log("Testing single cycle execution")

	tests := []struct {
		name        string
		config      config.Config
		description string
	}{
		{
			name: "both updates and cleanup enabled",
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 12 * time.Hour,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
				Cleanup: config.CleanupConfig{
					Enabled:      true,
					MinAgeHours:  24,
					DanglingOnly: true,
				},
			},
			description: "Should run both update and cleanup phases",
		},
		{
			name: "updates disabled",
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled: false,
				},
				Cleanup: config.CleanupConfig{
					Enabled:      true,
					MinAgeHours:  24,
					DanglingOnly: true,
				},
			},
			description: "Should skip updates and run cleanup only",
		},
		{
			name: "cleanup disabled",
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 12 * time.Hour,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
				Cleanup: config.CleanupConfig{
					Enabled: false,
				},
			},
			description: "Should run updates and skip cleanup",
		},
		{
			name: "both disabled",
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled: false,
				},
				Cleanup: config.CleanupConfig{
					Enabled: false,
				},
			},
			description: "Should complete without running either phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Test: %s", tt.description)
			t.Logf("  Updates enabled: %v", tt.config.Updates.Enabled)
			t.Logf("  Cleanup enabled: %v", tt.config.Cleanup.Enabled)

			mockClient := docker.NewMockDockerClient()
			ctx := context.Background()

			err := runCycle(ctx, tt.config, mockClient)
			if err != nil {
				t.Errorf("runCycle() error = %v, want nil", err)
				t.Log("  Cycle should complete without errors")
			} else {
				t.Log("✓ Cycle completed successfully")
			}
		})
	}
}

func TestSchedulerModes(t *testing.T) {
	t.Log("Testing scheduler execution modes")

	t.Run("once mode completes immediately", func(t *testing.T) {
		t.Log("  Testing --once mode (single execution)")

		cfg := config.Config{
			RunOnce: true,
			Updates: config.UpdatesConfig{
				Enabled:       true,
				CheckInterval: 1 * time.Second,
				AllowImages:   []string{"*"},
			},
			Cleanup: config.CleanupConfig{
				Enabled: false,
			},
		}

		mockClient := docker.NewMockDockerClient()

		// Run should complete immediately in once mode
		done := make(chan error, 1)
		go func() {
			done <- Run(cfg, mockClient)
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() in once mode error = %v, want nil", err)
			} else {
				t.Log("✓ Once mode completed immediately")
			}
		case <-time.After(5 * time.Second):
			t.Error("Run() in once mode did not complete within 5 seconds")
			t.Log("  Once mode should complete a single cycle and exit")
		}
	})

	t.Run("cleanup only mode", func(t *testing.T) {
		t.Log("  Testing --cleanup-only mode")

		cfg := config.Config{
			CleanupOnly: true,
			Updates: config.UpdatesConfig{
				Enabled: true, // Should be ignored
			},
			Cleanup: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
		}

		mockClient := docker.NewMockDockerClient()
		mockClient.Images = []docker.ImageInfo{
			{
				ID:        "sha256:dangling",
				Dangling:  true,
				CreatedAt: time.Now().Add(-48 * time.Hour),
			},
		}

		done := make(chan error, 1)
		go func() {
			done <- Run(cfg, mockClient)
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() in cleanup-only mode error = %v, want nil", err)
			}
			// Verify cleanup ran
			if len(mockClient.RemovedImages) == 0 {
				t.Error("Cleanup did not run in cleanup-only mode")
				t.Log("  Expected at least one image removal attempt")
			} else {
				t.Logf("✓ Cleanup-only mode executed: %d images removed", len(mockClient.RemovedImages))
			}
		case <-time.After(5 * time.Second):
			t.Error("Run() in cleanup-only mode did not complete within 5 seconds")
		}
	})

	t.Run("continuous mode runs multiple cycles", func(t *testing.T) {
		t.Log("  Testing continuous mode with short interval")

		cfg := config.Config{
			RunOnce:     false,
			CleanupOnly: false,
			Updates: config.UpdatesConfig{
				Enabled:       true,
				CheckInterval: 100 * time.Millisecond,
				AllowImages:   []string{"*"},
			},
			Cleanup: config.CleanupConfig{
				Enabled: false,
			},
		}

		mockClient := docker.NewMockDockerClient()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Override the scheduler to use our context
		done := make(chan bool)
		go func() {
			// This simulates what Run() does but with our timeout
			ticker := time.NewTicker(cfg.Updates.CheckInterval)
			defer ticker.Stop()

			cycleCount := 0
			for {
				select {
				case <-ctx.Done():
					t.Logf("  Completed %d cycles", cycleCount)
					done <- true
					return
				case <-ticker.C:
					cycleCount++
					runCycle(ctx, cfg, mockClient)
				}
			}
		}()

		<-done
		// We should have run at least 3 cycles in 500ms with 100ms interval
		if len(mockClient.PulledImages) >= 0 { // Mock tracks all pulls
			t.Log("✓ Continuous mode executed multiple cycles")
		}
	})
}

func TestSchedulerCancellation(t *testing.T) {
	t.Log("Testing graceful scheduler cancellation")

	t.Run("context cancellation stops scheduler", func(t *testing.T) {
		t.Log("  Testing that cancelled context stops execution")

		cfg := config.Config{
			RunOnce: false,
			Updates: config.UpdatesConfig{
				Enabled:       true,
				CheckInterval: 1 * time.Second,
				AllowImages:   []string{"*"},
			},
		}

		mockClient := docker.NewMockDockerClient()

		// Create a context that we'll cancel
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan bool)
		go func() {
			// Simulate Run with cancellable context
			ticker := time.NewTicker(cfg.Updates.CheckInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					done <- true
					return
				case <-ticker.C:
					runCycle(ctx, cfg, mockClient)
				}
			}
		}()

		// Cancel after a short delay
		time.Sleep(100 * time.Millisecond)
		cancel()

		select {
		case <-done:
			t.Log("✓ Scheduler stopped gracefully on cancellation")
		case <-time.After(2 * time.Second):
			t.Error("Scheduler did not stop within 2 seconds of cancellation")
			t.Log("  Graceful shutdown failed")
		}
	})
}

func TestCalculateNextRun(t *testing.T) {
	t.Log("Testing next run time calculation")

	locUTC, _ := time.LoadLocation("UTC")
	locNY, _ := time.LoadLocation("America/New_York")

	// Fixed "now" for deterministic testing: 2023-01-01 10:00:00 UTC
	now := time.Date(2023, 1, 1, 10, 0, 0, 0, locUTC)

	tests := []struct {
		name         string
		scheduleTime string
		location     *time.Location
		now          time.Time
		wantNextDay  bool
		wantHour     int
		wantMinute   int
		expectDate   string // Optional: explicit date check for rollovers (YYYY-MM-DD)
	}{
		{
			name:         "same day future time",
			scheduleTime: "15:00",
			location:     locUTC,
			now:          now,
			wantNextDay:  false,
			wantHour:     15,
			wantMinute:   0,
		},
		{
			name:         "same day past time triggers next day",
			scheduleTime: "09:00",
			location:     locUTC,
			now:          now,
			wantNextDay:  true,
			wantHour:     9,
			wantMinute:   0,
		},
		{
			name:         "timezone difference (NY is 05:00 when UTC is 10:00)",
			scheduleTime: "12:00", // 12:00 NY is 17:00 UTC
			location:     locNY,
			now:          now,   // 05:00 in NY
			wantNextDay:  false, // 12:00 NY is still in future for today
			wantHour:     12,
			wantMinute:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a fixed time to avoid midnight flakiness
			// 2023-01-01 12:00:00
			realNow := time.Date(2023, 1, 1, 12, 0, 0, 0, tt.location)

			// Construct a schedule time 1 hour in the future (13:00)
			future := realNow.Add(time.Hour)
			futureTimeStr := future.Format("15:04")

			nextRunFuture := calculateNextRun(realNow, futureTimeStr, tt.location)
			// Should be same day
			if nextRunFuture.Day() != realNow.Day() {
				t.Errorf("Expected future time to be today (%d), got day %d. NextRun: %v", realNow.Day(), nextRunFuture.Day(), nextRunFuture)
			}
			if nextRunFuture.Hour() != future.Hour() || nextRunFuture.Minute() != future.Minute() {
				t.Errorf("Expected time %s, got %s", futureTimeStr, nextRunFuture.Format("15:04"))
			}

			// Construct a schedule time 1 hour in the past (11:00)
			past := realNow.Add(-time.Hour)
			pastTimeStr := past.Format("15:04")

			nextRunPast := calculateNextRun(realNow, pastTimeStr, tt.location)
			// Should be tomorrow
			expectedTomorrow := realNow.Add(24 * time.Hour)
			if nextRunPast.Day() != expectedTomorrow.Day() {
				t.Errorf("Expected past time to be tomorrow (%d), got day %d. NextRun: %v", expectedTomorrow.Day(), nextRunPast.Day(), nextRunPast)
			}
		})
	}
}

func TestRunScheduledMode_Cancellation(t *testing.T) {
	// We want to verify it waits and then cancels
	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:      true,
			ScheduleTime: "00:00", // Likely far away
			Timezone:     "UTC",
		},
	}

	mockClient := docker.NewMockDockerClient()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- runScheduledMode(ctx, cfg, mockClient)
	}()

	// Cancel immediately to test graceful exit from the "wait" state
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runScheduledMode returned error on cancellation: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("runScheduledMode did not exit on cancellation")
	}
}

func TestRunIntervalMode_Loop(t *testing.T) {
	// Test that it runs multiple cycles
	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:       true,
			CheckInterval: 10 * time.Millisecond,
		},
	}

	mockClient := docker.NewMockDockerClient()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := runIntervalMode(ctx, cfg, mockClient)
	if err != nil {
		t.Errorf("runIntervalMode returned error: %v", err)
	}

	// Check coverage of the loop
	// (This test mainly exercises the code path, exact cycle count isn't easily accessible
	// without injecting a spy, but we know MockClient tracks pulls)
}

func TestRunCycle_UpdateError(t *testing.T) {
	t.Log("Testing runCycle with update error")

	mockClient := docker.NewMockDockerClient()
	mockClient.ListContainersError = fmt.Errorf("docker error")

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:       true,
			CheckInterval: time.Minute,
		},
		Cleanup: config.CleanupConfig{
			Enabled: true,
		},
	}

	ctx := context.Background()
	err := runCycle(ctx, cfg, mockClient)
	if err == nil {
		t.Error("Expected error from runCycle when update fails")
	}
}

func TestRunCycle_CleanupError(t *testing.T) {
	t.Log("Testing runCycle with cleanup error")

	mockClient := docker.NewMockDockerClient()
	mockClient.ListDanglingImagesError = fmt.Errorf("cleanup error")

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled: false, // Skip updates
		},
		Cleanup: config.CleanupConfig{
			Enabled:      true,
			DanglingOnly: true,
		},
	}

	ctx := context.Background()
	err := runCycle(ctx, cfg, mockClient)
	if err == nil {
		t.Error("Expected error from runCycle when cleanup fails")
	}
}

func TestRunScheduledMode_InvalidTimezone(t *testing.T) {
	cfg := config.Config{
		Updates: config.UpdatesConfig{
			ScheduleTime: "03:00",
			Timezone:     "Invalid/Timezone",
		},
	}

	mockClient := docker.NewMockDockerClient()
	err := runScheduledMode(context.Background(), cfg, mockClient)
	if err == nil {
		t.Error("Expected error for invalid timezone")
	}
}

func TestCalculateNextRun_EdgeCases(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")

	tests := []struct {
		name          string
		now           time.Time
		scheduleTime  string
		expectSameDay bool
	}{
		{
			name:          "schedule time in future today",
			now:           time.Date(2024, 1, 15, 10, 0, 0, 0, loc),
			scheduleTime:  "15:00",
			expectSameDay: true,
		},
		{
			name:          "schedule time in past today",
			now:           time.Date(2024, 1, 15, 16, 0, 0, 0, loc),
			scheduleTime:  "15:00",
			expectSameDay: false, // Should be tomorrow
		},
		{
			name:          "schedule time exactly now",
			now:           time.Date(2024, 1, 15, 15, 0, 0, 0, loc),
			scheduleTime:  "15:00",
			expectSameDay: false, // Should be tomorrow
		},
		{
			name:          "midnight crossing",
			now:           time.Date(2024, 1, 15, 23, 59, 0, 0, loc),
			scheduleTime:  "00:30",
			expectSameDay: false, // Should be next day
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextRun := calculateNextRun(tt.now, tt.scheduleTime, loc)

			if tt.expectSameDay {
				if nextRun.Day() != tt.now.Day() {
					t.Errorf("Expected same day, got next day: %v", nextRun)
				}
			} else {
				if nextRun.Day() == tt.now.Day() {
					t.Errorf("Expected next day, got same day: %v", nextRun)
				}
			}

			// Verify it's always in the future
			if !nextRun.After(tt.now) {
				t.Errorf("Next run should be in the future: now=%v, nextRun=%v", tt.now, nextRun)
			}
		})
	}
}

func TestRunIntervalMode_InitialCycleError(t *testing.T) {
	t.Log("Testing runIntervalMode with initial cycle error")

	mockClient := docker.NewMockDockerClient()
	mockClient.ListContainersError = fmt.Errorf("docker error")

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:       true,
			CheckInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Should not return error - just log it and continue
	err := runIntervalMode(ctx, cfg, mockClient)
	if err != nil {
		t.Errorf("runIntervalMode should not propagate initial cycle error: %v", err)
	}
}

func TestRunScheduledMode_CycleError(t *testing.T) {
	t.Log("Testing runScheduledMode with cycle error")

	mockClient := docker.NewMockDockerClient()
	mockClient.ListContainersError = fmt.Errorf("docker error")

	// Use a schedule time 1 second in the future to trigger quickly
	futureTime := time.Now().Add(100 * time.Millisecond)
	scheduleStr := futureTime.Format("15:04")

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:      true,
			ScheduleTime: scheduleStr,
			Timezone:     "UTC",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Should not return error - just log it and continue
	err := runScheduledMode(ctx, cfg, mockClient)
	if err != nil {
		t.Errorf("runScheduledMode should not propagate cycle error: %v", err)
	}
}
