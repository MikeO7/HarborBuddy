package app

import (
	"fmt"
	"io"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/spf13/pflag"
)

type flagValues struct {
	configPath                   string
	interval                     time.Duration
	scheduleTime                 string
	timezone                     string
	once                         bool
	dryRun                       bool
	logLevel                     string
	cleanupOnly                  bool
	version                      bool
	updaterMode                  bool
	targetContainer              string
	newImage                     string
	helperStop                   time.Duration
	helperStartup                time.Duration
	helperRestart                string
	helperRetries                int
	helperRollbackImageRetention int
}

var hideFlagsFn = hideFlags

func newFlagSet(stderr io.Writer) (*pflag.FlagSet, *flagValues, error) {
	values := &flagValues{}
	flags := pflag.NewFlagSet("harborbuddy", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.SortFlags = false

	flags.StringVar(&values.configPath, "config", config.DefaultPath, "Path to config file")
	flags.DurationVar(&values.interval, "interval", 0, "Override update check interval (e.g., 15m, 1h)")
	flags.StringVar(&values.scheduleTime, "schedule-time", "", "Run at specific time daily (e.g., '03:00')")
	flags.StringVar(&values.timezone, "timezone", "", "Timezone for schedule (e.g., 'America/Los_Angeles', 'UTC')")
	flags.BoolVar(&values.once, "once", false, "Run a single update cycle and exit")
	flags.BoolVar(&values.dryRun, "dry-run", false, "Enable dry-run mode (no actual updates)")
	flags.StringVar(&values.logLevel, "log-level", "", "Logging level (debug, info, warn, error)")
	flags.BoolVar(&values.cleanupOnly, "cleanup-only", false, "Run only cleanup logic and exit")
	flags.BoolVar(&values.version, "version", false, "Show version and exit")
	flags.BoolVar(&values.updaterMode, "updater-mode", false, "self-update helper mode")
	flags.StringVar(&values.targetContainer, "target-container-id", "", "self-update target container")
	flags.StringVar(&values.newImage, "new-image-id", "", "self-update target image")
	flags.DurationVar(&values.helperStop, "helper-stop-timeout", 0, "self-update stop timeout")
	flags.DurationVar(&values.helperStartup, "helper-startup-timeout", 0, "self-update startup timeout")
	flags.StringVar(&values.helperRestart, "helper-restart-policy", "", "original self-update restart policy")
	flags.IntVar(&values.helperRetries, "helper-restart-max-retries", 0, "original self-update restart retry limit")
	flags.IntVar(&values.helperRollbackImageRetention, "helper-rollback-image-retention", 0, "self-update rollback image retention")

	if err := hideFlagsFn(flags, "updater-mode", "target-container-id", "new-image-id", "helper-stop-timeout", "helper-startup-timeout", "helper-restart-policy", "helper-restart-max-retries", "helper-rollback-image-retention"); err != nil {
		return nil, nil, err
	}
	return flags, values, nil
}

func hideFlags(flags *pflag.FlagSet, names ...string) error {
	for _, name := range names {
		if err := flags.MarkHidden(name); err != nil {
			return fmt.Errorf("hide flag %s: %w", name, err)
		}
	}
	return nil
}

func loadConfig(flags *pflag.FlagSet, values *flagValues, lookupEnv func(string) (string, bool)) (config.Config, error) {
	path, required := configPath(flags, values, lookupEnv)
	cfg, err := config.LoadFile(path, required)
	if err != nil {
		return config.Config{}, err
	}
	if err := cfg.ApplyEnvironment(func(name string) string {
		value, _ := lookupEnv(name)
		return value
	}); err != nil {
		return config.Config{}, err
	}
	applyFlagOverrides(flags, values, &cfg)
	return cfg, nil
}

func configPath(flags *pflag.FlagSet, values *flagValues, lookupEnv func(string) (string, bool)) (string, bool) {
	if flags.Changed("config") {
		return values.configPath, true
	}
	if path, ok := lookupEnv("HARBORBUDDY_CONFIG"); ok && path != "" {
		return path, true
	}
	return config.DefaultPath, false
}

func applyFlagOverrides(flags *pflag.FlagSet, values *flagValues, cfg *config.Config) {
	flags.Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "interval":
			cfg.Updates.CheckInterval = values.interval
		case "schedule-time":
			cfg.Updates.ScheduleTime = values.scheduleTime
		case "timezone":
			cfg.Updates.Timezone = values.timezone
		case "once":
			cfg.RunOnce = values.once
		case "dry-run":
			cfg.Updates.DryRun = values.dryRun
		case "log-level":
			cfg.Log.Level = values.logLevel
		case "cleanup-only":
			cfg.CleanupOnly = values.cleanupOnly
		}
	})
}
