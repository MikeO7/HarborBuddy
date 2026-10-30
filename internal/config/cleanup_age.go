package config

import (
	"fmt"
	"path"
)

// MinAgeFor returns the minimum age in hours before a cleanup candidate may be removed.
// kind is the resource kind ("image", "volume", ...), name its name, dangling whether an
// image is untagged.
func (c CleanupConfig) MinAgeFor(kind, name string, dangling bool) int {
	switch {
	case kind == "image" && dangling && c.DanglingImageMinAgeHours != nil:
		return *c.DanglingImageMinAgeHours
	case kind == "volume" && c.CacheVolumeMinAgeHours != nil && c.isCacheVolume(name):
		return *c.CacheVolumeMinAgeHours
	default:
		return c.MinAgeHours
	}
}

func (c CleanupConfig) isCacheVolume(name string) bool {
	for _, pattern := range c.CacheVolumes {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

func validateCleanupOverrides(cfg CleanupConfig) error {
	for _, age := range []struct {
		name  string
		value *int
	}{
		{"cleanup.dangling_image_min_age_hours", cfg.DanglingImageMinAgeHours},
		{"cleanup.cache_volume_min_age_hours", cfg.CacheVolumeMinAgeHours},
	} {
		if err := validateAge(age.name, age.value); err != nil {
			return err
		}
	}
	if err := validateKeepImages(cfg.KeepImages); err != nil {
		return err
	}
	for _, pattern := range cfg.CacheVolumes {
		if pattern == "" {
			return fmt.Errorf("cleanup.cache_volumes contains an empty pattern")
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("cleanup.cache_volumes pattern %q is invalid: %w", pattern, err)
		}
	}
	return nil
}

func validateAge(name string, value *int) error {
	switch {
	case value == nil:
		return nil
	case *value < 0:
		return fmt.Errorf("%s cannot be negative", name)
	case *value > maxCleanupMinAgeHours:
		return fmt.Errorf("%s cannot exceed %d", name, maxCleanupMinAgeHours)
	default:
		return nil
	}
}

// applyCleanupOverrideEnvironment reads the per-resource age settings.
func (c *Config) applyCleanupOverrideEnvironment(getenv environmentReader) error {
	for _, age := range []struct {
		name   string
		target **int
	}{
		{"HARBORBUDDY_CLEANUP_DANGLING_IMAGE_MIN_AGE_HOURS", &c.Cleanup.DanglingImageMinAgeHours},
		{"HARBORBUDDY_CLEANUP_CACHE_VOLUME_MIN_AGE_HOURS", &c.Cleanup.CacheVolumeMinAgeHours},
	} {
		if getenv(age.name) == "" {
			continue
		}
		var value int
		if err := applyInt(getenv, age.name, &value); err != nil {
			return err
		}
		*age.target = &value
	}
	if raw := getenv("HARBORBUDDY_CLEANUP_CACHE_VOLUMES"); raw != "" {
		c.Cleanup.CacheVolumes = splitList(raw)
	}
	if raw := getenv("HARBORBUDDY_CLEANUP_KEEP_IMAGES"); raw != "" {
		c.Cleanup.KeepImages = splitList(raw)
	}
	return nil
}
