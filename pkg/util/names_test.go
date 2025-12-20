package util

import "testing"

func TestGetImageFriendlyName(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected string
	}{
		{
			name:     "nil labels",
			labels:   nil,
			expected: "",
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: "",
		},
		{
			name: "opencontainers title",
			labels: map[string]string{
				"org.opencontainers.image.title": "my-app",
			},
			expected: "my-app",
		},
		{
			name: "docker compose service",
			labels: map[string]string{
				"com.docker.compose.service": "web",
			},
			expected: "web",
		},
		{
			name: "priority check",
			labels: map[string]string{
				"org.opencontainers.image.title": "primary",
				"com.docker.compose.service":     "secondary",
			},
			expected: "primary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetImageFriendlyName(tt.labels); got != tt.expected {
				t.Errorf("GetImageFriendlyName() = %v, want %v", got, tt.expected)
			}
		})
	}
}
