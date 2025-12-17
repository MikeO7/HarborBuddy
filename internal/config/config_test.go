package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	t.Log("Testing default configuration values")

	cfg := Default()

	tests := []struct {
		name  string
		got   interface{}
		want  interface{}
		field string
	}{
		{"docker host", cfg.Docker.Host, "unix:///var/run/docker.sock", "Docker.Host"},
		{"docker tls", cfg.Docker.TLS, false, "Docker.TLS"},
		{"updates enabled", cfg.Updates.Enabled, true, "Updates.Enabled"},
		{"update all", cfg.Updates.UpdateAll, true, "Updates.UpdateAll"},
		{"check interval", cfg.Updates.CheckInterval, 30 * time.Minute, "Updates.CheckInterval"},
		{"dry run", cfg.Updates.DryRun, false, "Updates.DryRun"},
		{"cleanup enabled", cfg.Cleanup.Enabled, true, "Cleanup.Enabled"},
		{"min age hours", cfg.Cleanup.MinAgeHours, 24, "Cleanup.MinAgeHours"},
		{"dangling only", cfg.Cleanup.DanglingOnly, true, "Cleanup.DanglingOnly"},
		{"log level", cfg.Log.Level, "info", "Log.Level"},
		{"log json", cfg.Log.JSON, false, "Log.JSON"},
		{"log max size", cfg.Log.MaxSize, 10, "Log.MaxSize"},
		{"log max backups", cfg.Log.MaxBackups, 1, "Log.MaxBackups"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Default().%s = %v, want %v", tt.field, tt.got, tt.want)
				t.Logf("  Field: %s", tt.field)
				t.Logf("  Got:  %v (type: %T)", tt.got, tt.got)
				t.Logf("  Want: %v (type: %T)", tt.want, tt.want)
			} else {
				t.Logf("✓ %s correctly set to %v", tt.field, tt.want)
			}
		})
	}

	// Test allow images default
	t.Run("allow images default", func(t *testing.T) {
		if len(cfg.Updates.AllowImages) != 1 || cfg.Updates.AllowImages[0] != "*" {
			t.Errorf("Default().Updates.AllowImages = %v, want [*]", cfg.Updates.AllowImages)
			t.Logf("  Expected single wildcard entry")
			t.Logf("  Got: %v", cfg.Updates.AllowImages)
		} else {
			t.Logf("✓ AllowImages correctly defaulted to [*]")
		}
	})
}

