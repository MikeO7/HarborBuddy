package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Docker.Host != "" {
		t.Fatalf("Docker.Host = %q, want empty so Docker SDK environment is honored", cfg.Docker.Host)
	}
	if cfg.Updates.CheckInterval != 12*time.Hour {
		t.Fatalf("CheckInterval = %v, want 12h", cfg.Updates.CheckInterval)
	}
	if cfg.Updates.StartupTimeout != 30*time.Second {
		t.Fatalf("StartupTimeout = %v, want 30s", cfg.Updates.StartupTimeout)
	}
	if cfg.Updates.StopTimeout != 10*time.Second {
		t.Fatalf("StopTimeout = %v, want 10s", cfg.Updates.StopTimeout)
	}
	if !cfg.Updates.SelfUpdate {
		t.Fatal("SelfUpdate = false, want enabled by default")
	}
	if len(cfg.Updates.AllowImages) != 1 || cfg.Updates.AllowImages[0] != "*" {
		t.Fatalf("AllowImages = %v, want [*]", cfg.Updates.AllowImages)
	}
}

func TestLoadFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yml")
	cfg, err := LoadFile(path, false)
	if err != nil {
		t.Fatalf("optional missing file returned error: %v", err)
	}
	if cfg.Updates.StartupTimeout != 30*time.Second {
		t.Fatal("optional missing file did not return defaults")
	}
	if _, err := LoadFile(path, true); err == nil {
		t.Fatal("required missing file returned nil error")
	}
}

func TestLoadFileMergesStrictYAMLOverDefaults(t *testing.T) {
	path := writeConfig(t, `
docker:
  host: tcp://docker.example:2376
updates:
  check_interval: 45m
  startup_timeout: 1m
  dry_run: true
  self_update: false
cleanup:
  enabled: false
log:
  level: debug
`)
	cfg, err := LoadFile(path, true)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if cfg.Docker.Host != "tcp://docker.example:2376" || cfg.Updates.CheckInterval != 45*time.Minute {
		t.Fatalf("YAML values not loaded: %+v", cfg)
	}
	if cfg.Updates.StopTimeout != 10*time.Second {
		t.Fatalf("default StopTimeout was not preserved: %v", cfg.Updates.StopTimeout)
	}
	if cfg.Updates.StartupTimeout != time.Minute || !cfg.Updates.DryRun || cfg.Updates.SelfUpdate || cfg.Cleanup.Enabled {
		t.Fatalf("YAML overrides not applied: %+v", cfg)
	}
}

func TestLoadFileRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "unknown", content: "updates:\n  mystery: true\n", want: "field mystery not found"},
		{name: "removed", content: "docker:\n  tls: true\n", want: "removed configuration key"},
		{name: "multiple", content: "log:\n  level: info\n---\nlog:\n  level: debug\n", want: "multiple YAML documents"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadFile(writeConfig(t, test.content), true)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadFile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLoadFileAllowsEmptyDocument(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, ""), true)
	if err != nil {
		t.Fatalf("LoadFile(empty) error = %v", err)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("LoadFile(empty) Log.Level = %q, want default", cfg.Log.Level)
	}
}

func TestApplyEnvironment(t *testing.T) {
	env := map[string]string{
		"HARBORBUDDY_DOCKER_HOST":         "tcp://env:2375",
		"HARBORBUDDY_INTERVAL":            "2h",
		"HARBORBUDDY_SCHEDULE_TIME":       "03:15",
		"HARBORBUDDY_TIMEZONE":            "America/New_York",
		"HARBORBUDDY_DRY_RUN":             "true",
		"HARBORBUDDY_SELF_UPDATE_ENABLED": "false",
		"HARBORBUDDY_STOP_TIMEOUT":        "20s",
		"HARBORBUDDY_STARTUP_TIMEOUT":     "40s",
		"HARBORBUDDY_UPDATES_ENABLED":     "false",
		"HARBORBUDDY_CLEANUP_ENABLED":     "false",
		"HARBORBUDDY_LOG_LEVEL":           "debug",
		"HARBORBUDDY_LOG_JSON":            "true",
		"HARBORBUDDY_LOG_FILE":            "/tmp/harborbuddy.log",
		"HARBORBUDDY_LOG_MAX_SIZE":        "20",
		"HARBORBUDDY_LOG_MAX_BACKUPS":     "4",
	}
	cfg := Default()
	if err := cfg.ApplyEnvironment(func(name string) string { return env[name] }); err != nil {
		t.Fatalf("ApplyEnvironment() error = %v", err)
	}
	if cfg.Docker.Host != env["HARBORBUDDY_DOCKER_HOST"] || cfg.Updates.CheckInterval != 2*time.Hour {
		t.Fatalf("environment was not applied: %+v", cfg)
	}
	if cfg.Updates.StartupTimeout != 40*time.Second || cfg.Updates.Enabled || cfg.Updates.SelfUpdate || cfg.Cleanup.Enabled {
		t.Fatalf("environment values were not applied: %+v", cfg)
	}
	if !cfg.Log.JSON || cfg.Log.MaxSize != 20 || cfg.Log.MaxBackups != 4 {
		t.Fatalf("logging environment was not applied: %+v", cfg.Log)
	}
}

func TestApplyEnvironmentRejectsInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "HARBORBUDDY_INTERVAL", value: "soon"},
		{name: "HARBORBUDDY_DRY_RUN", value: "sometimes"},
		{name: "HARBORBUDDY_SELF_UPDATE_ENABLED", value: "sometimes"},
		{name: "HARBORBUDDY_STARTUP_TIMEOUT", value: "later"},
		{name: "HARBORBUDDY_LOG_MAX_SIZE", value: "large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			err := cfg.ApplyEnvironment(func(name string) string {
				if name == test.name {
					return test.value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), test.name) {
				t.Fatalf("ApplyEnvironment() error = %v, want strict %s error", err, test.name)
			}
		})
	}
}

func TestApplyEnvironmentTimezoneFallback(t *testing.T) {
	cfg := Default()
	if err := cfg.ApplyEnvironment(func(name string) string {
		if name == "TZ" {
			return "Europe/London"
		}
		return ""
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Timezone != "Europe/London" {
		t.Fatalf("Timezone = %q, want TZ fallback", cfg.Updates.Timezone)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "empty Docker host is valid", edit: func(*Config) {}},
		{name: "interval", edit: func(c *Config) { c.Updates.CheckInterval = 0 }, want: "check_interval"},
		{name: "startup timeout", edit: func(c *Config) { c.Updates.StartupTimeout = 0 }, want: "startup_timeout"},
		{name: "schedule", edit: func(c *Config) { c.Updates.ScheduleTime = "25:00" }, want: "schedule_time"},
		{name: "timezone", edit: func(c *Config) { c.Updates.ScheduleTime = "03:00"; c.Updates.Timezone = "Mars/Base" }, want: "timezone"},
		{name: "log level", edit: func(c *Config) { c.Log.Level = "verbose" }, want: "log.level"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			test.edit(&cfg)
			err := cfg.Validate()
			if test.want == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harborbuddy.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
