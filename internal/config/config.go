package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "/config/harborbuddy.yml"

// defaultTimezone is the schedule timezone when neither configuration nor TZ names one.
const defaultTimezone = "UTC"

type Config struct {
	Docker  DockerConfig  `yaml:"docker"`
	Updates UpdatesConfig `yaml:"updates"`
	Cleanup CleanupConfig `yaml:"cleanup"`
	Log     LogConfig     `yaml:"log"`

	RunOnce     bool `yaml:"-"`
	CleanupOnly bool `yaml:"-"`
	// IntervalOverride records that the environment or a flag set the check
	// interval, so a schedule_time that outranks it can be reported.
	IntervalOverride bool `yaml:"-"`
}

type DockerConfig struct {
	Host string `yaml:"host"`
}

type UpdatesConfig struct {
	Enabled                bool          `yaml:"enabled"`
	CheckInterval          time.Duration `yaml:"check_interval"`
	ScheduleTime           string        `yaml:"schedule_time"`
	Timezone               string        `yaml:"timezone"`
	DryRun                 bool          `yaml:"dry_run"`
	SelfUpdate             bool          `yaml:"self_update"`
	AllowImages            []string      `yaml:"allow_images"`
	DenyImages             []string      `yaml:"deny_images"`
	StopTimeout            time.Duration `yaml:"stop_timeout"`
	StartupTimeout         time.Duration `yaml:"startup_timeout"`
	RollbackImageRetention int           `yaml:"rollback_image_retention"`

	// timezoneConfigured records that the YAML file set timezone, so TZ stays a fallback.
	timezoneConfigured bool
}

type CleanupConfig struct {
	Enabled           bool `yaml:"enabled"`
	MinAgeHours       int  `yaml:"min_age_hours"`
	DanglingOnly      bool `yaml:"dangling_only"`
	All               bool `yaml:"all"`
	StoppedContainers bool `yaml:"stopped_containers"`
	UnusedNetworks    bool `yaml:"unused_networks"`
	UnusedVolumes     bool `yaml:"unused_volumes"`
	BuildCache        bool `yaml:"build_cache"`
	// DanglingImageMinAgeHours overrides MinAgeHours for untagged images; nil = MinAgeHours.
	DanglingImageMinAgeHours *int `yaml:"dangling_image_min_age_hours"`
	// CacheVolumes are volume-name globs (path.Match) whose unused volumes use
	// CacheVolumeMinAgeHours (nil = MinAgeHours) when volume cleanup is on.
	CacheVolumes           []string `yaml:"cache_volumes"`
	CacheVolumeMinAgeHours *int     `yaml:"cache_volume_min_age_hours"`
	// KeepImages are image patterns (the allow/deny syntax) never removed by cleanup, for
	// images that only host tools run.
	KeepImages []string `yaml:"keep_images"`
	// KeepVolumes are volume-name globs (path.Match) never removed by cleanup, for volumes that
	// may hold data that cannot be recreated even while nothing mounts them.
	KeepVolumes []string `yaml:"keep_volumes"`
	// KeepVolumeLabels keep every volume carrying a label: "key=value", or "key" for any value.
	KeepVolumeLabels []string `yaml:"keep_volume_labels"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	JSON       bool   `yaml:"json"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
}

func Default() Config {
	return Config{
		Docker: DockerConfig{},
		Updates: UpdatesConfig{
			Enabled:        true,
			CheckInterval:  12 * time.Hour,
			Timezone:       defaultTimezone,
			SelfUpdate:     true,
			AllowImages:    []string{"*"},
			StopTimeout:    10 * time.Second,
			StartupTimeout: 30 * time.Second,
		},
		Cleanup: CleanupConfig{
			Enabled:      true,
			MinAgeHours:  24,
			DanglingOnly: true,
		},
		Log: LogConfig{
			Level:      "info",
			MaxSize:    10,
			MaxBackups: 1,
		},
	}
}

func LoadFile(path string, required bool) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path) //nolint:gosec // The configuration path is intentionally user-selectable.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}

	// An empty timezone after decoding means the file did not set one.
	cfg.Updates.Timezone = ""
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, migrationHint(err))
	}
	if err := ensureSingleDocument(decoder); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	cfg.Updates.timezoneConfigured = cfg.Updates.Timezone != ""
	if !cfg.Updates.timezoneConfigured {
		cfg.Updates.Timezone = defaultTimezone
	}
	return cfg, nil
}

// ensureSingleDocument rejects a second non-empty YAML document. Empty trailing documents,
// such as a file that ends with "---", are accepted.
func ensureSingleDocument(decoder *yaml.Decoder) error {
	for {
		var extra yaml.Node
		if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if !emptyDocument(extra) {
			return errors.New("multiple YAML documents are not supported")
		}
	}
}

func emptyDocument(document yaml.Node) bool {
	for _, child := range document.Content {
		if child.Tag != "!!null" {
			return false
		}
	}
	return true
}

func migrationHint(err error) error {
	message := err.Error()
	for _, removed := range []string{"field tls not found", "field update_all not found", "field logging not found"} {
		if strings.Contains(message, removed) {
			return fmt.Errorf("%w (removed configuration key; see the migration notes)", err)
		}
	}
	return err
}
