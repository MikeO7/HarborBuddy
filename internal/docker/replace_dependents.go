package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// NamespaceDependentsChecker is implemented by clients that can find other
// containers relying on a container's namespaces or links.
type NamespaceDependentsChecker interface {
	CheckNamespaceDependents(context.Context, ContainerDetails) error
}

// CheckNamespaceDependents rejects replacing a container that another
// container, running or stopped, joins (network_mode, pid, ipc, or uts
// "container:<this>", which Compose's "service:<this>" becomes) or links to.
// Recreating the provider gives it a new ID and namespaces, silently cutting
// dependents off (for example a VPN gateway and the clients routed through it).
func (d *DockerClient) CheckNamespaceDependents(ctx context.Context, current ContainerDetails) error {
	listed, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers to check namespace dependents: %w", err)
	}
	names := make(map[string]struct{}, len(listed.Items))
	for _, item := range listed.Items {
		for _, name := range item.Names {
			names[strings.TrimPrefix(name, "/")] = struct{}{}
		}
	}
	for _, item := range listed.Items {
		if item.ID == current.Summary.ID {
			continue
		}
		inspected, err := d.cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect container %s to check namespace dependents: %w", item.ID, err)
		}
		if shared := sharedWith(inspected.Container.HostConfig, current.Summary, names); shared != "" {
			name := strings.TrimPrefix(inspected.Container.Name, "/")
			return &UnsupportedError{Reason: fmt.Sprintf(
				"container %s depends on this container's %s; recreating it would disconnect %s, so update them together manually",
				name, shared, name,
			)}
		}
	}
	return nil
}

// sharedWith names what host shares with target, or returns "".
func sharedWith(host *containertypes.HostConfig, target ContainerSummary, names map[string]struct{}) string {
	if host == nil {
		return ""
	}
	joins := []struct {
		kind string
		mode string
	}{
		{kind: "network namespace", mode: string(host.NetworkMode)},
		{kind: "PID namespace", mode: string(host.PidMode)},
		{kind: "IPC namespace", mode: string(host.IpcMode)},
		{kind: "UTS namespace", mode: string(host.UTSMode)},
	}
	for _, join := range joins {
		if ref, ok := strings.CutPrefix(join.mode, "container:"); ok && refersTo(ref, target, names) {
			return join.kind
		}
	}
	for _, link := range host.Links {
		source, _, _ := strings.Cut(link, ":")
		if refersTo(source, target, names) {
			return "link"
		}
	}
	return ""
}

// refersTo resolves a container reference the way Docker does: an exact name
// or full ID first, then an ID prefix that is not another container's name.
func refersTo(ref string, target ContainerSummary, names map[string]struct{}) bool {
	ref = strings.TrimPrefix(ref, "/")
	switch ref {
	case "":
		return false
	case target.Name, target.ID:
		return true
	}
	if _, isName := names[ref]; isName {
		return false
	}
	return strings.HasPrefix(target.ID, ref)
}
