package cleanup

import (
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

// keepReason says why a keep list protects resource, or "" when none does.
func keepReason(resource docker.CleanupResource, cfg config.CleanupConfig) string {
	switch {
	case resource.Kind == docker.CleanupImage && cfg.KeepsImage(imageReferences(resource)):
		return "image is in cleanup.keep_images"
	case resource.Kind == docker.CleanupVolume && cfg.KeepsVolume(resource.Name):
		return "volume is in cleanup.keep_volumes"
	case resource.Kind == docker.CleanupVolume && cfg.KeepsVolumeLabels(resource.Labels):
		return "volume label is in cleanup.keep_volume_labels"
	default:
		return ""
	}
}

// imageReferences lists the names keep_images patterns are matched against:
// every tag and digest reference, plus the repository part of each digest
// reference. An image pulled by digest (tool@sha256:...) has no tag, so its
// digest reference and repository name are the only way to recognise it.
func imageReferences(resource docker.CleanupResource) []string {
	var references []string
	for _, reference := range append(strings.Split(resource.Name, ","), resource.RepoDigests...) {
		if reference == "" || reference == "<none>:<none>" || reference == "<none>@<none>" {
			continue
		}
		references = append(references, reference)
		if repository, _, ok := strings.Cut(reference, "@"); ok && repository != "" {
			references = append(references, repository)
		}
	}
	return references
}