func TestLoadFromFile(t *testing.T) {
	t.Log("Testing configuration file loading")

	t.Run("non-existent file returns defaults", func(t *testing.T) {
		t.Log("  Testing with non-existent file")
		cfg, err := LoadFromFile("/nonexistent/path/config.yml")
		if err != nil {
			t.Errorf("LoadFromFile() error = %v, want nil for non-existent file", err)
			t.Logf("  Should return defaults, not error, for missing file")
		}
		if cfg.Docker.Host != "unix:///var/run/docker.sock" {
			t.Errorf("LoadFromFile() with non-existent file should return defaults")
			t.Logf("  Got Docker.Host: %s", cfg.Docker.Host)
			t.Logf("  Want: unix:///var/run/docker.sock")
		} else {
			t.Logf("✓ Non-existent file correctly returns defaults")
		}
	})

	t.Run("valid yaml file", func(t *testing.T) {
		t.Log("  Creating temporary config file")
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yml")

		yamlContent := `
docker:
  host: "tcp://localhost:2375"
  tls: true

updates:
  enabled: true
  check_interval: "1h"
  dry_run: true
  allow_images:
    - "nginx:*"
  deny_images:
    - "postgres:*"

cleanup:
  enabled: false
  min_age_hours: 48
  dangling_only: false

log:
  level: "debug"
  json: true
  file: "/var/log/harborbuddy.log"
  max_size: 50
`
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}
		t.Logf("  Wrote test config to: %s", cfgPath)

		cfg, err := LoadFromFile(cfgPath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v, want nil", err)
		}
		t.Log("  Successfully loaded config file")

		tests := []struct {
			name  string
			got   interface{}
			want  interface{}
			field string
		}{
			{"docker host", cfg.Docker.Host, "tcp://localhost:2375", "Docker.Host"},
			{"docker tls", cfg.Docker.TLS, true, "Docker.TLS"},
			{"check interval", cfg.Updates.CheckInterval, time.Hour, "Updates.CheckInterval"},
			{"dry run", cfg.Updates.DryRun, true, "Updates.DryRun"},
			{"cleanup enabled", cfg.Cleanup.Enabled, false, "Cleanup.Enabled"},
			{"min age hours", cfg.Cleanup.MinAgeHours, 48, "Cleanup.MinAgeHours"},
			{"dangling only", cfg.Cleanup.DanglingOnly, false, "Cleanup.DanglingOnly"},
			{"log level", cfg.Log.Level, "debug", "Log.Level"},
			{"log json", cfg.Log.JSON, true, "Log.JSON"},
			{"log file", cfg.Log.File, "/var/log/harborbuddy.log", "Log.File"},
			{"log max size", cfg.Log.MaxSize, 50, "Log.MaxSize"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.got != tt.want {
					t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
					t.Logf("  YAML value not correctly parsed")
				} else {
					t.Logf("✓ %s correctly loaded: %v", tt.field, tt.want)
				}
			})
		}

		// Test arrays
		t.Run("allow_images", func(t *testing.T) {
			if len(cfg.Updates.AllowImages) != 1 || cfg.Updates.AllowImages[0] != "nginx:*" {
				t.Errorf("AllowImages = %v, want [nginx:*]", cfg.Updates.AllowImages)
				t.Logf("  Array parsing failed")
			} else {
				t.Logf("✓ AllowImages correctly loaded: %v", cfg.Updates.AllowImages)
			}
		})

		t.Run("deny_images", func(t *testing.T) {
			if len(cfg.Updates.DenyImages) != 1 || cfg.Updates.DenyImages[0] != "postgres:*" {
				t.Errorf("DenyImages = %v, want [postgres:*]", cfg.Updates.DenyImages)
				t.Logf("  Array parsing failed")
			} else {
				t.Logf("✓ DenyImages correctly loaded: %v", cfg.Updates.DenyImages)
			}
		})
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		t.Log("  Testing with invalid YAML")
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "bad.yml")

		invalidYAML := `
this is not valid yaml: [
  - broken
`
		if err := os.WriteFile(cfgPath, []byte(invalidYAML), 0644); err != nil {
			t.Fatalf("Failed to write bad config: %v", err)
		}

		_, err := LoadFromFile(cfgPath)
		if err == nil {
			t.Error("LoadFromFile() with invalid YAML should return error")
			t.Log("  Invalid YAML should not parse successfully")
		} else {
			t.Logf("✓ Invalid YAML correctly returned error: %v", err)
		}
	})
}

