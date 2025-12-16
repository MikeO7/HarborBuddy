package selfupdate

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/pkg/log"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func TestRunUpdater(t *testing.T) {
	// Capture logs
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-123"
	targetName := "harborbuddy"
	newImage := "harborbuddy:latest"

	// Setup initial state: Target running
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  targetName,
			State: &types.ContainerState{Running: true},
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// We need to simulate the target stopping asynchronously
	go func() {
		time.Sleep(100 * time.Millisecond)
		// Update the container state safely using the new method
		mockClient.SetContainerState(targetID, false)
	}()

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err != nil {
		t.Fatalf("RunUpdater failed: %v", err)
	}

	// Verify actions
	// 1. Target removed
	if len(mockClient.RemovedContainers) != 1 || mockClient.RemovedContainers[0] != targetID {
		t.Errorf("Expected target container %s to be removed, got %v", targetID, mockClient.RemovedContainers)
	}

	// 2. New container created
	if len(mockClient.CreatedContainers) != 1 {
		t.Fatalf("Expected 1 container creation, got %d", len(mockClient.CreatedContainers))
	}
	creation := mockClient.CreatedContainers[0]
	if creation.NewImage != newImage {
		t.Errorf("Expected new container image %s, got %s", newImage, creation.NewImage)
	}

	// 3. Renamed
	if len(mockClient.RenamedContainers) != 1 {
		t.Errorf("Expected 1 rename operation, got %d", len(mockClient.RenamedContainers))
	} else {
		if mockClient.RenamedContainers[0].NewName != targetName {
			t.Errorf("Expected rename to %s, got %s", targetName, mockClient.RenamedContainers[0].NewName)
		}
	}

	// 4. Started
	if len(mockClient.StartedContainers) != 1 {
		t.Errorf("Expected 1 start operation, got %d", len(mockClient.StartedContainers))
	}

	// 5. Verify Logs
	logs := logBuf.String()
	expectedSubstrings := []string{
		"Updater: 🔄 Started",
		"Updater: 🚀 Starting new container",
		"Updater: ✅ Update complete",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(logs, s) {
			t.Errorf("Log output missing expected string: %q", s)
		}
	}
}

func TestTrigger(t *testing.T) {
	// Trigger calls os.Exit, so we can't test it easily without subprocesses or mocking os.Exit.
	// We will skip testing Trigger directly in this unit test file.
}
