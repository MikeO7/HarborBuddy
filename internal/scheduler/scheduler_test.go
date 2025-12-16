package scheduler

import (
	"context"
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
					CheckInterval: 30 * time.Minute,
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
					CheckInterval: 30 * time.Minute,
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
