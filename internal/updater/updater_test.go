package updater

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
	"github.com/rs/zerolog"
)

func init() {
	// Initialize logger for tests
	log.Initialize(log.Config{Level: "debug"})
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
			testLogger := zerolog.New(zerolog.NewConsoleWriter())
			err := RunUpdateCycle(ctx, tt.config, mockClient, &testLogger)

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
		testLogger := zerolog.New(zerolog.NewConsoleWriter())

		err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
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
		testLogger := zerolog.New(zerolog.NewConsoleWriter())

		err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
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

func TestCheckForUpdateLogging(t *testing.T) {
	// Capture logs
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()
	cfg := config.Default()

	// Setup: One container needs update
	containerID := "container1"
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      containerID,
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old",
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"nginx:latest": {
			ID: "sha256:new",
		},
	}

	// Run cycle
	testLogger := zerolog.New(&logBuf)
	_ = RunUpdateCycle(ctx, cfg, mockClient, &testLogger)

	// Verify Log
	logs := logBuf.String()
	expected := "🚀 Update found for nginx"
	if !strings.Contains(logs, expected) {
		t.Errorf("Log missing expected string: %q", expected)
		t.Logf("Actual logs: %s", logs)
	}
}

func TestCheckForUpdateLogging_FriendlyNames(t *testing.T) {
	// Capture logs
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()
	cfg := config.Default()

	// Setup: One container needs update
	containerID := "container1"
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      containerID,
			Name:    "my-container",
			Image:   "private/image:latest",
			ImageID: "sha256:old",
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"private/image:latest": {
			ID: "sha256:new",
			Labels: map[string]string{
				"org.opencontainers.image.title": "MyFriendlyApp",
			},
		},
	}

	// Run cycle
	testLogger := zerolog.New(&logBuf)
	_ = RunUpdateCycle(ctx, cfg, mockClient, &testLogger)

	// Verify Log
	logs := logBuf.String()
	// Should see "Update found for my-container ... MyFriendlyApp"
	// Expected format: 🚀 Update found for my-container (private/image:latest): sha256:old- -> MyFriendlyApp
	expectedPart1 := "Update found for my-container"
	expectedPart2 := "MyFriendlyApp"

	if !strings.Contains(logs, expectedPart1) {
		t.Errorf("Log missing container name: %q", expectedPart1)
	}
	if !strings.Contains(logs, expectedPart2) {
		t.Errorf("Log missing friendly app name: %q", expectedPart2)
	}
	if t.Failed() {
		t.Logf("Actual logs: %s", logs)
	}
}

