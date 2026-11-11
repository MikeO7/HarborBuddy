package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
	d.dependents.keep(listed.Items)
	for _, item := range listed.Items {
		if item.ID == current.Summary.ID {
			continue
		}
		host, found, err := d.dependentHostConfig(ctx, item.ID)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if shared := sharedWith(host, current.Summary, names); shared != "" {
			name := primaryName(item.Names)
			return &UnsupportedError{Reason: fmt.Sprintf(
				"container %s depends on this container's %s; recreating it would disconnect %s, so update them together manually",
				name, shared, name,
			)}
		}
	}
	return nil
}

// dependentCache remembers each container's host configuration between
// checks. Namespace modes and links are fixed when a container is created, so
// one inspection per container ID serves every candidate in every cycle.
type dependentCache struct {
	mu    sync.Mutex
	hosts map[string]*containertypes.HostConfig
}

// keep drops entries for containers that no longer exist.
func (c *dependentCache) keep(listed []containertypes.Summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := make(map[string]*containertypes.HostConfig, len(listed))
	for _, item := range listed {
		if host, ok := c.hosts[item.ID]; ok {
			current[item.ID] = host
		}
	}
	c.hosts = current
}

func (d *DockerClient) dependentHostConfig(ctx context.Context, id string) (*containertypes.HostConfig, bool, error) {
	d.dependents.mu.Lock()
	host, ok := d.dependents.hosts[id]
	d.dependents.mu.Unlock()
	if ok {
		return host, true, nil
	}
	inspected, err := d.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect container %s to check namespace dependents: %w", id, err)
	}
	d.dependents.mu.Lock()
	d.dependents.hosts[id] = inspected.Container.HostConfig
	d.dependents.mu.Unlock()
	return inspected.Container.HostConfig, true, nil
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
