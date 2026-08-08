package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func (d *DockerClient) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	containers, err := d.cli.ContainerList(ctx, containertypes.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("list running containers: %w", err)
	}

	result := make([]ContainerSummary, 0, len(containers))
	for _, current := range containers {
		name := ""
		if len(current.Names) > 0 {
			name = strings.TrimPrefix(current.Names[0], "/")
		}
		result = append(result, ContainerSummary{
			ID:        current.ID,
			Name:      name,
			ImageRef:  current.Image,
			ImageID:   current.ImageID,
			Labels:    current.Labels,
			CreatedAt: time.Unix(current.Created, 0),
		})
	}
	return result, nil
}

func (d *DockerClient) InspectContainer(ctx context.Context, id string) (ContainerDetails, error) {
	inspect, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerDetails{}, fmt.Errorf("inspect container %s: %w", id, err)
	}

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
