package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envReader(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestTimezonePrecedence(t *testing.T) {
	withZone := "updates:\n  timezone: America/New_York\n"
	for _, test := range []struct {
		name    string
		content string // empty means no config file
		env     map[string]string
		want    string
	}{
		{name: "default", want: "UTC"},
		{name: "TZ fills a missing file", env: map[string]string{"TZ": "Europe/London"}, want: "Europe/London"},
		{name: "TZ fills a file without timezone", content: "log:\n  level: info\n", env: map[string]string{"TZ": "Europe/London"}, want: "Europe/London"},
		{name: "YAML beats TZ", content: withZone, env: map[string]string{"TZ": "Europe/London"}, want: "America/New_York"},
		{name: "explicit UTC beats TZ", content: "updates:\n  timezone: UTC\n", env: map[string]string{"TZ": "Europe/London"}, want: "UTC"},
		{name: "HARBORBUDDY_TIMEZONE beats YAML", content: withZone, env: map[string]string{"HARBORBUDDY_TIMEZONE": "Asia/Tokyo", "TZ": "Europe/London"}, want: "Asia/Tokyo"},
		{name: "YAML without environment", content: withZone, want: "America/New_York"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.yml")
			if test.content != "" {
				path = writeConfig(t, test.content)
			}
			cfg, err := LoadFile(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ApplyEnvironment(envReader(test.env)); err != nil {
				t.Fatal(err)
			}
			if cfg.Updates.Timezone != test.want {
				t.Fatalf("Timezone = %q, want %q", cfg.Updates.Timezone, test.want)
			}
		})
	}
}

func TestLoadFileAcceptsTrailingEmptyDocuments(t *testing.T) {
	for _, content := range []string{
		"log:\n  level: debug\n---\n",
		"log:\n  level: debug\n---\n---\n",
		"log:\n  level: debug\n---\n# trailing comment\n",
		"log:\n  level: debug\n...\n",
	} {
		cfg, err := LoadFile(writeConfig(t, content), true)
		if err != nil {
			t.Fatalf("LoadFile(%q) error = %v", content, err)
		}
		if cfg.Log.Level != "debug" || cfg.Updates.Timezone != DefaultTimezone {
			t.Fatalf("LoadFile(%q) = %+v", content, cfg)
		}
	}
	content := "log:\n  level: debug\n---\n---\nlog:\n  level: info\n"
	if _, err := LoadFile(writeConfig(t, content), true); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("LoadFile(second non-empty document) error = %v", err)
	}
}

func TestApplyEnvironmentAcceptsYAMLBooleanSpellings(t *testing.T) {
	for value, want := range map[string]bool{
		"true": true, "TRUE": true, "yes": true, "Yes": true, "on": true, "ON": true, "1": true, " t ": true,
		"false": false, "False": false, "no": false, "NO": false, "off": false, "Off": false, "0": false,
	} {
		cfg := Default()
		cfg.Updates.DryRun = !want
		if err := cfg.ApplyEnvironment(envReader(map[string]string{"HARBORBUDDY_DRY_RUN": value})); err != nil {
			t.Fatalf("HARBORBUDDY_DRY_RUN=%q error = %v", value, err)
		}
		if cfg.Updates.DryRun != want {
			t.Fatalf("HARBORBUDDY_DRY_RUN=%q gave %v, want %v", value, cfg.Updates.DryRun, want)
		}
	}
	cfg := Default()
	err := cfg.ApplyEnvironment(envReader(map[string]string{"HARBORBUDDY_DRY_RUN": "maybe"}))
	if err == nil || !strings.Contains(err.Error(), "yes/no") {
		t.Fatalf("invalid boolean error = %v, want accepted spellings", err)
	}
}

func TestLogLevelIsCaseInsensitive(t *testing.T) {
	cfg := Default()
	if err := cfg.ApplyEnvironment(envReader(map[string]string{"HARBORBUDDY_LOG_LEVEL": " INFO "})); err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("Log.Level = %q, want info", cfg.Log.Level)
	}
	cfg.Log.Level = "WARN"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(WARN) error = %v", err)
	}
}

