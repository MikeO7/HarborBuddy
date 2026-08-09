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
	if !cfg.Cleanup.Enabled || !cfg.Cleanup.DanglingOnly || cfg.Cleanup.MinAgeHours != 24 {
		t.Fatalf("safe cleanup defaults = %+v", cfg.Cleanup)
	}
	if cfg.Cleanup.All || cfg.Cleanup.StoppedContainers || cfg.Cleanup.UnusedNetworks || cfg.Cleanup.UnusedVolumes || cfg.Cleanup.BuildCache {
		t.Fatalf("aggressive cleanup defaults must be disabled: %+v", cfg.Cleanup)
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
  all: true
  stopped_containers: true
  unused_networks: true
  unused_volumes: true
  build_cache: true
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
	if !cfg.Cleanup.All || !cfg.Cleanup.StoppedContainers || !cfg.Cleanup.UnusedNetworks || !cfg.Cleanup.UnusedVolumes || !cfg.Cleanup.BuildCache {
		t.Fatalf("cleanup YAML overrides not applied: %+v", cfg.Cleanup)
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
		{name: "invalid second document", content: "log:\n  level: info\n---\n[", want: "decode config file"},
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

func TestLoadFromFileDelegatesToOptionalLoader(t *testing.T) {
	path := writeConfig(t, "updates:\n  check_interval: 1h\n")
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if cfg.Updates.CheckInterval != time.Hour {
		t.Fatalf("CheckInterval = %v, want 1h", cfg.Updates.CheckInterval)
	}
}

func TestApplyEnvironment(t *testing.T) {
	env := map[string]string{
		"HARBORBUDDY_DOCKER_HOST":                "tcp://env:2375",
		"HARBORBUDDY_INTERVAL":                   "2h",
		"HARBORBUDDY_SCHEDULE_TIME":              "03:15",
		"HARBORBUDDY_TIMEZONE":                   "America/New_York",
		"HARBORBUDDY_DRY_RUN":                    "true",
		"HARBORBUDDY_SELF_UPDATE_ENABLED":        "false",
		"HARBORBUDDY_STOP_TIMEOUT":               "20s",
		"HARBORBUDDY_STARTUP_TIMEOUT":            "40s",
		"HARBORBUDDY_UPDATES_ENABLED":            "false",
		"HARBORBUDDY_CLEANUP_ENABLED":            "false",
		"HARBORBUDDY_CLEANUP_MIN_AGE_HOURS":      "72",
		"HARBORBUDDY_CLEANUP_DANGLING_ONLY":      "false",
		"HARBORBUDDY_CLEANUP_ALL":                "true",
		"HARBORBUDDY_CLEANUP_STOPPED_CONTAINERS": "true",
		"HARBORBUDDY_CLEANUP_UNUSED_NETWORKS":    "true",
		"HARBORBUDDY_CLEANUP_UNUSED_VOLUMES":     "true",
		"HARBORBUDDY_CLEANUP_BUILD_CACHE":        "true",
		"HARBORBUDDY_LOG_LEVEL":                  "debug",
		"HARBORBUDDY_LOG_JSON":                   "true",
		"HARBORBUDDY_LOG_FILE":                   "/tmp/harborbuddy.log",
		"HARBORBUDDY_LOG_MAX_SIZE":               "20",
		"HARBORBUDDY_LOG_MAX_BACKUPS":            "4",
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
	if cfg.Cleanup.MinAgeHours != 72 || cfg.Cleanup.DanglingOnly || !cfg.Cleanup.All || !cfg.Cleanup.StoppedContainers || !cfg.Cleanup.UnusedNetworks || !cfg.Cleanup.UnusedVolumes || !cfg.Cleanup.BuildCache {
		t.Fatalf("cleanup environment was not applied: %+v", cfg.Cleanup)
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
		{name: "HARBORBUDDY_STOP_TIMEOUT", value: "soon"},
		{name: "HARBORBUDDY_DRY_RUN", value: "sometimes"},
		{name: "HARBORBUDDY_SELF_UPDATE_ENABLED", value: "sometimes"},
		{name: "HARBORBUDDY_STARTUP_TIMEOUT", value: "later"},
		{name: "HARBORBUDDY_UPDATES_ENABLED", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_ENABLED", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_MIN_AGE_HOURS", value: "old"},
		{name: "HARBORBUDDY_CLEANUP_DANGLING_ONLY", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_ALL", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_STOPPED_CONTAINERS", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_UNUSED_NETWORKS", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_UNUSED_VOLUMES", value: "sometimes"},
		{name: "HARBORBUDDY_CLEANUP_BUILD_CACHE", value: "sometimes"},
		{name: "HARBORBUDDY_LOG_JSON", value: "sometimes"},
		{name: "HARBORBUDDY_LOG_MAX_SIZE", value: "large"},
		{name: "HARBORBUDDY_LOG_MAX_BACKUPS", value: "many"},
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

func TestApplyEnvironmentOverridesReadsProcessEnvironment(t *testing.T) {
	t.Setenv("HARBORBUDDY_LOG_LEVEL", "warn")
	cfg := Default()
	if err := cfg.ApplyEnvironmentOverrides(); err != nil {
		t.Fatalf("ApplyEnvironmentOverrides() error = %v", err)
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("Log.Level = %q, want warn", cfg.Log.Level)
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
		{name: "stop timeout", edit: func(c *Config) { c.Updates.StopTimeout = 0 }, want: "stop_timeout"},
		{name: "startup timeout", edit: func(c *Config) { c.Updates.StartupTimeout = 0 }, want: "startup_timeout"},
		{name: "schedule", edit: func(c *Config) { c.Updates.ScheduleTime = "25:00" }, want: "schedule_time"},
		{name: "timezone", edit: func(c *Config) { c.Updates.ScheduleTime = "03:00"; c.Updates.Timezone = "Mars/Base" }, want: "timezone"},
		{name: "allow pattern", edit: func(c *Config) { c.Updates.AllowImages = []string{"repo/*/image"} }, want: "unsupported image pattern"},
		{name: "deny pattern", edit: func(c *Config) { c.Updates.DenyImages = []string{"repo/**"} }, want: "unsupported image pattern"},
		{name: "negative age", edit: func(c *Config) { c.Cleanup.MinAgeHours = -1 }, want: "min_age_hours"},
		{name: "log level", edit: func(c *Config) { c.Log.Level = "verbose" }, want: "log.level"},
		{name: "log size", edit: func(c *Config) { c.Log.MaxSize = 0 }, want: "log.max_size"},
		{name: "log backups", edit: func(c *Config) { c.Log.MaxBackups = -1 }, want: "log.max_backups"},
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

func TestValidateAcceptsPatternAndScheduleBoundaries(t *testing.T) {
	cfg := Default()
	cfg.Updates.AllowImages = []string{"repo/*", "*:tag", "exact:tag", "*"}
	cfg.Updates.DenyImages = []string{"blocked/*"}
	cfg.Updates.ScheduleTime = "00:00"
	cfg.Updates.Timezone = "UTC"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
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
