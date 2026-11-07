package config

import (
	"fmt"
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/imageref"
)

// MatchImagePattern reports whether image matches pattern: "*", an exact reference, or one
// wildcard at the start or end. Update allow/deny lists and cleanup.keep_images share it.
func MatchImagePattern(image, pattern string) bool {
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

// KeepsImage reports whether any of an image's tags matches cleanup.keep_images.
// Like the update allow and deny lists, a pattern may match the short form
// (nginx:1.27) or the fully qualified form (docker.io/library/nginx:1.27).
func (c CleanupConfig) KeepsImage(tags []string) bool {
	for _, tag := range tags {
		for _, pattern := range c.KeepImages {
			if imageref.MatchAny(tag, pattern, MatchImagePattern) {
				return true
			}
		}
	}
	return false
}

func validateKeepImages(patterns []string) error {
	for _, pattern := range patterns {
		if pattern == "" {
			return fmt.Errorf("cleanup.keep_images contains an empty pattern")
		}
		if !validImagePattern(pattern) {
			return fmt.Errorf("cleanup.keep_images pattern %q is invalid: wildcard must be at one edge", pattern)
		}
	}
	return nil
}

// splitList reads a comma-separated environment list, dropping blanks.
func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
