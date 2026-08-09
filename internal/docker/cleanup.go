package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func (d *DockerClient) ListCleanupResources(ctx context.Context, kind CleanupResourceKind) ([]CleanupResource, error) {
	switch kind {
	case CleanupImage:
		return d.listCleanupImages(ctx)
	case CleanupContainer:
		return d.listCleanupContainers(ctx)
	case CleanupNetwork:
		return d.listCleanupNetworks(ctx)
	case CleanupVolume:
		return d.listCleanupVolumes(ctx)
	case CleanupBuildCache:
		return d.listCleanupBuildCache(ctx)
	default:
		return nil, fmt.Errorf("unsupported cleanup resource kind %q", kind)
	}
}

func (d *DockerClient) RemoveCleanupResource(ctx context.Context, resource CleanupResource) error {
	switch resource.Kind {
	case CleanupImage:
		return d.RemoveImage(ctx, resource.ID)
	case CleanupContainer:
		if _, err := d.cli.ContainerRemove(ctx, resource.ID, client.ContainerRemoveOptions{}); err != nil {
			return fmt.Errorf("remove stopped container %s: %w", resourceLabel(resource), err)
		}
	case CleanupNetwork:
		if _, err := d.cli.NetworkRemove(ctx, resource.ID, client.NetworkRemoveOptions{}); err != nil {
			return fmt.Errorf("remove unused network %s: %w", resourceLabel(resource), err)
		}
	case CleanupVolume:
		if _, err := d.cli.VolumeRemove(ctx, resource.ID, client.VolumeRemoveOptions{}); err != nil {
			return fmt.Errorf("remove unused volume %s: %w", resourceLabel(resource), err)
		}
	case CleanupBuildCache:
		return errors.New("build cache cannot be removed individually")
	default:
		return fmt.Errorf("resource kind %q cannot be removed individually", resource.Kind)
	}
	return nil
}

func (d *DockerClient) PruneBuildCache(ctx context.Context, before time.Time) (CleanupPruneResult, error) {
	filters := make(client.Filters).Add("until", before.UTC().Format(time.RFC3339Nano))
	result, err := d.cli.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: true, Filters: filters})
	if err != nil {
		return CleanupPruneResult{}, fmt.Errorf("prune build cache: %w", err)
	}
	return CleanupPruneResult{
		Deleted:        append([]string(nil), result.Report.CachesDeleted...),
		ReclaimedBytes: int64(result.Report.SpaceReclaimed), //nolint:gosec // Docker reports practical disk sizes.
	}, nil
}

func (d *DockerClient) listCleanupImages(ctx context.Context) ([]CleanupResource, error) {
	usage, err := d.cli.DiskUsage(ctx, client.DiskUsageOptions{Images: true, Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("list images for cleanup: %w", err)
	}
	resources := make([]CleanupResource, 0, len(usage.Images.Items))
	for _, image := range usage.Images.Items {
		dangling := len(image.RepoTags) == 0 || (len(image.RepoTags) == 1 && image.RepoTags[0] == "<none>:<none>")
		resources = append(resources, CleanupResource{
			Kind:      CleanupImage,
			ID:        image.ID,
			Name:      strings.Join(image.RepoTags, ","),
			CreatedAt: time.Unix(image.Created, 0),
			Size:      nonNegative(image.Size),
			Dangling:  dangling,
			InUse:     image.Containers > 0,
			Protected: hasRollbackImageTag(image.RepoTags),
		})
	}
	return resources, nil
}

func hasRollbackImageTag(tags []string) bool {
	for _, tag := range tags {
		if strings.HasPrefix(tag, rollbackImageRepositoryPrefix) {
			return true
		}
	}
	return false
}

func (d *DockerClient) listCleanupContainers(ctx context.Context) ([]CleanupResource, error) {
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Size: true})
	if err != nil {
		return nil, fmt.Errorf("list stopped containers for cleanup: %w", err)
	}
	resources := make([]CleanupResource, 0, len(listed.Items))
	for _, container := range listed.Items {
		resources = append(resources, CleanupResource{
			Kind:      CleanupContainer,
			ID:        container.ID,
			Name:      firstContainerName(container.Names),
			CreatedAt: time.Unix(container.Created, 0),
			Size:      nonNegative(container.SizeRw),
			InUse:     !removableContainerState(container.State),
		})
	}
	return resources, nil
}

func (d *DockerClient) listCleanupNetworks(ctx context.Context) ([]CleanupResource, error) {
	listed, err := d.cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list networks for cleanup: %w", err)
	}
	resources := make([]CleanupResource, 0, len(listed.Items))
	for _, network := range listed.Items {
		protected := network.Scope != "local" || network.Ingress || network.ConfigOnly || isDefaultNetwork(network.Name)
		inUse := true
		if !protected {
			inspected, inspectErr := d.cli.NetworkInspect(ctx, network.ID, client.NetworkInspectOptions{})
			if inspectErr != nil {
				return nil, fmt.Errorf("inspect network %s for cleanup: %w", network.Name, inspectErr)
			}
			inUse = len(inspected.Network.Containers) > 0 || len(inspected.Network.Services) > 0
		}
		resources = append(resources, CleanupResource{
			Kind: CleanupNetwork, ID: network.ID, Name: network.Name,
			CreatedAt: network.Created, InUse: inUse, Protected: protected,
		})
	}
	return resources, nil
}

func (d *DockerClient) listCleanupVolumes(ctx context.Context) ([]CleanupResource, error) {
	usage, err := d.cli.DiskUsage(ctx, client.DiskUsageOptions{Volumes: true, Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("list volumes for cleanup: %w", err)
	}
	resources := make([]CleanupResource, 0, len(usage.Volumes.Items))
	for _, volume := range usage.Volumes.Items {
		created, parseErr := time.Parse(time.RFC3339Nano, volume.CreatedAt)
		unknownUsage := volume.UsageData == nil || volume.UsageData.RefCount < 0
		inUse := unknownUsage || volume.UsageData.RefCount > 0
		size := int64(0)
		if volume.UsageData != nil {
			size = nonNegative(volume.UsageData.Size)
		}
		resources = append(resources, CleanupResource{
			Kind: CleanupVolume, ID: volume.Name, Name: volume.Name, CreatedAt: created,
			Size: size, InUse: inUse, Protected: parseErr != nil || unknownUsage,
		})
	}
	return resources, nil
}

func (d *DockerClient) listCleanupBuildCache(ctx context.Context) ([]CleanupResource, error) {
	usage, err := d.cli.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("list build cache for cleanup: %w", err)
	}
	resources := make([]CleanupResource, 0, len(usage.BuildCache.Items))
	for _, cache := range usage.BuildCache.Items {
		lastUsed := time.Time{}
		if cache.LastUsedAt != nil {
			lastUsed = *cache.LastUsedAt
		}
		resources = append(resources, CleanupResource{
			Kind: CleanupBuildCache, ID: cache.ID, Name: cache.Description,
			CreatedAt: cache.CreatedAt, LastUsedAt: lastUsed,
			Size: nonNegative(cache.Size), InUse: cache.InUse,
		})
	}
	return resources, nil
}

func removableContainerState(state containertypes.ContainerState) bool {
	return state == containertypes.StateCreated || state == containertypes.StateExited || state == containertypes.StateDead
}

func firstContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func isDefaultNetwork(name string) bool {
	return name == "bridge" || name == "host" || name == "none"
}

func resourceLabel(resource CleanupResource) string {
	if resource.Name != "" {
		return resource.Name
	}
	return resource.ID
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
