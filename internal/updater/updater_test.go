package updater

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
	// Initialize logger for tests
	log.Initialize("debug", false)
}

func TestRunUpdateCycle(t *testing.T) {
	t.Log("Testing update cycle execution")

	tests := []struct {
		name                 string
		containers           []docker.ContainerInfo
		images               map[string]docker.ImageInfo
		config               config.Config
		expectedPulls        int
		expectedReplacements int
		wantError            bool
		description          string
	}{
		{
			name:       "no containers",
			containers: []docker.ContainerInfo{},
			images:     map[string]docker.ImageInfo{},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        0,
			expectedReplacements: 0,
			wantError:            false,
			description:          "Empty container list should complete without errors",
		},
		{
			name: "container with same image (no update needed)",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "nginx",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
					Labels:  map[string]string{},
				},
			},
			images: map[string]docker.ImageInfo{
				"nginx:latest": {
					ID:       "sha256:old-nginx", // Same as container
					RepoTags: []string{"nginx:latest"},
				},
			},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        1,
			expectedReplacements: 0,
			wantError:            false,
			description:          "Container with current image should not be updated",
		},
		{
			name: "container with new image available",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "nginx",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
					Labels:  map[string]string{},
				},
			},
			images: map[string]docker.ImageInfo{
				"nginx:latest": {
					ID:       "sha256:new-nginx", // Different from container
					RepoTags: []string{"nginx:latest"},
				},
			},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        1,
			expectedReplacements: 1,
			wantError:            false,
			description:          "Container with outdated image should be updated",
		},
		{
			name: "excluded container not updated",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "postgres",
					Image:   "postgres:15",
					ImageID: "sha256:old-postgres",
					Labels: map[string]string{
						"com.harborbuddy.autoupdate": "false",
					},
				},
			},
			images: map[string]docker.ImageInfo{
				"postgres:15": {
					ID:       "sha256:new-postgres",
					RepoTags: []string{"postgres:15"},
				},
			},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        0,
			expectedReplacements: 0,
			wantError:            false,
			description:          "Container with opt-out label should be skipped",
		},
		{
			name: "dry run mode",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "nginx",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
					Labels:  map[string]string{},
				},
			},
			images: map[string]docker.ImageInfo{},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        true, // DRY RUN MODE
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        0, // No pulls in dry-run
			expectedReplacements: 0, // No replacements in dry-run
			wantError:            false,
			description:          "Dry-run mode should not perform any changes",
		},
		{
			name: "mixed containers - some eligible, some not",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "nginx",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
					Labels:  map[string]string{},
				},
				{
					ID:      "container2",
					Name:    "postgres",
					Image:   "postgres:15",
					ImageID: "sha256:old-postgres",
					Labels: map[string]string{
						"com.harborbuddy.autoupdate": "false",
					},
				},
				{
					ID:      "container3",
					Name:    "redis",
					Image:   "redis:latest",
					ImageID: "sha256:old-redis",
					Labels:  map[string]string{},
				},
			},
			images: map[string]docker.ImageInfo{
				"nginx:latest": {
					ID:       "sha256:new-nginx",
					RepoTags: []string{"nginx:latest"},
				},
				"redis:latest": {
					ID:       "sha256:new-redis",
					RepoTags: []string{"redis:latest"},
				},
			},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        2, // nginx and redis
			expectedReplacements: 2,
			wantError:            false,
			description:          "Should update eligible containers and skip excluded ones",
		},
		{
			name: "duplicate images - should pull once",
			containers: []docker.ContainerInfo{
				{
					ID:      "container1",
					Name:    "nginx1",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
				},
				{
					ID:      "container2",
					Name:    "nginx2",
					Image:   "nginx:latest",
					ImageID: "sha256:old-nginx",
				},
			},
			images: map[string]docker.ImageInfo{
				"nginx:latest": {
					ID:       "sha256:new-nginx",
					RepoTags: []string{"nginx:latest"},
				},
			},
			config: config.Config{
				Updates: config.UpdatesConfig{
					Enabled:       true,
					UpdateAll:     true,
					CheckInterval: 30 * time.Minute,
					DryRun:        false,
					AllowImages:   []string{"*"},
					DenyImages:    []string{},
				},
			},
			expectedPulls:        1, // Optimized from 2 to 1
			expectedReplacements: 2,
			wantError:            false,
			description:          "Multiple containers with same image should trigger only one pull",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Test: %s", tt.description)
			t.Logf("  Containers: %d", len(tt.containers))
			t.Logf("  Dry-run: %v", tt.config.Updates.DryRun)

			// Create mock client
			mockClient := docker.NewMockDockerClient()
			mockClient.Containers = tt.containers
			mockClient.PullImageReturns = tt.images

			// Run update cycle
			ctx := context.Background()
			err := RunUpdateCycle(ctx, tt.config, mockClient)

			// Check error expectation
			if tt.wantError && err == nil {
				t.Error("RunUpdateCycle() error = nil, want error")
				t.Log("  Expected an error but got none")
			} else if !tt.wantError && err != nil {
				t.Errorf("RunUpdateCycle() error = %v, want nil", err)
				t.Logf("  Unexpected error occurred")
			}

			// Verify pulls
			actualPulls := len(mockClient.PulledImages)
			if actualPulls != tt.expectedPulls {
				t.Errorf("Expected %d image pulls, got %d", tt.expectedPulls, actualPulls)
				t.Logf("  Pulled images: %v", mockClient.PulledImages)
			} else {
				t.Logf("✓ Correct number of pulls: %d", actualPulls)
			}

			// Verify replacements
			actualReplacements := len(mockClient.ReplacedContainers)
			if actualReplacements != tt.expectedReplacements {
				t.Errorf("Expected %d container replacements, got %d", tt.expectedReplacements, actualReplacements)
				t.Logf("  Replaced containers: %v", mockClient.ReplacedContainers)
			} else {
				t.Logf("✓ Correct number of replacements: %d", actualReplacements)
			}

			// Additional validation
			if !tt.config.Updates.DryRun && tt.expectedReplacements > 0 {
				t.Logf("  Verified update process completed")
				for i, req := range mockClient.ReplacedContainers {
					t.Logf("    [%d] Replaced: %s (old: %s, new: %s)", i+1, req.Name, req.OldID, req.NewID)
				}
			}
		})
	}
}

