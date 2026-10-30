package config

import (
	"strings"
	"testing"
)

func TestMatchImagePattern(t *testing.T) {
	for _, tc := range []struct {
		image, pattern string
		want           bool
	}{
		{"openthread/border-router:latest", "openthread/border-router:*", true},
		{"openthread/border-router:latest", "*", true},
		{"local/openbot-tools:1", "local/openbot-tools:1", true},
		{"ghcr.io/x/app:1", "*/app:1", true},
		{"ghcr.io/x/app:1", "local/*", false},
		{"app:1", "app:2", false},
	} {
		if got := MatchImagePattern(tc.image, tc.pattern); got != tc.want {
			t.Errorf("MatchImagePattern(%q, %q) = %v; want %v", tc.image, tc.pattern, got, tc.want)
		}
	}
}

func TestKeepsImage(t *testing.T) {
	cfg := CleanupConfig{KeepImages: []string{"openthread/border-router:*", "local/openbot-tools:*"}}
	for _, tc := range []struct {
		tags []string
		want bool
	}{
		{[]string{"openthread/border-router:latest"}, true},
		{[]string{"other:1", "local/openbot-tools:1"}, true},
		{[]string{"ghcr.io/mikeo7/harborbuddy:latest"}, false},
		{nil, false},
	} {
		if got := cfg.KeepsImage(tc.tags); got != tc.want {
			t.Errorf("KeepsImage(%v) = %v; want %v", tc.tags, got, tc.want)
		}
	}
	if (CleanupConfig{}).KeepsImage([]string{"app:1"}) {
		t.Error("empty keep list kept an image")
	}
}

func TestKeepImagesConfigAndValidation(t *testing.T) {
	env := map[string]string{"HARBORBUDDY_CLEANUP_KEEP_IMAGES": " openthread/border-router:*, ,local/openbot-tools:* "}
	cfg := Default()
	if err := cfg.ApplyEnvironment(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Cleanup.KeepImages, ","); got != "openthread/border-router:*,local/openbot-tools:*" {
		t.Fatalf("KeepImages = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	cfg.Cleanup.KeepImages = []string{"a*b*"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `cleanup.keep_images pattern "a*b*"`) {
		t.Fatalf("Validate() = %v", err)
	}
	cfg.Cleanup.KeepImages = []string{""}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cleanup.keep_images contains an empty pattern") {
		t.Fatalf("Validate() = %v", err)
	}
}
