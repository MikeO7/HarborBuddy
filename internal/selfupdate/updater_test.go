package selfupdate

import (
	"bytes"
	"context"
	"fmt"
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

func TestRunUpdaterTimeout(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-never-stops"
	newImage := "harborbuddy:latest"

	// Setup container that never stops
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  "stuck-container",
			State: &types.ContainerState{Running: true},
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// Use a very short timeout for quick test
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err := RunUpdater(shortCtx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestRunUpdaterInspectFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "debug",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "missing-target"
	newImage := "harborbuddy:latest"

	// Container doesn't exist
	mockClient.Containers = []docker.ContainerInfo{}

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected error when inspecting non-existent container, got nil")
	}
}

func TestRunUpdaterRemoveFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-123"
	newImage := "harborbuddy:latest"

	// Setup container that stops immediately but removal fails
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  "test-container",
			State: &types.ContainerState{Running: false}, // Already stopped
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// Make removal fail
	mockClient.RemoveContainerError = fmt.Errorf("removal failed")

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected error when removal fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to remove old container") {
		t.Errorf("Expected removal error, got: %v", err)
	}
}

func TestRunUpdaterCreateFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-123"
	newImage := "harborbuddy:latest"

	// Setup container
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  "test-container",
			State: &types.ContainerState{Running: false},
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// Make creation fail
	mockClient.CreateContainerError = fmt.Errorf("creation failed")

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected error when creation fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create new container") {
		t.Errorf("Expected creation error, got: %v", err)
	}
}

func TestRunUpdaterRenameFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-123"
	newImage := "harborbuddy:latest"

	// Setup container
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  "test-container",
			State: &types.ContainerState{Running: false},
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// Make rename fail
	mockClient.RenameContainerError = fmt.Errorf("rename failed")

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected error when rename fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to rename new container") {
		t.Errorf("Expected rename error, got: %v", err)
	}

	// Verify cleanup attempted (temp container should be removed)
	if len(mockClient.RemovedContainers) < 2 {
		t.Error("Expected temp container to be cleaned up after rename failure")
	}
}

func TestRunUpdaterStartFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	targetID := "target-123"
	newImage := "harborbuddy:latest"

	// Setup container
	mockClient.Containers = []docker.ContainerInfo{
		{
			ID:    targetID,
			Name:  "test-container",
			State: &types.ContainerState{Running: false},
			Config: &container.Config{
				Image: "harborbuddy:old",
			},
		},
	}

	// Make start fail
	mockClient.StartContainerError = fmt.Errorf("start failed")

	err := RunUpdater(ctx, mockClient, targetID, newImage)
	if err == nil {
		t.Error("Expected error when start fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to start new container") {
		t.Errorf("Expected start error, got: %v", err)
	}
}

func TestTrigger(t *testing.T) {
	// Trigger calls os.Exit, so we can't test it easily without subprocesses or mocking os.Exit.
	// We will skip testing Trigger directly in this unit test file.
}
