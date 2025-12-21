package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/client"
)

// Client is the interface for Docker operations
type Client interface {
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	InspectContainer(ctx context.Context, id string) (ContainerInfo, error)
	PullImage(ctx context.Context, image string) (ImageInfo, error)
	ListImages(ctx context.Context) ([]ImageInfo, error)
	RemoveImage(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout int) error
	StartContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	CreateContainerLike(ctx context.Context, old ContainerInfo, newImage string) (string, error)
	ReplaceContainer(ctx context.Context, oldID, newID, name string, stopTimeout time.Duration) error
	GetContainersUsingImage(ctx context.Context, imageID string) ([]string, error)
	RenameContainer(ctx context.Context, id, newName string) error
	CreateHelperContainer(ctx context.Context, original ContainerInfo, image, name string, cmd []string) (string, error)

	// Image functions
	InspectImage(ctx context.Context, image string) (ImageInfo, error)
	ListDanglingImages(ctx context.Context) ([]ImageInfo, error)
}

// DockerClient implements the Client interface using Docker SDK
type DockerClient struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient(host string) (*DockerClient, error) {
	opts := []client.Opt{
		client.WithHost(host),
		client.WithTLSClientConfigFromEnv(),
		client.WithAPIVersionNegotiation(),
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Test connection
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping docker daemon: %w", err)
	}

	return &DockerClient{cli: cli}, nil
}

// Close closes the Docker client connection
func (d *DockerClient) Close() error {
	if d.cli != nil {
		return d.cli.Close()
	}
	return nil
}
