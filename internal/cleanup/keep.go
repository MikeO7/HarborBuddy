package cleanup

import (
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

// keepReason says why a keep list protects resource, or "" when none does.
func keepReason(resource docker.CleanupResource, cfg config.CleanupConfig) string {
	switch {
	case resource.Kind == docker.CleanupImage && resource.Name != "" && cfg.KeepsImage(strings.Split(resource.Name, ",")):
		return "image is in cleanup.keep_images"
	case resource.Kind == docker.CleanupVolume && cfg.KeepsVolume(resource.Name):
		return "volume is in cleanup.keep_volumes"
	case resource.Kind == docker.CleanupVolume && cfg.KeepsVolumeLabels(resource.Labels):
		return "volume label is in cleanup.keep_volume_labels"
	default:
		return ""
	}
}
