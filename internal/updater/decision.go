package updater

import (
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/imageref"
)

const (
	AutoUpdateLabel = "com.harborbuddy.autoupdate"
	RoleLabel       = "com.harborbuddy.role"
	DaemonRole      = "daemon"
)

type updateDecision struct {
	Eligible bool
	Reason   string
}

func DetermineEligibility(container docker.ContainerSummary, cfg config.UpdatesConfig) updateDecision {
	if container.Labels[AutoUpdateLabel] == "false" {
		return updateDecision{Reason: "label com.harborbuddy.autoupdate=false"}
	}
	for _, pattern := range cfg.DenyImages {
		if matchesPattern(container.ImageRef, pattern) {
			return updateDecision{Reason: "matches deny pattern: " + pattern}
		}
	}
	if len(cfg.AllowImages) > 0 {
		for _, pattern := range cfg.AllowImages {
			if matchesPattern(container.ImageRef, pattern) {
				return updateDecision{Eligible: true, Reason: "eligible for updates"}
			}
		}
		return updateDecision{Reason: "does not match any allow pattern"}
	}
	return updateDecision{Eligible: true, Reason: "eligible for updates"}
}

// matchesPattern accepts a pattern that matches the raw reference, its familiar
// form with an implicit ":latest" tag, or its fully qualified form, so
// "*:latest" matches "nginx" and "nginx:*" matches "docker.io/library/nginx:1.27".
func matchesPattern(image, pattern string) bool {
	return imageref.MatchAny(image, pattern, config.MatchImagePattern)
}