func TestApplyEnvironmentOverrides(t *testing.T) {
	t.Log("Testing environment variable overrides")

	// Save original env
	originalEnv := make(map[string]string)
	envVars := []string{
		"HARBORBUDDY_DOCKER_HOST",
		"HARBORBUDDY_INTERVAL",
		"HARBORBUDDY_DRY_RUN",
		"HARBORBUDDY_LOG_LEVEL",
		"HARBORBUDDY_LOG_JSON",
		"HARBORBUDDY_LOG_FILE",
		"HARBORBUDDY_LOG_MAX_SIZE",
		"HARBORBUDDY_LOG_MAX_BACKUPS",
	}
	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
	}
	defer func() {
		t.Log("Restoring original environment")
		for key, val := range originalEnv {
			if val != "" {
				os.Setenv(key, val)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	tests := []struct {
		name     string
		envKey   string
		envValue string
		check    func(*Config) (interface{}, interface{}, string)
	}{
		{
			name:     "docker host override",
			envKey:   "HARBORBUDDY_DOCKER_HOST",
			envValue: "tcp://remote:2376",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Docker.Host, "tcp://remote:2376", "Docker.Host"
			},
		},
		{
			name:     "interval override",
			envKey:   "HARBORBUDDY_INTERVAL",
			envValue: "2h",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Updates.CheckInterval, 2 * time.Hour, "Updates.CheckInterval"
			},
		},
		{
			name:     "dry run override true",
			envKey:   "HARBORBUDDY_DRY_RUN",
			envValue: "true",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Updates.DryRun, true, "Updates.DryRun"
			},
		},
		{
			name:     "log level override",
			envKey:   "HARBORBUDDY_LOG_LEVEL",
			envValue: "debug",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Log.Level, "debug", "Log.Level"
			},
		},
		{
			name:     "log json override",
			envKey:   "HARBORBUDDY_LOG_JSON",
			envValue: "true",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Log.JSON, true, "Log.JSON"
			},
		},
		{
			name:     "log file override",
			envKey:   "HARBORBUDDY_LOG_FILE",
			envValue: "/tmp/hb.log",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Log.File, "/tmp/hb.log", "Log.File"
			},
		},
		{
			name:     "log max size override",
			envKey:   "HARBORBUDDY_LOG_MAX_SIZE",
			envValue: "100",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Log.MaxSize, 100, "Log.MaxSize"
			},
		},
		{
			name:     "log max backups override",
			envKey:   "HARBORBUDDY_LOG_MAX_BACKUPS",
			envValue: "5",
			check: func(c *Config) (interface{}, interface{}, string) {
				return c.Log.MaxBackups, 5, "Log.MaxBackups"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Setting %s=%s", tt.envKey, tt.envValue)
			os.Setenv(tt.envKey, tt.envValue)
			defer os.Unsetenv(tt.envKey)

			cfg := Default()
			cfg.ApplyEnvironmentOverrides()

			got, want, field := tt.check(&cfg)
			if got != want {
				t.Errorf("%s = %v, want %v", field, got, want)
				t.Logf("  Environment override failed")
				t.Logf("  Env var: %s=%s", tt.envKey, tt.envValue)
			} else {
				t.Logf("✓ %s correctly overridden to %v", field, want)
			}
		})
	}

	// Additional test cases for previously uncovered environment variables
	t.Run("schedule time override", func(t *testing.T) {
		os.Setenv("HARBORBUDDY_SCHEDULE_TIME", "15:30")
		defer os.Unsetenv("HARBORBUDDY_SCHEDULE_TIME")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.ScheduleTime != "15:30" {
			t.Errorf("ScheduleTime = %s, want 15:30", cfg.Updates.ScheduleTime)
		}
	})

	t.Run("timezone override", func(t *testing.T) {
		os.Setenv("HARBORBUDDY_TIMEZONE", "America/New_York")
		defer os.Unsetenv("HARBORBUDDY_TIMEZONE")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.Timezone != "America/New_York" {
			t.Errorf("Timezone = %s, want America/New_York", cfg.Updates.Timezone)
		}
	})

	t.Run("TZ fallback for timezone", func(t *testing.T) {
		os.Setenv("TZ", "Europe/London")
		defer os.Unsetenv("TZ")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.Timezone != "Europe/London" {
			t.Errorf("Timezone = %s, want Europe/London", cfg.Updates.Timezone)
		}
	})

	t.Run("HARBORBUDDY_TIMEZONE takes priority over TZ", func(t *testing.T) {
		os.Setenv("TZ", "Europe/London")
		os.Setenv("HARBORBUDDY_TIMEZONE", "America/Los_Angeles")
		defer os.Unsetenv("TZ")
		defer os.Unsetenv("HARBORBUDDY_TIMEZONE")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.Timezone != "America/Los_Angeles" {
			t.Errorf("Timezone = %s, want America/Los_Angeles", cfg.Updates.Timezone)
		}
	})

	t.Run("stop timeout override", func(t *testing.T) {
		os.Setenv("HARBORBUDDY_STOP_TIMEOUT", "30s")
		defer os.Unsetenv("HARBORBUDDY_STOP_TIMEOUT")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.StopTimeout != 30*time.Second {
			t.Errorf("StopTimeout = %v, want 30s", cfg.Updates.StopTimeout)
		}
	})

	t.Run("updates enabled override", func(t *testing.T) {
		os.Setenv("HARBORBUDDY_UPDATES_ENABLED", "false")
		defer os.Unsetenv("HARBORBUDDY_UPDATES_ENABLED")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Updates.Enabled != false {
			t.Errorf("Updates.Enabled = %v, want false", cfg.Updates.Enabled)
		}
	})

	t.Run("cleanup enabled override", func(t *testing.T) {
		os.Setenv("HARBORBUDDY_CLEANUP_ENABLED", "false")
		defer os.Unsetenv("HARBORBUDDY_CLEANUP_ENABLED")

		cfg := Default()
		cfg.ApplyEnvironmentOverrides()

		if cfg.Cleanup.Enabled != false {
			t.Errorf("Cleanup.Enabled = %v, want false", cfg.Cleanup.Enabled)
		}
	})
}

