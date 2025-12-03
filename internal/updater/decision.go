package updater

import (
	"strings"

	"github.com/mikeo/harborbuddy/internal/config"
	"github.com/mikeo/harborbuddy/internal/docker"
)

// UpdateDecision represents whether and why a container should be updated
type UpdateDecision struct {
	Eligible    bool
	Reason      string
	NeedsUpdate bool
}

// DetermineEligibility checks if a container is eligible for updates
func DetermineEligibility(container docker.ContainerInfo, cfg config.UpdatesConfig) UpdateDecision {
	// Check the autoupdate label
	if label, exists := container.Labels["com.harborbuddy.autoupdate"]; exists {
		if label == "false" {
			return UpdateDecision{
				Eligible: false,
				Reason:   "label com.harborbuddy.autoupdate=false",
			}
		}
	}

	// Check deny patterns
	for _, pattern := range cfg.DenyImages {
		if matchesPattern(container.Image, pattern) {
			return UpdateDecision{
				Eligible: false,
				Reason:   "matches deny pattern: " + pattern,
			}
		}
	}

	// Check allow patterns (if not empty)
	if len(cfg.AllowImages) > 0 {
		allowed := false
		for _, pattern := range cfg.AllowImages {
			if matchesPattern(container.Image, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return UpdateDecision{
				Eligible: false,
				Reason:   "does not match any allow pattern",
			}
		}
	}

	return UpdateDecision{
		Eligible: true,
		Reason:   "eligible for updates",
	}
}

// matchesPattern checks if an image matches a pattern
// Supports:
// - "*" matches everything
// - "repo:tag" exact match
// - "repo:*" matches any tag for repo
// - "registry.io/org/*" matches any repo under registry.io/org/
func matchesPattern(image, pattern string) bool {
	// Universal wildcard
	if pattern == "*" {
		return true
	}

	// Exact match
	if image == pattern {
		return true
	}

	// Pattern with wildcards
	if strings.Contains(pattern, "*") {
		// Convert pattern to prefix/suffix matching
		if strings.HasSuffix(pattern, "*") {
			// e.g., "postgres:*" or "registry.io/org/*"
			prefix := strings.TrimSuffix(pattern, "*")
			return strings.HasPrefix(image, prefix)
		}
		if strings.HasPrefix(pattern, "*") {
			// e.g., "*:latest"
			suffix := strings.TrimPrefix(pattern, "*")
			return strings.HasSuffix(image, suffix)
		}
	}

	return false
}
