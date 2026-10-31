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

func (c CleanupConfig) isCacheVolume(name string) bool { return matchGlobs(c.CacheVolumes, name) }

// KeepsVolume reports whether a volume name matches cleanup.keep_volumes.
func (c CleanupConfig) KeepsVolume(name string) bool { return matchGlobs(c.KeepVolumes, name) }

func matchGlobs(patterns []string, name string) bool {
	for _, pattern := range patterns {
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
	if err := validateGlobs("cleanup.cache_volumes", cfg.CacheVolumes); err != nil {
		return err
	}
	if err := validateKeepVolumeLabels(cfg.KeepVolumeLabels); err != nil {
		return err
	}
	return validateGlobs("cleanup.keep_volumes", cfg.KeepVolumes)
}

func validateGlobs(setting string, patterns []string) error {
	for _, pattern := range patterns {
		if pattern == "" {
			return fmt.Errorf("%s contains an empty pattern", setting)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("%s pattern %q is invalid: %w", setting, pattern, err)
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
	if raw := getenv("HARBORBUDDY_CLEANUP_KEEP_VOLUMES"); raw != "" {
		c.Cleanup.KeepVolumes = splitList(raw)
	}
	if raw := getenv("HARBORBUDDY_CLEANUP_KEEP_VOLUME_LABELS"); raw != "" {
		c.Cleanup.KeepVolumeLabels = splitList(raw)
	}
	return nil
}
