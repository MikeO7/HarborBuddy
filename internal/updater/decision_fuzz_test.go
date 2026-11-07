package updater

import (
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/imageref"
)

// rawPatternMatch is the documented pattern grammar applied to one string:
// "*", an exact value, or one wildcard at either edge.
func rawPatternMatch(image, pattern string) bool {
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

func FuzzMatchesPattern(f *testing.F) {
	f.Add("nginx:latest", "nginx:latest")
	f.Add("ghcr.io/org/app:latest", "ghcr.io/org/*")
	f.Add("redis:alpine", "*:alpine")
	f.Add("anything", "*")
	f.Add("image", "bad*pattern*")
	f.Add("nginx", "*:latest")
	f.Add("docker.io/library/nginx:1.27", "nginx:*")

	f.Fuzz(func(t *testing.T, image, pattern string) {
		got := matchesPattern(image, pattern)
		// A raw match must never be lost by normalization.
		if rawPatternMatch(image, pattern) && !got {
			t.Fatalf("raw match was rejected: image=%q pattern=%q", image, pattern)
		}
		// Every match must be explained by the raw grammar on some variant.
		explained := false
		for _, variant := range imageref.Variants(image) {
			explained = explained || rawPatternMatch(variant, pattern)
		}
		if got != explained {
			t.Fatalf("match=%v but variant explanation=%v: image=%q pattern=%q", got, explained, image, pattern)
		}
	})
}
