package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (c Config) Validate() error {
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

func validateUpdates(cfg UpdatesConfig) error {
	if cfg.ScheduleTime == "" && cfg.CheckInterval <= 0 {
		return errors.New("updates.check_interval must be positive when schedule_time is not set")
	}
	if cfg.StopTimeout <= 0 {
		return errors.New("updates.stop_timeout must be positive")
	}
	if cfg.StartupTimeout <= 0 {
		return errors.New("updates.startup_timeout must be positive")
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
	return nil
}

func validateLog(cfg LogConfig) error {
	if !validLogLevel(cfg.Level) {
		return fmt.Errorf("log.level %q must be debug, info, warn, or error", cfg.Level)
	}
	if cfg.MaxSize <= 0 {
		return errors.New("log.max_size must be positive")
	}
	if cfg.MaxBackups < 0 {
		return errors.New("log.max_backups cannot be negative")
	}
	return nil
}

func validLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
