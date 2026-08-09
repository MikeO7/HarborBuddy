package updater

import (
	"strings"
	"testing"
)

func FuzzMatchesPattern(f *testing.F) {
	f.Add("nginx:latest", "nginx:latest")
	f.Add("ghcr.io/org/app:latest", "ghcr.io/org/*")
	f.Add("redis:alpine", "*:alpine")
	f.Add("anything", "*")
	f.Add("image", "bad*pattern*")

	f.Fuzz(func(t *testing.T, image, pattern string) {
		got := matchesPattern(image, pattern)
		switch {
		case pattern == "*" || image == pattern:
			if !got {
				t.Fatalf("universal or exact pattern did not match: image=%q pattern=%q", image, pattern)
			}
		case strings.HasSuffix(pattern, "*"):
			want := strings.HasPrefix(image, strings.TrimSuffix(pattern, "*"))
			if got != want {
				t.Fatalf("prefix pattern result=%v want=%v", got, want)
			}
		case strings.HasPrefix(pattern, "*"):
			want := strings.HasSuffix(image, strings.TrimPrefix(pattern, "*"))
			if got != want {
				t.Fatalf("suffix pattern result=%v want=%v", got, want)
			}
		default:
			if got {
				t.Fatalf("unsupported pattern matched: image=%q pattern=%q", image, pattern)
			}
		}
	})
}
