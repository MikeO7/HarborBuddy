package updater

import (
	"testing"

	"github.com/mikeo/harborbuddy/internal/config"
	"github.com/mikeo/harborbuddy/internal/docker"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		pattern  string
		expected bool
	}{
		{"universal wildcard", "nginx:latest", "*", true},
		{"exact match", "nginx:latest", "nginx:latest", true},
		{"no match", "nginx:latest", "postgres:latest", false},
		{"tag wildcard", "nginx:latest", "nginx:*", true},
		{"tag wildcard no match", "postgres:latest", "nginx:*", false},
		{"prefix wildcard", "ghcr.io/org/app:v1", "ghcr.io/org/*", true},
		{"prefix wildcard no match", "docker.io/org/app:v1", "ghcr.io/org/*", false},
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
		name      string
		container docker.ContainerInfo
		cfg       config.UpdatesConfig
		eligible  bool
		reason    string
	}{
		{
			name: "default eligible",
			container: docker.ContainerInfo{
				Image:  "nginx:latest",
				Labels: map[string]string{},
			},
			cfg: config.UpdatesConfig{
				AllowImages: []string{"*"},
				DenyImages:  []string{},
			},
			eligible: true,
			reason:   "eligible for updates",
		},
		{
			name: "opt-out label",
			container: docker.ContainerInfo{
				Image: "nginx:latest",
				Labels: map[string]string{
					"com.harborbuddy.autoupdate": "false",
				},
			},
			cfg: config.UpdatesConfig{
				AllowImages: []string{"*"},
				DenyImages:  []string{},
			},
			eligible: false,
			reason:   "label com.harborbuddy.autoupdate=false",
		},
		{
			name: "deny pattern",
			container: docker.ContainerInfo{
				Image:  "postgres:15",
				Labels: map[string]string{},
			},
			cfg: config.UpdatesConfig{
				AllowImages: []string{"*"},
				DenyImages:  []string{"postgres:*"},
			},
			eligible: false,
			reason:   "matches deny pattern: postgres:*",
		},
		{
			name: "not in allow list",
			container: docker.ContainerInfo{
				Image:  "postgres:15",
				Labels: map[string]string{},
			},
			cfg: config.UpdatesConfig{
				AllowImages: []string{"nginx:*"},
				DenyImages:  []string{},
			},
			eligible: false,
			reason:   "does not match any allow pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DetermineEligibility(tt.container, tt.cfg)
			if decision.Eligible != tt.eligible {
				t.Errorf("DetermineEligibility() eligible = %v, want %v", decision.Eligible, tt.eligible)
			}
			if decision.Reason != tt.reason {
				t.Errorf("DetermineEligibility() reason = %q, want %q", decision.Reason, tt.reason)
			}
		})
	}
}
