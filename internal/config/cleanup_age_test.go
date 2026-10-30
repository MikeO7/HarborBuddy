package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestCleanupMinAgeForDefaultsToMinAgeHours(t *testing.T) {
	cfg := CleanupConfig{MinAgeHours: 168}
	for _, tc := range []struct {
		kind, name string
		dangling   bool
	}{
		{"image", "app:1", false}, {"image", "", true}, {"volume", "hb-gocache", false}, {"container", "x", false},
	} {
		if got := cfg.MinAgeFor(tc.kind, tc.name, tc.dangling); got != 168 {
			t.Errorf("MinAgeFor(%s, %s, %v) = %d; want 168", tc.kind, tc.name, tc.dangling, got)
		}
	}
}

func TestCleanupMinAgeForOverrides(t *testing.T) {
	cfg := CleanupConfig{MinAgeHours: 168, DanglingImageMinAgeHours: intPtr(1),
		CacheVolumes: []string{"*gocache", "*gomod"}, CacheVolumeMinAgeHours: intPtr(0)}
	for _, tc := range []struct {
		kind, name string
		dangling   bool
		want       int
	}{
		{"image", "", true, 1},
		{"image", "app:1", false, 168},
		{"volume", "hb-gocache", false, 0},
		{"volume", "tf-gomod", false, 0},
		{"volume", "postgres-data", false, 168},
		{"network", "hb-gocache", false, 168},
		{"container", "", true, 168},
	} {
		if got := cfg.MinAgeFor(tc.kind, tc.name, tc.dangling); got != tc.want {
			t.Errorf("MinAgeFor(%s, %s, %v) = %d; want %d", tc.kind, tc.name, tc.dangling, got, tc.want)
		}
	}
	patternsOnly := CleanupConfig{MinAgeHours: 24, CacheVolumes: []string{"*gocache"}}
	if got := patternsOnly.MinAgeFor("volume", "x-gocache", false); got != 24 {
		t.Errorf("patterns without an age = %d; want min_age_hours 24", got)
	}
}

func TestCleanupAgeOverridesFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harborbuddy.yml")
	content := "cleanup:\n  min_age_hours: 168\n  dangling_image_min_age_hours: 1\n  cache_volumes: [\"*gocache\", \"*gomod\"]\n  cache_volume_min_age_hours: 2\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Cleanup
	if c.DanglingImageMinAgeHours == nil || *c.DanglingImageMinAgeHours != 1 || c.CacheVolumeMinAgeHours == nil ||
		*c.CacheVolumeMinAgeHours != 2 || strings.Join(c.CacheVolumes, ",") != "*gocache,*gomod" {
		t.Fatalf("cleanup = %+v", c)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestCleanupAgeOverridesFromEnvironment(t *testing.T) {
	env := map[string]string{
		"HARBORBUDDY_CLEANUP_DANGLING_IMAGE_MIN_AGE_HOURS": "1",
		"HARBORBUDDY_CLEANUP_CACHE_VOLUMES":                " *gocache, ,*gomod ",
		"HARBORBUDDY_CLEANUP_CACHE_VOLUME_MIN_AGE_HOURS":   "0",
	}
	cfg := Default()
	if err := cfg.ApplyEnvironment(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	c := cfg.Cleanup
	if *c.DanglingImageMinAgeHours != 1 || *c.CacheVolumeMinAgeHours != 0 || strings.Join(c.CacheVolumes, ",") != "*gocache,*gomod" {
		t.Fatalf("cleanup = %+v", c)
	}
	for _, name := range []string{"HARBORBUDDY_CLEANUP_DANGLING_IMAGE_MIN_AGE_HOURS", "HARBORBUDDY_CLEANUP_CACHE_VOLUME_MIN_AGE_HOURS"} {
		cfg := Default()
		if err := cfg.ApplyEnvironment(func(k string) string {
			if k == name {
				return "soon"
			}
			return ""
		}); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=soon: err = %v", name, err)
		}
	}
	untouched := Default()
	if err := untouched.ApplyEnvironment(func(string) string { return "" }); err != nil || untouched.Cleanup.DanglingImageMinAgeHours != nil ||
		untouched.Cleanup.CacheVolumeMinAgeHours != nil || untouched.Cleanup.CacheVolumes != nil {
		t.Fatalf("empty environment changed overrides: %+v, %v", untouched.Cleanup, err)
	}
}

func TestValidateCleanupAgeOverrides(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*CleanupConfig)
		want string
	}{
		{"negative dangling", func(c *CleanupConfig) { c.DanglingImageMinAgeHours = intPtr(-1) }, "cleanup.dangling_image_min_age_hours cannot be negative"},
		{"huge dangling", func(c *CleanupConfig) { c.DanglingImageMinAgeHours = intPtr(maxCleanupMinAgeHours + 1) }, "cleanup.dangling_image_min_age_hours cannot exceed"},
		{"negative cache", func(c *CleanupConfig) { c.CacheVolumeMinAgeHours = intPtr(-1) }, "cleanup.cache_volume_min_age_hours cannot be negative"},
		{"huge cache", func(c *CleanupConfig) { c.CacheVolumeMinAgeHours = intPtr(maxCleanupMinAgeHours + 1) }, "cleanup.cache_volume_min_age_hours cannot exceed"},
		{"empty pattern", func(c *CleanupConfig) { c.CacheVolumes = []string{""} }, "cleanup.cache_volumes contains an empty pattern"},
		{"bad pattern", func(c *CleanupConfig) { c.CacheVolumes = []string{"[gocache"} }, `cleanup.cache_volumes pattern "[gocache" is invalid`},
	} {
		cfg := Default()
		tc.edit(&cfg.Cleanup)
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Validate() = %v; want %q", tc.name, err, tc.want)
		}
	}
}