func TestIsSelf(t *testing.T) {
	t.Log("Testing detecting self container")

	tests := []struct {
		name          string
		id            string
		hostname      string
		cgroupContent string
		expected      bool
	}{
		{
			name:          "match by prefix hostname",
			id:            "abcdef1234567890",
			hostname:      "abcdef123456",
			cgroupContent: "",
			expected:      true,
		},
		{
			name:          "no match prefix hostname",
			id:            "abcdef1234567890",
			hostname:      "fedcba654321",
			cgroupContent: "",
			expected:      false,
		},
		{
			name:          "empty hostname should not match",
			id:            "abcdef1234567890",
			hostname:      "",
			cgroupContent: "",
			expected:      false,
		},
		{
			name:          "match by cgroup",
			id:            "abcdef1234567890",
			hostname:      "fedcba654321", // hostname non-match
			cgroupContent: "11:pids:/docker/abcdef1234567890\n",
			expected:      true,
		},
		{
			name:          "no match by cgroup",
			id:            "abcdef1234567890",
			hostname:      "fedcba654321",
			cgroupContent: "11:pids:/docker/othercontainer\n",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkIsSelf(tt.id, tt.hostname, tt.cgroupContent)
			if result != tt.expected {
				t.Errorf("checkIsSelf() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestUpdateContainer_Errors(t *testing.T) {
	t.Log("Testing container update error handling")

	// Setup generic happy path data
	container := docker.ContainerInfo{
		ID:      "container1",
		Name:    "nginx",
		Image:   "nginx:latest",
		ImageID: "sha256:old",
	}
	cfg := config.Default()
	ctx := context.Background()
	logger := log.WithContainer("container1", "nginx")

	t.Run("CreateContainerLike error", func(t *testing.T) {
		mockClient := docker.NewMockDockerClient()
		mockClient.Containers = []docker.ContainerInfo{container}
		mockClient.CreateContainerError = fmt.Errorf("name conflict")

		err := updateContainer(ctx, cfg, mockClient, container, logger)
		if err == nil {
			t.Error("Expected error when CreateContainerLike fails")
		} else if !strings.Contains(err.Error(), "failed to create new container") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("ReplaceContainer error", func(t *testing.T) {
		mockClient := docker.NewMockDockerClient()
		mockClient.Containers = []docker.ContainerInfo{container}
		mockClient.ReplaceContainerError = fmt.Errorf("network error")

		err := updateContainer(ctx, cfg, mockClient, container, logger)
		if err == nil {
			t.Error("Expected error when ReplaceContainer fails")
		} else if !strings.Contains(err.Error(), "failed to replace container") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("ReplaceContainer warning (non-fatal)", func(t *testing.T) {
		mockClient := docker.NewMockDockerClient()
		mockClient.Containers = []docker.ContainerInfo{container}
		// Mock a warning by returning an error starting with "warning"
		// This simulates the behavior documented in internal/updater/updater.go:306
		mockClient.ReplaceContainerError = fmt.Errorf("warning: could not remove old container")

		err := updateContainer(ctx, cfg, mockClient, container, logger)
		if err != nil {
			t.Errorf("Expected nil error for warning, got: %v", err)
		}
	})
}

func TestRunUpdateCycle_ContextCancellation(t *testing.T) {
	t.Log("Testing update cycle cancellation")

	mockClient := docker.NewMockDockerClient()
	// Simulate many containers to ensure we catch it in the loop
	containers := make([]docker.ContainerInfo, 10)
	for i := 0; i < 10; i++ {
		containers[i] = docker.ContainerInfo{
			ID:    fmt.Sprintf("container%d", i),
			Image: "test:latest",
		}
	}
	mockClient.Containers = containers

	cfg := config.Default()

	// Create a context that is already cancelled or cancels quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err == nil {
		t.Error("Expected error when context is cancelled")
	} else if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

func TestSafePullCache(t *testing.T) {
	t.Log("Testing SafePullCache functionality")

	t.Run("first call triggers pull", func(t *testing.T) {
		cache := NewSafePullCache()
		ctx := context.Background()
		callCount := 0

		pullFunc := func() (docker.ImageInfo, error) {
			callCount++
			return docker.ImageInfo{ID: "sha256:test"}, nil
		}

		info, err, hit := cache.GetOrPull(ctx, "test:latest", pullFunc)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if hit {
			t.Error("Expected cache miss on first call")
		}
		if info.ID != "sha256:test" {
			t.Errorf("Expected ID sha256:test, got %s", info.ID)
		}
		if callCount != 1 {
			t.Errorf("Expected pullFunc called once, got %d", callCount)
		}
	})

	t.Run("second call uses cache", func(t *testing.T) {
		cache := NewSafePullCache()
		ctx := context.Background()
		callCount := 0

		pullFunc := func() (docker.ImageInfo, error) {
			callCount++
			return docker.ImageInfo{ID: "sha256:test"}, nil
		}

		// First call
		_, _, _ = cache.GetOrPull(ctx, "test:latest", pullFunc)

		// Second call should hit cache
		info, err, hit := cache.GetOrPull(ctx, "test:latest", pullFunc)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !hit {
			t.Error("Expected cache hit on second call")
		}
		if info.ID != "sha256:test" {
			t.Errorf("Expected ID sha256:test, got %s", info.ID)
		}
		if callCount != 1 {
			t.Errorf("Expected pullFunc called only once, got %d", callCount)
		}
	})

	t.Run("context cancellation during wait", func(t *testing.T) {
		cache := NewSafePullCache()

		// Create a context that cancels quickly
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// Start a slow pull
		slowPull := func() (docker.ImageInfo, error) {
			time.Sleep(100 * time.Millisecond)
			return docker.ImageInfo{ID: "sha256:slow"}, nil
		}

		// Start first call in goroutine
		go cache.GetOrPull(context.Background(), "slow:latest", slowPull)

		// Wait for the first call to start
		time.Sleep(5 * time.Millisecond)

		// Second call should time out waiting
		_, err, _ := cache.GetOrPull(ctx, "slow:latest", slowPull)
		if err == nil {
			t.Error("Expected context timeout error")
		}
	})

	t.Run("pull error is cached", func(t *testing.T) {
		cache := NewSafePullCache()
		ctx := context.Background()
		pullErr := fmt.Errorf("network error")

		pullFunc := func() (docker.ImageInfo, error) {
			return docker.ImageInfo{}, pullErr
		}

		// First call - should get error
		_, err, _ := cache.GetOrPull(ctx, "error:latest", pullFunc)
		if err != pullErr {
			t.Errorf("Expected pullErr, got %v", err)
		}

		// Second call - should get cached error
		_, err, hit := cache.GetOrPull(ctx, "error:latest", pullFunc)
		if !hit {
			t.Error("Expected cache hit for error result")
		}
		if err != pullErr {
			t.Errorf("Expected cached pullErr, got %v", err)
		}
	})
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

func TestRunUpdateCycle_DenyList(t *testing.T) {
	t.Log("Testing update cycle with deny list")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "postgres",
			Image:   "postgres:15",
			ImageID: "sha256:old-postgres",
			Labels:  map[string]string{},
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"postgres:15": {
			ID: "sha256:new-postgres",
		},
	}

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:     true,
			UpdateAll:   true,
			AllowImages: []string{"*"},
			DenyImages:  []string{"postgres:*"}, // Deny postgres
		},
	}

	ctx := context.Background()
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should not update postgres
	if len(mockClient.ReplacedContainers) != 0 {
		t.Errorf("Expected 0 replacements (denied), got %d", len(mockClient.ReplacedContainers))
	}
}

func TestRunUpdateCycle_AllowList(t *testing.T) {
	t.Log("Testing update cycle with allow list")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old-nginx",
			Labels:  map[string]string{},
		},
		{
			ID:      "container2",
			Name:    "redis",
			Image:   "redis:latest",
			ImageID: "sha256:old-redis",
			Labels:  map[string]string{},
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"nginx:latest": {ID: "sha256:new-nginx"},
		"redis:latest": {ID: "sha256:new-redis"},
	}

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:     true,
			UpdateAll:   true,
			AllowImages: []string{"nginx:*"}, // Only allow nginx
			DenyImages:  []string{},
		},
	}

	ctx := context.Background()
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should only update nginx, not redis
	if len(mockClient.PulledImages) != 1 {
		t.Errorf("Expected 1 pull (nginx only), got %d: %v", len(mockClient.PulledImages), mockClient.PulledImages)
	}
}

func TestRunUpdateCycle_InspectContainerError(t *testing.T) {
	t.Log("Testing update cycle with InspectContainer error")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old-nginx",
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"nginx:latest": {ID: "sha256:new-nginx"},
	}
	mockClient.InspectContainerError = fmt.Errorf("container not found")

	cfg := config.Default()
	ctx := context.Background()

	// Should not fail the entire cycle, just skip this container
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("Expected nil error (continue on inspect error), got: %v", err)
	}
}

func TestRunUpdateCycle_ContextCancelledDuringUpdatePhase(t *testing.T) {
	t.Log("Testing context cancellation during update phase")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old-nginx",
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"nginx:latest": {ID: "sha256:new-nginx"},
	}

	cfg := config.Default()

	// Create context that we'll cancel during the update phase
	ctx, cancel := context.WithCancel(context.Background())

	// Run the update cycle in goroutine
	errChan := make(chan error, 1)
	go func() {
		testLogger := zerolog.New(zerolog.NewConsoleWriter())
		errChan <- RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	}()

	// Wait a bit for the update to start, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errChan
	// May or may not be cancelled depending on timing
	if err != nil && err != context.Canceled {
		t.Logf("Got error (expected context.Canceled or nil): %v", err)
	}
}

