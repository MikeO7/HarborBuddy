package docker

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockDockerClient is a mock implementation of the Client interface for testing
type MockDockerClient struct {
	mu sync.Mutex

	// Containers to return from ListContainers
	Containers []ContainerInfo

	// Images to return from ListImages
	Images []ImageInfo

	// Record of operations for verification
	PulledImages       []string
	RemovedImages      []string
	StoppedContainers  []string
	StartedContainers  []string
	RemovedContainers  []string
	CreatedContainers  []CreateRequest
	ReplacedContainers []ReplaceRequest
	RenamedContainers  []RenameRequest
	CreatedHelpers     []CreateHelperRequest

	// Control behavior
	ListContainersError          error
	InspectContainerError        error
	PullImageError               error
	ListImagesError              error
	RemoveImageError             error
	StopContainerError           error
	CreateContainerError         error
	StartContainerError          error
	RemoveContainerError         error
	ReplaceContainerError        error
	GetContainersUsingImageError error
	ListDanglingImagesError      error
	RenameContainerError         error
	CreateHelperContainerError   error

	// Image pull simulation
	PullImageReturns map[string]ImageInfo
}

// CreateRequest records container creation attempts
type CreateRequest struct {
	OldContainer ContainerInfo
	NewImage     string
}

// ReplaceRequest records container replacement attempts
type ReplaceRequest struct {
	OldID       string
	NewID       string
	Name        string
	StopTimeout time.Duration
}

// RenameRequest records container rename attempts
type RenameRequest struct {
	ID      string
	NewName string
}

// CreateHelperRequest records helper creation attempts
type CreateHelperRequest struct {
	Original ContainerInfo
	Image    string
	Name     string
	Cmd      []string
}

// NewMockDockerClient creates a new mock Docker client
func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		Containers:       []ContainerInfo{},
		Images:           []ImageInfo{},
		PullImageReturns: make(map[string]ImageInfo),
	}
}

// ListContainers returns the configured containers
func (m *MockDockerClient) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ListContainersError != nil {
		return nil, m.ListContainersError
	}
	return m.Containers, nil
}

// InspectContainer returns a container by ID
func (m *MockDockerClient) InspectContainer(ctx context.Context, id string) (ContainerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.InspectContainerError != nil {
		return ContainerInfo{}, m.InspectContainerError
	}

	for _, c := range m.Containers {
		if c.ID == id {
			return c, nil
		}
	}

	return ContainerInfo{}, fmt.Errorf("container not found: %s", id)
}

// PullImage simulates pulling an image
func (m *MockDockerClient) PullImage(ctx context.Context, image string) (ImageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PulledImages = append(m.PulledImages, image)

	if m.PullImageError != nil {
		return ImageInfo{}, m.PullImageError
	}

	if img, ok := m.PullImageReturns[image]; ok {
		return img, nil
	}

	return ImageInfo{
		ID:       "sha256:new-" + image,
		RepoTags: []string{image},
	}, nil
}

// ListImages returns the configured images
func (m *MockDockerClient) ListImages(ctx context.Context) ([]ImageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ListImagesError != nil {
		return nil, m.ListImagesError
	}
	return m.Images, nil
}

// RemoveImage records the removal
func (m *MockDockerClient) RemoveImage(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RemovedImages = append(m.RemovedImages, id)

	if m.RemoveImageError != nil {
		return m.RemoveImageError
	}
	return nil
}

// StopContainer records the stop
func (m *MockDockerClient) StopContainer(ctx context.Context, id string, timeout int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StoppedContainers = append(m.StoppedContainers, id)

	if m.StopContainerError != nil {
		return m.StopContainerError
	}
	return nil
}

// CreateContainerLike records the creation
func (m *MockDockerClient) CreateContainerLike(ctx context.Context, old ContainerInfo, newImage string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatedContainers = append(m.CreatedContainers, CreateRequest{
		OldContainer: old,
		NewImage:     newImage,
	})

	if m.CreateContainerError != nil {
		return "", m.CreateContainerError
	}

	return "new-container-id-" + old.Name, nil
}

// StartContainer records the start
func (m *MockDockerClient) StartContainer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StartedContainers = append(m.StartedContainers, id)

	if m.StartContainerError != nil {
		return m.StartContainerError
	}
	return nil
}

// RemoveContainer records the removal
func (m *MockDockerClient) RemoveContainer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RemovedContainers = append(m.RemovedContainers, id)

	if m.RemoveContainerError != nil {
		return m.RemoveContainerError
	}
	return nil
}

// ReplaceContainer records the replacement
func (m *MockDockerClient) ReplaceContainer(ctx context.Context, oldID, newID, name string, stopTimeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ReplacedContainers = append(m.ReplacedContainers, ReplaceRequest{
		OldID:       oldID,
		NewID:       newID,
		Name:        name,
		StopTimeout: stopTimeout,
	})

	if m.ReplaceContainerError != nil {
		return m.ReplaceContainerError
	}
	return nil
}

// GetContainersUsingImage returns list of containers using image
func (m *MockDockerClient) GetContainersUsingImage(ctx context.Context, imageID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.GetContainersUsingImageError != nil {
		return nil, m.GetContainersUsingImageError
	}

	var ids []string
	for _, c := range m.Containers {
		if c.ImageID == imageID {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

// ListDanglingImages returns list of dangling images
func (m *MockDockerClient) ListDanglingImages(ctx context.Context) ([]ImageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ListDanglingImagesError != nil {
		return nil, m.ListDanglingImagesError
	}

	var dangling []ImageInfo
	for _, img := range m.Images {
		if img.Dangling {
			dangling = append(dangling, img)
		}
	}
	return dangling, nil
}

// RenameContainer records the rename
func (m *MockDockerClient) RenameContainer(ctx context.Context, id, newName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RenamedContainers = append(m.RenamedContainers, RenameRequest{
		ID:      id,
		NewName: newName,
	})

	if m.RenameContainerError != nil {
		return m.RenameContainerError
	}
	return nil
}

// CreateHelperContainer records the helper creation
func (m *MockDockerClient) CreateHelperContainer(ctx context.Context, original ContainerInfo, image, name string, cmd []string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatedHelpers = append(m.CreatedHelpers, CreateHelperRequest{
		Original: original,
		Image:    image,
		Name:     name,
		Cmd:      cmd,
	})

	if m.CreateHelperContainerError != nil {
		return "", m.CreateHelperContainerError
	}

	return "helper-container-id-" + name, nil
}

// Close does nothing for the mock
func (m *MockDockerClient) Close() error {
	return nil
}

// Reset clears all recorded operations
func (m *MockDockerClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PulledImages = []string{}
	m.RemovedImages = []string{}
	m.StoppedContainers = []string{}
	m.StartedContainers = []string{}
	m.RemovedContainers = []string{}
	m.CreatedContainers = []CreateRequest{}
	m.ReplacedContainers = []ReplaceRequest{}
	m.RenamedContainers = []RenameRequest{}
	m.CreatedHelpers = []CreateHelperRequest{}
}
