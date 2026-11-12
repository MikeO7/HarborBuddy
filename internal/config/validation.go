package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxCleanupMinAgeHours = int((1<<63 - 1) / int64(time.Hour))

// minCheckInterval is the shortest accepted interval; shorter values would spin the scheduler.
const minCheckInterval = time.Second

func (c Config) Validate() error {
	if err := validateInterval(c); err != nil {
		return err
	}
	if err := validateUpdates(c.Updates); err != nil {
		return err
	}
	if err := validateImagePatterns(c.Updates.AllowImages, c.Updates.DenyImages); err != nil {
		return err
	}
	if err := validateCleanup(c.Cleanup); err != nil {
		return err
	}
	return validateLog(c.Log)
}

// validateInterval checks the interval only when the interval scheduler will
// use it: a daily schedule and the one-shot modes never read it.
func validateInterval(c Config) error {
	if c.Updates.ScheduleTime != "" || c.RunOnce || c.CleanupOnly || c.Updates.CheckInterval >= minCheckInterval {
		return nil
	}
	return fmt.Errorf("updates.check_interval must be at least %s when schedule_time is not set", minCheckInterval)
}

func validateUpdates(cfg UpdatesConfig) error {
	if cfg.StopTimeout <= 0 {
		return errors.New("updates.stop_timeout must be positive")
	}
	if cfg.StartupTimeout <= 0 {
		return errors.New("updates.startup_timeout must be positive")
	}
	if cfg.RollbackImageRetention < 0 {
		return errors.New("updates.rollback_image_retention cannot be negative")
	}
	if cfg.ScheduleTime == "" {
		return nil
	}
	if _, err := time.Parse("15:04", cfg.ScheduleTime); err != nil {
		return fmt.Errorf("updates.schedule_time must use HH:MM: %w", err)
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return fmt.Errorf("updates.timezone %q is invalid: %w", cfg.Timezone, err)
	}
	return nil
}

func validateImagePatterns(allow, deny []string) error {
	patterns := make([]string, 0, len(allow)+len(deny))
	patterns = append(patterns, allow...)
	patterns = append(patterns, deny...)
	for _, pattern := range patterns {
		if pattern == "" {
			return fmt.Errorf("updates.allow_images and updates.deny_images must not contain an empty pattern")
		}
		if !validImagePattern(pattern) {
			return fmt.Errorf("unsupported image pattern %q: wildcard must be at one edge", pattern)
		}
	}
	return nil
}

func validImagePattern(pattern string) bool {
	wildcards := strings.Count(pattern, "*")
	if wildcards == 0 || pattern == "*" {
		return true
	}
	return wildcards == 1 && (strings.HasPrefix(pattern, "*") || strings.HasSuffix(pattern, "*"))
}

func validateCleanup(cfg CleanupConfig) error {
	if cfg.MinAgeHours < 0 {
		return errors.New("cleanup.min_age_hours cannot be negative")
	}
	if cfg.MinAgeHours > maxCleanupMinAgeHours {
		return fmt.Errorf("cleanup.min_age_hours cannot exceed %d", maxCleanupMinAgeHours)
	}
	return validateCleanupOverrides(cfg)
}

func validateLog(cfg LogConfig) error {
	if !validLogLevel(cfg.Level) {
		return fmt.Errorf("log.level %q must be debug, info, warn, or error", cfg.Level)
	}
	if cfg.MaxSize <= 0 {
		return errors.New("log.max_size must be positive")
	}
	if cfg.MaxBackups < 0 {
		return errors.New("log.max_backups must not be negative")
	}
	// Only file logging rotates, so 0 is harmless without a log file.
	if cfg.File != "" && cfg.MaxBackups < 1 {
		return errors.New("log.max_backups must be at least 1 when log.file is set (the rotation library treats 0 as keep every rotated file)")
	}
	return nil
}

func validLogLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
