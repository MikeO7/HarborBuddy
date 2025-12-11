package cleanup

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
	log.Initialize("debug", false)
}

func TestRunCleanup(t *testing.T) {
	t.Log("Testing image cleanup functionality")

	now := time.Now()
	yesterday := now.Add(-25 * time.Hour)
	lastWeek := now.Add(-8 * 24 * time.Hour)

	tests := []struct {
		name            string
		images          []docker.ImageInfo
		config          config.CleanupConfig
		expectedRemoved int
		description     string
	}{
		{
			name:   "cleanup disabled",
			images: []docker.ImageInfo{},
			config: config.CleanupConfig{
				Enabled:      false,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
			expectedRemoved: 0,
			description:     "When cleanup is disabled, no images should be removed",
		},
		{
			name: "remove dangling images only",
			images: []docker.ImageInfo{
				{
					ID:        "sha256:dangling1",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: yesterday,
				},
				{
					ID:        "sha256:tagged1",
					RepoTags:  []string{"nginx:latest"},
					Dangling:  false,
					CreatedAt: yesterday,
				},
			},
			config: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
			expectedRemoved: 1,
			description:     "Only dangling images should be removed when DanglingOnly=true",
		},
		{
			name: "respect min age threshold",
			images: []docker.ImageInfo{
				{
					ID:        "sha256:recent",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: now.Add(-1 * time.Hour), // Too recent
				},
				{
					ID:        "sha256:old",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: yesterday, // Old enough
				},
			},
			config: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
			expectedRemoved: 1,
			description:     "Only images older than MinAgeHours should be removed",
		},
		{
			name: "remove all unused when DanglingOnly=false",
			images: []docker.ImageInfo{
				{
					ID:        "sha256:dangling1",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: yesterday,
				},
				{
					ID:        "sha256:unused1",
					RepoTags:  []string{"unused:tag"},
					Dangling:  false,
					CreatedAt: yesterday,
				},
			},
			config: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: false,
			},
			expectedRemoved: 2,
			description:     "All eligible images should be removed when DanglingOnly=false",
		},
		{
			name: "multiple old dangling images",
			images: []docker.ImageInfo{
				{
					ID:        "sha256:old1",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: lastWeek,
				},
				{
					ID:        "sha256:old2",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: lastWeek,
				},
				{
					ID:        "sha256:old3",
					RepoTags:  []string{},
					Dangling:  true,
					CreatedAt: lastWeek,
				},
			},
			config: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
			expectedRemoved: 3,
			description:     "All eligible dangling images should be removed",
		},
		{
			name: "no eligible images",
			images: []docker.ImageInfo{
				{
					ID:        "sha256:recent1",
					RepoTags:  []string{"nginx:latest"},
					Dangling:  false,
					CreatedAt: now.Add(-1 * time.Hour),
				},
				{
					ID:        "sha256:recent2",
					RepoTags:  []string{"redis:latest"},
					Dangling:  false,
					CreatedAt: now.Add(-2 * time.Hour),
				},
			},
			config: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
			expectedRemoved: 0,
			description:     "No images should be removed when none are eligible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Test: %s", tt.description)
			t.Logf("  Images: %d", len(tt.images))
			t.Logf("  Enabled: %v, DanglingOnly: %v, MinAge: %dh",
				tt.config.Enabled, tt.config.DanglingOnly, tt.config.MinAgeHours)

			mockClient := docker.NewMockDockerClient()
			mockClient.Images = tt.images

			cfg := config.Config{
				Cleanup: tt.config,
			}

			ctx := context.Background()
			err := RunCleanup(ctx, cfg, mockClient)
			if err != nil {
				t.Errorf("RunCleanup() error = %v, want nil", err)
				t.Log("  Cleanup should complete without errors")
			}

			actualRemoved := len(mockClient.RemovedImages)
			if actualRemoved != tt.expectedRemoved {
				t.Errorf("Expected %d images removed, got %d", tt.expectedRemoved, actualRemoved)
				t.Logf("  Removed images: %v", mockClient.RemovedImages)
				t.Log("  Checking eligibility logic:")
				for i, img := range tt.images {
					age := time.Since(img.CreatedAt)
					t.Logf("    [%d] ID: %s, Dangling: %v, Age: %v, Eligible: %v",
						i, img.ID[:12], img.Dangling, age.Round(time.Hour),
						isEligibleForCleanup(img, tt.config, time.Duration(tt.config.MinAgeHours)*time.Hour))
				}
			} else {
				t.Logf("✓ Correct number of images removed: %d", actualRemoved)
				if actualRemoved > 0 {
					t.Logf("  Removed: %v", mockClient.RemovedImages)
				}
			}
		})
	}
}

