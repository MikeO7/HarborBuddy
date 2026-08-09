package docker

import (
	"context"
	"net/netip"
	"reflect"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func (d *DockerClient) replacementConfig(ctx context.Context, current ContainerDetails) (*containertypes.Config, *containertypes.HostConfig, *network.NetworkingConfig) {
	containerConfig := cloneContainerConfig(current.Config)
	containerConfig.Image = current.Summary.ImageRef
	if hostnameMatchesContainerID(current.Config.Hostname, current.Summary.ID) {
		containerConfig.Hostname = ""
	}
	clearImageDefaults(ctx, d, current, containerConfig)

	hostConfig := cloneHostConfig(current.Host)
	preserveInspectedMounts(hostConfig, current.Mounts)

	networks := make(map[string]*network.EndpointSettings, len(current.Networks))
	for name, settings := range current.Networks {
		networks[name] = sanitizedEndpoint(settings)
	}
	return containerConfig, hostConfig, &network.NetworkingConfig{EndpointsConfig: networks}
}

func cloneHostConfig(source *containertypes.HostConfig) *containertypes.HostConfig {
	cloned := *source
	cloned.Binds = append([]string(nil), source.Binds...)
	cloned.Mounts = append([]mount.Mount(nil), source.Mounts...)
	if cloned.MemorySwappiness != nil && *cloned.MemorySwappiness == 0 {
		cloned.MemorySwappiness = nil
	}
	return &cloned
}

func hostnameMatchesContainerID(hostname, containerID string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	containerID = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(containerID), "sha256:"))
	return len(hostname) >= 12 && strings.HasPrefix(containerID, hostname)
}

func cloneContainerConfig(source *containertypes.Config) *containertypes.Config {
	cloned := *source
	cloned.Env = append([]string(nil), source.Env...)
	cloned.Cmd = append([]string(nil), source.Cmd...)
	cloned.Entrypoint = append([]string(nil), source.Entrypoint...)
	cloned.Labels = cloneStringMap(source.Labels)
	cloned.OnBuild = append([]string(nil), source.OnBuild...)
	cloned.Shell = append([]string(nil), source.Shell...)
	return &cloned
}

func clearImageDefaults(ctx context.Context, client *DockerClient, current ContainerDetails, config *containertypes.Config) {
	oldImage, err := client.inspectImage(ctx, current.Summary.ImageID)
	if err != nil || oldImage.Config == nil {
		return
	}
	if reflect.DeepEqual(current.Config.Cmd, oldImage.Config.Cmd) {
		config.Cmd = nil
	}
	if reflect.DeepEqual(current.Config.Entrypoint, oldImage.Config.Entrypoint) {
		config.Entrypoint = nil
	}
	if reflect.DeepEqual(current.Config.Healthcheck, oldImage.Config.Healthcheck) {
		config.Healthcheck = nil
	}
}

func preserveInspectedMounts(host *containertypes.HostConfig, inspected []containertypes.MountPoint) {
	targets := configuredMountTargets(host)
	for _, current := range inspected {
		if current.Destination == "" {
			continue
		}
		if _, exists := targets[current.Destination]; exists {
			continue
		}
		spec, ok := reusableMount(current)
		if !ok {
			continue
		}
		host.Mounts = append(host.Mounts, spec)
		targets[current.Destination] = struct{}{}
	}
}

func configuredMountTargets(host *containertypes.HostConfig) map[string]struct{} {
	targets := make(map[string]struct{}, len(host.Binds)+len(host.Mounts))
	for _, bind := range host.Binds {
		targets[bindTarget(bind)] = struct{}{}
	}
	for _, current := range host.Mounts {
		targets[current.Target] = struct{}{}
	}
	return targets
}

func reusableMount(current containertypes.MountPoint) (mount.Mount, bool) {
	spec := mount.Mount{Type: current.Type, Target: current.Destination, ReadOnly: !current.RW}
	switch current.Type {
	case mount.TypeVolume:
		spec.Source = current.Name
		spec.VolumeOptions = &mount.VolumeOptions{NoCopy: strings.Contains(current.Mode, "nocopy")}
	case mount.TypeBind:
		spec.Source = current.Source
		spec.BindOptions = &mount.BindOptions{Propagation: current.Propagation}
	case mount.TypeTmpfs:
		// Docker recreates tmpfs mounts using the target and HostConfig defaults.
	case mount.TypeNamedPipe, mount.TypeCluster, mount.TypeImage:
		return mount.Mount{}, false
	default:
		return mount.Mount{}, false
	}
	return spec, true
}

func sanitizedEndpoint(settings *network.EndpointSettings) *network.EndpointSettings {
	if settings == nil {
		return nil
	}
	cloned := settings.Copy()
	cloned.NetworkID = ""
	cloned.EndpointID = ""
	cloned.Gateway = netip.Addr{}
	cloned.IPAddress = netip.Addr{}
	cloned.IPPrefixLen = 0
	cloned.IPv6Gateway = netip.Addr{}
	cloned.GlobalIPv6Address = netip.Addr{}
	cloned.GlobalIPv6PrefixLen = 0
	cloned.DNSNames = nil
	if cloned.IPAMConfig == nil {
		cloned.MacAddress = nil
	}
	return cloned
}
