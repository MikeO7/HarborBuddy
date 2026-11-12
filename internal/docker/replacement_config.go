package docker

import (
	"context"
	"maps"
	"net/netip"
	"path"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

// replacementConfig derives the replacement's configuration from inspection.
// The returned warning reports that old-image defaults could not be separated
// from user settings; it never prevents the replacement.
func (d *DockerClient) replacementConfig(ctx context.Context, current ContainerDetails) (*containertypes.Config, *containertypes.HostConfig, *network.NetworkingConfig, error) {
	containerConfig := cloneContainerConfig(current.Config)
	containerConfig.Image = current.Summary.ImageRef
	if hostnameMatchesContainerID(current.Config.Hostname, current.Summary.ID) {
		containerConfig.Hostname = ""
	}
	warning := d.stripImageDefaults(ctx, current, containerConfig)

	hostConfig := cloneHostConfig(current.Host)
	preserveInspectedMounts(hostConfig, current.Mounts)

	networks := make(map[string]*network.EndpointSettings, len(current.Networks))
	for name, settings := range current.Networks {
		networks[name] = sanitizedEndpoint(settings)
	}
	return containerConfig, hostConfig, &network.NetworkingConfig{EndpointsConfig: networks}, warning
}

func cloneHostConfig(source *containertypes.HostConfig) *containertypes.HostConfig {
	cloned := *source
	cloned.Binds = append([]string(nil), source.Binds...)
	cloned.Mounts = append([]mount.Mount(nil), source.Mounts...)
	// Inspection cannot tell an explicit swappiness of 0 from the zero some
	// engines report when none was set, and cgroup v2 hosts reject the field
	// on create, so a zero is treated as unset.
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
	cloned.ExposedPorts = maps.Clone(source.ExposedPorts)
	cloned.Volumes = maps.Clone(source.Volumes)
	return &cloned
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
		targets[mountTargetKey(bindTarget(bind))] = struct{}{}
	}
	for _, current := range host.Mounts {
		targets[mountTargetKey(current.Target)] = struct{}{}
	}
	return targets
}

// mountTargetKey normalizes a configured target the way Docker reports an
// inspected mount destination, so "/data/" and "/data" are the same mount.
func mountTargetKey(target string) string {
	if target == "" {
		return ""
	}
	return path.Clean(target)
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
	// Inspection reports a generated MAC address the same way as a configured
	// one. The form older engines derive from the dynamic IPv4 address is
	// certainly generated and would collide once that address is reused, so it
	// is dropped; any other address is kept so a configured one survives.
	if cloned.IPAMConfig == nil && macDerivedFromAddress(settings.MacAddress, settings.IPAddress) {
		cloned.MacAddress = nil
	}
	return cloned
}

// macDerivedFromAddress reports whether mac is 02:42 followed by the bytes of
// the endpoint's IPv4 address, which is how engines before Docker 28 generate
// bridge MAC addresses.
func macDerivedFromAddress(mac network.HardwareAddr, address netip.Addr) bool {
	if len(mac) != 6 || !address.Unmap().Is4() {
		return false
	}
	ip := address.Unmap().As4()
	return mac[0] == 0x02 && mac[1] == 0x42 && [4]byte(mac[2:]) == ip
}
