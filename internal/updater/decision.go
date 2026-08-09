package updater

import (
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

const (
	AutoUpdateLabel = "com.harborbuddy.autoupdate"
	RoleLabel       = "com.harborbuddy.role"
	DaemonRole      = "daemon"
)

type UpdateDecision struct {
	Eligible bool
	Reason   string
}

func DetermineEligibility(container docker.ContainerSummary, cfg config.UpdatesConfig) UpdateDecision {
	if container.Labels[AutoUpdateLabel] == "false" {
		return UpdateDecision{Reason: "label com.harborbuddy.autoupdate=false"}
	}
	for _, pattern := range cfg.DenyImages {
		if matchesPattern(container.ImageRef, pattern) {
			return UpdateDecision{Reason: "matches deny pattern: " + pattern}
		}
	}
	if len(cfg.AllowImages) > 0 {
		for _, pattern := range cfg.AllowImages {
			if matchesPattern(container.ImageRef, pattern) {
				return UpdateDecision{Eligible: true, Reason: "eligible for updates"}
			}
		}
		return UpdateDecision{Reason: "does not match any allow pattern"}
	}
	return UpdateDecision{Eligible: true, Reason: "eligible for updates"}
}

func matchesPattern(image, pattern string) bool {
	switch {
	case pattern == "*" || image == pattern:
		return true
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(image, strings.TrimSuffix(pattern, "*"))
	case strings.HasPrefix(pattern, "*"):
		return strings.HasSuffix(image, strings.TrimPrefix(pattern, "*"))
	default:
		return false
	}
}
