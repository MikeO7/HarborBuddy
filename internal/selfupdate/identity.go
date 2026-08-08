package selfupdate

import (
	"os"
	"regexp"
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

var cgroupIDPattern = regexp.MustCompile(`[[:xdigit:]]{12,64}`)

// DetectCurrentContainer returns the exact listed container hosting this
// process. An explicit stable container name or ID takes precedence, followed
// by Linux runtime identity and Docker's default hostname. Role labels are
// never positive identity evidence because the configured daemon may be remote.
func DetectCurrentContainer(containers []docker.ContainerSummary) string {
	if name := strings.TrimSpace(os.Getenv("HARBORBUDDY_CONTAINER_NAME")); name != "" {
		if id := uniqueNameMatch(containers, name); id != "" {
			return id
		}
	}
	if explicit := os.Getenv("HARBORBUDDY_CONTAINER_ID"); explicit != "" {
		if id := uniquePrefixMatch(containers, explicit); id != "" {
			return id
		}
	}
	hostname, _ := os.Hostname()
	var identity []byte
	for _, path := range []string{"/proc/self/cgroup", "/proc/1/cpuset", "/proc/self/mountinfo"} {
		contents, _ := os.ReadFile(path) //nolint:gosec // Paths are fixed procfs identity sources, not user input.
		identity = append(identity, contents...)
		identity = append(identity, '\n')
	}
	return detectCurrentContainer(containers, hostname, identity)
}

func detectCurrentContainer(containers []docker.ContainerSummary, hostname string, runtimeIdentity []byte) string {
	if id := uniqueCgroupMatch(containers, runtimeIdentity); id != "" {
		return id
	}
	return uniquePrefixMatch(containers, strings.TrimSpace(hostname))
}

func uniqueCgroupMatch(containers []docker.ContainerSummary, cgroup []byte) string {
	if len(cgroup) == 0 {
		return ""
	}
	matches := make(map[string]struct{})
	contents := string(cgroup)
	for _, container := range containers {
		id := normalizedContainerID(container.ID)
		if id != "" && strings.Contains(contents, id) {
			matches[container.ID] = struct{}{}
		}
	}
	for _, token := range cgroupIDPattern.FindAllString(contents, -1) {
		if id := uniquePrefixMatch(containers, token); id != "" {
			matches[id] = struct{}{}
		}
	}
	if len(matches) != 1 {
		return ""
	}
	for id := range matches {
		return id
	}
	return ""
}

func uniqueNameMatch(containers []docker.ContainerSummary, name string) string {
	match := ""
	for _, container := range containers {
		if container.Name != name {
			continue
		}
		if match != "" {
			return ""
		}
		match = container.ID
	}
	return match
}

func uniquePrefixMatch(containers []docker.ContainerSummary, identity string) string {
	identity = normalizedContainerID(identity)
	if len(identity) < 12 {
		return ""
	}
	match := ""
	for _, container := range containers {
		id := normalizedContainerID(container.ID)
		if id == "" || (!strings.HasPrefix(id, identity) && !strings.HasPrefix(identity, id)) {
			continue
		}
		if match != "" && match != container.ID {
			return ""
		}
		match = container.ID
	}
	return match
}

func normalizedContainerID(id string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(id), "sha256:"))
}
