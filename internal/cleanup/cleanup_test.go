package cleanup

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/pkg/log"
	"github.com/MikeO7/HarborBuddy/pkg/util"
	"github.com/rs/zerolog"
)

func init() {
	log.Initialize(log.Config{Level: "debug"})
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
			testLogger := zerolog.New(zerolog.NewConsoleWriter())
			err := RunCleanup(ctx, cfg, mockClient, &testLogger)
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
					logger := log.WithImage(shortID(img.ID), "test")
					t.Logf("    [%d] ID: %s, Dangling: %v, Age: %v, Eligible: %v",
						i, img.ID[:12], img.Dangling, age.Round(time.Hour),
						isEligibleForCleanup(img, tt.config, time.Duration(tt.config.MinAgeHours)*time.Hour, logger))
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
		testLogger := zerolog.New(zerolog.NewConsoleWriter())
		err := RunCleanup(ctx, cfg, mockClient, &testLogger)
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
		testLogger := zerolog.New(zerolog.NewConsoleWriter())
		err := RunCleanup(ctx, cfg, mockClient, &testLogger)
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

			logger := log.WithImage(shortID(tt.image.ID), "test")
			result := isEligibleForCleanup(tt.image, tt.config, tt.minAge, logger)
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

func TestIsEligibleForCleanup_EdgeCases(t *testing.T) {
	// Boundary testing exactly at MinAge
	now := time.Now()
	minAge := 24 * time.Hour

	t.Run("boundary condition - exact age", func(t *testing.T) {
		image := docker.ImageInfo{
			ID:        "sha256:boundary",
			Dangling:  true,
			CreatedAt: now.Add(-minAge), // Exactly 24h old
		}
		cfg := config.CleanupConfig{DanglingOnly: true}

		// Should be eligible (>= logic usually implied or > check)
		// Code logic: Check if image is old enough
		// age := time.Since(image.CreatedAt)
		// if age < minAge { return false }
		// So exact age is NOT (< minAge), thus eligible (true)

		eligible := isEligibleForCleanup(image, cfg, minAge, log.WithImage("test", "test"))
		if !eligible {
			t.Error("Exact age match should be eligible")
		}
	})

	t.Run("slightly older", func(t *testing.T) {
		image := docker.ImageInfo{
			ID:        "sha256:older",
			Dangling:  true,
			CreatedAt: now.Add(-minAge - 1*time.Minute),
		}
		eligible := isEligibleForCleanup(image, config.CleanupConfig{DanglingOnly: true}, minAge, log.WithImage("test", "test"))
		if !eligible {
			t.Error("Older than minAge should be eligible")
		}
	})

	t.Run("slightly newer", func(t *testing.T) {
		image := docker.ImageInfo{
			ID:        "sha256:newer",
			Dangling:  true,
			CreatedAt: now.Add(-minAge + 1*time.Minute),
		}
		eligible := isEligibleForCleanup(image, config.CleanupConfig{DanglingOnly: true}, minAge, log.WithImage("test", "test"))
		if eligible {
			t.Error("Newer than minAge should NOT be eligible")
		}
	})
}

func TestRunCleanup_ContextCancellation(t *testing.T) {
	t.Log("Testing cleanup context cancellation")

	yesterday := time.Now().Add(-25 * time.Hour)
	mockClient := docker.NewMockDockerClient()

	// Create many images so we have time to cancel
	images := make([]docker.ImageInfo, 100)
	for i := 0; i < 100; i++ {
		images[i] = docker.ImageInfo{
			ID:        fmt.Sprintf("sha256:image%d", i),
			Dangling:  true,
			CreatedAt: yesterday,
		}
	}
	mockClient.Images = images

	cfg := config.Config{
		Cleanup: config.CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: true,
		},
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunCleanup(ctx, cfg, mockClient, &testLogger)
	if err == nil {
		t.Error("Expected error when context is cancelled")
	} else if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sha256:1234567890abcdef", "sha256:12345"}, // 23 chars -> truncate to 12
		{"short", "short"},
		{"exactly12chs", "exactly12chs"},  // Exactly 12 chars
		{"thirteenchars", "thirteenchar"}, // 13 chars -> truncate to 12
		{"", ""},
		{"abcdefghijkl", "abcdefghijkl"},  // 12 chars exactly
		{"abcdefghijklm", "abcdefghijkl"}, // 13 chars -> truncate to 12
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shortID(tt.input)
			if result != tt.expected {
				t.Errorf("shortID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRunCleanup_WithRepoTags(t *testing.T) {
	t.Log("Testing cleanup with images that have repo tags")

	yesterday := time.Now().Add(-25 * time.Hour)
	mockClient := docker.NewMockDockerClient()
	mockClient.Images = []docker.ImageInfo{
		{
			ID:        "sha256:taggedimage",
			RepoTags:  []string{"nginx:latest", "nginx:v1.0"},
			Dangling:  false,
			CreatedAt: yesterday,
			Size:      100 * 1024 * 1024, // 100MB
		},
	}

	cfg := config.Config{
		Cleanup: config.CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: false, // Remove all, not just dangling
		},
	}

	ctx := context.Background()
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunCleanup(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("RunCleanup() error = %v", err)
	}

	if len(mockClient.RemovedImages) != 1 {
		t.Errorf("Expected 1 image removed, got %d", len(mockClient.RemovedImages))
	}
}

func TestRunCleanup_ListImagesError_NonDangling(t *testing.T) {
	t.Log("Testing cleanup with ListImages error (non-dangling mode)")

	mockClient := docker.NewMockDockerClient()
	mockClient.ListImagesError = fmt.Errorf("docker error")

	cfg := config.Config{
		Cleanup: config.CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: false, // Uses ListImages instead of ListDanglingImages
		},
	}

	ctx := context.Background()
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunCleanup(ctx, cfg, mockClient, &testLogger)
	if err == nil {
		t.Error("Expected error from ListImages")
	}
}

func TestGetImageFriendlyName(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected string
	}{
		{
			name:     "nil labels",
			labels:   nil,
			expected: "",
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: "",
		},
		{
			name: "opencontainers title",
			labels: map[string]string{
				"org.opencontainers.image.title": "my-app",
			},
			expected: "my-app",
		},
		{
			name: "docker compose service",
			labels: map[string]string{
				"com.docker.compose.service": "web",
			},
			expected: "web",
		},
		{
			name: "priority check",
			labels: map[string]string{
				"org.opencontainers.image.title": "primary",
				"com.docker.compose.service":     "secondary",
			},
			expected: "primary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := util.GetImageFriendlyName(tt.labels); got != tt.expected {
				t.Errorf("GetImageFriendlyName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRunCleanup_FriendlyNames(t *testing.T) {
	// Capture logs
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	now := time.Now()
	yesterday := now.Add(-25 * time.Hour)
	mockClient := docker.NewMockDockerClient()

	mockClient.Images = []docker.ImageInfo{
		{
			ID:        "sha256:dangling-friendly",
			RepoTags:  []string{},
			Dangling:  true,
			CreatedAt: yesterday,
			Labels: map[string]string{
				"com.docker.compose.service": "my-service",
			},
		},
	}

	cfg := config.Config{
		Cleanup: config.CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: true,
		},
	}

	testLogger := zerolog.New(&logBuf)
	err := RunCleanup(context.Background(), cfg, mockClient, &testLogger)
	if err != nil {
		t.Fatalf("RunCleanup failed: %v", err)
	}

	logs := logBuf.String()
	expected := "my-service"
	if !strings.Contains(logs, expected) {
		t.Errorf("Log missing friendly name: %q", expected)
		t.Logf("Actual logs: %s", logs)
	}
	expectedID := shortID("sha256:dangling-friendly")
	if !strings.Contains(logs, expectedID) {
		t.Errorf("Log missing image ID: %q", expectedID)
	}
}