func TestValidate(t *testing.T) {
	t.Log("Testing configuration validation")

	tests := []struct {
		name      string
		setup     func(*Config)
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid config",
			setup:     func(c *Config) {},
			wantError: false,
			errorMsg:  "",
		},
		{
			name: "empty docker host",
			setup: func(c *Config) {
				c.Docker.Host = ""
			},
			wantError: true,
			errorMsg:  "docker.host cannot be empty",
		},
		{
			name: "negative check interval",
			setup: func(c *Config) {
				c.Updates.CheckInterval = -1 * time.Second
			},
			wantError: true,
			errorMsg:  "check_interval must be positive",
		},
		{
			name: "zero check interval",
			setup: func(c *Config) {
				c.Updates.CheckInterval = 0
			},
			wantError: true,
			errorMsg:  "check_interval must be positive",
		},
		{
			name: "negative min age",
			setup: func(c *Config) {
				c.Cleanup.MinAgeHours = -1
			},
			wantError: true,
			errorMsg:  "min_age_hours cannot be negative",
		},
		{
			name: "invalid log level",
			setup: func(c *Config) {
				c.Log.Level = "invalid"
			},
			wantError: true,
			errorMsg:  "invalid log level",
		},
		{
			name: "invalid timezone",
			setup: func(c *Config) {
				c.Updates.ScheduleTime = "12:00"
				c.Updates.Timezone = "Mars/Olympus"
			},
			wantError: true,
			errorMsg:  "invalid timezone",
		},
		{
			name: "negative stop timeout",
			setup: func(c *Config) {
				c.Updates.StopTimeout = -5 * time.Second
			},
			wantError: true,
			errorMsg:  "updates.stop_timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Testing validation: %s", tt.name)
			cfg := Default()
			tt.setup(&cfg)

			err := cfg.Validate()
			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() error = nil, want error containing %q", tt.errorMsg)
					t.Log("  Expected validation to fail")
				} else {
					t.Logf("✓ Validation correctly failed: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
					t.Log("  Expected validation to pass")
				} else {
					t.Log("✓ Validation correctly passed")
				}
			}
		})
	}
}

