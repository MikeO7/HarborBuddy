package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type environmentReader func(string) string

func (c *Config) ApplyEnvironment(getenv func(string) string) error {
	c.applyDockerEnvironment(getenv)
	if err := c.applyUpdateEnvironment(getenv); err != nil {
		return err
	}
	if err := c.applyCleanupEnvironment(getenv); err != nil {
		return err
	}
	return c.applyLogEnvironment(getenv)
}

func (c *Config) applyDockerEnvironment(getenv environmentReader) {
	applyString(getenv, "HARBORBUDDY_DOCKER_HOST", &c.Docker.Host)
}

func (c *Config) applyUpdateEnvironment(getenv environmentReader) error {
	if err := applyDuration(getenv, "HARBORBUDDY_INTERVAL", &c.Updates.CheckInterval); err != nil {
		return err
	}
	applyString(getenv, "HARBORBUDDY_SCHEDULE_TIME", &c.Updates.ScheduleTime)
	c.applyTimezone(getenv)
	if err := applyBool(getenv, "HARBORBUDDY_DRY_RUN", &c.Updates.DryRun); err != nil {
		return err
	}
	if err := applyBool(getenv, "HARBORBUDDY_SELF_UPDATE_ENABLED", &c.Updates.SelfUpdate); err != nil {
		return err
	}
	if err := applyDuration(getenv, "HARBORBUDDY_STOP_TIMEOUT", &c.Updates.StopTimeout); err != nil {
		return err
	}
	if err := applyDuration(getenv, "HARBORBUDDY_STARTUP_TIMEOUT", &c.Updates.StartupTimeout); err != nil {
		return err
	}
	if err := applyInt(getenv, "HARBORBUDDY_ROLLBACK_IMAGE_RETENTION", &c.Updates.RollbackImageRetention); err != nil {
		return err
	}
	return applyBool(getenv, "HARBORBUDDY_UPDATES_ENABLED", &c.Updates.Enabled)
}

func (c *Config) applyCleanupEnvironment(getenv environmentReader) error {
	settings := []struct {
		name   string
		target *bool
	}{
		{name: "HARBORBUDDY_CLEANUP_ENABLED", target: &c.Cleanup.Enabled},
		{name: "HARBORBUDDY_CLEANUP_DANGLING_ONLY", target: &c.Cleanup.DanglingOnly},
		{name: "HARBORBUDDY_CLEANUP_ALL", target: &c.Cleanup.All},
		{name: "HARBORBUDDY_CLEANUP_STOPPED_CONTAINERS", target: &c.Cleanup.StoppedContainers},
		{name: "HARBORBUDDY_CLEANUP_UNUSED_NETWORKS", target: &c.Cleanup.UnusedNetworks},
		{name: "HARBORBUDDY_CLEANUP_UNUSED_VOLUMES", target: &c.Cleanup.UnusedVolumes},
		{name: "HARBORBUDDY_CLEANUP_BUILD_CACHE", target: &c.Cleanup.BuildCache},
	}
	if err := applyInt(getenv, "HARBORBUDDY_CLEANUP_MIN_AGE_HOURS", &c.Cleanup.MinAgeHours); err != nil {
		return err
	}
	for _, setting := range settings {
		if err := applyBool(getenv, setting.name, setting.target); err != nil {
			return err
		}
	}
	return c.applyCleanupOverrideEnvironment(getenv)
}

func (c *Config) applyLogEnvironment(getenv environmentReader) error {
	if value := getenv("HARBORBUDDY_LOG_LEVEL"); value != "" {
		c.Log.Level = strings.ToLower(strings.TrimSpace(value))
	}
	if err := applyBool(getenv, "HARBORBUDDY_LOG_JSON", &c.Log.JSON); err != nil {
		return err
	}
	applyString(getenv, "HARBORBUDDY_LOG_FILE", &c.Log.File)
	if err := applyInt(getenv, "HARBORBUDDY_LOG_MAX_SIZE", &c.Log.MaxSize); err != nil {
		return err
	}
	return applyInt(getenv, "HARBORBUDDY_LOG_MAX_BACKUPS", &c.Log.MaxBackups)
}

// applyTimezone gives HARBORBUDDY_TIMEZONE precedence over updates.timezone, like every
// other HARBORBUDDY_* variable. The standard TZ variable is only a fallback for when neither
// names a timezone.
func (c *Config) applyTimezone(getenv environmentReader) {
	if value := getenv("HARBORBUDDY_TIMEZONE"); value != "" {
		c.Updates.Timezone = value
		return
	}
	if value := getenv("TZ"); value != "" && !c.Updates.timezoneConfigured {
		c.Updates.Timezone = value
	}
}

func applyString(getenv environmentReader, name string, target *string) {
	if value := getenv(name); value != "" {
		*target = value
	}
}

func applyDuration(getenv environmentReader, name string, target *time.Duration) error {
	value := getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return invalidEnv(name, value, err)
	}
	*target = parsed
	return nil
}

func applyBool(getenv environmentReader, name string, target *bool) error {
	value := getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := parseBool(value)
	if err != nil {
		return invalidEnv(name, value, err)
	}
	*target = parsed
	return nil
}

// parseBool accepts the boolean spellings YAML accepts, case-insensitively, plus 1/0 and the
// other strconv.ParseBool forms.
func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on":
		return true, nil
	case "no", "off":
		return false, nil
	}
	parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
	if err != nil {
		return false, fmt.Errorf("want true/false, yes/no, on/off, or 1/0: %w", err)
	}
	return parsed, nil
}

func applyInt(getenv environmentReader, name string, target *int) error {
	value := getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return invalidEnv(name, value, err)
	}
	*target = parsed
	return nil
}

func invalidEnv(name, value string, err error) error {
	return fmt.Errorf("invalid %s=%q: %w", name, value, err)
}
