package docker

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
)

func (d *DockerClient) disconnectNetworks(ctx context.Context, current ContainerDetails) ([]string, error) {
	names := make([]string, 0, len(current.Networks))
	for name := range current.Networks {
		names = append(names, name)
	}
	sort.Strings(names)

	disconnected := make([]string, 0, len(names))
	for _, name := range names {
		if err := d.cli.NetworkDisconnect(ctx, name, current.Summary.ID, true); err != nil {
			return disconnected, fmt.Errorf("disconnect network %s: %w", name, err)
		}
		disconnected = append(disconnected, name)
	}
	return disconnected, nil
}

func (d *DockerClient) suppressRestartPolicy(ctx context.Context, current ContainerDetails) (bool, error) {
	if restartPolicyDisabled(current) {
		return false, nil
	}
	if _, err := d.cli.ContainerUpdate(ctx, current.Summary.ID, containertypes.UpdateConfig{
		Resources:     current.Host.Resources,
		RestartPolicy: containertypes.RestartPolicy{Name: "no"},
	}); err != nil {
		return false, fmt.Errorf("temporarily disable old container restart policy: %w", err)
	}
	return true, nil
}

func (d *DockerClient) restoreRestartPolicy(ctx context.Context, current ContainerDetails) error {
	if restartPolicyDisabled(current) {
		return nil
	}
	if _, err := d.cli.ContainerUpdate(ctx, current.Summary.ID, containertypes.UpdateConfig{
		Resources:     current.Host.Resources,
		RestartPolicy: current.Host.RestartPolicy,
	}); err != nil {
		return fmt.Errorf("restore original restart policy: %w", err)
	}
	return nil
}

func restartPolicyDisabled(current ContainerDetails) bool {
	return current.Host.RestartPolicy.Name == "" || current.Host.RestartPolicy.Name == "no"
}

func (d *DockerClient) ensureContainerStopped(ctx context.Context, id string, timeoutSeconds int) error {
	if err := d.cli.ContainerStop(ctx, id, containertypes.StopOptions{Timeout: &timeoutSeconds}); err != nil {
		details, inspectErr := d.InspectContainer(ctx, id)
		if inspectErr == nil && details.State != nil && !details.State.Running {
			return nil
		}
		return errors.Join(fmt.Errorf("stop old container: %w", err), inspectErr)
	}
	return nil
}

func (d *DockerClient) restoreOldContainer(parent context.Context, current ContainerDetails, renamed bool, disconnected []string, replacementID string, timeoutSeconds int) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()

	var rollbackErrors []error
	if replacementID != "" {
		rollbackErrors = appendError(rollbackErrors, d.removeReplacement(rollbackCtx, replacementID, timeoutSeconds))
	}
	if renamed {
		rollbackErrors = appendError(rollbackErrors, d.restoreContainerName(rollbackCtx, current))
	}
	rollbackErrors = append(rollbackErrors, d.reconnectNetworks(rollbackCtx, current, disconnected)...)
	rollbackErrors = appendError(rollbackErrors, d.restoreRestartPolicy(rollbackCtx, current))
	rollbackErrors = appendError(rollbackErrors, d.restartOldContainer(rollbackCtx, current.Summary.ID))
	return errors.Join(rollbackErrors...)
}

func (d *DockerClient) removeReplacement(ctx context.Context, id string, timeoutSeconds int) error {
	_ = d.cli.ContainerStop(ctx, id, containertypes.StopOptions{Timeout: &timeoutSeconds})
	if err := d.cli.ContainerRemove(ctx, id, containertypes.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove failed replacement: %w", err)
	}
	return nil
}

func (d *DockerClient) restoreContainerName(ctx context.Context, current ContainerDetails) error {
	if err := d.cli.ContainerRename(ctx, current.Summary.ID, current.Summary.Name); err != nil {
		return fmt.Errorf("restore original container name: %w", err)
	}
	return nil
}

func (d *DockerClient) reconnectNetworks(ctx context.Context, current ContainerDetails, names []string) []error {
	var reconnectErrors []error
	for _, name := range names {
		if err := d.cli.NetworkConnect(ctx, name, current.Summary.ID, sanitizedEndpoint(current.Networks[name])); err != nil {
			reconnectErrors = append(reconnectErrors, fmt.Errorf("reconnect original container to %s: %w", name, err))
		}
	}
	return reconnectErrors
}

func (d *DockerClient) restartOldContainer(ctx context.Context, id string) error {
	if err := d.cli.ContainerStart(ctx, id, containertypes.StartOptions{}); err != nil {
		return fmt.Errorf("restart original container: %w", err)
	}
	return nil
}

func appendError(errs []error, err error) []error {
	if err != nil {
		return append(errs, err)
	}
	return errs
}