func TestUpdateCycleErrorHandling(t *testing.T) {
	t.Log("Testing update cycle error handling")

	t.Run("docker list containers error", func(t *testing.T) {
		t.Log("  Testing recovery from ListContainers error")

		mockClient := docker.NewMockDockerClient()
		mockClient.ListContainersError = fmt.Errorf("docker daemon not available")

		cfg := config.Default()
		ctx := context.Background()

		err := RunUpdateCycle(ctx, cfg, mockClient)
		if err == nil {
			t.Error("RunUpdateCycle() should return error when ListContainers fails")
			t.Log("  Expected Docker connection error to propagate")
		} else {
			t.Logf("✓ Error correctly propagated: %v", err)
		}
	})

	t.Run("image pull error doesn't stop cycle", func(t *testing.T) {
		t.Log("  Testing that pull errors don't abort entire cycle")

		mockClient := docker.NewMockDockerClient()
		mockClient.Containers = []docker.ContainerInfo{
			{
				ID:      "container1",
				Name:    "nginx",
				Image:   "nginx:latest",
				ImageID: "sha256:old",
				Labels:  map[string]string{},
			},
			{
				ID:      "container2",
				Name:    "redis",
				Image:   "redis:latest",
				ImageID: "sha256:old",
				Labels:  map[string]string{},
			},
		}
		mockClient.PullImageError = fmt.Errorf("network timeout")

		cfg := config.Default()
		ctx := context.Background()

		err := RunUpdateCycle(ctx, cfg, mockClient)
		if err != nil {
			t.Errorf("RunUpdateCycle() = %v, want nil (errors should not abort cycle)", err)
			t.Log("  Individual container errors should be logged but not fail the cycle")
		} else {
			t.Log("✓ Cycle completed despite pull errors")
		}

		if len(mockClient.ReplacedContainers) > 0 {
			t.Error("No containers should be replaced when pull fails")
			t.Logf("  Replacements: %v", mockClient.ReplacedContainers)
		} else {
			t.Log("✓ No replacements attempted after pull failure")
		}
	})
}
