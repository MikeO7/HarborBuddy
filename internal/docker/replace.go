package docker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func (d *DockerClient) CheckReplacement(current ContainerDetails, target ImageInfo) error {
	return checkReplacement(current, target, false)
}

func checkReplacement(current ContainerDetails, target ImageInfo, currentAlreadyStopped bool) error {
	if current.Summary.ID == "" || current.Summary.Name == "" {
		return &UnsupportedError{Reason: "container identity is incomplete"}
	}
	if current.Config == nil || current.Host == nil || current.State == nil {
		return &UnsupportedError{Reason: "container inspection is incomplete"}
	}
	if !current.State.Running && !currentAlreadyStopped {
		return &UnsupportedError{Reason: "container is no longer running"}
	}
	if target.ID == "" {
		return &UnsupportedError{Reason: "target image identity is missing"}
	}
	if current.Host.AutoRemove {
		return &UnsupportedError{Reason: "auto-remove containers cannot be rolled back safely"}
	}
	if hasContainerNamespaceDependency(current.Host) {
		return &UnsupportedError{Reason: "container namespace dependencies cannot be recreated safely"}
	}
	if current.Config.Labels["com.docker.swarm.task.id"] != "" {
		return &UnsupportedError{Reason: "Docker Swarm task containers must be updated by their service"}
	}
	return validateInspectedMounts(current)
}

func hasContainerNamespaceDependency(host *containertypes.HostConfig) bool {
	return host.NetworkMode.IsContainer() ||
		host.PidMode.IsContainer() ||
		host.IpcMode.IsContainer() ||
		strings.HasPrefix(string(host.UTSMode), "container:")
}

func validateInspectedMounts(current ContainerDetails) error {
	targets := configuredMountTargets(current.Host)
	for _, inspected := range current.Mounts {
		if _, exists := targets[inspected.Destination]; exists {
			continue
		}
		if err := validateReusableMount(inspected); err != nil {
			return err
		}
	}
	return nil
}

func validateReusableMount(current containertypes.MountPoint) error {
	switch current.Type {
	case mount.TypeVolume:
		if current.Name == "" {
			return &UnsupportedError{Reason: fmt.Sprintf("volume mounted at %s has no reusable Docker volume name", current.Destination)}
		}
	case mount.TypeBind:
		if current.Source == "" {
			return &UnsupportedError{Reason: fmt.Sprintf("bind mount at %s has no reusable source", current.Destination)}
		}
	case mount.TypeTmpfs:
	case mount.TypeNamedPipe, mount.TypeCluster, mount.TypeImage:
		return &UnsupportedError{Reason: fmt.Sprintf("mount type %s at %s cannot be recreated safely", current.Type, current.Destination)}
	default:
		return &UnsupportedError{Reason: fmt.Sprintf("unknown mount type %s at %s cannot be recreated safely", current.Type, current.Destination)}
	}
	return nil
}

func (d *DockerClient) ReplaceContainer(ctx context.Context, current ContainerDetails, target ImageInfo, options ReplaceOptions) (ReplaceResult, error) {
	if err := checkReplacement(current, target, options.CurrentAlreadyStopped); err != nil {
		return ReplaceResult{}, err
	}
	options = normalizeReplaceOptions(options)
	timeoutSeconds, err := dockerTimeoutSeconds(options.StopTimeout)
	if err != nil {
		return ReplaceResult{}, err
	}

	containerConfig, hostConfig, networkConfig := d.replacementConfig(ctx, current)
	backupName := fmt.Sprintf("%s-harborbuddy-backup-%d", current.Summary.Name, time.Now().UnixNano())
	result := ReplaceResult{BackupName: backupName}

	restartSuppressed, err := d.suppressRestartPolicy(ctx, current)
	if err != nil {
		return result, err
	}
	if err := d.ensureContainerStopped(ctx, current.Summary.ID, timeoutSeconds); err != nil {
		return result, d.handleStopFailure(ctx, current, restartSuppressed, err)
	}

	disconnected, err := d.disconnectNetworks(ctx, current)
	if err != nil {
		return result, d.rollbackFailure(ctx, current, false, disconnected, "", timeoutSeconds, "release old container network endpoints", err)
	}
	if err := d.cli.ContainerRename(ctx, current.Summary.ID, backupName); err != nil {
		return result, d.rollbackFailure(ctx, current, false, disconnected, "", timeoutSeconds, "rename old container to "+backupName, err)
	}

	created, err := d.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, current.Summary.Name)
	if err != nil {
		return result, d.rollbackFailure(ctx, current, true, disconnected, "", timeoutSeconds, "create replacement container", err)
	}
	result.NewContainerID = created.ID

	if err := d.startReplacement(ctx, created.ID, target, options); err != nil {
		return result, errors.Join(err, d.restoreOldContainer(ctx, current, true, disconnected, created.ID, timeoutSeconds))
	}
	if err := d.cli.ContainerRemove(ctx, current.Summary.ID, containertypes.RemoveOptions{Force: true}); err != nil {
		result.BackupCleanupErr = fmt.Errorf("remove backup container %s: %w", backupName, err)
	}
	return result, nil
}

func (d *DockerClient) startReplacement(ctx context.Context, id string, target ImageInfo, options ReplaceOptions) error {
	created, err := d.InspectContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("inspect replacement container: %w", err)
	}
	if created.Summary.ImageID != target.ID {
		return fmt.Errorf("replacement image mismatch: got %s, want %s", created.Summary.ImageID, target.ID)
	}
	if err := d.cli.ContainerStart(ctx, id, containertypes.StartOptions{}); err != nil {
		return fmt.Errorf("start replacement container: %w", err)
	}
	var healthcheck *containertypes.HealthConfig
	if created.Config != nil {
		healthcheck = created.Config.Healthcheck
	}
	if err := d.waitUntilReady(ctx, id, healthcheck, options); err != nil {
		return fmt.Errorf("replacement did not become ready: %w", err)
	}
	return nil
}

func (d *DockerClient) handleStopFailure(ctx context.Context, current ContainerDetails, restartSuppressed bool, stopErr error) error {
	if !restartSuppressed {
		return stopErr
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return errors.Join(stopErr, d.restoreRestartPolicy(rollbackCtx, current))
}

func (d *DockerClient) rollbackFailure(ctx context.Context, current ContainerDetails, renamed bool, disconnected []string, replacementID string, timeoutSeconds int, action string, cause error) error {
	rollbackErr := d.restoreOldContainer(ctx, current, renamed, disconnected, replacementID, timeoutSeconds)
	return errors.Join(fmt.Errorf("%s: %w", action, cause), rollbackErr)
}

func normalizeReplaceOptions(options ReplaceOptions) ReplaceOptions {
	if options.StopTimeout <= 0 {
		options.StopTimeout = 10 * time.Second
	}
	if options.StartupTimeout <= 0 {
		options.StartupTimeout = 30 * time.Second
	}
	if options.StabilizationTime <= 0 {
		options.StabilizationTime = 2 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	return options
}

func dockerTimeoutSeconds(timeout time.Duration) (int, error) {
	seconds := timeout / time.Second
	if timeout%time.Second != 0 {
		seconds++
	}
	if strconv.IntSize == 32 && seconds > time.Duration(math.MaxInt32) {
		return 0, fmt.Errorf("stop timeout %s exceeds Docker's platform limit", timeout)
	}
	return int(seconds), nil
}
