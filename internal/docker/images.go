package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// PullImage pulls the latest version of an image
func (d *DockerClient) PullImage(ctx context.Context, imageName string) (ImageInfo, error) {
	reader, err := d.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Consume the pull output
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to read pull output for %s: %w", imageName, err)
	}

	// Inspect the pulled image to get its ID
	inspect, _, err := d.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to inspect pulled image %s: %w", imageName, err)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	// Convert ImageConfig to container.Config
	var imageConfig *container.Config
	if inspect.Config != nil {
		imageConfig = &container.Config{
			User:       inspect.Config.User,
			Env:        inspect.Config.Env,
			Entrypoint: inspect.Config.Entrypoint,
			Cmd:        inspect.Config.Cmd,
			WorkingDir: inspect.Config.WorkingDir,
			Labels:     inspect.Config.Labels,
			StopSignal: inspect.Config.StopSignal,
		}
	}

	return ImageInfo{
		ID:        inspect.ID,
		RepoTags:  inspect.RepoTags,
		Dangling:  len(inspect.RepoTags) == 0,
		CreatedAt: createdAt,
		Size:      inspect.Size,
		Config:    imageConfig,
	}, nil
}

// InspectImage returns detailed information about an image
// This is essentially same as PullImage's internal inspect but exposed directly
func (d *DockerClient) InspectImage(ctx context.Context, imageName string) (ImageInfo, error) {
	inspect, _, err := d.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	// Convert ImageConfig to container.Config
	var imageConfig *container.Config
	if inspect.Config != nil {
		imageConfig = &container.Config{
			User:       inspect.Config.User,
			Env:        inspect.Config.Env,
			Entrypoint: inspect.Config.Entrypoint,
			Cmd:        inspect.Config.Cmd,
			WorkingDir: inspect.Config.WorkingDir,
			Labels:     inspect.Config.Labels,
			StopSignal: inspect.Config.StopSignal,
		}
	}

	return ImageInfo{
		ID:        inspect.ID,
		RepoTags:  inspect.RepoTags,
		Dangling:  len(inspect.RepoTags) == 0,
		CreatedAt: createdAt,
		Size:      inspect.Size,
		Config:    imageConfig,
	}, nil
}

// ListImages returns a list of all images
func (d *DockerClient) ListImages(ctx context.Context) ([]ImageInfo, error) {
	images, err := d.cli.ImageList(ctx, image.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	result := make([]ImageInfo, 0, len(images))
	for _, img := range images {
		result = append(result, ImageInfo{
			ID:        img.ID,
			RepoTags:  img.RepoTags,
			Dangling:  len(img.RepoTags) == 0 || (len(img.RepoTags) == 1 && img.RepoTags[0] == "<none>:<none>"),
			CreatedAt: time.Unix(img.Created, 0),
			Size:      img.Size,
		})
	}

	return result, nil
}

// ListDanglingImages returns a list of dangling images
func (d *DockerClient) ListDanglingImages(ctx context.Context) ([]ImageInfo, error) {
	filters := filters.NewArgs()
	filters.Add("dangling", "true")

	images, err := d.cli.ImageList(ctx, image.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list dangling images: %w", err)
	}

	result := make([]ImageInfo, 0, len(images))
	for _, img := range images {
		result = append(result, ImageInfo{
			ID:        img.ID,
			RepoTags:  img.RepoTags,
			Dangling:  true,
			CreatedAt: time.Unix(img.Created, 0),
			Size:      img.Size,
		})
	}

	return result, nil
}

// RemoveImage removes an image by ID
func (d *DockerClient) RemoveImage(ctx context.Context, imageID string) error {
	_, err := d.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         false, // Don't force remove images in use
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}

	return nil
}

// GetImageID gets the ID of an image by name
func (d *DockerClient) GetImageID(ctx context.Context, imageName string) (string, error) {
	inspect, _, err := d.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	return inspect.ID, nil
}
