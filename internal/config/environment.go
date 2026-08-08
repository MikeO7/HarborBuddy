package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type environmentReader func(string) string

func (c *Config) ApplyEnvironmentOverrides() error {
	return c.ApplyEnvironment(os.Getenv)
}

func (c *Config) ApplyEnvironment(getenv func(string) string) error {
	if err := c.applyDockerEnvironment(getenv); err != nil {
		return err
	}
	if err := c.applyUpdateEnvironment(getenv); err != nil {
		return err
	}
	if err := c.applyCleanupEnvironment(getenv); err != nil {
		return err
	}
	return c.applyLogEnvironment(getenv)
}

func (c *Config) applyDockerEnvironment(getenv environmentReader) error {
	applyString(getenv, "HARBORBUDDY_DOCKER_HOST", &c.Docker.Host)
	return nil
}

func (c *Config) applyUpdateEnvironment(getenv environmentReader) error {
	if err := applyDuration(getenv, "HARBORBUDDY_INTERVAL", &c.Updates.CheckInterval); err != nil {
		return err
	}
	applyString(getenv, "HARBORBUDDY_SCHEDULE_TIME", &c.Updates.ScheduleTime)
	applyTimezone(getenv, &c.Updates.Timezone)
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
	return applyBool(getenv, "HARBORBUDDY_UPDATES_ENABLED", &c.Updates.Enabled)
}

func (c *Config) applyCleanupEnvironment(getenv environmentReader) error {
	return applyBool(getenv, "HARBORBUDDY_CLEANUP_ENABLED", &c.Cleanup.Enabled)
}

func (c *Config) applyLogEnvironment(getenv environmentReader) error {
	applyString(getenv, "HARBORBUDDY_LOG_LEVEL", &c.Log.Level)
	if err := applyBool(getenv, "HARBORBUDDY_LOG_JSON", &c.Log.JSON); err != nil {
		return err
	}
	applyString(getenv, "HARBORBUDDY_LOG_FILE", &c.Log.File)
	if err := applyInt(getenv, "HARBORBUDDY_LOG_MAX_SIZE", &c.Log.MaxSize); err != nil {
		return err
	}
	return applyInt(getenv, "HARBORBUDDY_LOG_MAX_BACKUPS", &c.Log.MaxBackups)
}

func applyTimezone(getenv environmentReader, target *string) {
	if value := getenv("HARBORBUDDY_TIMEZONE"); value != "" {
		*target = value
		return
	}
	applyString(getenv, "TZ", target)
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
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return invalidEnv(name, value, err)
	}
	*target = parsed
	return nil
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
