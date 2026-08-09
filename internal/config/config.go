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

type Config struct {
	Docker  DockerConfig  `yaml:"docker"`
	Updates UpdatesConfig `yaml:"updates"`
	Cleanup CleanupConfig `yaml:"cleanup"`
	Log     LogConfig     `yaml:"log"`

	RunOnce     bool `yaml:"-"`
	CleanupOnly bool `yaml:"-"`
}

type DockerConfig struct {
	Host string `yaml:"host"`
}

type UpdatesConfig struct {
	Enabled        bool          `yaml:"enabled"`
	CheckInterval  time.Duration `yaml:"check_interval"`
	ScheduleTime   string        `yaml:"schedule_time"`
	Timezone       string        `yaml:"timezone"`
	DryRun         bool          `yaml:"dry_run"`
	SelfUpdate     bool          `yaml:"self_update"`
	AllowImages    []string      `yaml:"allow_images"`
	DenyImages     []string      `yaml:"deny_images"`
	StopTimeout    time.Duration `yaml:"stop_timeout"`
	StartupTimeout time.Duration `yaml:"startup_timeout"`
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
			Timezone:       "UTC",
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

func LoadFromFile(path string) (Config, error) {
	return LoadFile(path, false)
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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("decode config file %q: %w", path, migrationHint(err))
	}
	if err := ensureSingleDocument(decoder); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	return cfg, nil
}

func ensureSingleDocument(decoder *yaml.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple YAML documents are not supported")
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
