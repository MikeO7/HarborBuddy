package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// ListContainers returns a list of all running containers
func (d *DockerClient) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		info, err := d.InspectContainer(ctx, c.ID)
		if err != nil {
			// Log but don't fail the entire list
			continue
		}
		result = append(result, info)
	}

	return result, nil
}

// InspectContainer returns detailed information about a container
func (d *DockerClient) InspectContainer(ctx context.Context, id string) (ContainerInfo, error) {
	inspect, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerInfo{}, fmt.Errorf("failed to inspect container %s: %w", id, err)
	}

	// Extract container name (remove leading /)
	name := strings.TrimPrefix(inspect.Name, "/")

	// Parse created time
	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	// Build networking config from current state
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: inspect.NetworkSettings.Networks,
	}

	return ContainerInfo{
		ID:            inspect.ID,
		Name:          name,
		Image:         inspect.Config.Image,
		ImageID:       inspect.Image,
		Labels:        inspect.Config.Labels,
		CreatedAt:     createdAt,
		Config:        inspect.Config,
		HostConfig:    inspect.HostConfig,
		NetworkConfig: networkConfig,
	}, nil
}

// StopContainer stops a container with the specified timeout
func (d *DockerClient) StopContainer(ctx context.Context, id string, timeout int) error {
	stopTimeout := timeout
	opts := container.StopOptions{
		Timeout: &stopTimeout,
	}

	if err := d.cli.ContainerStop(ctx, id, opts); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", id, err)
	}

	return nil
}

// StartContainer starts a container
func (d *DockerClient) StartContainer(ctx context.Context, id string) error {
	if err := d.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container %s: %w", id, err)
	}

	return nil
}

// RemoveContainer removes a container
func (d *DockerClient) RemoveContainer(ctx context.Context, id string) error {
	opts := container.RemoveOptions{
		Force: true,
	}

	if err := d.cli.ContainerRemove(ctx, id, opts); err != nil {
		return fmt.Errorf("failed to remove container %s: %w", id, err)
	}

	return nil
}

// CreateContainerLike creates a new container with the same configuration as the old one but with a new image
func (d *DockerClient) CreateContainerLike(ctx context.Context, old ContainerInfo, newImage string) (string, error) {
	// Clone the config to avoid modifying the original
	config := &container.Config{
		Hostname:        old.Config.Hostname,
		Domainname:      old.Config.Domainname,
		User:            old.Config.User,
		AttachStdin:     old.Config.AttachStdin,
		AttachStdout:    old.Config.AttachStdout,
		AttachStderr:    old.Config.AttachStderr,
		ExposedPorts:    old.Config.ExposedPorts,
		Tty:             old.Config.Tty,
		OpenStdin:       old.Config.OpenStdin,
		StdinOnce:       old.Config.StdinOnce,
		Env:             old.Config.Env,
		Cmd:             old.Config.Cmd,
		Image:           newImage, // Use the new image
		Volumes:         old.Config.Volumes,
		WorkingDir:      old.Config.WorkingDir,
		Entrypoint:      old.Config.Entrypoint,
		NetworkDisabled: old.Config.NetworkDisabled,
		MacAddress:      old.Config.MacAddress,
		OnBuild:         old.Config.OnBuild,
		Labels:          old.Config.Labels,
		StopSignal:      old.Config.StopSignal,
		StopTimeout:     old.Config.StopTimeout,
		Shell:           old.Config.Shell,
	}

	// Create the new container with a temporary name
	tempName := old.Name + "-new"
	resp, err := d.cli.ContainerCreate(ctx, config, old.HostConfig, old.NetworkConfig, nil, tempName)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

// ReplaceContainer replaces an old container with a new one using a blue-green approach
func (d *DockerClient) ReplaceContainer(ctx context.Context, oldID, newID, name string, stopTimeout time.Duration) error {
	backupName := fmt.Sprintf("%s-old-%d", name, time.Now().Unix())
	timeoutSec := int(stopTimeout.Seconds())

	// 1. Stop the old container
	if err := d.StopContainer(ctx, oldID, timeoutSec); err != nil {
		return fmt.Errorf("failed to stop old container: %w", err)
	}

	// 2. Rename the old container to a backup name
	if err := d.cli.ContainerRename(ctx, oldID, backupName); err != nil {
		// If rename fails, try to restart the old container to prevent downtime
		_ = d.StartContainer(ctx, oldID)
		return fmt.Errorf("failed to rename old container to backup name: %w", err)
	}

	// 3. Rename the new container to the original name
	if err := d.cli.ContainerRename(ctx, newID, name); err != nil {
		// Rollback: try to rename old container back
		_ = d.cli.ContainerRename(ctx, oldID, name)
		_ = d.StartContainer(ctx, oldID)
		// Cleanup the new container
		_ = d.RemoveContainer(ctx, newID)
		return fmt.Errorf("failed to rename new container: %w", err)
	}

	// 4. Start the new container
	if err := d.StartContainer(ctx, newID); err != nil {
		// Rollback: Stop new container, rename old one back, and restart it
		_ = d.StopContainer(ctx, newID, timeoutSec)
		_ = d.RemoveContainer(ctx, newID)
		_ = d.cli.ContainerRename(ctx, oldID, name)
		_ = d.StartContainer(ctx, oldID)
		return fmt.Errorf("failed to start new container: %w", err)
	}

	// 5. Success: Remove the old container
	if err := d.RemoveContainer(ctx, oldID); err != nil {
		// This is not a critical error, but should be logged
		// At this point, the service is up on the new container
		return fmt.Errorf("warning: failed to remove old backup container %s: %w", backupName, err)
	}

	return nil
}

// GetContainersUsingImage returns the IDs of containers using the specified image
func (d *DockerClient) GetContainersUsingImage(ctx context.Context, imageID string) ([]string, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("ancestor", imageID)

	containers, err := d.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers using image: %w", err)
	}

	var ids []string
	for _, c := range containers {
		ids = append(ids, c.ID)
	}

	return ids, nil
}
