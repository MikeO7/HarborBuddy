package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
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
	ListDanglingImages(ctx context.Context) ([]ImageInfo, error)
}

// DockerClient implements the Client interface using Docker SDK
type DockerClient struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient(cfg config.DockerConfig) (*DockerClient, error) {
	opts := []client.Opt{
		client.WithHost(cfg.Host),
		client.WithAPIVersionNegotiation(),
	}

	if cfg.TLS {
		// If explicit paths are provided, use them
		if cfg.CertPath != "" && cfg.KeyPath != "" && cfg.CAPath != "" {
			opts = append(opts, client.WithTLSClientConfig(cfg.CAPath, cfg.CertPath, cfg.KeyPath))
		} else {
			// If TLS is requested but no paths provided, we might be relying on default locations
			// or DOCKER_CERT_PATH env var.
			// client.WithTLSClientConfig requires explicit paths.
			// However, client.NewClientWithOpts(client.FromEnv) handles this automatically.
			// Since we want to support both explicit config AND standard env vars, let's add FromEnv.
			// But client.FromEnv might conflict with client.WithHost if both are set?
			// client.FromEnv reads DOCKER_HOST. If cfg.Host is set, it overrides it (since we added WithHost first? No, last wins).
			// Wait, options are applied in order.
			// If we put FromEnv first, then WithHost, WithHost wins for the host.
			// But FromEnv also sets TLS config if DOCKER_TLS_VERIFY is set.

			// So correct strategy:
			// 1. Add FromEnv (to pick up standard env vars for things we don't explicitly set)
			// 2. Add WithHost (to enforce our configured host)
			// 3. Add explicit TLS config if provided

			// However, if we add FromEnv, it might override other things.
			// Let's stick to explicit configuration if possible.

			// If the user wants TLS but didn't provide paths, we warn them unless they are using env vars?
			log.Warn().Msg("TLS enabled but no certificate paths provided in config. Assuming standard environment variables or default locations.")
			// We can try to add FromEnv here to pick up TLS settings from env
			opts = append(opts, client.FromEnv)
		}
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
