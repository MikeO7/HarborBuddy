package docker

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// stripImageDefaults removes settings that container inspection reports only
// because the old image supplied them. Docker merges image defaults into the
// inspected config, so copying it verbatim would pin the replacement to the old
// image's ENV (e.g. PG_VERSION), labels, user, ports, and volumes. Values the
// user set, including overrides of image variables, differ from the old image
// and are kept. When the old image cannot be inspected the config is left as
// inspected and a warning is returned so the caller can surface it.
func (d *DockerClient) stripImageDefaults(ctx context.Context, current ContainerDetails, config *containertypes.Config) error {
	inspected, err := d.cli.ImageInspect(ctx, current.Summary.ImageID)
	if err == nil && inspected.Config == nil {
		err = errors.New("image has no configuration")
	}
	if err != nil {
		return fmt.Errorf("inspect previous image %s to separate its defaults; the replacement keeps the old image's inherited settings: %w", current.Summary.ImageID, err)
	}
	old := inspected.Config
	clearInheritedCommand(current.Config, config, &containertypes.Config{
		Cmd: old.Cmd, Entrypoint: old.Entrypoint, Healthcheck: old.Healthcheck, OnBuild: old.OnBuild, Shell: old.Shell,
	})
	config.Env = withoutInheritedEnv(config.Env, old.Env)
	config.Labels = withoutInheritedLabels(config.Labels, old.Labels)
	config.WorkingDir = clearIfEqual(config.WorkingDir, old.WorkingDir)
	config.User = clearIfEqual(config.User, old.User)
	config.StopSignal = clearIfEqual(config.StopSignal, old.StopSignal)
	config.ExposedPorts = withoutInheritedPorts(config.ExposedPorts, old.ExposedPorts)
	config.Volumes = withoutInheritedVolumes(config.Volumes, old.Volumes)
	return nil
}

func clearInheritedCommand(inspected, config, old *containertypes.Config) {
	if reflect.DeepEqual(inspected.Cmd, old.Cmd) {
		config.Cmd = nil
	}
	if reflect.DeepEqual(inspected.Entrypoint, old.Entrypoint) {
		config.Entrypoint = nil
	}
	if reflect.DeepEqual(inspected.Healthcheck, old.Healthcheck) {
		config.Healthcheck = nil
	}
	if reflect.DeepEqual(inspected.OnBuild, old.OnBuild) {
		config.OnBuild = nil
	}
	if reflect.DeepEqual(inspected.Shell, old.Shell) {
		config.Shell = nil
	}
}

func withoutInheritedEnv(env, inherited []string) []string {
	defaults := make(map[string]struct{}, len(inherited))
	for _, entry := range inherited {
		defaults[entry] = struct{}{}
	}
	var kept []string
	for _, entry := range env {
		if _, fromImage := defaults[entry]; !fromImage {
			kept = append(kept, entry)
		}
	}
	return kept
}

func withoutInheritedLabels(labels, inherited map[string]string) map[string]string {
	for key, value := range inherited {
		if current, ok := labels[key]; ok && current == value {
			delete(labels, key)
		}
	}
	return labels
}

func clearIfEqual(value, inherited string) string {
	if value == inherited {
		return ""
	}
	return value
}

func withoutInheritedPorts(ports map[network.Port]struct{}, inherited map[string]struct{}) map[network.Port]struct{} {
	if len(ports) == 0 {
		return ports
	}
	kept := make(map[network.Port]struct{}, len(ports))
	for port := range ports {
		if _, fromImage := inherited[port.String()]; !fromImage {
			kept[port] = struct{}{}
		}
	}
	return kept
}

func withoutInheritedVolumes(volumes, inherited map[string]struct{}) map[string]struct{} {
	if len(volumes) == 0 {
		return volumes
	}
	kept := make(map[string]struct{}, len(volumes))
	for target := range volumes {
		if _, fromImage := inherited[target]; !fromImage {
			kept[target] = struct{}{}
		}
	}
	return kept
}
