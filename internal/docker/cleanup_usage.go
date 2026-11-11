package docker

import (
	"context"
	"fmt"
	"strings"

	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// containerUsage records what every container, running or stopped, references.
// Docker's own counters are not enough for cleanup: network inspection lists
// only running containers, and image listings do not count stopped containers
// on every image store.
type containerUsage struct {
	images   map[string]struct{}
	networks map[string]struct{} // network IDs and names
}

func (d *DockerClient) listContainerUsage(ctx context.Context) (containerUsage, error) {
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return containerUsage{}, fmt.Errorf("list all containers for cleanup usage: %w", err)
	}
	usage := containerUsage{images: make(map[string]struct{}), networks: make(map[string]struct{})}
	for _, container := range listed.Items {
		addNonEmpty(usage.images, container.ImageID)
		addNonEmpty(usage.networks, container.HostConfig.NetworkMode)
		if container.NetworkSettings == nil {
			continue
		}
		for name, endpoint := range container.NetworkSettings.Networks {
			addNonEmpty(usage.networks, name)
			if endpoint != nil {
				addNonEmpty(usage.networks, endpoint.NetworkID)
			}
		}
	}
	return usage, nil
}

func (u containerUsage) usesImage(id string) bool {
	_, ok := u.images[id]
	return ok
}

func (u containerUsage) usesNetwork(id, name string) bool {
	_, byID := u.networks[id]
	_, byName := u.networks[name]
	return byID || byName
}

// parentImageIDs returns the images that another image is built on. Docker
// refuses to delete them, so cleanup treats them as in use.
func parentImageIDs(images []imagetypes.Summary) map[string]struct{} {
	parents := make(map[string]struct{})
	for _, image := range images {
		addNonEmpty(parents, image.ParentID)
	}
	return parents
}

// imageInUse reports whether any container or child image depends on imageID.
func (d *DockerClient) imageInUse(ctx context.Context, imageID string) (bool, error) {
	if inUse, err := d.imageUsedByAnyContainer(ctx, imageID); err != nil || inUse {
		return inUse, err
	}
	images, err := d.cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return false, fmt.Errorf("list images before image removal: %w", err)
	}
	_, hasChildren := parentImageIDs(images.Items)[imageID]
	return hasChildren, nil
}

// markImagesInUse sets InUse on every image a container or child image needs.
func (d *DockerClient) markImagesInUse(ctx context.Context, images []ImageInfo, all []imagetypes.Summary) ([]ImageInfo, error) {
	usage, err := d.listContainerUsage(ctx)
	if err != nil {
		return nil, err
	}
	parents := parentImageIDs(all)
	for index := range images {
		_, hasChildren := parents[images[index].ID]
		images[index].InUse = images[index].InUse || hasChildren || usage.usesImage(images[index].ID)
	}
	return images, nil
}

// repoDigests drops the placeholder Docker reports for an image with no digest.
func repoDigests(digests []string) []string {
	result := make([]string, 0, len(digests))
	for _, digest := range digests {
		if digest != "" && digest != "<none>@<none>" {
			result = append(result, digest)
		}
	}
	return result
}

// isBackupContainerName reports whether name carries the marker ReplaceContainer
// gives the original container while its replacement starts. A container that
// still carries it is the user's original after an interrupted or failed rollback.
func isBackupContainerName(name string) bool {
	return strings.Contains(name, backupNameMarker)
}

func addNonEmpty(set map[string]struct{}, value string) {
	if value != "" {
		set[value] = struct{}{}
	}
}
