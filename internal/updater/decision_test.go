package updater

import (
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name, image, pattern string
		want                 bool
	}{
		{name: "exact", image: "nginx:latest", pattern: "nginx:latest", want: true},
		{name: "universal", image: "anything:tag", pattern: "*", want: true},
		{name: "prefix", image: "ghcr.io/org/app:latest", pattern: "ghcr.io/org/*", want: true},
		{name: "suffix", image: "redis:alpine", pattern: "*:alpine", want: true},
		{name: "mismatch", image: "redis:latest", pattern: "nginx:*", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesPattern(test.image, test.pattern); got != test.want {
				t.Fatalf("matchesPattern(%q, %q) = %v, want %v", test.image, test.pattern, got, test.want)
			}
		})
	}
}

func TestDetermineEligibility(t *testing.T) {
	tests := []struct {
		name      string
		container docker.ContainerSummary
		cfg       config.UpdatesConfig
		eligible  bool
		reason    string
	}{
		{
			name:      "default eligible",
			container: docker.ContainerSummary{ImageRef: "nginx:latest"},
			cfg:       config.UpdatesConfig{AllowImages: []string{"*"}},
			eligible:  true,
			reason:    "eligible for updates",
		},
		{
			name: "explicit opt out",
			container: docker.ContainerSummary{ImageRef: "nginx:latest", Labels: map[string]string{
				AutoUpdateLabel: "false",
			}},
			cfg:    config.UpdatesConfig{AllowImages: []string{"*"}},
			reason: "label com.harborbuddy.autoupdate=false",
		},
		{
			name:      "deny wins",
			container: docker.ContainerSummary{ImageRef: "postgres:16"},
			cfg:       config.UpdatesConfig{AllowImages: []string{"*"}, DenyImages: []string{"postgres:*"}},
			reason:    "matches deny pattern: postgres:*",
		},
		{
			name:      "allow mismatch",
			container: docker.ContainerSummary{ImageRef: "redis:latest"},
			cfg:       config.UpdatesConfig{AllowImages: []string{"nginx:*"}},
			reason:    "does not match any allow pattern",
		},
		{
			name: "daemon role does not imply self",
			container: docker.ContainerSummary{ImageRef: "harborbuddy:latest", Labels: map[string]string{
				"com.harborbuddy.role": "daemon",
			}},
			cfg:      config.UpdatesConfig{AllowImages: []string{"*"}},
			eligible: true,
			reason:   "eligible for updates",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := DetermineEligibility(test.container, test.cfg)
			if decision.Eligible != test.eligible || decision.Reason != test.reason {
				t.Fatalf("DetermineEligibility() = %+v, want eligible=%v reason=%q", decision, test.eligible, test.reason)
			}
		})
	}
}
