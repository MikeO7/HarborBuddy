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

func TestTrigger_Success(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	ctx := context.Background()

	myContainer := docker.ContainerInfo{
		ID:   "my-container-123",
		Name: "harborbuddy",
		Config: &container.Config{
			Image: "harborbuddy:old",
		},
	}
	newImage := "harborbuddy:latest"

	// Track exit call
	exitCalled := false
	exitCode := -1
	originalExitFunc := ExitFunc
	ExitFunc = func(code int) {
		exitCalled = true
		exitCode = code
	}
	defer func() { ExitFunc = originalExitFunc }()

	err := Trigger(ctx, mockClient, myContainer, newImage)
	// Trigger returns nil after calling exitFunc (which we mocked)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify exit was called with code 0
	if !exitCalled {
		t.Error("Expected exitFunc to be called")
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Verify helper was created
	if len(mockClient.CreatedHelpers) != 1 {
		t.Fatalf("Expected 1 helper creation, got %d", len(mockClient.CreatedHelpers))
	}

	helper := mockClient.CreatedHelpers[0]
	if helper.Image != newImage {
		t.Errorf("Expected helper image %s, got %s", newImage, helper.Image)
	}

	// Verify helper name contains container name
	if !strings.Contains(helper.Name, "harborbuddy-updater-") {
		t.Errorf("Expected helper name to contain 'harborbuddy-updater-', got %s", helper.Name)
	}

	// Verify command includes updater mode flags
	cmdStr := strings.Join(helper.Cmd, " ")
	if !strings.Contains(cmdStr, "--updater-mode") {
		t.Error("Expected command to include --updater-mode")
	}
	if !strings.Contains(cmdStr, "--target-container-id") {
		t.Error("Expected command to include --target-container-id")
	}
	if !strings.Contains(cmdStr, myContainer.ID) {
		t.Errorf("Expected command to include container ID %s", myContainer.ID)
	}

	// Verify helper was started
	if len(mockClient.StartedContainers) != 1 {
		t.Error("Expected helper container to be started")
	}

	// Verify logs
	logs := logBuf.String()
	if !strings.Contains(logs, "Self-Update: Triggering helper process") {
		t.Error("Expected trigger log message")
	}
}

func TestTrigger_CreateHelperFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	mockClient.CreateHelperContainerError = fmt.Errorf("failed to create helper")

	ctx := context.Background()
	myContainer := docker.ContainerInfo{
		ID:   "my-container-123",
		Name: "harborbuddy",
	}

	// Should NOT call exit if helper creation fails
	exitCalled := false
	originalExitFunc := ExitFunc
	ExitFunc = func(code int) {
		exitCalled = true
	}
	defer func() { ExitFunc = originalExitFunc }()

	err := Trigger(ctx, mockClient, myContainer, "harborbuddy:latest")
	if err == nil {
		t.Error("Expected error when helper creation fails")
	}
	if !strings.Contains(err.Error(), "failed to create helper") {
		t.Errorf("Expected create helper error, got: %v", err)
	}
	if exitCalled {
		t.Error("Exit should not be called when helper creation fails")
	}
}

func TestTrigger_StartHelperFails(t *testing.T) {
	var logBuf bytes.Buffer
	log.Initialize(log.Config{
		Level:  "info",
		Output: &logBuf,
	})

	mockClient := docker.NewMockDockerClient()
	mockClient.StartContainerError = fmt.Errorf("failed to start helper")

	ctx := context.Background()
	myContainer := docker.ContainerInfo{
		ID:   "my-container-123",
		Name: "harborbuddy",
	}

	// Should NOT call exit if helper start fails
	exitCalled := false
	originalExitFunc := ExitFunc
	ExitFunc = func(code int) {
		exitCalled = true
	}
	defer func() { ExitFunc = originalExitFunc }()

	err := Trigger(ctx, mockClient, myContainer, "harborbuddy:latest")
	if err == nil {
		t.Error("Expected error when helper start fails")
	}
	if !strings.Contains(err.Error(), "failed to start helper") {
		t.Errorf("Expected start helper error, got: %v", err)
	}
	if exitCalled {
		t.Error("Exit should not be called when helper start fails")
	}
}
