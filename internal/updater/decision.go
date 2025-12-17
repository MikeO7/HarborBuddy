package updater

import (
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
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
	// Check for wildcards directly to avoid full string search if possible
	// Optimization: Avoid strings.Contains, strings.HasSuffix, and strings.TrimSuffix
	// for common wildcard patterns to reduce allocations and CPU cycles.
	pLen := len(pattern)
	if pLen > 0 {
		if pattern[pLen-1] == '*' {
			// e.g., "postgres:*" or "registry.io/org/*"
			// Check if image starts with pattern[:pLen-1]
			// This avoids allocating a new string for the prefix
			return strings.HasPrefix(image, pattern[:pLen-1])
		}
		if pattern[0] == '*' {
			// e.g., "*:latest"
			// Check if image ends with pattern[1:]
			return strings.HasSuffix(image, pattern[1:])
		}
	}

	return false
}
