package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func (d *DockerClient) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("list running containers: %w", err)
	}
	containers := listed.Items

	result := make([]ContainerSummary, 0, len(containers))
	for _, current := range containers {
		name := ""
		if len(current.Names) > 0 {
			name = strings.TrimPrefix(current.Names[0], "/")
		}
		result = append(result, ContainerSummary{
			ID:        current.ID,
			Name:      name,
			ImageRef:  d.containerImageRef(ctx, current),
			ImageID:   current.ImageID,
			Labels:    current.Labels,
			CreatedAt: time.Unix(current.Created, 0),
		})
	}
	return result, nil
}

// containerImageRef recovers the configured reference after a pull moves its
// local tag to a newer image. In that state, Docker's list endpoint reports the
// running image ID, while container inspection retains Config.Image.
func (d *DockerClient) containerImageRef(ctx context.Context, current containertypes.Summary) string {
	if current.Image != current.ImageID && !strings.HasPrefix(current.Image, "sha256:") {
		return current.Image
	}
	result, err := d.cli.ContainerInspect(ctx, current.ID, client.ContainerInspectOptions{})
	if err == nil && result.Container.Config != nil && result.Container.Config.Image != "" {
		return result.Container.Config.Image
	}
	return current.Image
}

func (d *DockerClient) InspectContainer(ctx context.Context, id string) (ContainerDetails, error) {
	result, err := d.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("inspect container %s: %w", id, err)
	}
	inspect := result.Container

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)
	labels := map[string]string(nil)
	imageRef := ""
	if inspect.Config != nil {
		labels = inspect.Config.Labels
		imageRef = inspect.Config.Image
	}

	var networks map[string]*network.EndpointSettings
	if inspect.NetworkSettings != nil {
		networks = inspect.NetworkSettings.Networks
	}

	return ContainerDetails{
		Summary: ContainerSummary{
			ID:        inspect.ID,
			Name:      strings.TrimPrefix(inspect.Name, "/"),
			ImageRef:  imageRef,
			ImageID:   inspect.Image,
			Labels:    labels,
			CreatedAt: createdAt,
		},
		Config:   inspect.Config,
		Host:     inspect.HostConfig,
		Mounts:   append([]containertypes.MountPoint(nil), inspect.Mounts...),
		Networks: networks,
		State:    inspect.State,
	}, nil
}
