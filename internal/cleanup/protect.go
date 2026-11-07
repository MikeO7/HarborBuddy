package cleanup

import "github.com/MikeO7/HarborBuddy/internal/docker"

// Options carries facts from the same cycle's update phase into cleanup.
type Options struct {
	// ProtectedContainerIDs lists containers whose update failed or was rolled
	// back in this cycle. A failed rollback can leave the user's original
	// container stopped, so cleanup must not treat it as a stopped container to
	// delete. Keeping it also keeps its volumes referenced.
	ProtectedContainerIDs []string
}

const (
	failedUpdateContainerReason = "container was part of a failed update in this cycle"
	failedUpdateNetworkReason   = "network cleanup waits a cycle after a failed update, which may have left a container disconnected"
)

// protect marks resources that the update phase of this cycle may still need.
// A failed rollback can also leave the original container disconnected from
// its networks, so network removal waits for a cycle without failed updates.
func (o Options) protect(listed []docker.CleanupResource) []docker.CleanupResource {
	if len(o.ProtectedContainerIDs) == 0 {
		return listed
	}
	resources := append([]docker.CleanupResource(nil), listed...)
	ids := make(map[string]struct{}, len(o.ProtectedContainerIDs))
	for _, id := range o.ProtectedContainerIDs {
		ids[id] = struct{}{}
	}
	for index := range resources {
		resource := &resources[index]
		if resource.Protected {
			continue
		}
		_, failed := ids[resource.ID]
		switch {
		case resource.Kind == docker.CleanupContainer && failed:
			resource.Protected, resource.ProtectedReason = true, failedUpdateContainerReason
		case resource.Kind == docker.CleanupNetwork:
			resource.Protected, resource.ProtectedReason = true, failedUpdateNetworkReason
		}
	}
	return resources
}