func TestValidateIntervalAndBackupLimits(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "nanosecond interval", edit: func(c *Config) { c.Updates.CheckInterval = time.Nanosecond }, want: "at least 1s"},
		{name: "sub-second interval", edit: func(c *Config) { c.Updates.CheckInterval = 999 * time.Millisecond }, want: "at least 1s"},
		{name: "one second interval", edit: func(c *Config) { c.Updates.CheckInterval = time.Second }},
		{name: "daily schedule ignores interval", edit: func(c *Config) { c.Updates.CheckInterval = 0; c.Updates.ScheduleTime = "03:00" }},
		{name: "zero backups", edit: func(c *Config) { c.Log.MaxBackups = 0 }, want: "log.max_backups must be at least 1"},
		{name: "one backup", edit: func(c *Config) { c.Log.MaxBackups = 1 }},
	} {
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

func TestStartupWarningsShortInterval(t *testing.T) {
	cfg := Default()
	cfg.Updates.CheckInterval = 5 * time.Second
	warnings := StartupWarnings(cfg, nil)
	if len(warnings) != 1 || warnings[0].Setting != "updates.check_interval" || !strings.Contains(warnings[0].Message, "rate limits") {
		t.Fatalf("StartupWarnings() = %+v, want short interval warning", warnings)
	}
	for _, edit := range []func(*Config){
		func(c *Config) { c.Updates.CheckInterval = time.Minute },
		func(c *Config) { c.Updates.ScheduleTime = "03:00" },
		func(c *Config) { c.RunOnce = true },
		func(c *Config) { c.CleanupOnly = true },
	} {
		quiet := cfg
		edit(&quiet)
		if warnings := StartupWarnings(quiet, nil); len(warnings) != 0 {
			t.Fatalf("StartupWarnings() = %+v, want none", warnings)
		}
	}
}

func TestStartupWarningsUnknownEnvironment(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"TZ=UTC",
		"HARBORBUDDY_SELF_UPDATE=false",
		"HARBORBUDDY_CHECK_INTERVAL=1h",
		"HARBORBUDDY_MYSTERY=1",
		"HARBORBUDDY_NO_VALUE",
		"HARBORBUDDY_SELF_UPDATE_ENABLED=false",
		"HARBORBUDDY_INTERVAL=1h",
		"HARBORBUDDY_TIMEZONE=UTC",
		"HARBORBUDDY_CLEANUP_KEEP_VOLUME_LABELS=a",
		"HARBORBUDDY_CLEANUP_DANGLING_IMAGE_MIN_AGE_HOURS=1",
		"HARBORBUDDY_CONFIG=/config/x.yml",
		"HARBORBUDDY_HEALTH_FILE=/tmp/h.json",
		"HARBORBUDDY_CONTAINER_NAME=harborbuddy",
		"HARBORBUDDY_CONTAINER_ID=abc",
	}
	warnings := StartupWarnings(Default(), environ)
	want := []Warning{
		{Setting: "HARBORBUDDY_CHECK_INTERVAL", Message: "unrecognized environment variable is ignored; did you mean HARBORBUDDY_INTERVAL?"},
		{Setting: "HARBORBUDDY_MYSTERY", Message: "unrecognized environment variable is ignored"},
		{Setting: "HARBORBUDDY_NO_VALUE", Message: "unrecognized environment variable is ignored"},
		{Setting: "HARBORBUDDY_SELF_UPDATE", Message: "unrecognized environment variable is ignored; did you mean HARBORBUDDY_SELF_UPDATE_ENABLED?"},
	}
	if len(warnings) != len(want) {
		t.Fatalf("StartupWarnings() = %+v, want %+v", warnings, want)
	}
	for i := range want {
		if warnings[i] != want[i] {
			t.Fatalf("warning %d = %+v, want %+v", i, warnings[i], want[i])
		}
	}
}
