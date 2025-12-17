package docker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
)

func TestNewMockDockerClient(t *testing.T) {
	mock := NewMockDockerClient()

	if mock == nil {
		t.Fatal("NewMockDockerClient returned nil")
	}

	if mock.Containers == nil {
		t.Error("Containers slice should be initialized")
	}

	if mock.Images == nil {
		t.Error("Images slice should be initialized")
	}

	if mock.PullImageReturns == nil {
		t.Error("PullImageReturns map should be initialized")
	}
}

func TestMockDockerClient_ListContainers(t *testing.T) {
	t.Run("returns configured containers", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Containers = []ContainerInfo{
			{ID: "abc123", Name: "test1"},
			{ID: "def456", Name: "test2"},
		}

		containers, err := mock.ListContainers(context.Background())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(containers) != 2 {
			t.Errorf("Expected 2 containers, got %d", len(containers))
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.ListContainersError = fmt.Errorf("mock error")

		_, err := mock.ListContainers(context.Background())
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_InspectContainer(t *testing.T) {
	t.Run("returns container by ID", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Containers = []ContainerInfo{
			{ID: "abc123", Name: "test1"},
		}

		container, err := mock.InspectContainer(context.Background(), "abc123")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if container.Name != "test1" {
			t.Errorf("Expected name 'test1', got '%s'", container.Name)
		}
	})

	t.Run("returns error for missing container", func(t *testing.T) {
		mock := NewMockDockerClient()

		_, err := mock.InspectContainer(context.Background(), "missing")
		if err == nil {
			t.Error("Expected error for missing container")
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.InspectContainerError = fmt.Errorf("inspect failed")

		_, err := mock.InspectContainer(context.Background(), "any")
		if err == nil {
			t.Error("Expected configured error")
		}
	})
}

func TestMockDockerClient_PullImage(t *testing.T) {
	t.Run("records pull and returns default info", func(t *testing.T) {
		mock := NewMockDockerClient()

		info, err := mock.PullImage(context.Background(), "nginx:latest")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.PulledImages) != 1 || mock.PulledImages[0] != "nginx:latest" {
			t.Errorf("Expected pull to be recorded, got %v", mock.PulledImages)
		}

		if info.ID != "sha256:new-nginx:latest" {
			t.Errorf("Expected default ID, got %s", info.ID)
		}
	})

	t.Run("returns configured image info", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.PullImageReturns["nginx:latest"] = ImageInfo{
			ID:       "sha256:custom",
			RepoTags: []string{"nginx:latest"},
		}

		info, err := mock.PullImage(context.Background(), "nginx:latest")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if info.ID != "sha256:custom" {
			t.Errorf("Expected custom ID, got %s", info.ID)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.PullImageError = fmt.Errorf("pull failed")

		_, err := mock.PullImage(context.Background(), "nginx:latest")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_ListImages(t *testing.T) {
	t.Run("returns configured images", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Images = []ImageInfo{
			{ID: "img1", Dangling: false},
			{ID: "img2", Dangling: true},
		}

		images, err := mock.ListImages(context.Background())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(images) != 2 {
			t.Errorf("Expected 2 images, got %d", len(images))
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.ListImagesError = fmt.Errorf("list failed")

		_, err := mock.ListImages(context.Background())
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_RemoveImage(t *testing.T) {
	t.Run("records removal", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.RemoveImage(context.Background(), "img123")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.RemovedImages) != 1 || mock.RemovedImages[0] != "img123" {
			t.Errorf("Expected removal to be recorded, got %v", mock.RemovedImages)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.RemoveImageError = fmt.Errorf("remove failed")

		err := mock.RemoveImage(context.Background(), "img123")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_StopContainer(t *testing.T) {
	t.Run("records stop", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.StopContainer(context.Background(), "container123", 10)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.StoppedContainers) != 1 || mock.StoppedContainers[0] != "container123" {
			t.Errorf("Expected stop to be recorded, got %v", mock.StoppedContainers)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.StopContainerError = fmt.Errorf("stop failed")

		err := mock.StopContainer(context.Background(), "container123", 10)
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_StartContainer(t *testing.T) {
	t.Run("records start", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.StartContainer(context.Background(), "container123")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.StartedContainers) != 1 || mock.StartedContainers[0] != "container123" {
			t.Errorf("Expected start to be recorded, got %v", mock.StartedContainers)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.StartContainerError = fmt.Errorf("start failed")

		err := mock.StartContainer(context.Background(), "container123")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_RemoveContainer(t *testing.T) {
	t.Run("records removal", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.RemoveContainer(context.Background(), "container123")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.RemovedContainers) != 1 || mock.RemovedContainers[0] != "container123" {
			t.Errorf("Expected removal to be recorded, got %v", mock.RemovedContainers)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.RemoveContainerError = fmt.Errorf("remove failed")

		err := mock.RemoveContainer(context.Background(), "container123")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_CreateContainerLike(t *testing.T) {
	t.Run("records creation and returns ID", func(t *testing.T) {
		mock := NewMockDockerClient()
		oldContainer := ContainerInfo{ID: "old123", Name: "test-container"}

		newID, err := mock.CreateContainerLike(context.Background(), oldContainer, "nginx:latest")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if newID != "new-container-id-test-container" {
			t.Errorf("Expected generated ID, got %s", newID)
		}

		if len(mock.CreatedContainers) != 1 {
			t.Errorf("Expected creation to be recorded")
		}
		if mock.CreatedContainers[0].NewImage != "nginx:latest" {
			t.Errorf("Expected image to be recorded")
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.CreateContainerError = fmt.Errorf("create failed")

		_, err := mock.CreateContainerLike(context.Background(), ContainerInfo{}, "nginx:latest")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_ReplaceContainer(t *testing.T) {
	t.Run("records replacement", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.ReplaceContainer(context.Background(), "old123", "new456", "test-container", 10*time.Second)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.ReplacedContainers) != 1 {
			t.Errorf("Expected replacement to be recorded")
		}
		req := mock.ReplacedContainers[0]
		if req.OldID != "old123" || req.NewID != "new456" || req.Name != "test-container" {
			t.Errorf("Replacement not recorded correctly: %+v", req)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.ReplaceContainerError = fmt.Errorf("replace failed")

		err := mock.ReplaceContainer(context.Background(), "old", "new", "name", time.Second)
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_GetContainersUsingImage(t *testing.T) {
	t.Run("returns containers using image", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Containers = []ContainerInfo{
			{ID: "c1", ImageID: "img1"},
			{ID: "c2", ImageID: "img2"},
			{ID: "c3", ImageID: "img1"},
		}

		containers, err := mock.GetContainersUsingImage(context.Background(), "img1")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(containers) != 2 {
			t.Errorf("Expected 2 containers, got %d", len(containers))
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.GetContainersUsingImageError = fmt.Errorf("get failed")

		_, err := mock.GetContainersUsingImage(context.Background(), "img1")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_ListDanglingImages(t *testing.T) {
	t.Run("returns only dangling images", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Images = []ImageInfo{
			{ID: "img1", Dangling: false},
			{ID: "img2", Dangling: true},
			{ID: "img3", Dangling: true},
		}

		images, err := mock.ListDanglingImages(context.Background())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(images) != 2 {
			t.Errorf("Expected 2 dangling images, got %d", len(images))
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.ListDanglingImagesError = fmt.Errorf("list failed")

		_, err := mock.ListDanglingImages(context.Background())
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_RenameContainer(t *testing.T) {
	t.Run("records rename", func(t *testing.T) {
		mock := NewMockDockerClient()

		err := mock.RenameContainer(context.Background(), "container123", "new-name")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(mock.RenamedContainers) != 1 {
			t.Errorf("Expected rename to be recorded")
		}
		if mock.RenamedContainers[0].ID != "container123" || mock.RenamedContainers[0].NewName != "new-name" {
			t.Errorf("Rename not recorded correctly")
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.RenameContainerError = fmt.Errorf("rename failed")

		err := mock.RenameContainer(context.Background(), "container", "new-name")
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_CreateHelperContainer(t *testing.T) {
	t.Run("records helper creation", func(t *testing.T) {
		mock := NewMockDockerClient()
		original := ContainerInfo{ID: "orig123", Name: "original"}
		cmd := []string{"/app/harborbuddy", "--updater-mode"}

		helperID, err := mock.CreateHelperContainer(context.Background(), original, "img:latest", "helper-name", cmd)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if helperID != "helper-container-id-helper-name" {
			t.Errorf("Expected generated helper ID, got %s", helperID)
		}

		if len(mock.CreatedHelpers) != 1 {
			t.Errorf("Expected helper creation to be recorded")
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.CreateHelperContainerError = fmt.Errorf("create helper failed")

		_, err := mock.CreateHelperContainer(context.Background(), ContainerInfo{}, "img", "name", nil)
		if err == nil {
			t.Error("Expected error")
		}
	})
}

func TestMockDockerClient_Close(t *testing.T) {
	mock := NewMockDockerClient()
	err := mock.Close()
	if err != nil {
		t.Errorf("Close should return nil, got %v", err)
	}
}

func TestMockDockerClient_Reset(t *testing.T) {
	mock := NewMockDockerClient()

	// Add some records
	mock.PulledImages = append(mock.PulledImages, "img1")
	mock.RemovedImages = append(mock.RemovedImages, "img1")
	mock.StoppedContainers = append(mock.StoppedContainers, "c1")
	mock.StartedContainers = append(mock.StartedContainers, "c1")
	mock.RemovedContainers = append(mock.RemovedContainers, "c1")
	mock.CreatedContainers = append(mock.CreatedContainers, CreateRequest{})
	mock.ReplacedContainers = append(mock.ReplacedContainers, ReplaceRequest{})
	mock.RenamedContainers = append(mock.RenamedContainers, RenameRequest{})
	mock.CreatedHelpers = append(mock.CreatedHelpers, CreateHelperRequest{})

	mock.Reset()

	if len(mock.PulledImages) != 0 {
		t.Error("PulledImages should be empty after reset")
	}
	if len(mock.RemovedImages) != 0 {
		t.Error("RemovedImages should be empty after reset")
	}
	if len(mock.StoppedContainers) != 0 {
		t.Error("StoppedContainers should be empty after reset")
	}
	if len(mock.StartedContainers) != 0 {
		t.Error("StartedContainers should be empty after reset")
	}
	if len(mock.RemovedContainers) != 0 {
		t.Error("RemovedContainers should be empty after reset")
	}
	if len(mock.CreatedContainers) != 0 {
		t.Error("CreatedContainers should be empty after reset")
	}
	if len(mock.ReplacedContainers) != 0 {
		t.Error("ReplacedContainers should be empty after reset")
	}
	if len(mock.RenamedContainers) != 0 {
		t.Error("RenamedContainers should be empty after reset")
	}
	if len(mock.CreatedHelpers) != 0 {
		t.Error("CreatedHelpers should be empty after reset")
	}
}

func TestMockDockerClient_SetContainerState(t *testing.T) {
	t.Run("updates existing container state", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Containers = []ContainerInfo{
			{ID: "c1", State: &types.ContainerState{Running: true}},
		}

		mock.SetContainerState("c1", false)

		if mock.Containers[0].State.Running {
			t.Error("Container state should be stopped")
		}
	})

	t.Run("does nothing for missing container", func(t *testing.T) {
		mock := NewMockDockerClient()
		mock.Containers = []ContainerInfo{
			{ID: "c1", State: &types.ContainerState{Running: true}},
		}

		// Should not panic
		mock.SetContainerState("nonexistent", false)

		// Original container should be unchanged
		if !mock.Containers[0].State.Running {
			t.Error("Container state should remain running")
		}
	})
}