func TestParseBytesString(t *testing.T) {
	tests := []struct {
		input    string
		want     int
		wantErr  bool
		errorMsg string
	}{
		{"10m", 10, false, ""},
		{"50M", 50, false, ""},
		{"1g", 1024, false, ""},
		{"2G", 2048, false, ""},
		{"100", 0, true, "missing unit"},
		{"10k", 1, false, ""},
		{"", 0, true, "empty"},
		{"invalid", 0, true, "invalid syntax"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseBytesString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseBytesString(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseBytesString(%q) unexpected error: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("parseBytesString(%q) = %d, want %d", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestApplyLoggingCompatibility(t *testing.T) {
	cfg := Default()
	cfg.Logging = LoggingConfig{
		Driver: "json-file",
		Options: map[string]string{
			"max-size": "50m",
			"max-file": "3",
		},
	}

	cfg.ApplyLoggingCompatibility()

	if cfg.Log.MaxSize != 50 {
		t.Errorf("ApplyLoggingCompatibility() MaxSize = %d, want 50", cfg.Log.MaxSize)
	}
	if cfg.Log.MaxBackups != 3 {
		t.Errorf("ApplyLoggingCompatibility() MaxBackups = %d, want 3", cfg.Log.MaxBackups)
	}

	// Test precedence (should overwrite default)
	cfg = Default() // MaxSize=10, MaxBackups=1
	cfg.Logging = LoggingConfig{
		Options: map[string]string{
			"max-size": "1g", // 1024
		},
	}
	cfg.ApplyLoggingCompatibility()
	if cfg.Log.MaxSize != 1024 {
		t.Errorf("ApplyLoggingCompatibility() MaxSize = %d, want 1024", cfg.Log.MaxSize)
	}
	if cfg.Log.MaxBackups != 1 {
		t.Errorf("ApplyLoggingCompatibility() MaxBackups should remain default 1, got %d", cfg.Log.MaxBackups)
	}
}

func TestValidate_ScheduleTime(t *testing.T) {
	tests := []struct {
		name         string
		scheduleTime string
		timezone     string
		wantError    bool
	}{
		{"valid schedule time", "03:00", "UTC", false},
		{"valid schedule with timezone", "15:30", "America/New_York", false},
		{"invalid schedule time - text", "invalid", "UTC", true},
		{"invalid schedule time - wrong format", "3pm", "UTC", true},
		{"invalid schedule time hour", "25:00", "UTC", true},
		{"invalid schedule time minute", "03:60", "UTC", true},
		{"empty schedule time (valid - uses interval)", "", "UTC", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Updates.ScheduleTime = tt.scheduleTime
			cfg.Updates.Timezone = tt.timezone

			err := cfg.Validate()
			if tt.wantError && err == nil {
				t.Error("Expected validation error, got nil")
			} else if !tt.wantError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestLoadFromFile_ReadError(t *testing.T) {
	// Test with a directory path (can't read as file)
	tmpDir := t.TempDir()

	_, err := LoadFromFile(tmpDir)
	if err == nil {
		t.Error("Expected error when reading directory as file")
	}
}

func TestApplyLoggingCompatibility_EdgeCases(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = nil
		cfg.ApplyLoggingCompatibility() // Should not panic
		if cfg.Log.MaxSize != 10 {
			t.Errorf("MaxSize should remain default, got %d", cfg.Log.MaxSize)
		}
	})

	t.Run("empty options map", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = map[string]string{}
		cfg.ApplyLoggingCompatibility()
		if cfg.Log.MaxSize != 10 {
			t.Errorf("MaxSize should remain default, got %d", cfg.Log.MaxSize)
		}
	})

	t.Run("invalid max-size format ignored", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = map[string]string{
			"max-size": "invalid",
		}
		cfg.ApplyLoggingCompatibility()
		if cfg.Log.MaxSize != 10 {
			t.Errorf("MaxSize should remain default on parse error, got %d", cfg.Log.MaxSize)
		}
	})

	t.Run("invalid max-file format ignored", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = map[string]string{
			"max-file": "not-a-number",
		}
		cfg.ApplyLoggingCompatibility()
		if cfg.Log.MaxBackups != 1 {
			t.Errorf("MaxBackups should remain default on parse error, got %d", cfg.Log.MaxBackups)
		}
	})

	t.Run("zero max-file ignored", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = map[string]string{
			"max-file": "0",
		}
		cfg.ApplyLoggingCompatibility()
		if cfg.Log.MaxBackups != 1 {
			t.Errorf("MaxBackups should remain default when 0, got %d", cfg.Log.MaxBackups)
		}
	})

	t.Run("negative max-file ignored", func(t *testing.T) {
		cfg := Default()
		cfg.Logging.Options = map[string]string{
			"max-file": "-1",
		}
		cfg.ApplyLoggingCompatibility()
		if cfg.Log.MaxBackups != 1 {
			t.Errorf("MaxBackups should remain default when negative, got %d", cfg.Log.MaxBackups)
		}
	})
}

func TestParseDockerSize_SmallValueReturnsMinimum(t *testing.T) {
	// Test that very small values (less than 1MB) return 1MB minimum
	result, err := parseBytesString("100k") // 100KB = 0.097MB, rounds to 1MB
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result != 1 {
		t.Errorf("Expected 1 (minimum MB), got %d", result)
	}
}

func TestValidate_AllLogLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}
	for _, level := range validLevels {
		t.Run("valid level "+level, func(t *testing.T) {
			cfg := Default()
			cfg.Log.Level = level
			if err := cfg.Validate(); err != nil {
				t.Errorf("Level %s should be valid, got error: %v", level, err)
			}
		})
	}

	invalidLevels := []string{"DEBUG", "INFO", "WARNING", "fatal", "panic", "trace", ""}
	for _, level := range invalidLevels {
		t.Run("invalid level "+level, func(t *testing.T) {
			cfg := Default()
			cfg.Log.Level = level
			if err := cfg.Validate(); err == nil {
				t.Errorf("Level %q should be invalid", level)
			}
		})
	}
}
