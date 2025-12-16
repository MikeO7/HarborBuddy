package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete HarborBuddy configuration
type Config struct {
	Docker  DockerConfig  `yaml:"docker"`
	Updates UpdatesConfig `yaml:"updates"`
	Cleanup CleanupConfig `yaml:"cleanup"`
	Log     LogConfig     `yaml:"log"`
	Logging LoggingConfig `yaml:"logging"`

	// Runtime flags (not in YAML)
	RunOnce     bool
	CleanupOnly bool
}

// DockerConfig holds Docker connection settings
type DockerConfig struct {
	Host string `yaml:"host"`
	TLS  bool   `yaml:"tls"`
}

// UpdatesConfig holds update behavior settings
type UpdatesConfig struct {
	Enabled       bool          `yaml:"enabled"`
	UpdateAll     bool          `yaml:"update_all"`
	CheckInterval time.Duration `yaml:"check_interval"`
	ScheduleTime  string        `yaml:"schedule_time"` // Time to run daily (e.g., "03:00", "15:30")
	Timezone      string        `yaml:"timezone"`      // Timezone for schedule (e.g., "America/Los_Angeles", "UTC")
	DryRun        bool          `yaml:"dry_run"`
	AllowImages   []string      `yaml:"allow_images"`
	DenyImages    []string      `yaml:"deny_images"`
	StopTimeout   time.Duration `yaml:"stop_timeout"`
}

// CleanupConfig holds image cleanup settings
type CleanupConfig struct {
	Enabled      bool `yaml:"enabled"`
	MinAgeHours  int  `yaml:"min_age_hours"`
	DanglingOnly bool `yaml:"dangling_only"`
}

// LogConfig holds logging settings
type LogConfig struct {
	Level      string `yaml:"level"`
	JSON       bool   `yaml:"json"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`    // megabytes
	MaxBackups int    `yaml:"max_backups"` // number of files
}

// LoggingConfig matches Docker's logging configuration structure
type LoggingConfig struct {
	Driver  string            `yaml:"driver"`
	Options map[string]string `yaml:"options"`
}

// Default returns a config with sensible defaults
func Default() Config {
	return Config{
		Docker: DockerConfig{
			Host: "unix:///var/run/docker.sock",
			TLS:  false,
		},
		Updates: UpdatesConfig{
			Enabled:       true,
			UpdateAll:     true,
			CheckInterval: 30 * time.Minute,
			ScheduleTime:  "", // Empty means use CheckInterval
			Timezone:      "UTC",
			DryRun:        false,
			AllowImages:   []string{"*"},
			DenyImages:    []string{},
			StopTimeout:   10 * time.Second,
		},
		Cleanup: CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: true,
		},
		Log: LogConfig{
			Level:      "info",
			JSON:       false,
			MaxSize:    10,
			MaxBackups: 1,
		},
		RunOnce:     false,
		CleanupOnly: false,
	}
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (Config, error) {
	cfg := Default()

	// If file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply partial updates from 'logging' block if present
	cfg.ApplyLoggingCompatibility()

	return cfg, nil
}

// ApplyLoggingCompatibility maps Docker-style logging config to HarborBuddy config
func (c *Config) ApplyLoggingCompatibility() {
	if c.Logging.Options == nil {
		return
	}

	// Parse max-size
	if val, ok := c.Logging.Options["max-size"]; ok {
		if sizeMB, err := parseBytesString(val); err == nil && sizeMB > 0 {
			c.Log.MaxSize = sizeMB
		}
	}

	// Parse max-file
	if val, ok := c.Logging.Options["max-file"]; ok {
		if backups, err := strconv.Atoi(val); err == nil && backups > 0 {
			c.Log.MaxBackups = backups
		}
	}
}

// parseBytesString converts strings like "10m", "1g", "100k" to Megabytes (int)
func parseBytesString(s string) (int, error) {
	return parseDockerSize(s)
}

