package docker

import (
	"net/netip"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// helperNetworkNone isolates a helper that reaches Docker through a mounted
// Unix socket and therefore needs no network at all.
const helperNetworkNone containertypes.NetworkMode = "none"

// helperHostConfig copies only what the helper needs to reach the Docker API
// with the daemon's privileges: the socket and certificate mounts, the
// identity and security settings that govern socket access, and, for a remote
// daemon only, the network path to it.
func helperHostConfig(source *containertypes.HostConfig, dockerHost string, relevantPaths []string) *containertypes.HostConfig {
	host := &containertypes.HostConfig{
		Binds:         relevantBinds(source.Binds, relevantPaths),
		Mounts:        relevantMounts(source.Mounts, relevantPaths),
		RestartPolicy: containertypes.RestartPolicy{Name: "no"},
		// The helper is kept after it exits so its logs stay readable; the
		// daemon prunes finished helpers once they are older than a day.
		AutoRemove:     false,
		GroupAdd:       append([]string(nil), source.GroupAdd...),
		SecurityOpt:    append([]string(nil), source.SecurityOpt...),
		CapAdd:         append([]string(nil), source.CapAdd...),
		CapDrop:        append([]string(nil), source.CapDrop...),
		Privileged:     source.Privileged,
		ReadonlyRootfs: source.ReadonlyRootfs,
		UsernsMode:     source.UsernsMode,
		Runtime:        source.Runtime,
	}
	if helperUsesLocalSocket(dockerHost) {
		host.NetworkMode = helperNetworkNone
		return host
	}
	host.NetworkMode = source.NetworkMode
	host.DNS = append([]netip.Addr(nil), source.DNS...)
	host.DNSOptions = append([]string(nil), source.DNSOptions...)
	host.DNSSearch = append([]string(nil), source.DNSSearch...)
	host.ExtraHosts = append([]string(nil), source.ExtraHosts...)
	return host
}

// helperNetworkConfig attaches a remote-daemon helper to the daemon's
// networks with dynamic addressing only. Static IPs, MAC addresses, and aliases
// belong to the still-running daemon, so copying them would collide.
func helperNetworkConfig(current ContainerDetails, dockerHost string) *network.NetworkingConfig {
	if helperUsesLocalSocket(dockerHost) || !current.Host.NetworkMode.IsPrivate() || current.Host.NetworkMode.IsNone() {
		return nil
	}
	endpoints := make(map[string]*network.EndpointSettings, len(current.Networks))
	for name, settings := range current.Networks {
		endpoints[name] = helperEndpoint(settings)
	}
	if len(endpoints) == 0 {
		return nil
	}
	return &network.NetworkingConfig{EndpointsConfig: endpoints}
}

func helperEndpoint(settings *network.EndpointSettings) *network.EndpointSettings {
	if settings == nil {
		return nil
	}
	return &network.EndpointSettings{
		Links:      append([]string(nil), settings.Links...),
		DriverOpts: cloneStringMap(settings.DriverOpts),
		GwPriority: settings.GwPriority,
	}
}

func helperUsesLocalSocket(dockerHost string) bool {
	return dockerSocketPath(dockerHost) != ""
}
