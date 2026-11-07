package docker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

var dockerPlatformIntSize = strconv.IntSize

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
		return ReplaceResult{FailureStage: "validate_replacement"}, err
	}
	options = normalizeReplaceOptions(options)
	timeoutSeconds, err := dockerTimeoutSeconds(options.StopTimeout)
	if err != nil {
		return ReplaceResult{FailureStage: "validate_stop_timeout"}, err
	}

	containerConfig, hostConfig, networkConfig, configWarning := d.replacementConfig(ctx, current)
	result := ReplaceResult{ConfigWarning: configWarning}
	if err := ctx.Err(); err != nil {
		result.FailureStage = FailureStageCancelledBeforeStart
		return result, fmt.Errorf("replacement not started: %w", err)
	}

	// From the first mutation on, shutdown must not abandon the transaction
	// half-way: the original would be left stopped under its backup name.
	// Finish (or roll back) on a context detached from cancellation but bounded.
	txCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionTimeout(options))
	defer cancel()
	defer beginTransaction(current.Summary.ID)()
	return d.replaceInTransaction(txCtx, current, target, options, timeoutSeconds, &containertypes.CreateRequest{
		Config:           containerConfig,
		HostConfig:       hostConfig,
		NetworkingConfig: networkConfig,
	}, result)
}

func (d *DockerClient) replaceInTransaction(ctx context.Context, current ContainerDetails, target ImageInfo, options ReplaceOptions, timeoutSeconds int, spec *containertypes.CreateRequest, result ReplaceResult) (ReplaceResult, error) {
	// Rename first: the backup name records the restart policy, so if
	// HarborBuddy dies at any later step, recovery can find the original and
	// restore it. Docker renames running containers.
	backupName := backupContainerName(current.Summary.Name, time.Now(), !restartPolicyDisabled(current), current.Host.RestartPolicy)
	result.BackupName = backupName
	if _, err := d.cli.ContainerRename(ctx, current.Summary.ID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
		result.FailureStage = "rename_old_container"
		return result, fmt.Errorf("rename old container to %s: %w", backupName, err)
	}

	restartSuppressed, err := d.suppressRestartPolicy(ctx, current)
	if err != nil {
		result.FailureStage = "suppress_restart_policy"
		result.RollbackAttempted = true
		result.RollbackErr = d.undoRename(ctx, current)
		return result, errors.Join(err, result.RollbackErr)
	}
	if err := d.ensureContainerStopped(ctx, current.Summary.ID, timeoutSeconds); err != nil {
		result.FailureStage = "stop_old_container"
		result.RollbackAttempted = true
		failure, rollbackErr := d.handleStopFailureDetails(ctx, current, restartSuppressed, err)
		result.RollbackErr = rollbackErr
		return result, failure
	}

	disconnected, err := d.disconnectNetworks(ctx, current)
	if err != nil {
		return d.failedReplacement(ctx, result, current, true, disconnected, "", timeoutSeconds, "disconnect_old_networks", "release old container network endpoints", err)
	}

	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           spec.Config,
		HostConfig:       spec.HostConfig,
		NetworkingConfig: spec.NetworkingConfig,
		Name:             current.Summary.Name,
	})
	if err != nil {
		return d.failedReplacement(ctx, result, current, true, disconnected, "", timeoutSeconds, "create_replacement", "create replacement container", err)
	}
	result.NewContainerID = created.ID

	if err := d.startReplacement(ctx, created.ID, target, options); err != nil {
		return d.failedReplacement(ctx, result, current, true, disconnected, created.ID, timeoutSeconds, "start_replacement", "start replacement container", err)
	}
	if _, err := d.cli.ContainerRemove(ctx, current.Summary.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
		result.BackupCleanupErr = fmt.Errorf("remove backup container %s: %w", backupName, err)
	}
	result.RollbackImageRetentionErr = d.retainRollbackImage(ctx, current.Summary.ImageRef, current.Summary.ImageID, options.RollbackImageRetention)
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
	if _, err := d.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
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
	failure, _ := d.handleStopFailureDetails(ctx, current, restartSuppressed, stopErr)
	return failure
}

func (d *DockerClient) handleStopFailureDetails(ctx context.Context, current ContainerDetails, restartSuppressed bool, stopErr error) (error, error) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var rollbackErrors []error
	if restartSuppressed {
		rollbackErrors = appendError(rollbackErrors, d.restoreRestartPolicy(rollbackCtx, current))
	}
	rollbackErrors = appendError(rollbackErrors, d.restoreContainerName(rollbackCtx, current))
	rollbackErr := errors.Join(rollbackErrors...)
	return errors.Join(stopErr, rollbackErr), rollbackErr
}

// undoRename gives the still-running original its name back after a failure
// before it was stopped.
func (d *DockerClient) undoRename(ctx context.Context, current ContainerDetails) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return d.restoreContainerName(rollbackCtx, current)
}

func (d *DockerClient) failedReplacement(ctx context.Context, result ReplaceResult, current ContainerDetails, renamed bool, disconnected []string, replacementID string, timeoutSeconds int, stage string, action string, cause error) (ReplaceResult, error) {
	rollbackErr := d.restoreOldContainer(ctx, current, renamed, disconnected, replacementID, timeoutSeconds)
	result.FailureStage = stage
	result.RollbackAttempted = true
	result.RollbackErr = rollbackErr
	return result, errors.Join(fmt.Errorf("%s: %w", action, cause), rollbackErr)
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
	if dockerPlatformIntSize == 32 && seconds > time.Duration(math.MaxInt32) {
		return 0, fmt.Errorf("stop timeout %s exceeds Docker's platform limit", timeout)
	}
	return int(seconds), nil
}
