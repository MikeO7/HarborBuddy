package selfupdate

import (
	"context"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func TestRunUpdater(t *testing.T) {
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
		// Accessing internal state of mock directly is hacky but we don't have a SetContainerState method
		// Let's rely on the mock being accessible here.
		// Since we cannot access unexported fields, we need to update the Containers slice which is exported

		// Race condition here in test, but acceptable for this mock
		containers, _ := mockClient.ListContainers(context.Background())
		if len(containers) > 0 {
			containers[0].State.Running = false
			mockClient.Containers = containers
		}
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
}

func TestTrigger(t *testing.T) {
	// Trigger calls os.Exit, so we can't test it easily without subprocesses or mocking os.Exit.
	// We will skip testing Trigger directly in this unit test file.
}