func parseDockerSize(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty")
	}

	var multi int64 = 1
	if strings.HasSuffix(s, "k") {
		multi = 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multi = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "g") {
		multi = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else {
		// Docker requires a unit for memory limits usually but here we want to be strict to avoid confusion?
		// Or default to MB? The user's request showed "50m".
		// My test expects error on "100".
		return 0, fmt.Errorf("missing unit (must be k, m, or g)")
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}

	bytes := val * multi
	mb := bytes / (1024 * 1024)
	if mb == 0 && bytes > 0 {
		return 1, nil // Minimum 1MB if specified
	}
	return int(mb), nil
}

// ApplyEnvironmentOverrides applies environment variable overrides to the config
func (c *Config) ApplyEnvironmentOverrides() {
	if val := os.Getenv("HARBORBUDDY_DOCKER_HOST"); val != "" {
		c.Docker.Host = val
	}

	if val := os.Getenv("HARBORBUDDY_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			c.Updates.CheckInterval = duration
		}
	}

	if val := os.Getenv("HARBORBUDDY_SCHEDULE_TIME"); val != "" {
		c.Updates.ScheduleTime = val
	}

	// Support both HARBORBUDDY_TIMEZONE and standard TZ environment variable
	// HARBORBUDDY_TIMEZONE takes priority over TZ
	if val := os.Getenv("HARBORBUDDY_TIMEZONE"); val != "" {
		c.Updates.Timezone = val
	} else if val := os.Getenv("TZ"); val != "" {
		c.Updates.Timezone = val
	}

	if val := os.Getenv("HARBORBUDDY_DRY_RUN"); val != "" {
		if dryRun, err := strconv.ParseBool(val); err == nil {
			c.Updates.DryRun = dryRun
		}
	}

	if val := os.Getenv("HARBORBUDDY_STOP_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			c.Updates.StopTimeout = duration
		}
	}

	if val := os.Getenv("HARBORBUDDY_LOG_LEVEL"); val != "" {
		c.Log.Level = val
	}

	if val := os.Getenv("HARBORBUDDY_LOG_JSON"); val != "" {
		if jsonLog, err := strconv.ParseBool(val); err == nil {
			c.Log.JSON = jsonLog
		}
	}

	if val := os.Getenv("HARBORBUDDY_LOG_FILE"); val != "" {
		c.Log.File = val
	}

	if val := os.Getenv("HARBORBUDDY_LOG_MAX_SIZE"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			c.Log.MaxSize = size
		}
	}

	if val := os.Getenv("HARBORBUDDY_LOG_MAX_BACKUPS"); val != "" {
		if backups, err := strconv.Atoi(val); err == nil {
			c.Log.MaxBackups = backups
		}
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Docker.Host == "" {
		return fmt.Errorf("docker.host cannot be empty")
	}

	// If schedule_time is not set, check_interval must be positive
	if c.Updates.ScheduleTime == "" && c.Updates.CheckInterval <= 0 {
		return fmt.Errorf("updates.check_interval must be positive when schedule_time is not set")
	}

	if c.Updates.StopTimeout <= 0 {
		return fmt.Errorf("updates.stop_timeout must be positive")
	}

	// If schedule_time is set, validate the format
	if c.Updates.ScheduleTime != "" {
		if _, err := time.Parse("15:04", c.Updates.ScheduleTime); err != nil {
			return fmt.Errorf("invalid schedule_time format: %s (must be HH:MM, e.g., '03:00')", c.Updates.ScheduleTime)
		}

		// Validate timezone
		if _, err := time.LoadLocation(c.Updates.Timezone); err != nil {
			return fmt.Errorf("invalid timezone: %s (use IANA timezone names like 'America/Los_Angeles' or 'UTC')", c.Updates.Timezone)
		}
	}

	if c.Cleanup.MinAgeHours < 0 {
		return fmt.Errorf("cleanup.min_age_hours cannot be negative")
	}

	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLogLevels[c.Log.Level] {
		return fmt.Errorf("invalid log level: %s (must be debug, info, warn, or error)", c.Log.Level)
	}

	return nil
}