func TestRunUpdateCycle_UpdateContainerError(t *testing.T) {
	t.Log("Testing update cycle with updateContainer error")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old-nginx",
		},
	}
	mockClient.PullImageReturns = map[string]docker.ImageInfo{
		"nginx:latest": {ID: "sha256:new-nginx"},
	}
	// Make create container fail
	mockClient.CreateContainerError = fmt.Errorf("create error")

	cfg := config.Default()
	ctx := context.Background()

	// Should not fail the entire cycle, just skip this container
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("Expected nil error (continue on update error), got: %v", err)
	}
}

func TestRunUpdateCycle_DryRunWithCandidates(t *testing.T) {
	t.Log("Testing dry run with actual update candidates")

	mockClient := docker.NewMockDockerClient()
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:      "container1",
			Name:    "nginx",
			Image:   "nginx:latest",
			ImageID: "sha256:old-nginx",
		},
	}
	// In dry run mode, we don't actually pull, so this shouldn't be used
	// But we need to have the update candidate exist

	cfg := config.Config{
		Updates: config.UpdatesConfig{
			Enabled:     true,
			UpdateAll:   true,
			DryRun:      true,
			AllowImages: []string{"*"},
		},
	}

	ctx := context.Background()
	testLogger := zerolog.New(zerolog.NewConsoleWriter())
	err := RunUpdateCycle(ctx, cfg, mockClient, &testLogger)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// No actual replacements in dry run
	if len(mockClient.ReplacedContainers) != 0 {
		t.Errorf("Expected 0 replacements in dry run, got %d", len(mockClient.ReplacedContainers))
	}
}
