package docker

import (
	"context"

	containertypes "github.com/moby/moby/api/types/container"
)

// stoppedContainerProtection reports why cleanup must keep a stopped
// container. A container with a restart policy is meant to come back: a
// failed update leaves the original stopped with its policy restored, and a
// stopped Compose service keeps its policy too.
func (d *DockerClient) stoppedContainerProtection(ctx context.Context, container containertypes.Summary, name string) (bool, string) {
	if isBackupContainerName(name) {
		return true, "container is a HarborBuddy update backup"
	}
	if container.Labels[SelfUpdateHelperLabel] != "" {
		return true, "container is a HarborBuddy self-update record"
	}
	details, err := d.InspectContainer(ctx, container.ID)
	if err != nil || details.Host == nil {
		return true, "restart policy is unknown"
	}
	if !restartPolicyDisabled(details) {
		return true, "container has a restart policy"
	}
	return false, ""
}
