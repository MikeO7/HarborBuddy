package docker

import (
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ContainerInfo holds information about a Docker container
type ContainerInfo struct {
	ID        string
	Name      string
	Image     string
	ImageID   string
	Labels    map[string]string
	CreatedAt time.Time
	State     *types.ContainerState

	// Config needed for recreation
	Config        *container.Config
	HostConfig    *container.HostConfig
	NetworkConfig *network.NetworkingConfig
}

// ImageInfo holds information about a Docker image
type ImageInfo struct {
	ID        string
	RepoTags  []string
	Dangling  bool
	CreatedAt time.Time
	Size      int64
}
