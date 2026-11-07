package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	recoveryTimeout     = 2 * time.Minute
	composeServiceLabel = "com.docker.compose.service"
)

// BackupRecovery describes one "<name>-harborbuddy-backup-*" container found
// by RecoverStrandedBackups. Restored is true once the backup was renamed back;
// otherwise Reason explains why it was left alone, and Deferred marks reasons
// that are expected to clear on their own (a transaction or helper in flight).
type BackupRecovery struct {
	ContainerID  string
	BackupName   string
	OriginalName string
	Restored     bool
	Deferred     bool
	Reason       string
	Warning      error
	Err          error
}

// BackupRecoverer is implemented by clients that can restore originals that an
// interrupted replacement left stopped under their backup name.
type BackupRecoverer interface {
	RecoverStrandedBackups(ctx context.Context, dryRun bool) ([]BackupRecovery, error)
}

// RecoverStrandedBackups restores stopped HarborBuddy backups whose original
// name is free: it renames each back, reconnects its primary network, restores
// the restart policy recorded in the backup name, and starts it. It leaves a
// backup alone when its original name is taken, it is running, this process is
// replacing it, or a self-update helper is running. Dry-run only reports.
func (d *DockerClient) RecoverStrandedBackups(ctx context.Context, dryRun bool) ([]BackupRecovery, error) {
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers for stranded backup recovery: %w", err)
	}
	names := make(map[string]struct{}, len(listed.Items))
	helperRunning := false
	for _, item := range listed.Items {
		for _, name := range item.Names {
			names[strings.TrimPrefix(name, "/")] = struct{}{}
		}
		if item.Labels[SelfUpdateHelperLabel] == "true" && item.State == containertypes.StateRunning {
			helperRunning = true
		}
	}

	var recoveries []BackupRecovery
	for _, item := range listed.Items {
		backup := primaryName(item.Names)
		original, policy, ok := parseBackupName(backup)
		if !ok {
			continue
		}
		recovery := BackupRecovery{ContainerID: item.ID, BackupName: backup, OriginalName: original}
		recovery.Reason = deferredRecovery(ctx, item.ID, helperRunning)
		recovery.Deferred = recovery.Reason != ""
		if !recovery.Deferred {
			recovery.Reason = recoveryBlocker(item, original, names, dryRun)
		}
		if recovery.Reason == "" {
			recovery.Restored, recovery.Warning, recovery.Err = d.restoreBackup(ctx, item.ID, original, policy)
		}
		recoveries = append(recoveries, recovery)
	}
	return recoveries, nil
}

// primaryName returns the container's own name, skipping legacy link aliases
// such as "/consumer/alias" that Docker also lists.
func primaryName(names []string) string {
	for _, name := range names {
		if trimmed := strings.TrimPrefix(name, "/"); !strings.Contains(trimmed, "/") {
			return trimmed
		}
	}
	return ""
}

// deferredRecovery reports transient reasons to leave a backup for a later cycle.
func deferredRecovery(ctx context.Context, id string, helperRunning bool) string {
	switch {
	case transactionActive(id):
		return "a replacement of this container is in progress"
	case helperRunning:
		return "a self-update helper is running"
	case ctx.Err() != nil:
		return "shutdown requested"
	default:
		return ""
	}
}

func recoveryBlocker(item containertypes.Summary, original string, names map[string]struct{}, dryRun bool) string {
	_, nameTaken := names[original]
	switch {
	case nameTaken:
		return "original name " + original + " is in use; remove the backup after confirming the replacement"
	case item.State == containertypes.StateRunning || item.State == containertypes.StateRestarting || item.State == containertypes.StatePaused:
		return "backup container is " + string(item.State)
	case dryRun:
		return "dry-run; would restore " + original
	default:
		return ""
	}
}

// restoreBackup finishes on a detached, bounded context so shutdown cannot
// leave a half-restored container.
func (d *DockerClient) restoreBackup(parent context.Context, id, original string, policy *containertypes.RestartPolicy) (bool, error, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), recoveryTimeout)
	defer cancel()

	details, err := d.InspectContainer(ctx, id)
	if err == nil && (details.Config == nil || details.Host == nil) {
		err = errors.New("inspection is incomplete")
	}
	if err != nil {
		return false, nil, fmt.Errorf("inspect stranded backup: %w", err)
	}
	if _, err := d.cli.ContainerRename(ctx, id, client.ContainerRenameOptions{NewName: original}); err != nil {
		return false, nil, fmt.Errorf("rename stranded backup to %s: %w", original, err)
	}
	warning := d.reconnectPrimaryNetwork(ctx, details)
	if policy != nil {
		details.Host.RestartPolicy = *policy
		if err := d.restoreRestartPolicy(ctx, details); err != nil {
			warning = errors.Join(warning, err)
		}
	}
	if err := d.restartOldContainer(ctx, id); err != nil {
		return true, warning, err
	}
	return true, warning, nil
}

// reconnectPrimaryNetwork reattaches the network named by the container's
// network mode when the interrupted transaction had disconnected everything.
// Docker does not reattach it on start. Secondary networks, extra aliases, and
// static addresses are not recorded anywhere durable, so the warning asks the
// operator to verify them.
func (d *DockerClient) reconnectPrimaryNetwork(ctx context.Context, details ContainerDetails) error {
	if len(details.Networks) > 0 {
		return nil
	}
	mode := details.Host.NetworkMode
	if mode == "" || mode.IsNone() || mode.IsHost() || mode.IsContainer() {
		return nil
	}
	name := string(mode)
	if mode.IsDefault() {
		name = "bridge"
	}
	endpoint := &network.EndpointSettings{}
	if service := details.Config.Labels[composeServiceLabel]; service != "" && mode.IsUserDefined() {
		endpoint.Aliases = []string{service}
	}
	if _, err := d.cli.NetworkConnect(ctx, name, client.NetworkConnectOptions{Container: details.Summary.ID, EndpointConfig: endpoint}); err != nil {
		return fmt.Errorf("reconnect restored container to network %s: %w", name, err)
	}
	return fmt.Errorf("reconnected only primary network %s; verify any additional networks, aliases, and static addresses", name)
}