func TestCleanupErrorHandling(t *testing.T) {
	t.Log("Testing cleanup error handling")

	t.Run("list images error", func(t *testing.T) {
		t.Log("  Testing recovery from ListImages error")

		mockClient := docker.NewMockDockerClient()
		mockClient.ListImagesError = fmt.Errorf("docker daemon error")
		// Also set ListDanglingImagesError to ensure failure regardless of which method is called
		mockClient.ListDanglingImagesError = fmt.Errorf("docker daemon error")

		cfg := config.Config{
			Cleanup: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
		}

		ctx := context.Background()
		err := RunCleanup(ctx, cfg, mockClient)
		if err == nil {
			t.Error("RunCleanup() should return error when ListImages fails")
			t.Log("  Expected Docker error to propagate")
		} else {
			t.Logf("✓ Error correctly propagated: %v", err)
		}
	})

	t.Run("remove image error continues cleanup", func(t *testing.T) {
		t.Log("  Testing that remove errors don't abort cleanup")

		yesterday := time.Now().Add(-25 * time.Hour)
		mockClient := docker.NewMockDockerClient()
		mockClient.Images = []docker.ImageInfo{
			{
				ID:        "sha256:image1",
				RepoTags:  []string{},
				Dangling:  true,
				CreatedAt: yesterday,
			},
			{
				ID:        "sha256:image2",
				RepoTags:  []string{},
				Dangling:  true,
				CreatedAt: yesterday,
			},
		}
		mockClient.RemoveImageError = fmt.Errorf("image in use")

		cfg := config.Config{
			Cleanup: config.CleanupConfig{
				Enabled:      true,
				MinAgeHours:  24,
				DanglingOnly: true,
			},
		}

		ctx := context.Background()
		err := RunCleanup(ctx, cfg, mockClient)
		if err != nil {
			t.Errorf("RunCleanup() = %v, want nil (errors should not abort cleanup)", err)
			t.Log("  Individual image errors should be logged but not fail cleanup")
		} else {
			t.Log("✓ Cleanup completed despite remove errors")
		}

		// Verify all images were attempted
		if len(mockClient.RemovedImages) != 2 {
			t.Errorf("Expected 2 removal attempts, got %d", len(mockClient.RemovedImages))
			t.Log("  All eligible images should be attempted even if some fail")
		} else {
			t.Log("✓ All eligible images were attempted")
		}
	})
}

func TestIsEligibleForCleanup(t *testing.T) {
	t.Log("Testing image cleanup eligibility logic")

	now := time.Now()
	tests := []struct {
		name     string
		image    docker.ImageInfo
		config   config.CleanupConfig
		minAge   time.Duration
		expected bool
	}{
		{
			name: "dangling and old enough",
			image: docker.ImageInfo{
				ID:        "sha256:test1",
				Dangling:  true,
				CreatedAt: now.Add(-25 * time.Hour),
			},
			config: config.CleanupConfig{
				DanglingOnly: true,
			},
			minAge:   24 * time.Hour,
			expected: true,
		},
		{
			name: "dangling but too recent",
			image: docker.ImageInfo{
				ID:        "sha256:test2",
				Dangling:  true,
				CreatedAt: now.Add(-1 * time.Hour),
			},
			config: config.CleanupConfig{
				DanglingOnly: true,
			},
			minAge:   24 * time.Hour,
			expected: false,
		},
		{
			name: "not dangling with DanglingOnly",
			image: docker.ImageInfo{
				ID:        "sha256:test3",
				Dangling:  false,
				CreatedAt: now.Add(-25 * time.Hour),
			},
			config: config.CleanupConfig{
				DanglingOnly: true,
			},
			minAge:   24 * time.Hour,
			expected: false,
		},
		{
			name: "not dangling but old with DanglingOnly=false",
			image: docker.ImageInfo{
				ID:        "sha256:test4",
				Dangling:  false,
				CreatedAt: now.Add(-25 * time.Hour),
			},
			config: config.CleanupConfig{
				DanglingOnly: false,
			},
			minAge:   24 * time.Hour,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Image age: %v", time.Since(tt.image.CreatedAt).Round(time.Hour))
			t.Logf("  Dangling: %v, DanglingOnly: %v", tt.image.Dangling, tt.config.DanglingOnly)

			result := isEligibleForCleanup(tt.image, tt.config, tt.minAge)
			if result != tt.expected {
				t.Errorf("isEligibleForCleanup() = %v, want %v", result, tt.expected)
				t.Logf("  Eligibility check failed")
				t.Logf("  MinAge threshold: %v", tt.minAge)
			} else {
				t.Logf("✓ Eligibility correctly determined: %v", result)
			}
		})
	}
}
