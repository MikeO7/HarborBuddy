package updater

import (
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		pattern  string
		expected bool
	}{
		// Exact Match
		{"exact match", "nginx:latest", "nginx:latest", true},
		{"exact match fail", "nginx:latest", "nginx:1.19", false},

		// Wildcard *
		{"universal wildcard", "anything:at-all", "*", true},
		{"universal wildcard empty", "", "*", true},

		// Prefix Match (repo:*)
		{"prefix match", "nginx:latest", "nginx:*", true},
		{"prefix match 2", "postgres:14", "postgres:*", true},
		{"prefix match fail", "redis:latest", "nginx:*", false},
		{"prefix registry match", "ghcr.io/org/image:tag", "ghcr.io/org/*", true},

		// Suffix Match (*:tag)
		{"suffix match", "nginx:latest", "*:latest", true},
		{"suffix match fail", "nginx:alpine", "*:latest", false},
		{"suffix match 2", "redis:alpine", "*:alpine", true},

		// No Wildcard
		{"no wildcard partial fail", "nginx:latest", "nginx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.image, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.image, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestDetermineEligibility(t *testing.T) {
	tests := []struct {
		name           string
		container      docker.ContainerInfo
		config         config.UpdatesConfig
		expectEligible bool
		expectReason   string
	}{
		{
			name: "default eligible",
			container: docker.ContainerInfo{
				Image:  "nginx:latest",
				Labels: map[string]string{},
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"*"},
				DenyImages:  []string{},
			},
			expectEligible: true,
			expectReason:   "eligible for updates",
		},
		{
			name: "label opt-out",
			container: docker.ContainerInfo{
				Image: "nginx:latest",
				Labels: map[string]string{
					"com.harborbuddy.autoupdate": "false",
				},
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"*"},
			},
			expectEligible: false,
			expectReason:   "label com.harborbuddy.autoupdate=false",
		},
		{
			name: "deny list match",
			container: docker.ContainerInfo{
				Image: "postgres:14",
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"*"},
				DenyImages:  []string{"postgres:*"},
			},
			expectEligible: false,
			expectReason:   "matches deny pattern: postgres:*",
		},
		{
			name: "allow list match",
			container: docker.ContainerInfo{
				Image: "nginx:latest",
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"nginx:*"},
			},
			expectEligible: true,
			expectReason:   "eligible for updates",
		},
		{
			name: "allow list mismatch",
			container: docker.ContainerInfo{
				Image: "redis:latest",
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"nginx:*"},
			},
			expectEligible: false,
			expectReason:   "does not match any allow pattern",
		},
		{
			name: "deny takes precedence over allow",
			container: docker.ContainerInfo{
				Image: "nginx:latest",
			},
			config: config.UpdatesConfig{
				AllowImages: []string{"nginx:*"},
				DenyImages:  []string{"nginx:*"},
			},
			expectEligible: false,
			expectReason:   "matches deny pattern: nginx:*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DetermineEligibility(tt.container, tt.config)

			if decision.Eligible != tt.expectEligible {
				t.Errorf("Eligible = %v, want %v", decision.Eligible, tt.expectEligible)
			}

			if decision.Reason != tt.expectReason {
				t.Errorf("Reason = %q, want %q", decision.Reason, tt.expectReason)
			}
		})
	}
}
