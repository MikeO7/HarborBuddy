package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete HarborBuddy configuration
type Config struct {
	Docker  DockerConfig  `yaml:"docker"`
	Updates UpdatesConfig `yaml:"updates"`
	Cleanup CleanupConfig `yaml:"cleanup"`
	Log     LogConfig     `yaml:"log"`

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
	DryRun        bool          `yaml:"dry_run"`
	AllowImages   []string      `yaml:"allow_images"`
	DenyImages    []string      `yaml:"deny_images"`
}

// CleanupConfig holds image cleanup settings
type CleanupConfig struct {
	Enabled      bool `yaml:"enabled"`
	MinAgeHours  int  `yaml:"min_age_hours"`
	DanglingOnly bool `yaml:"dangling_only"`
}

// LogConfig holds logging settings
type LogConfig struct {
	Level string `yaml:"level"`
	JSON  bool   `yaml:"json"`
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
			DryRun:        false,
			AllowImages:   []string{"*"},
			DenyImages:    []string{},
		},
		Cleanup: CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: true,
		},
		Log: LogConfig{
			Level: "info",
			JSON:  false,
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

	return cfg, nil
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

	if val := os.Getenv("HARBORBUDDY_DRY_RUN"); val != "" {
		if dryRun, err := strconv.ParseBool(val); err == nil {
			c.Updates.DryRun = dryRun
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
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Docker.Host == "" {
		return fmt.Errorf("docker.host cannot be empty")
	}

	if c.Updates.CheckInterval <= 0 {
		return fmt.Errorf("updates.check_interval must be positive")
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
