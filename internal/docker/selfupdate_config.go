package docker

import (
	"errors"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func selfUpdateHelperConfig(current ContainerDetails, request SelfUpdateHelperRequest) (*containertypes.Config, *containertypes.HostConfig, *network.NetworkingConfig, error) {
	if current.Config == nil || current.Host == nil {
		return nil, nil, nil, errors.New("cannot start self-update helper: current container inspection is incomplete")
	}
	if request.Name == "" || request.TargetContainerID == "" || request.TargetImageID == "" {
		return nil, nil, nil, errors.New("cannot start self-update helper: helper name, target container, and target image are required")
	}

	environment, dockerHost, certPath := helperDockerEnvironment(current.Config.Env, request.DockerHost)
	relevantPaths := []string{dockerSocketPath(dockerHost)}
	if certPath != "" {
		relevantPaths = append(relevantPaths, certPath)
	}

	containerConfig := &containertypes.Config{
		Image:      request.TargetImageID,
		User:       current.Config.User,
		Env:        environment,
		Entrypoint: []string{selfUpdateBinary},
		Cmd: []string{
			"--updater-mode",
			"--target-container-id", request.TargetContainerID,
			"--new-image-id", request.TargetImageID,
			"--helper-restart-policy", string(current.Host.RestartPolicy.Name),
			"--helper-restart-max-retries", strconv.Itoa(current.Host.RestartPolicy.MaximumRetryCount),
		},
		Labels: map[string]string{
			SelfUpdateHelperLabel: "true",
			SelfUpdateTargetLabel: request.TargetContainerID,
		},
	}
	if request.StopTimeout > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, "--helper-stop-timeout", request.StopTimeout.String())
	}
	if request.StartupTimeout > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, "--helper-startup-timeout", request.StartupTimeout.String())
	}
	if request.RollbackImageRetention > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, "--helper-rollback-image-retention", strconv.Itoa(request.RollbackImageRetention))
	}

	hostConfig := &containertypes.HostConfig{
		Binds:         relevantBinds(current.Host.Binds, relevantPaths),
		Mounts:        relevantMounts(current.Host.Mounts, relevantPaths),
		NetworkMode:   current.Host.NetworkMode,
		RestartPolicy: containertypes.RestartPolicy{Name: "no"},
		AutoRemove:    true,
		DNS:           append([]netip.Addr(nil), current.Host.DNS...),
		DNSOptions:    append([]string(nil), current.Host.DNSOptions...),
		DNSSearch:     append([]string(nil), current.Host.DNSSearch...),
		ExtraHosts:    append([]string(nil), current.Host.ExtraHosts...),
		GroupAdd:      append([]string(nil), current.Host.GroupAdd...),
		SecurityOpt:   append([]string(nil), current.Host.SecurityOpt...),
	}
	return containerConfig, hostConfig, helperNetworkConfig(current), nil
}

func helperNetworkConfig(current ContainerDetails) *network.NetworkingConfig {
	endpoints := make(map[string]*network.EndpointSettings, len(current.Networks))
	if current.Host.NetworkMode.IsPrivate() && !current.Host.NetworkMode.IsNone() {
		for name, settings := range current.Networks {
			endpoint := sanitizedEndpoint(settings)
			if endpoint != nil {
				endpoint.Aliases = nil
			}
			endpoints[name] = endpoint
		}
	}
	if len(endpoints) == 0 {
		return nil
	}
	return &network.NetworkingConfig{EndpointsConfig: endpoints}
}

func helperDockerEnvironment(source []string, configuredHost string) (environment []string, dockerHost, certPath string) {
	const harborBuddyDockerHost = "HARBORBUDDY_DOCKER_HOST"
	allowed := map[string]bool{
		harborBuddyDockerHost:   true,
		"DOCKER_HOST":           true,
		"DOCKER_TLS_VERIFY":     true,
		"DOCKER_CERT_PATH":      true,
		"DOCKER_API_VERSION":    true,
		"DOCKER_CONTEXT":        true,
		"HARBORBUDDY_LOG_LEVEL": true,
		"HARBORBUDDY_LOG_JSON":  true,
	}
	values, order := allowedEnvironment(source, allowed)

	dockerHost = configuredHost
	if dockerHost == "" {
		dockerHost = values[harborBuddyDockerHost]
	}
	if dockerHost == "" {
		dockerHost = values["DOCKER_HOST"]
	}
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}
	values[harborBuddyDockerHost] = dockerHost
	if !containsString(order, harborBuddyDockerHost) {
		order = append(order, harborBuddyDockerHost)
	}

	environment = make([]string, 0, len(order))
	for _, key := range order {
		environment = append(environment, key+"="+values[key])
	}
	return environment, dockerHost, values["DOCKER_CERT_PATH"]
}

func allowedEnvironment(source []string, allowed map[string]bool) (map[string]string, []string) {
	values := make(map[string]string, len(allowed))
	order := make([]string, 0, len(allowed))
	for _, entry := range source {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !allowed[key] {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	return values, order
}

func dockerSocketPath(host string) string {
	const unixPrefix = "unix://"
	if strings.HasPrefix(host, unixPrefix) {
		return filepath.Clean(strings.TrimPrefix(host, unixPrefix))
	}
	return ""
}

func relevantBinds(binds, relevantPaths []string) []string {
	result := make([]string, 0, len(binds))
	for _, bind := range binds {
		if pathRelevant(bindTarget(bind), relevantPaths) {
			result = append(result, bind)
		}
	}
	return result
}

func relevantMounts(mounts []mount.Mount, relevantPaths []string) []mount.Mount {
	result := make([]mount.Mount, 0, len(mounts))
	for _, current := range mounts {
		if pathRelevant(current.Target, relevantPaths) {
			result = append(result, current)
		}
	}
	return result
}

func bindTarget(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 {
		return ""
	}
	if len(parts) >= 3 && isBindMode(parts[len(parts)-1]) {
		return parts[len(parts)-2]
	}
	return parts[len(parts)-1]
}

func isBindMode(value string) bool {
	for _, option := range strings.Split(value, ",") {
		switch option {
		case "ro", "rw", "z", "Z", "private", "rprivate", "shared", "rshared", "slave", "rslave", "nocopy", "nosuid", "nodev", "rbind":
		default:
			return false
		}
	}
	return value != ""
}

func pathRelevant(target string, relevantPaths []string) bool {
	if target == "" {
		return false
	}
	target = filepath.Clean(target)
	for _, relevant := range relevantPaths {
		if relevant == "" {
			continue
		}
		relevant = filepath.Clean(relevant)
		if target == relevant || strings.HasPrefix(relevant, target+string(filepath.Separator)) || strings.HasPrefix(target, relevant+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
